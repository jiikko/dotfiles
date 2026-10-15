package boundarycheck

import (
	"strings"
	"testing"
)

// 🚨 **検査器そのものを検査する。** 各 module の呼び出しは「今の実装に違反が無い」ことしか示さず、
// **検査器が違反を見逃すようになった**ことは検出しない (doctor/internal/displaycheck と同じ構え)。
// fixture は testdata/ に置く (Go のツールチェインが無視するディレクトリなので、わざと壊した package を置いても
// 本体のビルドに混ざらない)。

func waitDelaySpec() Spec {
	return Spec{
		Root: "testdata/waitdelay", SkipDirs: []string{"tools"},
		Allow: map[string]string{
			"assign.go":  "代入がある (fixture)",
			"marker.go":  "注記で免除 (fixture)",
			"missing.go": "代入が無い offender (fixture。Allow には載っているので出る指摘は offender だけ)",
		},
	}
}

func problemsOf(t *testing.T, s Spec) (report, []string) {
	t.Helper()
	r, fatal := check(s)
	if fatal != "" {
		t.Fatalf("検査できなかった: %s", fatal)
	}
	return r, r.problems
}

func wantProblem(t *testing.T, problems []string, substr string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, substr) {
			return
		}
	}
	t.Fatalf("%q を含む指摘が出ていない: %v", substr, problems)
}

func TestWaitDelayFixtureReportsExactlyTheViolations(t *testing.T) {
	r, problems := problemsOf(t, waitDelaySpec())
	if len(problems) != 3 {
		t.Fatalf("指摘が 3 件でない: %v", problems)
	}
	wantProblem(t, problems, "missing.go:6: WaitDelay を張っていない")
	wantProblem(t, problems, "alias.go: os/exec を別名 import している (xe)")
	wantProblem(t, problems, "notallowed.go: *exec.Cmd を作っているが Allow に無い")
	// 走査の射程: tools/ (SkipDirs)・_skipped/・sub/testdata/ は見ない。sub/ok.go は見る
	if r.files != 7 || r.perDir["."] != 6 || r.perDir["sub"] != 1 {
		t.Fatalf("走査の数が違う: files=%d perDir=%v", r.files, r.perDir)
	}
	// seen = assign / marker / missing / notallowed (alias.go は xe. なので字句に出ない)、enforced = 注記の marker.go を除く 3
	if r.seen != 4 || r.enforced != 3 {
		t.Fatalf("seen=%d enforced=%d, want 4 / 3", r.seen, r.enforced)
	}
}

func TestCleanFixtureHasNoProblems(t *testing.T) {
	s := Spec{Root: "testdata/clean", Allow: map[string]string{"assign.go": "代入がある (fixture)"}}
	if _, problems := problemsOf(t, s); len(problems) != 0 {
		t.Fatalf("違反の無い fixture で指摘が出た: %v", problems)
	}
}

// 🚨 **未使用の Allow を違反にする**。Allow は「Cmd を作るファイル」の母集合なので、作らなくなったら外す。
func TestUnusedAllowIsReported(t *testing.T) {
	s := Spec{Root: "testdata/clean", Allow: map[string]string{"assign.go": "代入がある (fixture)", "errtype.go": "Cmd を作らないのに載せた"}}
	_, problems := problemsOf(t, s)
	wantProblem(t, problems, "errtype.go: Allow に載っているが Cmd を作っていない")
}

func TestAllowWithoutReasonIsReported(t *testing.T) {
	s := Spec{Root: "testdata/clean", Allow: map[string]string{"assign.go": " "}}
	_, problems := problemsOf(t, s)
	wantProblem(t, problems, "assign.go: Allow に理由が書かれていない")
}

