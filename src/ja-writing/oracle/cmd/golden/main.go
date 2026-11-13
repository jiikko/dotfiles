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
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"atomicfile"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

const runTimeout = 20 * time.Minute

type paths struct{ corpus, words, golden, manifest string }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	pin := flag.Bool("pin", false, "本家のファイルを取り直して oracle/upstream.sha256 を書く")
	only := flag.String("only", "", "記録を 1 本だけ作り直す ("+strings.Join(goldenNames(), "・")+" のどれか。ほかは作り直さない)")
	flag.Parse()
	p := paths{corpus: "testdata/corpus", words: "testdata/words", golden: "testdata/golden", manifest: "oracle/upstream.sha256"}
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
	if only != "" && !slices.ContainsFunc(goldens, func(g golden) bool { return g.name == only }) {
		return fmt.Errorf("-only は %s のどれかを受けます: %q", strings.Join(goldenNames(), "・"), only)
	}
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
	e := genEnv{ctx: ctx, p: p, corpus: corpus, header: header}
	files := map[string][]byte{}
	for _, g := range goldens {
		if only != "" && g.name != only {
			continue
		}
		gen := func(r oracle.Runner) ([]byte, error) { return g.build(e, r) }
		var rec []byte
		if g.same == nil {
			rec, err = twice(runner, gen)
		} else {
			rec, err = twiceSame(runner, gen, g.same)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", g.what, err)
		}
		files[filepath.Join(p.golden, g.file)] = rec
		if g.table != nil {
			table, err := g.table(rec)
			if err != nil {
				return fmt.Errorf("%s から作る Go の表: %w", g.what, err)
			}
			files[g.tablePath] = table
		}
	}
	if err := writeAll(files); err != nil {
		return err
	}
	if only == "" {
		fmt.Printf("Python %s / unicodedata %s / 入力 %d ファイル・%d 単位。git diff を読んでから commit する\n",
			info.PythonVersion, info.UnicodedataVersion, len(corpus.Files), corpus.Units())
	}
	return nil
}

// genEnv は記録を作るときに要るもの。header は入力の集まりの一覧を持つ。持たない記録は WithoutCorpus で外す。
type genEnv struct {
	ctx    context.Context
	p      paths
	corpus oracle.Corpus
	header oracle.Header
}

// golden は記録 1 本の作り方。name は -only で名指す名前、file は testdata/golden/ の中の名前、what はエラーに添える名前。
// same が nil でなければ 2 回の生成の「同じ」をそれで判定する。table があれば、書き出す記録のバイト列から Go の表を作って tablePath に書く
// (表と記録が同じ生成から作られる)。全部の記録を作るときも、この並びで作り、全部できてから書く。
type golden struct {
	name, file, what string
	build            func(e genEnv, r oracle.Runner) ([]byte, error)
	same             func(a, b []byte) bool
	table            func(record []byte) ([]byte, error)
	tablePath        string
}

