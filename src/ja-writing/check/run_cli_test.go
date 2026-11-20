package check

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"ja-writing/cmdenv"
)

// start の登録 (状態フォルダの armed/) がある run は、本家が登録を探しにいく場面だけ内部エラーで止める (登録は start と一緒に写す)。
// 登録を探さない場面 (--new-run・--no-record) は armed/ があっても本家と同じく run を最後まで動かす。
func TestRunStopsWhereUpstreamWouldReadArmedEntries(t *testing.T) {
	dir := fixtureRoot(t)
	for _, name := range []string{".git", ".yomiyasu", ".yomiyasu/armed"} {
		if err := os.Mkdir(dir+"/"+name, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"src.md": vSourceText, "final.md": vFinalText} {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt := childRuntime(t)
	call := func(args ...string) (int, string) {
		var stdout, stderr bytes.Buffer
		rc := MainWith(context.Background(), cmdenv.Env{Args: append([]string{"run"}, args...), Stdout: &stdout, Stderr: &stderr, Cwd: dir, Getenv: os.Getenv}, rt)
		return rc, stderr.String()
	}
	for _, args := range [][]string{
		{"--source", "src.md", "--final", "final.md"},
		{"--source", "src.md", "--final", "src.md"},
	} {
		rc, stderr := call(args...)
		if rc != 3 || !strings.Contains(stderr, "内部エラー: start の登録（armed/）を使う run はまだ実装していません") {
			t.Errorf("%v: rc=%d stderr=%q", args, rc, stderr)
		}
	}
	for _, args := range [][]string{
		{"--source", "src.md", "--final", "final.md", "--new-run"},
		{"--source", "src.md", "--final", "final.md", "--no-record"},
	} {
		if rc, stderr := call(args...); rc != 0 {
			t.Errorf("%v: rc=%d stderr=%q", args, rc, stderr)
		}
	}
}
