package doctorview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"glogx/internal/doctorview/doctortest"
	"tmpclean"
)

// openTmpTab は一時領域のタブを開いた DoctorView (件数は counts、repo の tmp/ は無し)。
func openTmpTab(t *testing.T, counts tmpclean.Counts, err error) (*DoctorView, string) {
	t.Helper()
	v := testView(t)
	v.deps.TmpCount = func(context.Context) (tmpclean.Counts, error) { return counts, err }
	var toast string
	doctortest.RunCmds(t, v.open(), func(msg tea.Msg) (tea.Cmd, bool) {
		next, tst, ok := v.Receive(msg)
		if tst != "" {
			toast = tst
		}
		return next, ok
	})
	v.tab = TabTmp
	return v, toast
}

func withSessionTotal(n int) tmpclean.Counts {
	c := doctortest.FakeTmpCounts
	c.Session.Total = n
	return c
}

func TestTmpCount(t *testing.T) {
	cases := map[int]string{0: "0件", 41: "41件", 7117: "7,117件", 10_000: "1万件", 16103: "1.6万件", 100_000: "10万件", 1_234_567: "123.5万件"}
	for n, want := range cases {
		if got := tmpCount(n); got != want {
			t.Errorf("tmpCount(%d) = %q, want %q", n, got, want)
		}
	}
}

// 起動時に消える場所の合計で印を決める。タブ行は印だけ、見出しは件数と語を出す (期待はリテラルで書く)。
func TestTmpThresholds(t *testing.T) {
	cases := []struct {
		session int
		tab     string
		header  string
		toast   bool
	}{
		{16103, "✅", "✅ ok", false},
		{100_000, "🚨", "🚨 注意", false},
		{300_000, "⛔", "⛔ 多い", true},
	}
	for _, c := range cases {
		v, toast := openTmpTab(t, withSessionTotal(c.session), nil)
		if got := v.tmpTabSummary(testOpts(20)); got != c.tab {
			t.Errorf("session=%d: タブ行の印 %q, want %q", c.session, got, c.tab)
		}
		if out := testText(v, 30); !strings.Contains(out, c.header) {
			t.Errorf("session=%d: 見出しに %q が無い:\n%s", c.session, c.header, out)
		}
		if (toast != "") != c.toast {
			t.Errorf("session=%d: toast=%q (出すべき=%v)", c.session, toast, c.toast)
		}
	}
}

// 数えられなければ判定不能と出し、件数 0 (= ✅) に丸めない。
func TestTmpCountErrorIsUndecided(t *testing.T) {
	v, toast := openTmpTab(t, tmpclean.Counts{}, errors.New("scratchpad の根を使えない"))
	if got := v.tmpTabSummary(testOpts(20)); got != "❓" {
		t.Errorf("タブ行の印 %q, want ❓", got)
	}
	out := testText(v, 30)
	if !strings.Contains(out, "❓ 判定不能") || !strings.Contains(out, "scratchpad の根を使えない") || strings.Contains(out, "✅ ok") {
		t.Errorf("判定不能が出ていない:\n%s", out)
	}
	if toast != "" {
		t.Errorf("判定不能で toast を出した: %q", toast)
	}
}

// 1 領域 1 行 + 最多の 1 行。Enter で内訳を開くと「最多」を消して内訳を出す。$TMPDIR の接頭辞には * を付ける。
func TestTmpSectionRowsAndExpand(t *testing.T) {
	v, _ := openTmpTab(t, doctortest.FakeTmpCounts, nil)
	out := testText(v, 30)
	for _, want := range []string{"Claude Code の作業場所", "1.6万件", "最多: -Users-me-dotfiles 1.4万件", "$TMPDIR", "最多: 空の dir 1,182件 / tmp.* 489件", "/private/tmp のその他", "(消さない)"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が無い:\n%s", want, out)
		}
	}
	_ = v.Lines(testOpts(30))
	if v.rows[v.cur.index].key != tmpSessionRowKey {
		t.Fatalf("前提: カーソルが作業場所の行に無い: %q", v.rows[v.cur.index].key)
	}
	v.HandleKey("enter", 30)
	out = testText(v, 30)
	if strings.Contains(out, "最多: -Users-me-dotfiles") || !strings.Contains(out, "-Users-me-src-app 1,339件") {
		t.Errorf("開いても内訳に変わらない:\n%s", out)
	}
}

