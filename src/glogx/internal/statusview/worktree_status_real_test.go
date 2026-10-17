package statusview

import (
	"glogx/internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func realRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(repoTmpDir(t), "glogx-status-real") //nolint:usetesting // 使い捨て repo は ./tmp 規約 (このファイル冒頭の doc)。t.TempDir はシステム TMPDIR に置くため使わない
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
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
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
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "add a")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
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
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "a"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	chdirTo(t, root)
	write(t, root, "a.txt", "two\n")
	v := realStatusView(t)
	cursorTo(t, v, "a.txt")
	v.HandleKey("X", StatusViewport{page: 20})
	// 確認中に別プロセスが stage した (XY が " M" → "M " へ変わる)
	cmd := exec.Command("git", "add", "a.txt")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
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
	if notice, ok := testenv.NoticeOKOf(v.TakeNotice()); !ok || !strings.Contains(notice, "ありません") {
		t.Errorf("notice = %q (ok=%v), want 「stage するものがありません」", notice, ok)
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
