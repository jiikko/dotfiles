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

func TestGateRootAndSubdirReadSameScopeOnBothSides(t *testing.T) {
	// other/ に既にある組があり、sub/ だけを編集した: 起点に root と sub を渡しても、既にある組で赤にしない。
	root := repoWith(t, map[string]string{"other/X.swift": helperA, "other/Y.swift": strings.Replace(helperA, "struct A", "struct Y", 1), "sub/S.swift": "func s(_ x: Int) -> Int { x + 1 }\n"})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "sub/S.swift", "func s(_ x: Int) -> Int { x + 2 }\n")
	if rc, out, errs := runGateCmd(t, "-base", "HEAD", root, filepath.Join(root, "sub")); rc != 0 {
		t.Errorf("既にある組で赤になった: rc=%d out=%q err=%q", rc, out, errs)
	}
}

func TestGateReadsPathsWithNewline(t *testing.T) {
	root := committedRepo(t)
	writeFile(t, root, "Sources/odd\nname.swift", "func odd(_ x: Int) -> Int { x * 3 }\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "odd")
	if rc, out, errs := runGateCmd(t, "-base", "HEAD~1", "-head", "HEAD", root); rc != 0 {
		t.Errorf("パスに改行を含むファイルで判定不能になった: rc=%d out=%q err=%q", rc, out, errs)
	}
}

func TestGateNotesBaseIsHead(t *testing.T) {
	root := committedRepo(t)
	if _, out, _ := runGateCmd(t, "-base", "HEAD", root); !strings.Contains(out, "base が HEAD と同じ") {
		t.Errorf("base が HEAD と同じことを出さない: %q", out)
	}
}

func TestGateSameEditToBothCopiesPasses(t *testing.T) {
	// 既にある写し A・B に同じ修正を入れる (共通の写しを直す普通の場面) のは、重複の量が増えていないので緑。
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/A.swift", strings.Replace(helperA, `"conn"`, `"conn2"`, 1))
	writeFile(t, root, "Sources/B.swift", strings.Replace(helperB, `"conn"`, `"conn2"`, 1))
	if rc, out, errs := runGateCmd(t, "-base", "HEAD", root); rc != 0 {
		t.Errorf("既にある写しに同じ修正を入れて赤になった: rc=%d out=%q err=%q", rc, out, errs)
	}
}

// untracked から読んだファイルは CI の checkout に無いので、手元と CI の結果の差として別に出す。追跡している変更・commit を head にした比較では出さない。
func TestGateListsUntrackedFiles(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/U.swift": unrelated})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/U.swift", unrelated+"\n")
	writeFile(t, root, "Sources/B.swift", helperB)
	rc, out, errs := runGateCmd(t, "-v", "-base", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "・untracked から読んだファイル 1 (CI には無い。-v で一覧)") || !strings.Contains(out, "untracked: Sources/B.swift\n") || strings.Contains(out, "untracked: Sources/U.swift") {
		t.Errorf("untracked の B だけを一覧に: rc=%d out=%q err=%q", rc, out, errs)
	}
	_, out, _ = runGateCmd(t, "-format", "json", "-base", "HEAD", root)
	if !strings.Contains(out, `"untracked_files": [
    "Sources/B.swift"
  ]`) {
		t.Errorf("json の untracked_files: %q", out)
	}
	gitRun(t, root, "add", "Sources/B.swift")
	if _, out, _ := runGateCmd(t, "-format", "json", "-base", "HEAD", root); !strings.Contains(out, `"untracked_files": []`) {
		t.Errorf("追跡したら一覧から消える (空の配列): %q", out)
	}
}
