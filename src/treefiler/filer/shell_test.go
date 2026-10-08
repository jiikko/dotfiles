package filer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellPromptBuildsExecWithHistory(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("!")
	if !m.OwnsKeys() {
		t.Fatal("! の入力中なのに OwnsKeys が false")
	}
	typeText(m, "echo hi")
	if m.HandleKey("enter") != Exec {
		t.Fatal("Enter で Exec を返さない")
	}
	r, ok := m.TakeExec()
	if !ok || r.Dir != filepath.Dir(m.cur.path()) || r.Argv[len(r.Argv)-1] != "echo hi" {
		t.Fatalf("頼むプロセス = %+v (ファイルなら親のフォルダで、最後の引数がコマンド行)", r)
	}
	if len(r.Env) != 1 || r.Env[0] != "f="+m.cur.path() {
		t.Fatalf("$f が選んだパスでない: %v", r.Env)
	}
	m.HandleKey("!")
	m.HandleKey("up")
	if m.prompt.line.String() != "echo hi" {
		t.Fatalf("↑ で履歴が戻らない: %q", m.prompt.line.String())
	}
	if m.HandleKey("esc"); m.OwnsKeys() {
		t.Fatal("Esc で入力が閉じない")
	}
	if _, ok := m.TakeExec(); ok {
		t.Fatal("取り消したのにプロセスを頼んだ")
	}
	if m.HandleKey("s") != Exec {
		t.Fatal("s で Exec を返さない")
	}
	if r, _ := m.TakeExec(); r.Argv[len(r.Argv)-1] != "-i" || r.Dir != filepath.Dir(m.cur.path()) {
		t.Fatalf("s のシェル = %+v", r)
	}
}

// 包むスクリプトは本当にコマンドを走らせ、3 秒未満で終わったら 1 文字待ってから戻る (待ちは stdin から読む)。
func TestWaitIfQuickRunsCommand(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("/bin/sh", "-c", waitIfQuick, "treefiler", "/bin/sh", "echo ran > out.txt")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("x") // 1 文字を与えると待ちが終わる
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("包むスクリプトが失敗: %v\n%s", err, out)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "out.txt")); err != nil || strings.TrimSpace(string(b)) != "ran" {
		t.Fatalf("コマンドが走っていない: %q %v", b, err)
	}
	if !strings.Contains(string(out), "[done]") {
		t.Fatalf("すぐ終わったのにキー待ちの案内が出ない: %q", out)
	}
}

// 貼り付けは入力欄にだけ入る。入力していないときは捨てる (キーとして走らない)。
func TestPasteGoesToInputOnly(t *testing.T) {
	m := newTest(t)
	cur := m.cur
	m.Paste("jjjq")
	if m.cur != cur {
		t.Fatal("入力していないときの貼り付けがキーとして走った")
	}
	m.HandleKey("/")
	m.Paste("file10")
	if m.search.line.String() != "file10" || m.cur.name != "file10.txt" {
		t.Fatalf("検索への貼り付け: %q → %s", m.search.line.String(), m.cur.name)
	}
	m.HandleKey("esc")
	m.HandleKey("!")
	m.Paste("echo a\nb\x1b[31m")
	if got := m.prompt.line.String(); got != "echo a b[31m" {
		t.Fatalf("コマンドへの貼り付け = %q (改行は空白、制御文字は落とす)", got)
	}
}
