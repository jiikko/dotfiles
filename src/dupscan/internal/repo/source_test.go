package repo

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// commitAll は作業ツリーの全部を commit し、その hash を返す。
func commitAll(t *testing.T, root string) string {
	t.Helper()
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c")
	h, err := CommitHash(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func commitFiles(t *testing.T, root, hash string, prefixes ...string) []string {
	t.Helper()
	c := &Commit{Scope: Scope{Root: root, Prefixes: prefixes}, Exts: []string{".swift"}, Hash: hash}
	files, err := c.Files()
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// base の側も作業ツリーの側と同じ範囲を読む。起点が subdir だけなら、その外のファイルを base に数えない。
func TestCommitFilesNarrowsToPrefixes(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q")
	write(t, root, "a/X.swift", "func x() {}\n")
	write(t, root, "b/Y.swift", "func y() {}\n")
	write(t, root, "c/Z.swift", "func z() {}\n")
	h := commitAll(t, root)
	for _, tc := range []struct {
		prefixes []string
		want     []string
	}{
		{[]string{"a/"}, []string{"a/X.swift"}},
		{[]string{"a/", "b/"}, []string{"a/X.swift", "b/Y.swift"}},
		{[]string{"a/", ""}, []string{"a/X.swift", "b/Y.swift", "c/Z.swift"}},
	} {
		if got := commitFiles(t, root, h, tc.prefixes...); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("prefixes %q: got %v, want %v", tc.prefixes, got, tc.want)
		}
	}
}

// symlink の先と submodule の中身は別のファイル・別の repo なので、名前が .swift でも base に数えない。実行権限の付いたファイルは数える。
func TestCommitFilesSkipsSymlinksAndSubmodules(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q")
	write(t, root, "R.swift", "func r() {}\n")
	write(t, root, "E.swift", "func e() {}\n")
	if err := os.Chmod(filepath.Join(root, "E.swift"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("R.swift", filepath.Join(root, "L.swift")); err != nil {
		t.Fatal(err)
	}
	h := commitAll(t, root)
	run(t, root, "update-index", "--add", "--cacheinfo", "160000,"+h+",S.swift") // 作業ツリーに置かず index だけに足す (add -A は消えたものとして外す)
	run(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "submodule")
	h, err := CommitHash(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := commitFiles(t, root, h, ""), []string{"E.swift", "R.swift"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// 設定ファイルが base に無いのは「設定なし」で、失敗ではない。同じ名前がディレクトリなら、どのパスかを名指しして失敗にする。
func TestCommitReadRoot(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q")
	write(t, root, ".dupscan.json", `{"version":1}`)
	write(t, root, "cfg/x.txt", "x\n")
	c := &Commit{Scope: Scope{Root: root, Prefixes: []string{""}}, Hash: commitAll(t, root)}
	if b, ok, err := c.ReadRoot(".dupscan.json"); err != nil || !ok || string(b) != `{"version":1}` {
		t.Errorf("在るファイル: %q %v %v", b, ok, err)
	}
	if b, ok, err := c.ReadRoot("missing.json"); err != nil || ok || b != nil {
		t.Errorf("無いファイル: %q %v %v", b, ok, err)
	}
	if _, ok, err := c.ReadRoot("cfg"); err == nil || ok || !strings.Contains(err.Error(), ":cfg") {
		t.Errorf("ディレクトリはパスを名指しした失敗を期待 (git cat-file の失敗は blob の hash しか言わない): %v %v", ok, err)
	}
}

// 共通の祖先が無い 2 つの commit は比べる相手が決まらないので、判定不能 (ErrNoBase) にする。
func TestMergeBaseWithoutCommonAncestorIsNoBase(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q")
	write(t, root, "A.swift", "func a() {}\n")
	a := commitAll(t, root)
	run(t, root, "checkout", "-q", "--orphan", "other")
	write(t, root, "B.swift", "func b() {}\n")
	b := commitAll(t, root)
	if _, err := MergeBase(root, a, b); !errors.Is(err, ErrNoBase) {
		t.Errorf("ErrNoBase を期待: %v", err)
	}
	if got, err := MergeBase(root, a, a); err != nil || got != a {
		t.Errorf("同じ commit の merge-base はその commit: %q %v", got, err)
	}
}

// base の commit を読みかけで止めるときは、git を止めてから待つ。読み残したまま待つと、後ろの大きな blob で pipe を埋めた git が書けずに止まり、
// GitOpTimeout (30 秒) まで戻らない。直した版は即座に返る。10 秒は時間切れより十分短い見張りで、合否を所要で決めるのではなく「止まらない」ことを見る。
func TestCommitReadAllStopsGitOnError(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q")
	big := strings.Repeat("let x = 1\n", 60000) // 600KB: pipe の容量を大きく超える
	for _, f := range []string{"A.swift", "B.swift", "C.swift", "D.swift"} {
		write(t, root, f, f+big)
	}
	run(t, root, "add", ".")
	run(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c")
	h, err := CommitHash(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	c := &Commit{Scope: Scope{Root: root, Prefixes: []string{""}}, Exts: []string{".swift"}, Hash: h}
	files, err := c.Files()
	if err != nil || len(files) != 4 {
		t.Fatalf("Files: %v %v", files, err)
	}
	c.blobs[files[0]] = strings.Repeat("0", 40) // 無い object: cat-file は `missing` を返し、後ろの 3 つは読まれずに残る
	done := make(chan error, 1)
	go func() {
		_, err := c.ReadAll(files)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "blob として返さない") {
			t.Errorf("無い blob で失敗を期待: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("読みかけで止めた ReadAll が戻らない (git の終了を待ち続けている)")
	}
}
