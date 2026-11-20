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
		rc := MainWith(context.Background(), processEnv(append([]string{"run"}, args...), dir, &stdout, &stderr), rt)
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

// run は環境変数を Env から読み (状態フォルダの YOMIYASU_STATE_DIR)、子の環境も Env の環境変数から組む (プロセスの環境を混ぜない。hook が MainWith に
// プロセスと違う Env を渡しても食い違わない)。子は Env の環境変数にだけある印でテストのバイナリを ja-writing として走らせる。
func TestRunReadsEnvironmentFromEnv(t *testing.T) {
	dir := fixtureRoot(t)
	if err := os.Mkdir(dir+"/.git", 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"src.md": vSourceText, "final.md": vFinalText} {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	exe := testExecutable(t)
	rt := childRuntime(t)
	rt.ChildEnv = DefaultRuntime().ChildEnv
	// 先に置いた値が勝つ (Lookup は同じ名前の先のものを読む)。プロセスの環境に YOMIYASU_STATE_DIR があっても Env の値を使う
	environ := append([]string{"YOMIYASU_STATE_DIR=" + dir + "/from-env"}, os.Environ()...)
	environ = append(environ, childEnvName+"="+childReal, "JA_REAL="+exe)
	var stdout, stderr bytes.Buffer
	env := cmdenv.Env{Args: []string{"run", "--source", "src.md", "--final", "final.md"}, Stdout: &stdout, Stderr: &stderr, Cwd: dir, Environ: environ}
	if rc := MainWith(context.Background(), env, rt); rc != 0 {
		t.Fatalf("rc=%d stdout=%q stderr=%q", rc, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(dir + "/from-env/state.json"); err != nil {
		t.Fatalf("Env の YOMIYASU_STATE_DIR に書いていない: %v", err)
	}
	if _, err := os.Stat(dir + "/.yomiyasu"); err == nil {
		t.Fatal("プロセスの環境で状態フォルダを決めた")
	}
}

// Env の作業フォルダがプロセスの作業ディレクトリと違えば、どちらかを黙って使わずに内部エラーで止める。
func TestRunRefusesEnvCwdOtherThanProcess(t *testing.T) {
	dir := fixtureRoot(t)
	other := t.TempDir()
	for _, args := range [][]string{{"run", "--source", "src.md", "--final", "final.md"}, {"verify", "--final", "final.md"}} {
		var stdout, stderr bytes.Buffer
		rc := MainWith(context.Background(), processEnv(args, other, &stdout, &stderr), childRuntime(t))
		if rc != 3 || !strings.Contains(stderr.String(), "内部エラー: 作業フォルダ (Env.Cwd) "+other+" が、プロセスの作業フォルダと違います") {
			t.Fatalf("%v: rc=%d stdout=%q stderr=%q (cwd %s)", args, rc, stdout.String(), stderr.String(), dir)
		}
	}
}

// 一時フォルダの親は tempfile.gettempdir と同じく、候補 (TMPDIR・TEMP・TMP・/tmp…) を順に、試しのファイルを作って消せるかで選ぶ。
// 無いフォルダ・ファイル・書けないフォルダは飛ばす。空の値は候補にしない。
func TestGettempdirSkipsUnusableCandidates(t *testing.T) {
	base := t.TempDir()
	file := base + "/afile"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	readOnly := base + "/ro"
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	usable, usable2, usable3 := base+"/ok", base+"/ok2", base+"/ok3"
	for _, dir := range []string{usable, usable2, usable3} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	lookupOf := func(values map[string]string) cmdenv.Env {
		var environ []string
		for name, value := range values {
			environ = append(environ, name+"="+value)
		}
		return cmdenv.Env{Environ: environ}
	}
	for _, tc := range []struct {
		values map[string]string
		want   string
	}{
		{map[string]string{"TMPDIR": base + "/missing", "TEMP": file, "TMP": usable}, usable},
		{map[string]string{"TMPDIR": readOnly, "TEMP": "", "TMP": usable}, usable},
		{map[string]string{"TMPDIR": file}, "/tmp"},
		// 使える候補が 2 つ以上なら、TMPDIR・TEMP・TMP の順の先のもの
		{map[string]string{"TMPDIR": usable, "TEMP": usable2, "TMP": usable3}, usable},
		{map[string]string{"TEMP": usable2, "TMP": usable3}, usable2},
	} {
		got, err := gettempdir(lookupOf(tc.values))
		if err != nil || got != tc.want {
			t.Errorf("%v: %q %v (期待 %q)", tc.values, got, err, tc.want)
		}
	}
	for _, dir := range []string{usable, usable2, usable3} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("試しのファイルが残った: %v %v", entries, err)
		}
	}
}
