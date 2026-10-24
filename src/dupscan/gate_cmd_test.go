package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"subproc"
)

func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	full := append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	if out, err := subproc.GitCommand(context.Background(), full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// committedRepo は helperA だけを commit した repo を作る。
func committedRepo(t *testing.T) string {
	t.Helper()
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	return root
}

func runGateCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	rc := run(append([]string{"gate"}, args...), &out, &errb)
	return rc, out.String(), errb.String()
}

func TestGateNoChangeIsOneLine(t *testing.T) {
	root := committedRepo(t)
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 0 || strings.Count(out, "\n") != 1 || !strings.Contains(out, "新しい重複なし") {
		t.Errorf("rc=%d out=%q err=%q", rc, out, errs)
	}
}

func TestGateUncommittedAndUntrackedCopiesFail(t *testing.T) {
	root := committedRepo(t)
	writeFile(t, root, "Sources/B.swift", helperB) // untracked の新しいファイル
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "Sources/B.swift") || !strings.Contains(out, "同じ形: Sources/A.swift") {
		t.Errorf("untracked の写しで rc=1 と寄せる先を期待: rc=%d out=%q err=%q", rc, out, errs)
	}
	gitRun(t, root, "add", "Sources/B.swift") // 追跡したが未コミット
	if rc, out, _ := runGateCmd(t, "-base", "HEAD", root); rc != 1 {
		t.Errorf("未コミットの写しで rc=1 を期待: rc=%d out=%q", rc, out)
	}
}

func TestGateWithoutBaseIsUndecidable(t *testing.T) {
	root := committedRepo(t) // upstream も origin/HEAD も無い
	rc, _, errs := runGateCmd(t, root)
	if rc != 2 || !strings.Contains(errs, "判定不能") {
		t.Errorf("rc=%d (期待 2) err=%q", rc, errs)
	}
}

func TestGateHeadCommitDoesNotTouchRepo(t *testing.T) {
	root := committedRepo(t)
	writeFile(t, root, "Sources/B.swift", helperB)
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "copy")
	index := filepath.Join(root, ".git", "index")
	before, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	rc, out, errs := runGateCmd(t, "-base", "HEAD~1", "-head", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "Sources/B.swift") {
		t.Errorf("commit どうしの比較で写しを期待: rc=%d out=%q err=%q", rc, out, errs)
	}
	after, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("gate が調べる repo の .git/index を書き換えた")
	}
}
