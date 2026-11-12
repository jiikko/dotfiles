package oracle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"path"
	"slices"
	"strconv"
	"strings"
)

// 記録の単位ごとの当たりは、Python の re.finditer の結果 [start, end, (各グループの start, end)...] を
// 1 行 "<start> <end> <g1start> <g1end> ..." (一致しなかったグループは -1 -1) にして並べたもの。位置は str の添字 (rune 単位)。

// Digest は全単位の一致を並べて 1 つの SHA-256 にする。Python の driver (py/record_patterns.py) と同じ並べ方で、
// 一致のある単位だけを、単位の並び順に「<単位名>\n」と一致の行を書く。
type Digest struct{ h hash.Hash }

// NewDigest は空の Digest を作る。
func NewDigest() *Digest { return &Digest{h: sha256.New()} }

// Unit は一致のある単位を始める。一致が 0 件の単位では呼ばない。
func (d *Digest) Unit(key string) { _, _ = d.h.Write([]byte(key + "\n")) }

// Match は直前の Unit の一致を 1 つ足す。spans は start, end, 各グループの start, end の並び。
func (d *Digest) Match(spans ...int) {
	parts := make([]string, len(spans))
	for i, v := range spans {
		parts[i] = strconv.Itoa(v)
	}
	_, _ = d.h.Write([]byte(strings.Join(parts, " ") + "\n"))
}

// Sum は 16 進の SHA-256 を返す。
func (d *Digest) Sum() string { return hex.EncodeToString(d.h.Sum(nil)) }

// Last は直前の Unit の一致 1 つの lastindex と lastgroup を足す (Python の driver の last_digest と同じ並べ方)。
// 無いもの (None) は、lastindex は -1、lastgroup は "-"。
func (d *Digest) Last(index int, group string) {
	_, _ = d.h.Write([]byte(strconv.Itoa(index) + " " + group + "\n"))
}

// WindowDigest は窓 (pos / endpos) つきの search / match の結果を並べて 1 つの SHA-256 にする。Python の driver (py/record_patterns.py の
// window_digest) と同じ並べ方。窓の選び方は WindowUnits と WindowBounds にある。
type WindowDigest struct {
	*Digest
	Calls int
}

// NewWindowDigest は空の WindowDigest を作る。
func NewWindowDigest() *WindowDigest { return &WindowDigest{Digest: NewDigest()} }

// Row は 1 回の呼び出しの結果を足す。spans は一致しなければ nil、一致したら start, end, 各グループの start, end。
func (d *WindowDigest) Row(unit string, pos, endpos int, op string, spans []int) {
	row := "-"
	if spans != nil {
		parts := make([]string, len(spans))
		for i, v := range spans {
			parts[i] = strconv.Itoa(v)
		}
		row = strings.Join(parts, " ")
	}
	_, _ = d.h.Write([]byte(unit + "|" + strconv.Itoa(pos) + "|" + strconv.Itoa(endpos) + "|" + op + "|" + row + "\n"))
	d.Calls++
}

// WindowUnits はパターンごとに窓を当てる単位の数 (最初に当たった単位から数える)。
const WindowUnits = 8

// WindowBounds は最初の一致 [start, end) と単位の長さ length から、窓の pos と endpos の候補 (昇順・重複なし) を作る。
func WindowBounds(start, end, length int) (poss, endposs []int) {
	return uniqSorted(max(0, start-1), start, min(length, start+1)), uniqSorted(max(0, end-1), end, min(length, end+1))
}

func uniqSorted(vs ...int) []int {
	slices.Sort(vs)
	return slices.Compact(vs)
}

// PatternRecord は本家の道具が使った 1 つの (パターン, フラグ) と、入力の集まりに当てた一致の記録。
type PatternRecord struct {
	Pattern       string         `json:"pattern"`
	Flags         int            `json:"flags"`
	CompiledFlags int            `json:"compiled_flags"`
	Groups        int            `json:"groups"`
	GroupIndex    map[string]int `json:"groupindex"`
	MatchedUnits  int            `json:"matched_units"`
	Matches       int            `json:"matches"`
	Digest        string         `json:"digest"`
	Sample        []SampleUnit   `json:"sample"`
	WindowDigest  string         `json:"window_digest"`
	WindowCalls   int            `json:"window_calls"`
	// LastDigest はグループを持つパターンだけにある、各一致の lastindex と lastgroup を Digest と同じ並べ方にした SHA-256 (Digest.Last)。
	LastDigest string `json:"last_digest,omitempty"`
	// Static は本家のソースの re.xxx("文字列") の literal にあるパターン (道具を動かして使われなかったものも含む)。
	Static bool `json:"static,omitempty"`
}

// SampleUnit は digest が合わなかったときに差を見るための、最初の数単位の一致 (各行は Digest.Match の spans)。
type SampleUnit struct {
	Unit    string  `json:"unit"`
	Matches [][]int `json:"matches"`
}

// ToolRun は本家の道具を 1 回動かした記録 (パターンを集めるための入力。rc は落ちた run を見つけるために残す)。
type ToolRun struct {
	Module string   `json:"module"`
	Argv   []string `json:"argv"`
	RC     int      `json:"rc"`
}