var goldens = []golden{
	{name: "visibility", file: oracle.VisibilityFile, what: "markdown_visibility の関数の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		cases, err := oracle.VisibilityCases(e.corpus)
		if err != nil {
			return nil, err
		}
		return buildVisibility(e.ctx, r, cases, e.header)
	}},
	{name: "bold", file: oracle.BoldFile, what: "太字 (bold_problems) の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildBold(e.ctx, r, e.header, e.corpus)
	}},
	{name: "lint-base", file: oracle.LintBaseFile, what: "lint の土台の関数の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		cases, err := oracle.LintBaseCases(e.corpus)
		if err != nil {
			return nil, err
		}
		return buildLintBase(e.ctx, r, cases, e.header)
	}, table: buildLintTables, tablePath: oracle.LintEmojiTablesPath},
	{name: "lint-cli", file: oracle.LintCLIFile, what: "lint の CLI の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		files, err := oracle.LintCLIFiles(e.corpus, e.p.words)
		if err != nil {
			return nil, err
		}
		g, err := r.RecordLintCLI(e.ctx, e.header, oracle.LintCLICases(e.corpus, visibility.DocTypeValues(), visibility.ReaderValues()), files)
		if err != nil {
			return nil, err
		}
		return oracle.MarshalLintCLI(g)
	}},
	{name: "lint-raw", file: oracle.LintRawFile, what: "lint の検出の生の並びの記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		files, err := oracle.LintCLIFiles(e.corpus, e.p.words)
		if err != nil {
			return nil, err
		}
		g, err := r.RecordLintRaw(e.ctx, e.header, oracle.LintRawCases(e.corpus), files)
		if err != nil {
			return nil, err
		}
		return oracle.MarshalLintRaw(g)
	}},
	{name: "lint-post", file: oracle.LintPostFile, what: "lint の検出より後の処理の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		files, err := oracle.LintCLIFiles(e.corpus, e.p.words)
		if err != nil {
			return nil, err
		}
		g, err := r.RecordLintPost(e.ctx, e.header, oracle.LintPostCases(visibility.DocTypeValues(), visibility.ReaderValues()), files)
		if err != nil {
			return nil, err
		}
		return oracle.MarshalLintPost(g)
	}},
	{name: "patterns", file: oracle.PatternsFile, what: "パターンの記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildPatterns(e.ctx, r, e.corpus, e.p.words, e.header)
	}, same: samePatternsIgnoringText},
	{name: "smoke", file: oracle.SmokeFile, what: "関数と CLI の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildSmoke(e.ctx, r, e.header.WithoutCorpus())
	}},
	{name: "unicode", file: oracle.UnicodeFile, what: "全コードポイントの記録 (unicode)", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildUnicode(e.ctx, r, e.header.WithoutCorpus())
	}, table: buildTables, tablePath: oracle.PyunicodeTablesPath},
	{name: "json", file: oracle.JSONFile, what: "json の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildJSON(e.ctx, r, e.header.WithoutCorpus())
	}},
	{name: "pystr", file: oracle.PystrFile, what: "str の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildPystr(e.ctx, r, e.header.WithoutCorpus())
	}},
	{name: "pyre", file: oracle.PyreFile, what: "re の記録 (pyre)", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildPyre(e.ctx, r, e.header.WithoutCorpus())
	}, table: buildPyreTables, tablePath: oracle.PyreTablesPath},
	{name: "difflib", file: oracle.DifflibFile, what: "difflib の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildDifflib(e.ctx, r, e.header.WithoutCorpus(), e.p.corpus)
	}},
	{name: "pause_comma", file: oracle.PauseCommaFile, what: "pause_comma の記録", build: func(e genEnv, r oracle.Runner) ([]byte, error) {
		return buildPauseComma(e.ctx, r, e.header.WithoutCorpus(), e.corpus)
	}},
}

func goldenNames() []string {
	names := make([]string, len(goldens))
	for i, g := range goldens {
		names[i] = g.name
	}
	return names
}

// checkEnv は記録の環境が今と違うときに止める。記録がまだ無いときと update のときは通す。
func checkEnv(p paths, now oracle.Header, update bool) error {
	for _, g := range goldens {
		var old struct {
			Header oracle.Header `json:"header"`
		}
		err := oracle.ReadGoldenHeader(p.golden, g.file, &old)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if diff := old.Header.EnvMismatch(now); diff != "" && !update {
			return fmt.Errorf("%s の記録を取った環境が今と違います (%s)。作り直すなら UPDATE=1 を付ける", g.file, diff)
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

func buildLintBase(ctx context.Context, r oracle.Runner, cases []oracle.VisCase, h oracle.Header) ([]byte, error) {
	g, err := r.RecordLintBase(ctx, h, cases)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalLintBase(g)
}

// buildLintTables は書き出す lint-base.json のバイト列を読み直して、絵文字の Go の表にする (ファイルと表が同じ記録から作られることを保つ)。
func buildLintTables(lintBase []byte) ([]byte, error) {
	g, err := oracle.ParseLintBase(lintBase)
	if err != nil {
		return nil, err
	}
	return oracle.GenerateLintEmojiTables(g)
}

// writeAll は記録と表を書く。全部を作り終えてから呼ぶので、途中で生成が落ちたときは 1 つも書き換わらない。
func writeAll(files map[string][]byte) error {
	for _, path := range slices.Sorted(maps.Keys(files)) {
		if err := atomicfile.Write(path, files[path], 0o644); err != nil {
			return fmt.Errorf("%s へ書けません: %w", path, err)
		}
		fmt.Printf("%s: %d バイト\n", path, len(files[path]))
	}
	return nil
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

func buildBold(ctx context.Context, r oracle.Runner, h oracle.Header, c oracle.Corpus) ([]byte, error) {
	g, err := r.RecordBold(ctx, h, c)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalBold(g)
}

func buildPauseComma(ctx context.Context, r oracle.Runner, h oracle.Header, c oracle.Corpus) ([]byte, error) {
	g, err := r.RecordPauseComma(ctx, h, c)
	if err != nil {
		return nil, err
	}
	return oracle.MarshalPauseComma(g)
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
