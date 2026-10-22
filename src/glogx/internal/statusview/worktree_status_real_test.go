package statusview

import (
	"glogx/internal/app"
	"glogx/internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func realRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(testenv.RepoTmpDir(t), "glogx-status-real") //nolint:usetesting // 使い捨て repo は ./tmp 規約 (このファイル冒頭の doc)。t.TempDir はシステム TMPDIR に置くため使わない
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for _, args := range [][]string{
		{"init", "-q", "-b", "master"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		testenv.GitInRepo(t, dir, args...)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// chdirTo は cwd を dir へ移す (t.Chdir はテスト終了時に戻す)。
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

// realStatusView は実 git を読む viewer (stub なし)。
func realStatusView(t *testing.T) *StatusView {
	t.Helper()
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	st, err := loadWorktreeStatus()
	if err != nil {
		t.Fatalf("loadWorktreeStatus: %v", err)
	}
	v.Receive(StatusLoadMsg{st: st, gen: v.gen})
	return &v
}

func cursorTo(t *testing.T, v *StatusView, path string) {
	t.Helper()
	for i, r := range v.rows {
		if r.path == path {
			v.cursor = i
			return
		}
	}
	t.Fatalf("%q が一覧に無い: %+v", path, v.rows)
}

// glob 文字を含むファイル名を捨てたとき、名前が「似ているだけ」の隣のファイルを巻き込まないこと
// (literal pathspec が無いと a?b.txt のパターンが axb.txt に当たる)。
func TestRealDiscardDoesNotTakeGlobNeighbors(t *testing.T) {
	root := realRepo(t)
	chdirTo(t, root)
	write(t, root, "a?b.txt", "target\n")
	write(t, root, "axb.txt", "neighbor\n")
	v := realStatusView(t)
	cursorTo(t, v, "a?b.txt")
	v.HandleKey("X", StatusViewport{page: 20})
	if !v.discarding {
		t.Fatal("X で確認が開いていない")
	}
	v.HandleKey("y", StatusViewport{page: 20})
	if exists(root, "a?b.txt") {
		t.Error("対象が消えていない")
	}
	if !exists(root, "axb.txt") {
		t.Fatal("glob として解釈され、隣のファイルまで消えた (literal pathspec が効いていない)")
	}
}

// サブディレクトリを cwd にしても、stage / unstage / diff が正しいファイルに当たること。
func TestRealOpsFromSubdirectory(t *testing.T) {
	root := realRepo(t)
	write(t, root, "src/deep/a.go", "package a\n")
	testenv.GitInRepo(t, root, "add", "-A")
	testenv.GitInRepo(t, root, "commit", "-q", "-m", "add a")
	write(t, root, "src/deep/a.go", "package a\n\nvar X = 1\n")
	chdirTo(t, filepath.Join(root, "src", "deep")) // repo root ではない場所から操作する

	v := realStatusView(t)
	cursorTo(t, v, "src/deep/a.go")
	row, _ := v.current()

	// diff が取れること (cwd 相対だと空になる)
	msg, _ := v.fetchDiff(row, false)().(StatusPreviewMsg)
	if msg.err != nil {
		t.Fatalf("diff: %v", msg.err)
	}
	v.ReceivePreview(msg)
	if got, _ := v.preview.Get(previewKey(row)); len(got) == 0 || strings.Contains(got[0], "差分はありません") {
		t.Fatalf("サブディレクトリから diff が取れていない: %v", got)
	}

	// stage できること
	v.HandleKey(" ", StatusViewport{page: 20})
	if notice, ok := testenv.NoticeOKOf(v.TakeNotice()); notice != "" && !ok {
		t.Fatalf("stage が失敗した: %s", notice)
	}
	st, err := loadWorktreeStatus()
	if err != nil {
		t.Fatal(err)
	}
	if _, staged := st.find(sectionStaged, "src/deep/a.go"); !staged {
		t.Fatalf("stage されていない: %+v", st.rows)
	}

	// unstage で戻せること
	v.Receive(StatusLoadMsg{st: st, gen: v.gen})
	cursorTo(t, v, "src/deep/a.go")
	v.HandleKey(" ", StatusViewport{page: 20})
	st, err = loadWorktreeStatus()
	if err != nil {
		t.Fatal(err)
	}
	if _, staged := st.find(sectionStaged, "src/deep/a.go"); staged {
		t.Fatalf("unstage できていない: %+v", st.rows)
	}
}

// untracked ディレクトリを捨てるとき、そのディレクトリだけを消すこと。
func TestRealDiscardUntrackedDirKeepsSiblings(t *testing.T) {
	root := realRepo(t)
	chdirTo(t, root)
	write(t, root, "junk/x.txt", "junk\n")
	write(t, root, "keep.txt", "keep\n")
	v := realStatusView(t)
	cursorTo(t, v, "junk/")
	v.HandleKey("X", StatusViewport{page: 20})
	v.HandleKey("y", StatusViewport{page: 20})
	if exists(root, "junk/x.txt") {
		t.Error("ディレクトリが消えていない")
	}
	if !exists(root, "keep.txt") {
		t.Fatal("無関係の untracked まで消えた")
	}
}

// 確認を出した後に別プロセスがファイルを変えたら捨てない (spec 4 節の不変条件 1 を実 git で)。
func TestRealDiscardAbortsWhenFileChangedDuringConfirm(t *testing.T) {
	root := realRepo(t)
	write(t, root, "a.txt", "one\n")
	testenv.GitInRepo(t, root, "add", "-A")
	testenv.GitInRepo(t, root, "commit", "-q", "-m", "a")
	chdirTo(t, root)
	write(t, root, "a.txt", "two\n")
	v := realStatusView(t)
	cursorTo(t, v, "a.txt")
	v.HandleKey("X", StatusViewport{page: 20})
	// 確認中に別プロセスが stage した (XY が " M" → "M " へ変わる)
	testenv.GitInRepo(t, root, "add", "a.txt")
	v.HandleKey("y", StatusViewport{page: 20})
	body, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "two\n" {
		t.Fatalf("確認中に状態が変わったのに捨ててしまった (中身 = %q)", body)
	}
	if notice, ok := testenv.NoticeOKOf(v.TakeNotice()); ok || !strings.Contains(notice, "変わった") {
		t.Errorf("notice = %q (ok=%v), want 中止の警告", notice, ok)
	}
}

// 空の repo (変更ゼロ) でも落ちず、stage 対象が無いことを伝えること。
func TestRealCleanRepoIsSafe(t *testing.T) {
	root := realRepo(t)
	chdirTo(t, root)
	v := realStatusView(t)
	if !v.st.clean() {
		t.Fatalf("新品の repo が clean でない: %+v", v.st.rows)
	}
	v.HandleKey("a", StatusViewport{page: 20})
	if notice, kind := v.TakeNotice(); kind != app.NoticeRefused || !strings.Contains(notice, "ありません") {
		t.Errorf("notice = %q (kind=%v), want 断りの「stage するものがありません」", notice, kind)
	}
	// 行が無い状態で各キーを叩いても panic しないこと
	for _, key := range []string{" ", "X", "d", "j", "k", "tab", "g", "G", "r"} {
		v.HandleKey(key, StatusViewport{page: 20})
	}
	if lines := v.Lines(StatusRenderOpts{Width: 120, Page: 10}); len(lines) != 10 {
		t.Fatalf("行数 = %d, want 10", len(lines))
	}
}

// 存在しないパスへの操作が「成功」に見えないこと (沈黙 = 成功にしない)。
func TestRealFailedOpIsReportedAsFailure(t *testing.T) {
	root := realRepo(t)
	chdirTo(t, root)
	write(t, root, "gone.txt", "x\n")
	v := realStatusView(t)
	cursorTo(t, v, "gone.txt")
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	v.HandleKey(" ", StatusViewport{page: 20}) // 消えたファイルを stage しようとする
	notice, ok := testenv.NoticeOKOf(v.TakeNotice())
	if notice == "" {
		t.Fatal("失敗が黙って捨てられた (notice が空)")
	}
	if ok {
		t.Fatalf("失敗を成功として報告した: %q", notice)
	}
}

// gitIn は root で git を走らせて stdout を返す (失敗はテストを落とす)。
func gitIn(t *testing.T, root, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// MM の行の X は index の中身を作業ツリーへ書き戻す。確認中に別プロセスが index だけを差し替えたら (XY も作業ツリーも
// 同じまま)、見せていない中身を書くことになるので捨てない。
func TestRealDiscardAbortsWhenOnlyIndexChangedDuringConfirm(t *testing.T) {
	root := realRepo(t)
	write(t, root, "a.txt", "one\n")
	gitIn(t, root, "", "add", "a.txt")
	gitIn(t, root, "", "commit", "-q", "-m", "a")
	write(t, root, "a.txt", "two\n")
	gitIn(t, root, "", "add", "a.txt")
	write(t, root, "a.txt", "three\n") // MM: index = two, 作業ツリー = three
	chdirTo(t, root)
	v := realStatusView(t)
	v.cursor = -1
	for i, r := range v.rows {
		if r.section == sectionUnstaged && r.path == "a.txt" {
			v.cursor = i
		}
	}
	if v.cursor < 0 {
		t.Fatalf("前提: a.txt の unstaged 行が無い: %+v", v.rows)
	}
	v.HandleKey("X", StatusViewport{page: 20})
	if !v.discarding {
		t.Fatal("前提: X で確認が開いていない")
	}
	blob := gitIn(t, root, "unseen\n", "hash-object", "-w", "--stdin")
	gitIn(t, root, "", "update-index", "--cacheinfo", "100644,"+blob+",a.txt") // index だけを差し替える (XY は MM のまま)
	v.HandleKey("y", StatusViewport{page: 20})
	body, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "three\n" {
		t.Fatalf("確認中に index が変わったのに書き戻した (中身 = %q)", body)
	}
}

// 未追跡のディレクトリの指紋は git clean が消す集合で見る: 確認中に ignore されたファイルが増えても (ディレクトリの mtime は
// 動く) 捨てる。消える集合にファイルが増えたら捨てない。
func TestRealDiscardUntrackedDirPrintFollowsCleanSet(t *testing.T) {
	root := realRepo(t)
	write(t, root, ".gitignore", "*.log\n")
	gitIn(t, root, "", "add", ".gitignore")
	gitIn(t, root, "", "commit", "-q", "-m", "ignore")
	chdirTo(t, root)

	write(t, root, "junk/x.txt", "junk\n")
	v := realStatusView(t)
	cursorTo(t, v, "junk/")
	v.HandleKey("X", StatusViewport{page: 20})
	write(t, root, "junk/run.log", "ignored\n") // clean -fd は消さない
	v.HandleKey("y", StatusViewport{page: 20})
	if exists(root, "junk/x.txt") {
		t.Fatalf("ignore されたファイルが増えただけで中止した: %v", v.notice)
	}

	write(t, root, "junk2/x.txt", "junk\n")
	v = realStatusView(t)
	cursorTo(t, v, "junk2/")
	v.HandleKey("X", StatusViewport{page: 20})
	write(t, root, "junk2/sub/y.txt", "見せていない\n") // clean -fd が消す集合に増えた
	v.HandleKey("y", StatusViewport{page: 20})
	if !exists(root, "junk2/sub/y.txt") {
		t.Fatal("確認の後に増えたファイルまで捨てた")
	}

	// サブディレクトリから起動しても、一覧の各ファイルの中身の変化を見る (ls-files の出力は既定で cwd 相対)
	if err := os.MkdirAll(filepath.Join(root, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdirTo(t, filepath.Join(root, "work"))
	write(t, root, "junk3/x.txt", "junk\n")
	v = realStatusView(t)
	cursorTo(t, v, "junk3/")
	v.HandleKey("X", StatusViewport{page: 20})
	write(t, root, "junk3/x.txt", "見せていない中身\n")
	v.HandleKey("y", StatusViewport{page: 20})
	if !exists(root, "junk3/x.txt") {
		t.Fatal("サブディレクトリから起動したとき、確認の後に書き換わったファイルを捨てた")
	}
}

// staged の rename の後に rename 先を編集した行 (RM) の X は、作業ツリーの編集だけを捨てる (rename 元は index に無いので
// restore に渡すと pathspec の不一致で全体が止まる)。
func TestRealDiscardRenamedThenEditedRow(t *testing.T) {
	root := realRepo(t)
	write(t, root, "old.txt", "one\n")
	gitIn(t, root, "", "add", "old.txt")
	gitIn(t, root, "", "commit", "-q", "-m", "old")
	gitIn(t, root, "", "mv", "old.txt", "new.txt")
	write(t, root, "new.txt", "one\nedited\n")
	chdirTo(t, root)
	v := realStatusView(t)
	v.cursor = -1
	for i, r := range v.rows {
		if r.section == sectionUnstaged && r.path == "new.txt" {
			v.cursor = i
		}
	}
	if v.cursor < 0 {
		t.Fatalf("前提: new.txt の unstaged 行が無い: %+v", v.rows)
	}
	v.HandleKey("X", StatusViewport{page: 20})
	v.HandleKey("y", StatusViewport{page: 20})
	body, err := os.ReadFile(filepath.Join(root, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "one\n" {
		t.Fatalf("作業ツリーの編集を捨てられていない (中身 = %q, notice = %q)", body, v.notice)
	}
	if got := gitIn(t, root, "", "status", "--porcelain"); got != "R  old.txt -> new.txt" {
		t.Errorf("staged の rename まで変わった: %q", got)
	}
}
