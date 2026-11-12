// Command golden は本家 yomiyasu (Python) を環境を固定して動かし、testdata/golden/ の記録を作り直す (make golden)。
//
//	golden           記録を作る。記録の Python / unicodedata のバージョンか本家の commit が今と違えば止まる (UPDATE=1 で上書き)
//	golden -pin      本家のファイルを固定の commit から取り直し、oracle/upstream.sha256 を書く
//
// 記録は 2 回作って同じバイト列になることを確かめてから書く。
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"atomicfile"
	"yomiyasu/oracle"
)

const runTimeout = 20 * time.Minute

type paths struct{ corpus, words, golden, manifest, tables string }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	pin := flag.Bool("pin", false, "本家のファイルを取り直して oracle/upstream.sha256 を書く")
	flag.Parse()
	p := paths{corpus: "testdata/corpus", words: "testdata/words", golden: "testdata/golden", manifest: "oracle/upstream.sha256", tables: oracle.PyunicodeTablesPath}
	var err error
	if *pin {
		err = runPin(ctx, p)
	} else {
		err = runGolden(ctx, p, os.Getenv("UPDATE") == "1")
	}
	cancel()
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "golden: %v\n", err)
		os.Exit(1)
	}
}

func runPin(ctx context.Context, p paths) error {
	text, err := oracle.Pin(ctx, oracle.HTTPGetter)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(p.manifest, []byte(text), 0o644); err != nil {
		return fmt.Errorf("%s へ書けません: %w", p.manifest, err)
	}
	fmt.Printf("%s を書きました (commit %s)。git diff を読んでから commit する\n", p.manifest, oracle.Commit)
	return nil
}

func runGolden(ctx context.Context, p paths, update bool) error {
	cache, err := oracle.DefaultCacheRoot()
	if err != nil {
		return err
	}
	tree, err := oracle.Fetch(ctx, cache, oracle.HTTPGetter)
	if err != nil {
		return err
	}
	runner, err := oracle.NewRunner(tree)
	if err != nil {
		return err
	}
	info, err := runner.Info(ctx)
	if err != nil {
		return err
	}
	corpus, err := oracle.LoadCorpus(p.corpus)
	if err != nil {
		return err
	}
	header := oracle.NewHeader(info, corpus)
	if err := checkEnv(p, header, update); err != nil {
		return err
	}
	patterns, err := twice(func() ([]byte, error) { return buildPatterns(ctx, runner, corpus, p.words, header) })
	if err != nil {
		return fmt.Errorf("パターンの記録: %w", err)
	}
	smoke, err := twice(func() ([]byte, error) { return buildSmoke(ctx, runner, header.WithoutCorpus()) })
	if err != nil {
		return fmt.Errorf("関数と CLI の記録: %w", err)
	}
	std := header.WithoutCorpus()
	unicode, err := twice(func() ([]byte, error) { return buildUnicode(ctx, runner, std) })
	if err != nil {
		return fmt.Errorf("全コードポイントの記録 (unicode): %w", err)
	}
	jsonRec, err := twice(func() ([]byte, error) { return buildJSON(ctx, runner, std) })
	if err != nil {
		return fmt.Errorf("json の記録: %w", err)
	}
	pystr, err := twice(func() ([]byte, error) { return buildPystr(ctx, runner, std) })
	if err != nil {
		return fmt.Errorf("str の記録: %w", err)
	}
	tables, err := buildTables(unicode)
	if err != nil {
		return err
	}
	for path, body := range map[string][]byte{
		filepath.Join(p.golden, oracle.PatternsFile): patterns, filepath.Join(p.golden, oracle.SmokeFile): smoke,
		filepath.Join(p.golden, oracle.UnicodeFile): unicode, filepath.Join(p.golden, oracle.JSONFile): jsonRec,
		filepath.Join(p.golden, oracle.PystrFile): pystr, p.tables: tables,
	} {
		if err := atomicfile.Write(path, body, 0o644); err != nil {
			return fmt.Errorf("%s へ書けません: %w", path, err)
		}
		fmt.Printf("%s: %d バイト\n", path, len(body))
	}
	fmt.Printf("Python %s / unicodedata %s / 入力 %d ファイル・%d 単位。git diff を読んでから commit する\n",
		info.PythonVersion, info.UnicodedataVersion, len(corpus.Files), corpus.Units())
	return nil
}

// checkEnv は記録の環境が今と違うときに止める。記録がまだ無いときと update のときは通す。
func checkEnv(p paths, now oracle.Header, update bool) error {
	for _, name := range []string{oracle.PatternsFile, oracle.SmokeFile, oracle.UnicodeFile, oracle.JSONFile, oracle.PystrFile} {
		var old struct {
			Header oracle.Header `json:"header"`
		}
		err := oracle.ReadGoldenHeader(p.golden, name, &old)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if diff := old.Header.EnvMismatch(now); diff != "" && !update {
			return fmt.Errorf("%s の記録を取った環境が今と違います (%s)。作り直すなら UPDATE=1 を付ける", name, diff)
		}
	}
	return nil
}

// twice は生成を 2 回走らせ、同じバイト列になったときだけ返す。
func twice(gen func() ([]byte, error)) ([]byte, error) {
	first, err := gen()
	if err != nil {
		return nil, err
	}
	second, err := gen()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(first, second) {
		return nil, fmt.Errorf("同じ入力で 2 回作った記録が違います (本家の出力が決定的でない。%d バイトと %d バイト)", len(first), len(second))
	}
	return first, nil
}

func buildPatterns(ctx context.Context, r oracle.Runner, c oracle.Corpus, words string, h oracle.Header) ([]byte, error) {
	rec, err := r.RecordPatterns(ctx, c, words)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalPatterns(oracle.PatternsGolden{Header: h, Runs: rec.Runs, Patterns: rec.Patterns})
}

func buildSmoke(ctx context.Context, r oracle.Runner, h oracle.Header) ([]byte, error) {
	g, err := r.RecordSmoke(ctx, h)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalSmoke(g)
}

func buildUnicode(ctx context.Context, r oracle.Runner, h oracle.Header) ([]byte, error) {
	g, err := r.RecordUnicode(ctx, h)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalUnicode(g)
}

func buildJSON(ctx context.Context, r oracle.Runner, h oracle.Header) ([]byte, error) {
	g, err := r.RecordJSON(ctx, h)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalJSONGolden(g)
}

func buildPystr(ctx context.Context, r oracle.Runner, h oracle.Header) ([]byte, error) {
	g, err := r.RecordPystr(ctx, h)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalPystr(g)
}

// buildTables は書き出す unicode.json のバイト列を読み直して Go の表にする (ファイルと表が同じ記録から作られることを保つ)。
func buildTables(unicode []byte) ([]byte, error) {
	g, err := oracle.ParseUnicode(unicode)
	if err != nil {
		return nil, err
	}
	return oracle.GenerateUnicodeTables(g)
}
