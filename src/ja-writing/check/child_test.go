package check

import (
	"io"
	"os"
	"testing"

	"ja-writing/cmdenv"
)

// テストのバイナリを run の子 (lint・diff) として起こし直す。check の run は子に自分自身 (ja-writing) を起こすが、テストでは os.Executable が
// テストのバイナリになるので、印 (childEnvName) を見たら本番の main と同じ入口 (entry.Run) を通って子として走る (main_ext_test.go の TestMain)。
// 子の本体を関数で直接呼ばない: 本番の子は entry.Run でシグナルの受け方を決めてから走るので、それを飛ばすと子を止める処理の退行が見えない。
const (
	childEnvName = "JA_WRITING_CHECK_CHILD"
	// childReal は本番と同じ ja-writing の Main を走らせる子。
	childReal = "1"
	// childHang は止まらない子: entry.Run でシグナルの受け方を本番と同じに整えてから、JA_PIDFILE に pid を書いて眠る (ctx を見ない。lint・diff の Main と同じ)。
	childHang = "hang"
)

// testExecutable は子として起こすテストのバイナリ。
func testExecutable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

// childRuntime は子にテストのバイナリを起こす Runtime (ほかは本番と同じ)。
func childRuntime(t *testing.T) Runtime {
	exe := testExecutable(t)
	rt := DefaultRuntime()
	rt.Executable = func() (string, error) { return exe, nil }
	rt.ChildEnv = childEnv(exe)
	rt.TempDir = func(cmdenv.Env) (string, error) { return t.TempDir(), nil }
	return rt
}

// childEnv は子の環境変数 (run の Env の環境変数に、子として走る印と、子の代わりの shell script が渡す本物の実行ファイルを足す)。
func childEnv(exe string) func(cmdenv.Env) []string {
	return func(env cmdenv.Env) []string {
		return append(env.Environment(), childEnvName+"="+childReal, "JA_REAL="+exe)
	}
}

// isolatedEnv は本物の check を動かす Env (環境変数は isolatedEnviron)。
func isolatedEnv(t *testing.T, args []string, cwd string, stdout, stderr io.Writer) cmdenv.Env {
	t.Helper()
	return cmdenv.Env{Args: args, Stdout: stdout, Stderr: stderr, Cwd: cwd, Environ: isolatedEnviron(t)}
}

// isolatedEnviron は、テストで本物の check (と子) を動かすときの環境変数。プロセスの環境を継がない: 継ぐと、YOMIYASU_STATE_DIR の
// ある環境では本物の状態フォルダに書き (作業フォルダの状態フォルダを見る確かめが空振りする)、HOME と TMPDIR も本物を使う。
func isolatedEnviron(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"home", "tmp"} {
		if err := os.Mkdir(dir+"/"+name, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return []string{"PATH=/usr/bin:/bin", "HOME=" + dir + "/home", "TMPDIR=" + dir + "/tmp"}
}
