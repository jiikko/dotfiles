package main

import (
	"strings"
	"testing"
)

// statusRec は `git status --porcelain -z` の 1 レコード (NUL 終端) を組む。
func statusRec(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
		b.WriteByte(0)
	}
	return b.String()
}

func TestParseWorktreeStatusMapsXYToSections(t *testing.T) {
	out := statusRec(
		"## master...origin/master [ahead 1]",
		"M  staged.go",   // index だけ
		" M unstaged.go", // 作業ツリーだけ
		"MM both.go",     // 両方 = 一部だけ staged
		"?? new.txt",
		"UU conflict.go",
	)
	st := parseWorktreeStatus(out)

	if st.branch != "master" || st.track != "ahead 1" {
		t.Fatalf("branch/track = %q/%q, want master/ahead 1", st.branch, st.track)
	}
	// both.go は Staged と Unstaged の両方に出る (spec 2 節)
	want := []struct {
		sec  worktreeSection
		path string
		code byte
	}{
		{sectionStaged, "staged.go", 'M'},
		{sectionStaged, "both.go", 'M'},
		{sectionUnstaged, "unstaged.go", 'M'},
		{sectionUnstaged, "both.go", 'M'},
		{sectionUntracked, "new.txt", '?'},
		{sectionConflicted, "conflict.go", 'U'},
	}
	got := st.ordered()
	if len(got) != len(want) {
		t.Fatalf("行数 = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].section != w.sec || got[i].path != w.path || got[i].code != w.code {
			t.Errorf("行 %d = (%d, %q, %c), want (%d, %q, %c)",
				i, got[i].section, got[i].path, got[i].code, w.sec, w.path, w.code)
		}
	}
	// partial (◐) が立つのは両側に変更があるものだけ
	for _, r := range got {
		wantPartial := r.path == "both.go"
		if r.partial != wantPartial {
			t.Errorf("%s (section %d) の partial = %v, want %v", r.path, r.section, r.partial, wantPartial)
		}
	}
}

func TestParseWorktreeStatusCounts(t *testing.T) {
	st := parseWorktreeStatus(statusRec("M  a", " M b", "?? c", "UU d"))
	staged, unstaged, untracked, conflicted := st.counts()
	if staged != 1 || unstaged != 1 || untracked != 1 || conflicted != 1 {
		t.Fatalf("counts = %d/%d/%d/%d, want 1/1/1/1", staged, unstaged, untracked, conflicted)
	}
	if st.clean() {
		t.Error("変更があるのに clean() = true")
	}
	if !parseWorktreeStatus("").clean() {
		t.Error("空の出力で clean() = false")
	}
}

// -z は空白を含むパスをクォートしないので、そのまま 1 つのパスとして読めること。
// (既定の LF 区切りだと "a b.txt" のように引用されて、素朴な分割ではずれる)
func TestParseWorktreeStatusKeepsPathWithSpaces(t *testing.T) {
	st := parseWorktreeStatus(statusRec("?? tmp/a b c.txt", " M src/日本語 ファイル.go"))
	got := st.ordered()
	if len(got) != 2 {
		t.Fatalf("行数 = %d, want 2: %+v", len(got), got)
	}
	if got[0].path != "src/日本語 ファイル.go" {
		t.Errorf("unstaged path = %q", got[0].path)
	}
	if got[1].path != "tmp/a b c.txt" {
		t.Errorf("untracked path = %q", got[1].path)
	}
}

// rename は 2 レコード使う (新 → 旧)。orig を拾い、git へ渡すパスは両方になること。
func TestParseWorktreeStatusRename(t *testing.T) {
	st := parseWorktreeStatus(statusRec("R  new.go", "old.go", " M other.go"))
	got := st.ordered()
	if len(got) != 2 {
		t.Fatalf("行数 = %d, want 2 (rename の旧パスを行として数えていないか): %+v", len(got), got)
	}
	r := got[0]
	if r.path != "new.go" || r.orig != "old.go" {
		t.Fatalf("rename 行 = (%q, orig %q), want (new.go, old.go)", r.path, r.orig)
	}
	paths := r.paths()
	if len(paths) != 2 || paths[0] != "new.go" || paths[1] != "old.go" {
		t.Errorf("paths() = %v, want [new.go old.go]", paths)
	}
	if got[1].path != "other.go" {
		t.Errorf("rename の次の行 = %q, want other.go (旧パスを消費できていない)", got[1].path)
	}
}

