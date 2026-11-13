package pausecomma

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"ja-writing/oracle"
	"ja-writing/visibility"
)

const (
	goldenDir = "../../testdata/golden"
	corpusDir = "../../testdata/corpus"
)

// proseKinds は本家の _PROSE_SCAN_KINDS (_METRIC_OPAQUE_KINDS と container_prefix)。骨組み (T3 の _prose_visible_text) が入るまで、
// 試験が見える字を取るのに使う。
var proseKinds = []string{"code", "frontmatter", "html_block", "reference_definition", "inline_code",
	"html_token", "autolink", "link_destination", "image", "bare_url", "container_prefix"}

// triggers は本家の driver が行を記録に入れる字 (record_pause_comma.py の TRIGGERS)。
var triggers = []string{"は、", "が、", "も、", "を、", "を，", "を,"}

func toRecord(hs []Hit) []oracle.PauseHit {
	out := []oracle.PauseHit{}
	for _, h := range hs {
		out = append(out, oracle.PauseHit{Pos: h.Pos, Chunk: oracle.WStr(h.Chunk), Phrase: oracle.WStr(h.Phrase)})
	}
	return out
}

func sameHits(got []Hit, want []oracle.PauseHit) bool {
	return slices.Equal(toRecord(got), append([]oracle.PauseHit{}, want...))
}

func checkTable(t *testing.T, name, text string, objects bool, want oracle.PauseLineTable) {
	t.Helper()
	a, err := visibility.AnalyzeMarkdown(text, true)
	if err != nil {
		t.Fatalf("%s: analyze_markdown: %v", name, err)
	}
	visible := func(line int) string {
		s, err := a.LineVisibleText(line, proseKinds)
		if err != nil {
			t.Fatalf("%s: line_visible_text(%d): %v", name, line, err)
		}
		return s
	}
	got := LineHits(a, visible, objects)
	keys := make([]int, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	if !slices.Equal(keys, append([]int{}, want.Keys...)) {
		t.Fatalf("%s (objects=%v): 表の行 %v、Python は %v", name, objects, keys, want.Keys)
	}
	var entries []oracle.PauseLineEntry
	for _, k := range keys {
		if len(got[k]) > 0 {
			entries = append(entries, oracle.PauseLineEntry{Line: k, Hits: toRecord(got[k])})
		}
	}
	if len(entries) != len(want.Hits) {
		t.Fatalf("%s (objects=%v): 当たりのある行 %d、Python は %d", name, objects, len(entries), len(want.Hits))
	}
	for k := range entries {
		if entries[k].Line != want.Hits[k].Line || !slices.Equal(entries[k].Hits, want.Hits[k].Hits) {
			t.Fatalf("%s (objects=%v): 行 %d の当たり %v、Python は 行 %d %v", name, objects, entries[k].Line, entries[k].Hits, want.Hits[k].Line, want.Hits[k].Hits)
		}
	}
}

// TestMatchesPythonRecording は、記録の全入力で Hits・ObjectHits・LineHits が本家の _pause_comma_hits・_pause_comma_object_hits・
// _pause_comma_line_hits と同じ値を返すことを確かめる。除外の条件ごとに、記録の入力で 1 回以上通ったことも数える
// (どこかの条件が入力から外れたまま緑にならないように)。
func TestMatchesPythonRecording(t *testing.T) {
	var g oracle.PauseCommaGolden
	if err := oracle.ReadGolden(goldenDir, oracle.PauseCommaFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	if len(g.Texts) != len(oracle.PauseCommaTexts) || len(g.Docs) != len(oracle.PauseCommaDocs) || len(g.Files) < 300 {
		t.Fatalf("記録の入力が足りない (文 %d / %d・文書 %d / %d・ファイル %d)", len(g.Texts), len(oracle.PauseCommaTexts), len(g.Docs), len(oracle.PauseCommaDocs), len(g.Files))
	}
	seen := map[string]int{}
	onCondition = func(name string) { seen[name]++ }
	defer func() { onCondition = nil }()
	checkStr := func(name, text string, c oracle.PauseStrCase) {
		if got := Hits(text); !sameHits(got, c.Hits) {
			t.Fatalf("%s: Hits(%q) = %v、Python は %v", name, text, toRecord(got), c.Hits)
		}
		if got := ObjectHits(text); !sameHits(got, c.Objects) {
			t.Fatalf("%s: ObjectHits(%q) = %v、Python は %v", name, text, toRecord(got), c.Objects)
		}
	}
	for k, c := range g.Texts {
		if string(c.Text) != oracle.PauseCommaTexts[k] {
			t.Fatalf("文 %d が記録と違う (make golden で作り直す)", k)
		}
		checkStr("文", string(c.Text), c)
	}
	for k, d := range g.Docs {
		if string(d.Text) != oracle.PauseCommaDocs[k] {
			t.Fatalf("文書 %d が記録と違う (make golden で作り直す)", k)
		}
		checkTable(t, "文書", string(d.Text), false, d.Table)
		checkTable(t, "文書", string(d.Text), true, d.TableObjects)
	}
	lines := 0
	for _, f := range g.Files {
		body, err := os.ReadFile(filepath.Join(corpusDir, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if got := oracle.SHA256Hex(body); got != f.SHA256 {
			t.Fatalf("%s が記録を取ったときと違う (make golden で作り直す)", f.Path)
		}
		text, _, err := visibility.DecodeSourceBytes(body)
		if err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		split := strings.Split(text, "\n")
		recorded := 0
		for k, line := range split {
			if !slices.ContainsFunc(triggers, func(tr string) bool { return strings.Contains(line, tr) }) {
				continue
			}
			if recorded >= len(f.Lines) || f.Lines[recorded].Line != k {
				t.Fatalf("%s: 行 %d が記録に無い (trigger の字の数え方が driver とずれている)", f.Path, k)
			}
			checkStr(f.Path, line, f.Lines[recorded])
			recorded++
		}
		if recorded != len(f.Lines) {
			t.Fatalf("%s: 記録の行 %d、Go で数えた行 %d", f.Path, len(f.Lines), recorded)
		}
		lines += recorded
		checkTable(t, f.Path, text, false, f.Table)
		checkTable(t, f.Path, text, true, f.TableObjects)
	}
	conditions := []string{"a", "b-to", "b-n", "b-na", "c", "d", "e", "f", "g", "g-mo", "h", "i", "i-letter", "j", "j-block", "k", "l", "m", "n",
		"o", "p", "q", "r", "r-inner", "r-before", "r-list", "r-mo", "s", "t", "hit",
		"obj-noun", "obj-prev", "obj-second", "obj-subject", "obj-attach", "obj-hit"}
	for _, name := range conditions {
		if seen[name] == 0 {
			t.Errorf("条件 %s が記録の入力で 1 回も通らなかった", name)
		}
	}
	if len(seen) != len(conditions) {
		t.Errorf("数えた条件 %v と、試験の一覧 %v の名前がずれている", seen, conditions)
	}
	t.Logf("文 %d・文書 %d・ファイル %d (行 %d) で Python と一致。条件ごとの回数: %v", len(g.Texts), len(g.Docs), len(g.Files), lines, seen)
}
