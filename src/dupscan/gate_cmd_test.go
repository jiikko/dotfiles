package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// --record は回ごとに 1 行の JSON を追記する (上書きしない)。渡さなければ何も書かない。
func TestGateRecordAppendsOneJSONLinePerRun(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	rec := filepath.Join(t.TempDir(), "gate.jsonl")
	if rc, _, errs := runGateCmd(t, "-record", rec, "-base", "HEAD", root); rc != 0 {
		t.Fatalf("1 回目: rc=%d err=%q", rc, errs)
	}
	writeFile(t, root, "Sources/B.swift", helperB)
	if rc, _, errs := runGateCmd(t, "-record", rec, "-base", "HEAD", root); rc != 1 {
		t.Fatalf("2 回目は写しで rc=1: rc=%d err=%q", rc, errs)
	}
	b, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	ls := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(ls) != 2 {
		t.Fatalf("2 回で 2 行を期待: %q", b)
	}
	for i, l := range ls {
		var r struct {
			Time       string
			Failed     bool
			Violations []json.RawMessage
		}
		if err := json.Unmarshal([]byte(l), &r); err != nil || r.Time == "" || r.Failed != (i == 1) || len(r.Violations) != i {
			t.Errorf("%d 行目: 時刻と、%d 回目の違反の数を期待: %v %q", i+1, i+1, err, l)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "gate.jsonl")); err == nil {
		t.Errorf("調べる repo に書いた")
	}
}

func TestGateRecordFailureIsUndecidable(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	if rc, _, errs := runGateCmd(t, "-record", filepath.Join(t.TempDir(), "no", "such", "dir.jsonl"), "-base", "HEAD", root); rc != 2 || !strings.Contains(errs, "--record") {
		t.Errorf("記録に失敗したら黙らず rc=2: rc=%d err=%q", rc, errs)
	}
}

// upstream の無い detached HEAD の worktree では、origin/HEAD との merge-base を base にする。
// origin/HEAD が先に進んでいても (他人の写し B)、merge-base より後の他人の変更は数えず、自分の写し C だけを名指しする。
func TestGateUsesOriginHeadOnDetachedHead(t *testing.T) {
	root := committedRepo(t)
	gitRun(t, root, "checkout", "-q", "-b", "trunk")
	writeFile(t, root, "Sources/B.swift", helperB)
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "someone else's copy")
	gitRun(t, root, "update-ref", "refs/remotes/origin/trunk", "trunk")
	gitRun(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	gitRun(t, root, "checkout", "-q", "--detach", "trunk~1")
	writeFile(t, root, "Sources/C.swift", helperC)
	rc, out, errs := runGateCmd(t, root)
	if rc != 1 || !strings.Contains(out, "新しい: Sources/C.swift") || strings.Contains(out, "Sources/B.swift") {
		t.Errorf("origin/HEAD との merge-base で、自分の写し C だけを名指しする: rc=%d out=%q err=%q", rc, out, errs)
	}
}

// base の commit が手元に無い (浅い clone) ときは、全部を新しいと数えずに判定不能 (rc=2) で終わる。
func TestGateShallowCloneWithoutBaseIsUndecidable(t *testing.T) {
	src := committedRepo(t)
	writeFile(t, src, "Sources/B.swift", helperB)
	gitRun(t, src, "add", ".")
	gitRun(t, src, "commit", "-q", "-m", "copy")
	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := subproc.GitCommand(context.Background(), "clone", "-q", "--depth", "1", "file://"+src, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v: %s", err, out)
	}
	rc, out, errs := runGateCmd(t, "-base", "HEAD~1", clone)
	if rc != 2 || !strings.Contains(errs, "手元に無い") || strings.Contains(out, "新しい") {
		t.Errorf("浅い clone で base が無ければ rc=2: rc=%d out=%q err=%q", rc, out, errs)
	}
}

// 整形 (改行・字下げ・空白・コメント) だけの変更は、本体の木が同じなので変わった単位にならず、写しの組があっても落ちない。
func TestGateFormattingOnlyChangeIsNotChange(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/B.swift", `struct B {
    // 整形し直した
    func makeLocation(_ p: String) -> StorageLocation {

        StorageLocation(
            connectionID: ConnectionID(rawValue: "conn"),   /* 同じ値 */
            path: ObjectPath(rawValue: p)
        )
    }
}
`)
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 0 || !strings.Contains(out, "変わった単位 0") {
		t.Errorf("整形だけの変更は変わった単位 0 で rc=0: rc=%d out=%q err=%q", rc, out, errs)
	}
}

func TestGateListsResolvedGroupAfterRemovingCopy(t *testing.T) {
	// 写し B を消して寄せた: 違反は無く、要約に消えた組が数えられ、-v と JSON で base の側の位置が出る。
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	if err := os.Remove(filepath.Join(root, "Sources/B.swift")); err != nil {
		t.Fatal(err)
	}
	rc, out, errs := runGateCmd(t, "-v", "-base", "HEAD", root)
	if rc != 0 || !strings.Contains(out, "消えた組 1・縮んだ組 0") || !strings.Contains(out, "消えた組: L1 (2 → 0)") ||
		!strings.Contains(out, "  base: Sources/B.swift:") {
		t.Errorf("消えた組の数と一覧を期待: rc=%d out=%q err=%q", rc, out, errs)
	}
	rc, out, errs = runGateCmd(t, "-format", "json", "-base", "HEAD", root)
	var s struct {
		Resolved []struct {
			Level   string `json:"level"`
			Remain  int    `json:"remain"`
			Members []struct {
				File string `json:"file"`
			} `json:"members"`
		} `json:"resolved"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || rc != 0 {
		t.Fatalf("rc=%d err=%v stderr=%q", rc, err, errs)
	}
	if len(s.Resolved) != 1 || s.Resolved[0].Remain != 0 || len(s.Resolved[0].Members) != 2 {
		t.Errorf("JSON の resolved に消えた組 1 つを期待: %+v", s.Resolved)
	}
	// 触っていないなら要約に出さない (1 行のまま)。
	gitRun(t, root, "checkout", "-q", "--", "Sources/B.swift")
	if _, out, _ := runGateCmd(t, "-base", "HEAD", root); strings.Contains(out, "消えた組") {
		t.Errorf("変わっていないのに消えた組を出した: %q", out)
	}
}