func TestParseWorktreeStatusSkipsBrokenRecords(t *testing.T) {
	// 2 文字未満 / セパレータが空白でない / 空レコード は捨てて、読めた分だけ返す
	st := parseWorktreeStatus(statusRec("M", "MMnospace", "", " M ok.go"))
	got := st.ordered()
	if len(got) != 1 || got[0].path != "ok.go" {
		t.Fatalf("ordered() = %+v, want ok.go の 1 行だけ", got)
	}
}

func TestParseBranchHeader(t *testing.T) {
	cases := []struct {
		in     string
		branch string
		track  string
	}{
		{"## master...origin/master [ahead 1]", "master", "ahead 1"},
		{"## master...origin/master [ahead 2, behind 3]", "master", "ahead 2, behind 3"},
		{"## master...origin/master", "master", ""},
		{"## local-only", "local-only", ""},
		{"## HEAD (no branch)", "HEAD (no branch)", ""},
	}
	for _, c := range cases {
		branch, track := parseBranchHeader(c.in)
		if branch != c.branch || track != c.track {
			t.Errorf("parseBranchHeader(%q) = (%q, %q), want (%q, %q)", c.in, branch, track, c.branch, c.track)
		}
	}
}

// conflict の XY は片側だけ見て判定できない (AA / DD のように X と Y が同じ文字でも unmerged)。
func TestConflictCodesAreClassifiedByPair(t *testing.T) {
	for _, code := range []string{"DD", "AU", "UD", "UA", "DU", "AA", "UU"} {
		st := parseWorktreeStatus(statusRec(code + " f.go"))
		got := st.ordered()
		if len(got) != 1 || got[0].section != sectionConflicted {
			t.Errorf("%s → %+v, want Conflicted の 1 行", code, got)
		}
	}
	// 似ているが conflict ではない組み合わせ (A + D) は通常の 2 区画へ
	st := parseWorktreeStatus(statusRec("AD f.go"))
	got := st.ordered()
	if len(got) != 2 || got[0].section != sectionStaged || got[1].section != sectionUnstaged {
		t.Errorf("AD → %+v, want Staged + Unstaged", got)
	}
}

func TestWorktreeRowIsDirOnlyForUntracked(t *testing.T) {
	st := parseWorktreeStatus(statusRec("?? tmp/", "?? tmp/f.txt", " M dir/"))
	rows := st.ordered()
	want := map[string]bool{"tmp/": true, "tmp/f.txt": false, "dir/": false}
	for _, r := range rows {
		if got := r.isDir(); got != want[r.path] {
			t.Errorf("%q (section %d) isDir() = %v, want %v", r.path, r.section, got, want[r.path])
		}
	}
}

func TestWorktreeStatusFind(t *testing.T) {
	st := parseWorktreeStatus(statusRec("MM a.go"))
	if r, ok := st.find(sectionUnstaged, "a.go"); !ok || r.x != 'M' || r.y != 'M' {
		t.Fatalf("find(Unstaged, a.go) = (%+v, %v)", r, ok)
	}
	if _, ok := st.find(sectionUntracked, "a.go"); ok {
		t.Error("find は section も一致条件にしていない")
	}
}

func TestSectionMutableExcludesConflicted(t *testing.T) {
	for _, sec := range []worktreeSection{sectionStaged, sectionUnstaged, sectionUntracked} {
		if !sec.mutable() {
			t.Errorf("section %d が mutable() = false", sec)
		}
	}
	if sectionConflicted.mutable() {
		t.Error("Conflicted が mutable() = true (conflict の解決はシェルの仕事)")
	}
}