// PatternRecording は RecordPatterns の結果。
type PatternRecording struct {
	Runs []ToolRun `json:"runs"`
	// StaticCalls は ast で数えた re.xxx("文字列") の呼び出しの数、StaticPatterns はその (パターン, フラグ) の重複を除いた数。
	StaticCalls    int             `json:"static_calls"`
	StaticPatterns int             `json:"static_patterns"`
	Patterns       []PatternRecord `json:"patterns"`
}

type toolSpec struct {
	Module string   `json:"module"`
	Argv   []string `json:"argv"`
	Stdin  *string  `json:"stdin"`
}

var docTypes = []string{"pr", "article", "incident", "proposal", "minutes", "manual", "slides", "essay", "formal", "promo", "mail", "paper", "script"}

var stances = []string{"勧め", "決まり", "説明"}

const (
	upstreamCorpusDir = "upstream/"
	wordsDirInCwd     = "words"
	docTypeFilesEach  = 12
)

// planRuns は入力の集まりから、パターンを集めるために本家の道具を動かす run の一覧を作る。
// lint は全ファイルを素で・種類を変えて・語の一覧つきで、diff と check は本家の raw_ai と書き直しの組などで動かす。
// 組み立て方は決定的 (ファイル順だけで決まる) で、記録を 2 回作って同じになることを保つ。
func planRuns(c Corpus) []toolSpec {
	var runs []toolSpec
	var md []string
	has := map[string]bool{}
	for _, f := range c.Files {
		has[f.Path] = true
		if ext := path.Ext(f.Path); ext == ".md" || ext == ".txt" {
			runs = append(runs, toolSpec{Module: "yomiyasu_lint", Argv: []string{"corpus/" + f.Path}})
		}
		if path.Ext(f.Path) == ".md" {
			md = append(md, f.Path)
		}
	}
	for i, dt := range docTypes {
		for j := i; j < len(md) && j < i+docTypeFilesEach*len(docTypes); j += len(docTypes) {
			runs = append(runs, toolSpec{Module: "yomiyasu_lint", Argv: []string{"--doc-type", dt, "--reader", "general", "corpus/" + md[j]}})
		}
	}
	for i := 0; i < len(md) && i < 40; i++ {
		runs = append(runs, toolSpec{Module: "yomiyasu_lint", Argv: []string{"--words", wordsDirInCwd, "corpus/" + md[i]}})
	}
	for _, f := range c.Files {
		base, ok := strings.CutPrefix(f.Path, upstreamCorpusDir+"raw_ai/")
		if !ok {
			continue
		}
		rewritten := upstreamCorpusDir + "yomiyasu_rewritten/" + base
		black := upstreamCorpusDir + "blacklist_ai/" + base
		raw := "corpus/" + f.Path
		if !has[rewritten] {
			continue
		}
		pairs := [][]string{{raw, "corpus/" + rewritten}}
		if has[black] {
			pairs = append(pairs, []string{"corpus/" + black, raw})
		}
		for i, pair := range pairs {
			runs = append(runs, toolSpec{Module: "yomiyasu_diff", Argv: pair})
			runs = append(runs, toolSpec{Module: "yomiyasu_diff", Argv: append([]string{"--stance", stances[(len(runs)+i)%len(stances)], "--words", wordsDirInCwd}, pair...)})
		}
		runs = append(runs,
			toolSpec{Module: "yomiyasu_diff", Argv: []string{"--endings", raw}},
			toolSpec{Module: "yomiyasu_check", Argv: []string{"run", "--source", raw, "--final", "corpus/" + rewritten, "--no-record", "--state-dir", "state"}},
		)
	}
	for _, argv := range [][]string{{"--version"}, {"--print-config", "claude-code"}, {"--print-config", "codex"}} {
		runs = append(runs, toolSpec{Module: "yomiyasu_hook", Argv: argv})
	}
	return runs
}

// RecordPatterns は入力の集まりと語の一覧のフォルダ (wordsDir) を使い捨ての cwd に写し、本家の道具を動かして
// 使われた全 (パターン, フラグ) を集め、各パターンを全単位に当てた記録を返す。
func (r Runner) RecordPatterns(ctx context.Context, c Corpus, wordsDir string) (PatternRecording, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return PatternRecording{}, err
	}
	defer sb.remove()
	files := map[string]string{}
	for _, f := range c.Files {
		body, rerr := c.Read(f.Path)
		if rerr != nil {
			return PatternRecording{}, rerr
		}
		files["corpus/"+f.Path] = body
	}
	words, err := readDirFiles(wordsDir)
	if err != nil {
		return PatternRecording{}, err
	}
	for name, body := range words {
		files[wordsDirInCwd+"/"+name] = body
	}
	if err := placeFiles(sb.cwd, files); err != nil {
		return PatternRecording{}, err
	}
	paths := make([]string, len(c.Files))
	for i, f := range c.Files {
		paths[i] = f.Path
	}
	in := struct {
		ScriptsDir string     `json:"scripts_dir"`
		Files      []string   `json:"files"`
		Runs       []toolSpec `json:"runs"`
	}{sb.tree.Path(ScriptsDir), paths, planRuns(c)}
	var out PatternRecording
	if err := r.runDriver(ctx, sb, "record_patterns.py", in, &out); err != nil {
		return PatternRecording{}, err
	}
	if len(out.Patterns) == 0 {
		return PatternRecording{}, errors.New("本家の道具が使ったパターンを 1 つも集められませんでした")
	}
	return out, nil
}