func TestNoOsExecReportsEveryImport(t *testing.T) {
	_, problems := problemsOf(t, Spec{Root: "testdata/noexec", NoOsExec: true})
	if len(problems) != 2 {
		t.Fatalf("指摘が 2 件でない: %v", problems)
	}
	wantProblem(t, problems, "imports.go: os/exec を import している")
	wantProblem(t, problems, "alias.go: os/exec を import している")
}

// 🚨 **検査できなかったものを緑にしない** (fail-closed)。
func TestUncheckableIsFatal(t *testing.T) {
	for name, s := range map[string]Spec{
		"Root が空":                  {},
		"Root が無い":                 {Root: "testdata/nosuchdir"},
		"非テストの .go が 0 件":          {Root: t.TempDir()},
		"パースできないファイル":              {Root: "testdata/broken"},
		"NoOsExec と Allow の同居":     {Root: "testdata/noexec", NoOsExec: true, Allow: map[string]string{"x.go": "理由"}},
		"NoOsExec と EnforcedFloor": {Root: "testdata/noexec", NoOsExec: true, EnforcedFloor: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, fatal := check(s); fatal == "" {
				t.Fatal("fatal にならない")
			}
		})
	}
}

// TestScanLinesCanary は、既知の入力に対して既知の答えが出ることを固定する。本走査と**同じ** scanLines を通す
// (式をコピーして canary を別に書くと、canary は「コピーした式」を検査するだけで本走査の破損を検出しない)。
func TestScanLinesCanary(t *testing.T) {
	cases := []struct {
		name         string
		lines        []string
		wantOffender bool
		wantSeen     int
		wantEnforced int
	}{
		{
			name:         "CommandContext で代入が無い",
			lines:        []string{`c := exec.CommandContext(ctx, "gh")`, `return c.Output()`},
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
		{
			name:         "CommandContext で直後に代入がある",
			lines:        []string{`c := exec.CommandContext(ctx, "gh")`, `c.WaitDelay = subproc.WaitDelay`},
			wantOffender: false, wantSeen: 1, wantEnforced: 1,
		},
		{
			// 🚨 `exec.Command(` は `exec.CommandContext(` を含まないので、片方だけのパターンにすると、
			// このケースかひとつ上のケースのどちらかが必ず素通りする。
			name:         "ctx 無しの exec.Command で代入が無い",
			lines:        []string{`c := exec.Command("gh")`, `return c.Output()`},
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
		{
			name:         "ctx 無しの exec.Command で直後に代入がある",
			lines:        []string{`c := exec.Command("gh")`, `c.WaitDelay = subproc.WaitDelay`},
			wantOffender: false, wantSeen: 1, wantEnforced: 1,
		},
		{
			// issue 303 の本体。語がコメントに出るだけでは通してはいけない
			name: "コメントで WaitDelay に言及しているだけ",
			lines: []string{
				`c := exec.CommandContext(ctx, "gh")`,
				`// 理由は subproc.WaitDelay の doc を見ること`,
				`return c.Output()`,
			},
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
		{
			// 語が**コード**に出るだけでも通さない (代入でない参照。`WaitDelay =` / `WaitDelay:` だけを代入と読む)
			name:         "コードで WaitDelay を参照しているが代入ではない",
			lines:        []string{`c := exec.Command("gh")`, `grace := 2 * subproc.WaitDelay`, `_ = grace`, `return c.Output()`},
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
		{
			name:         "subproc の起動口は数えない",
			lines:        []string{`c := subproc.CommandContext(ctx, "gh")`, `d := subproc.Command("gh")`, `return c.Output()`},
			wantOffender: false, wantSeen: 0, wantEnforced: 0,
		},
		{
			name:         "no-waitdelay の注記で免除されている",
			lines:        []string{`return exec.CommandContext(ctx, "x").Run() // subproc: no-waitdelay — 理由`},
			wantOffender: false, wantSeen: 1, wantEnforced: 0,
		},
		{
			name:         "waitdelay-in の注記で免除されている",
			lines:        []string{`return runGitCmd(exec.Command("gh")) // subproc: waitdelay-in runGitCmd`},
			wantOffender: false, wantSeen: 1, wantEnforced: 0,
		},
		{
			// コメント内の呼び出しは seen を水増ししてはいけない (floor の低下を隠す)
			name:         "コメントの中で exec.Command に言及しているだけ",
			lines:        []string{`// ここは exec.Command("gh") を使わないこと`, `return nil`},
			wantOffender: false, wantSeen: 0, wantEnforced: 0,
		},
		{
			name: "代入が lookahead の外にある",
			lines: append(append(
				[]string{`c := exec.CommandContext(ctx, "gh")`},
				make([]string, lookahead+1)...),
				`c.WaitDelay = subproc.WaitDelay`),
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			offenders, seen, enforced := scanLines("fixture.go", tc.lines)
			if got := len(offenders) > 0; got != tc.wantOffender {
				t.Fatalf("offender 判定 = %v, want %v (offenders=%v)", got, tc.wantOffender, offenders)
			}
			if seen != tc.wantSeen {
				t.Fatalf("seen = %d, want %d", seen, tc.wantSeen)
			}
			if enforced != tc.wantEnforced {
				t.Fatalf("enforced = %d, want %d", enforced, tc.wantEnforced)
			}
		})
	}
}

// TestImportFactCanary は段 B の import の読みを固定する (書式に左右されない AST で読む)。
func TestImportFactCanary(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		found bool
		alias string
	}{
		{"グループ import の素の os/exec", "package x\nimport (\n\t\"fmt\"\n\t\"os/exec\"\n)\n", true, ""},
		{"単独 import の素の os/exec", "package x\nimport \"os/exec\"\n", true, ""},
		{"グループ import の別名", "package x\nimport (\n\txe \"os/exec\"\n)\n", true, "xe"},
		{"単独 import の別名", "package x\nimport xe \"os/exec\"\n", true, "xe"},
		{"blank import", "package x\nimport _ \"os/exec\"\n", true, "_"},
		{"dot import", "package x\nimport . \"os/exec\"\n", true, "."},
		{"コメント内の os/exec は import ではない", "package x\n// import xe \"os/exec\" は禁止\n", false, ""},
		{"似た path は違う", "package x\nimport \"golang.org/x/sys/execabs\"\n", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := osExecImport("canary.go", []byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if f.found != tc.found || f.alias != tc.alias {
				t.Fatalf("got %+v, want found=%v alias=%q", f, tc.found, tc.alias)
			}
		})
	}
}

// 走査と go list の突き合わせ: go list が見る分は数まで一致、go list が見ないディレクトリ (gorules 等) の走査は許す。
func TestMismatchCanary(t *testing.T) {
	want := map[string]int{".": 3, "sub": 1}
	if msg := mismatch(map[string]int{".": 3, "sub": 1, "gorules": 1}, want); msg != "" {
		t.Fatalf("一致しているのに食い違いが出た: %s", msg)
	}
	for name, got := range map[string]map[string]int{
		"根の数が少ない":        {".": 2, "sub": 1},
		"ディレクトリを飛ばした":    {".": 3},
		"走査が 0 件":        {},
		"数が多い (重複して数えた)": {".": 3, "sub": 2},
	} {
		if mismatch(got, want) == "" {
			t.Fatalf("%s: 食い違いを見逃した (got=%v want=%v)", name, got, want)
		}
	}
	// 本物の go list: この package は 1 ディレクトリ・非テスト 1 ファイル
	counts, err := goListCounts(Spec{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if counts["."] != 1 || len(counts) != 1 {
		t.Fatalf("go list の数が期待と違う: %v", counts)
	}
}

// Run の go list との突き合わせを本物で通す (この package 自身は os/exec を import しないので厳しい設定で緑)。
func TestRunOnSelf(t *testing.T) {
	Run(t, Spec{Root: ".", NoOsExec: true})
}
