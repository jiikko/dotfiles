package check

import (
	"context"
	"fmt"
	"os"
	"testing"

	"ja-writing/cmdenv"
	"ja-writing/diff"
	"ja-writing/lint"
)

// childEnvName は、テストのバイナリを run の子 (lint・diff) として起こし直すときの印。check の run は子に自分自身 (ja-writing) を起こすが、
// テストでは os.Executable がテストのバイナリになるので、印を見たら ja-writing の lint・diff の Main を走らせて終わる。
const childEnvName = "JA_WRITING_CHECK_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnvName) == "1" {
		os.Exit(childMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func childMain(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "子の subcommand がありません")
		return 2
	}
	env, err := cmdenv.FromProcess(args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch args[0] {
	case "lint":
		return lint.Main(context.Background(), env)
	case "diff":
		return diff.Main(context.Background(), env)
	}
	fmt.Fprintln(os.Stderr, "知らない子の subcommand です: "+args[0])
	return 2
}

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
	rt.ChildEnv = func() []string { return append(os.Environ(), childEnvName+"=1", "JA_REAL="+exe) }
	rt.TempDir = func() string { return t.TempDir() }
	return rt
}