// Y は片付けを頼むプロンプトを**タブの全体で 1 つ**コピーする: どの行で押しても同じで、全領域の件数と多いものが載る。
// skill を名指しして承認を求める。案内の語も変える。
func TestTmpYCopiesPrompt(t *testing.T) {
	v, _ := openTmpTab(t, doctortest.FakeTmpCounts, nil)
	_ = v.Lines(testOpts(30))
	if v.rows[v.cur.index].key != tmpSessionRowKey {
		t.Fatalf("前提: カーソルが作業場所の行に無い: %q", v.rows[v.cur.index].key)
	}
	if act := v.HandleKey("Y", 30); act != DoctorCopyText {
		t.Fatalf("Y が解説のコピーにならない: %v", act)
	}
	fromSession := v.CopyPayload()
	for _, want := range []string{"/cleanup-tmp-dir", "合計 2.3万件", "Claude Code の作業場所", "-Users-me-dotfiles 1.4万件", "$TMPDIR の直下: 7,117件", "空の dir 1,182件", "tmp.* 489件", "/private/tmp のその他: 41件", "確認して"} {
		if !strings.Contains(fromSession, want) {
			t.Errorf("プロンプトに %q が無い:\n%s", want, fromSession)
		}
	}
	v.HandleKey("j", 30) // → $TMPDIR の行
	_ = v.Lines(testOpts(30))
	if v.rows[v.cur.index].key != tmpTmpdirRowKey {
		t.Fatalf("前提: カーソルが $TMPDIR の行に無い: %q", v.rows[v.cur.index].key)
	}
	v.HandleKey("Y", 30)
	if got := v.CopyPayload(); got != fromSession {
		t.Errorf("行によってプロンプトが違う:\n作業場所の行: %s\n$TMPDIR の行: %s", fromSession, got)
	}
	if !strings.Contains(v.Hint(200), "Y: 片付けを頼むプロンプトをコピー") {
		t.Errorf("案内の語が変わっていない: %q", v.Hint(200))
	}
	v.tab = TabSSD
	if strings.Contains(v.Hint(200), "片付けを頼む") {
		t.Errorf("他のタブまで案内が変わった: %q", v.Hint(200))
	}
}

// ファイル名由来の文字列 (project 名・接頭辞) は表示とコピーの前に無害化する。
func TestTmpSanitizesNames(t *testing.T) {
	c := doctortest.FakeTmpCounts
	c.Session.Projects = []tmpclean.Part{{Name: "-evil\x1b]0;x\x07name", Count: 9}}
	td := *c.Tmpdir
	td.Prefixes = []tmpclean.Part{{Name: "tmp.\x1b]0;y\x07", Count: 9}}
	c.Tmpdir = &td
	v, _ := openTmpTab(t, c, nil)
	_ = v.Lines(testOpts(30))
	for _, r := range v.rows {
		for _, s := range []string{r.text, r.copyText} {
			if strings.ContainsAny(s, "\x07") || strings.Contains(s, "\x1b]") {
				t.Errorf("制御文字が残っている: %q", s)
			}
		}
	}
}

// 数え終わるまでは走査中 (スピナーの根拠)。ほかのタブが全部届いていても、一時領域が届くまでは走査中。
func TestTmpScanningUntilReceived(t *testing.T) {
	v, _ := openTmpTab(t, doctortest.FakeTmpCounts, nil)
	if v.Scanning() {
		t.Fatal("前提: 全部届いた後は走査中でない")
	}
	v.tmp = nil
	if !v.Scanning() {
		t.Error("一時領域が数えている途中なのに走査中でない")
	}
}

// repo の tmp/ は cwd から親へたどった最初の .git (dir でも worktree のファイルでも) の隣。repo の外・tmp/ が無ければ ""。
func TestRepoTmpDir(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	for _, d := range []string{filepath.Join(repo, ".git"), filepath.Join(repo, "tmp"), filepath.Join(repo, "a", "b"), filepath.Join(wt, "tmp"), filepath.Join(root, "outside")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		filepath.Join(repo, "a", "b"):  filepath.Join(repo, "tmp"),
		wt:                             filepath.Join(wt, "tmp"),
		filepath.Join(root, "outside"): "",
	}
	for cwd, want := range cases {
		if got := repoTmpDir(cwd); got != want {
			t.Errorf("repoTmpDir(%q) = %q, want %q", cwd, got, want)
		}
	}
}

// エラー文と repo のパスも表示とコピーの前に無害化する。
func TestTmpSanitizesErrorAndRepoPath(t *testing.T) {
	v, _ := openTmpTab(t, tmpclean.Counts{}, errors.New("読めない: \x1b]0;z\x07"))
	if out := testText(v, 30); strings.Contains(out, "\x07") || strings.Contains(out, "\x1b]") {
		t.Errorf("エラー文に制御文字が残っている: %q", out)
	}
	dir := filepath.Join(t.TempDir(), "re\x07po")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	w := testView(t)
	w.deps.RepoTmpDir = func() string { return dir }
	runCmds(t, w, w.open())
	w.tab = TabTmp
	_ = w.Lines(testOpts(30))
	for _, r := range w.rows {
		for _, s := range []string{r.text, r.copyText, r.copyPath} {
			if strings.Contains(s, "\x07") {
				t.Errorf("repo のパスに制御文字が残っている: %q", s)
			}
		}
	}
}

// テストが TmpCount を差し替えずに doctor を開いたら、本物の /private/tmp を数えずに判定不能を返す。
func TestTmpUnstubbedDoesNotWalkRealTmp(t *testing.T) {
	v := &DoctorView{shown: true}
	msg, ok := v.tmpCmd(context.Background(), v.gen)().(doctorTmpMsg)
	if !ok {
		t.Fatal("tmpCmd の Msg の型が違う")
	}
	if !strings.Contains(msg.rep.err, "差し替えずに") || msg.rep.counts.Session.Total != 0 || msg.rep.repoTmp != "" {
		t.Fatalf("本物を数えた・判定不能になっていない: %+v", msg.rep)
	}
}
