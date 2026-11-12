// Command golden は本家 yomiyasu (Python) を環境を固定して動かし、testdata/golden/ の記録を作り直す (make golden)。
//
//	golden           記録を作る。記録の Python / unicodedata のバージョンか本家の commit が今と違えば止まる (UPDATE=1 で上書き)
//	golden -pin      本家のファイルを固定の commit から取り直し、oracle/upstream.sha256 を書く
//
// 記録は PYTHONHASHSEED を変えて 2 回作り、同じバイト列になることを確かめてから書く (パターンの記録は選択肢の並びだけ違ってよい)。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"atomicfile"
	"ja-writing/oracle"
)

const runTimeout = 20 * time.Minute

type paths struct{ corpus, words, golden, manifest, tables, pyreTables string }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	pin := flag.Bool("pin", false, "本家のファイルを取り直して oracle/upstream.sha256 を書く")
	only := flag.String("only", "", "visibility だけを作り直す (markdown_visibility の関数の記録。ほかは作り直さない)")
	flag.Parse()
	p := paths{corpus: "testdata/corpus", words: "testdata/words", golden: "testdata/golden", manifest: "oracle/upstream.sha256", tables: oracle.PyunicodeTablesPath, pyreTables: oracle.PyreTablesPath}
	var err error
	if *pin {
		err = runPin(ctx, p)
	} else {
		err = runGolden(ctx, p, os.Getenv("UPDATE") == "1", *only)
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

func runGolden(ctx context.Context, p paths, update bool, only string) error {
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
	visCases, err := oracle.VisibilityCases(corpus)
	if err != nil {
		return err
	}
	vis, err := twice(runner, func(runner oracle.Runner) ([]byte, error) { return buildVisibility(ctx, runner, visCases, header) })
	if err != nil {
		return fmt.Errorf("markdown_visibility の関数の記録: %w", err)
	}
	if only == "visibility" {
		path := filepath.Join(p.golden, oracle.VisibilityFile)
		if err := atomicfile.Write(path, vis, 0o644); err != nil {
			return fmt.Errorf("%s へ書けません: %w", path, err)
		}
		fmt.Printf("%s: %d バイト\n", path, len(vis))
		return nil
	}
	if only != "" {
		return fmt.Errorf("-only は visibility だけを受けます: %q", only)
	}
	patterns, err := twiceSame(runner, func(runner oracle.Runner) ([]byte, error) { return buildPatterns(ctx, runner, corpus, p.words, header) }, samePatternsIgnoringText)
	if err != nil {
		return fmt.Errorf("パターンの記録: %w", err)
	}
	smoke, err := twice(runner, func(runner oracle.Runner) ([]byte, error) { return buildSmoke(ctx, runner, header.WithoutCorpus()) })
	if err != nil {
		return fmt.Errorf("関数と CLI の記録: %w", err)
	}
	std := header.WithoutCorpus()
	unicode, err := twice(runner, func(runner oracle.Runner) ([]byte, error) { return buildUnicode(ctx, runner, std) })
	if err != nil {
		return fmt.Errorf("全コードポイントの記録 (unicode): %w", err)
	}
	jsonRec, err := twice(runner, func(runner oracle.Runner) ([]byte, error) { return buildJSON(ctx, runner, std) })
	if err != nil {
		return fmt.Errorf("json の記録: %w", err)
	}
	pystr, err := twice(runner, func(runner oracle.Runner) ([]byte, error) { return buildPystr(ctx, runner, std) })
	if err != nil {
		return fmt.Errorf("str の記録: %w", err)
	}
	pyre, err := twice(runner, func(runner oracle.Runner) ([]byte, error) { return buildPyre(ctx, runner, std) })
	if err != nil {
		return fmt.Errorf("re の記録 (pyre): %w", err)
	}
	difflibRec, err := twice(runner, func(runner oracle.Runner) ([]byte, error) { return buildDifflib(ctx, runner, std, p.corpus) })
	if err != nil {
		return fmt.Errorf("difflib の記録: %w", err)
	}
	tables, err := buildTables(unicode)
	if err != nil {
		return err
	}
	pyreTables, err := buildPyreTables(pyre)
	if err != nil {
		return err
	}
	for path, body := range map[string][]byte{
		filepath.Join(p.golden, oracle.PatternsFile): patterns, filepath.Join(p.golden, oracle.SmokeFile): smoke,
		filepath.Join(p.golden, oracle.UnicodeFile): unicode, filepath.Join(p.golden, oracle.JSONFile): jsonRec,
		filepath.Join(p.golden, oracle.PystrFile): pystr, filepath.Join(p.golden, oracle.PyreFile): pyre, p.tables: tables, p.pyreTables: pyreTables,
		filepath.Join(p.golden, oracle.DifflibFile):    difflibRec,
		filepath.Join(p.golden, oracle.VisibilityFile): vis,
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
	for _, name := range []string{oracle.PatternsFile, oracle.SmokeFile, oracle.UnicodeFile, oracle.JSONFile, oracle.PystrFile, oracle.PyreFile, oracle.DifflibFile, oracle.VisibilityFile} {
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

// secondHashSeed は 2 回目の生成の PYTHONHASHSEED。0 のまま 2 回回すと、hash の順に依存する出力 (set の並び) も同じ順で出て、見つけられない。
const secondHashSeed = "20261113"

// twice は生成を 2 回、PYTHONHASHSEED を変えて走らせ、同じバイト列になったときだけ返す。
func twice(r oracle.Runner, gen func(oracle.Runner) ([]byte, error)) ([]byte, error) {
	return twiceSame(r, gen, bytes.Equal)
}

// twiceSame は twice の「同じ」の判定を差し替えられる形。
func twiceSame(r oracle.Runner, gen func(oracle.Runner) ([]byte, error), same func(a, b []byte) bool) ([]byte, error) {
	first, err := gen(r.WithHashSeed("0"))
	if err != nil {
		return nil, err
	}
	second, err := gen(r.WithHashSeed(secondHashSeed))
	if err != nil {
		return nil, err
	}
	if !same(first, second) {
		return nil, fmt.Errorf("同じ入力で 2 回作った記録が違います (本家の出力が決定的でない。PYTHONHASHSEED の違いで出力が変わる出力が混ざっている可能性が高い。%d バイトと %d バイト)", len(first), len(second))
	}
	return first, nil
}

func buildPatterns(ctx context.Context, r oracle.Runner, c oracle.Corpus, words string, h oracle.Header) ([]byte, error) {
	rec, err := r.RecordPatterns(ctx, c, words)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalPatterns(oracle.PatternsGolden{Header: h, Runs: rec.Runs, StaticCalls: rec.StaticCalls, StaticPatterns: rec.StaticPatterns, Patterns: rec.Patterns})
}

func buildVisibility(ctx context.Context, r oracle.Runner, cases []oracle.VisCase, h oracle.Header) ([]byte, error) {
	g, err := r.RecordVisibility(ctx, h, cases)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalVisibility(g)
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

func buildPyre(ctx context.Context, r oracle.Runner, h oracle.Header) ([]byte, error) {
	g, err := r.RecordPyre(ctx, h)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalPyre(g)
}

func buildDifflib(ctx context.Context, r oracle.Runner, h oracle.Header, corpusDir string) ([]byte, error) {
	g, err := r.RecordDifflib(ctx, h, corpusDir)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalDifflib(g)
}

// buildPyreTables は書き出す pyre.json のバイト列を読み直して Go の表にする。
func buildPyreTables(pyre []byte) ([]byte, error) {
	g, err := oracle.ParsePyre(pyre)
	if err != nil {
		return nil, err
	}
	return oracle.GeneratePyreTables(g)
}

// samePatternsIgnoringText はパターンの記録 2 つが、パターンの文字列と並びを除いて同じか。本家は語の一覧から選択肢を set で作り、
// 選択肢の並びと、どのパターンをいつ compile するかの順が PYTHONHASHSEED で変わる (文字列と並びは違うが、当たりの記録 (digest・当たった数・窓の記録) の組は同じ)。
// 記録の文字列と並びは 1 回目 (seed 0) のもの。
func samePatternsIgnoringText(a, b []byte) bool {
	var ga, gb oracle.PatternsGolden
	if json.Unmarshal(a, &ga) != nil || json.Unmarshal(b, &gb) != nil || len(ga.Patterns) != len(gb.Patterns) {
		return false
	}
	canon := func(g oracle.PatternsGolden) ([]string, bool) {
		rows := make([]string, len(g.Patterns))
		for i, rec := range g.Patterns {
			if !rec.Static {
				rec.Pattern = ""
			}
			j, err := json.Marshal(rec)
			if err != nil {
				return nil, false
			}
			rows[i] = string(j)
		}
		slices.Sort(rows)
		return rows, true
	}
	ra, okA := canon(ga)
	rb, okB := canon(gb)
	ga.Patterns, gb.Patterns = nil, nil
	ja, err1 := json.Marshal(ga)
	jb, err2 := json.Marshal(gb)
	return okA && okB && err1 == nil && err2 == nil && slices.Equal(ra, rb) && bytes.Equal(ja, jb)
}
