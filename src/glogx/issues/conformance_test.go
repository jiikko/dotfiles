package issues

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func conformanceKinds(iss *Issue, src string) []ViolationKind {
	var kinds []ViolationKind
	for _, v := range NewBody(src).Conformance(iss) {
		kinds = append(kinds, v.Kind)
	}
	return kinds
}

func TestConformanceAcceptsIssueThatFollowsTheRules(t *testing.T) {
	iss := newIssue("/r/issues", "051-task-re93-remove-soft-delete.md")
	src := "# 051 (task): RE-93 論理削除をやめる\n\n起票日: 2026-11-20\n\n## 概要\n"
	if got := conformanceKinds(iss, src); len(got) != 0 {
		t.Fatalf("規約どおりの issue に警告が出た: %v", got)
	}
}

func TestConformanceFlagsUnnumberedFileInEpic(t *testing.T) {
	iss := newIssue("/r/issues", filepath.Join("epic", "051", "plan.md"))
	src := "# RE-93 論理削除の廃止 — 全体プラン\n\n調査日: 2026-11-18\n"
	want := []ViolationKind{ViolationNoNumber, ViolationH1Form, ViolationNoCreated}
	if got := conformanceKinds(iss, src); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestConformanceFlagsFilenameWithoutType(t *testing.T) {
	iss := newIssue("/r/issues", "050-foo.md")
	src := "# 050 (bug): foo\n\n起票日: 2026-11-20\n"
	if got := conformanceKinds(iss, src); !slices.Equal(got, []ViolationKind{ViolationNoType}) {
		t.Fatalf("got %v", got)
	}
}

func TestConformanceFlagsH1ThatDisagreesWithFilename(t *testing.T) {
	cases := map[string]struct {
		src  string
		want []ViolationKind
	}{
		"番号が違う":         {"# 052 (feat): x\n起票日: 2026-11-20\n", []ViolationKind{ViolationH1Number}},
		"type が違う":      {"# 051 (perf/design): x\n起票日: 2026-11-20\n", []ViolationKind{ViolationH1Type}},
		"括弧のない旧い形":      {"# 051 feat: x\n起票日: 2026-11-20\n", []ViolationKind{ViolationH1Form}},
		"H1 が無い":        {"起票日: 2026-11-20\n本文\n", []ViolationKind{ViolationNoH1}},
		"type の大小は問わない": {"# 051 (Feat): x\n起票日: 2026-11-20\n", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			iss := newIssue("/r/issues", "051-feat-x.md")
			if got := conformanceKinds(iss, c.src); !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// コードフェンスと front matter の中の H1 / 起票日は本文の見出しではない (規約を説明する issue が例を書く)
func TestConformanceIgnoresFenceAndFrontMatter(t *testing.T) {
	iss := newIssue("/r/issues", "051-feat-x.md")
	src := "---\nstatus: open\n---\n```markdown\n# 051 (feat): 例\n起票日: 2026-01-01\n```\n# 本物の見出し\n"
	want := []ViolationKind{ViolationH1Form, ViolationNoCreated}
	if got := conformanceKinds(iss, src); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestConformanceDeadlineForms(t *testing.T) {
	head := "# 051 (human): x\n\n起票日: 2026-11-20\n"
	cases := map[string]struct {
		src  string
		want []ViolationKind
	}{
		"行頭の期限":   {head + "期限: 2026-12-01\n", nil},
		"箇条書きの期限": {head + "- 期限: 2026-12-01\n", []ViolationKind{ViolationDeadlineForm}},
		"太字の期限":   {head + "**期限: 2026-12-01**\n", []ViolationKind{ViolationDeadlineForm}},
		"期限なし":    {head, []ViolationKind{ViolationHumanNoDeadline}},
		"日付の無い言及は書き方の警告にしない": {head + "期限: 2026-12-01\n- 期限: は箇条書きにしない\n", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			iss := newIssue("/r/issues", "051-human-x.md")
			if got := conformanceKinds(iss, c.src); !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// done と CATEGORY-NNN の形は対象の外 (Conformance の doc)
func TestConformanceSkipsDoneAndPrefixedIDs(t *testing.T) {
	done := newIssue("/r/issues", filepath.Join("done", "plan.md"))
	done.Status = StatusDone
	if got := conformanceKinds(done, "# メモ\n"); len(got) != 0 {
		t.Fatalf("done に警告が出た: %v", got)
	}
	prefixed := newIssue("/r/issues", "UI-005-button.md")
	if got := conformanceKinds(prefixed, "# UI-005 button\n"); !slices.Equal(got, []ViolationKind{ViolationNoCreated}) {
		t.Fatalf("CATEGORY-NNN でファイル名・H1 を見た: %v", got)
	}
}

func TestFixPromptForUnnumberedFileTellsHowToRenameAndRelink(t *testing.T) {
	iss := newIssue("/r/issues", filepath.Join("epic", "051", "plan.md"))
	got := NewBody("# RE-93 論理削除の廃止 — 全体プラン\n").FixPrompt(iss, "/r", "052")
	for _, want := range []string{
		"issues/epic/051/plan.md が issue の規約",
		"issues/epic/051/052-<type>-<slug>.md へ git mv",
		"`# 052 (<type>): RE-93 論理削除の廃止 — 全体プラン`",
		"起票日: YYYY-MM-DD",
		"issues/ の外 (docs/ など) へ移し",
		"grep -rn 'plan.md' /r/issues",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("指示に %q が無い:\n%s", want, got)
		}
	}
}

func TestFixPromptForH1KeepsNumberAndType(t *testing.T) {
	iss := newIssue("/r/issues", "051-feat-x.md")
	got := NewBody("# 051 feat: x を直す\n").FixPrompt(iss, "/r", "099")
	if !strings.Contains(got, "H1 を `# 051 (feat): x を直す` の形にする") {
		t.Fatalf("H1 の直し方が無い:\n%s", got)
	}
	if strings.Contains(got, "099") || strings.Contains(got, "git mv") {
		t.Fatalf("ファイル名が規約どおりなのに番号の振り直しを指示した:\n%s", got)
	}
}
