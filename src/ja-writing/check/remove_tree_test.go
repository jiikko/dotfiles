package check

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

// 一時フォルダそのものが無いときは、本家と同じく onexc (again) に回して親を 0o700 にし、消せなかったと知らせる
// (本家は根の lstat の FileNotFoundError を onexc に回す。Python 3.15 で、親 0o755 が 0o700 になり、知らせが 1 行出る)。
func TestRemoveTemporaryMissingRootRestoresParent(t *testing.T) {
	parent := t.TempDir() + "/parent"
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil { // umask に依らず 0o755 から始める
		t.Fatal(err)
	}
	var warned []string
	removeTemporary(parent+"/yomiyasu-check-x", func(m string) { warned = append(warned, m) })
	if len(warned) != 1 {
		t.Fatalf("知らせ=%q (1 行を期待)", warned)
	}
	if info, err := os.Stat(parent); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("親の権限 %v %v (本家と同じ 0o700 を期待)", info, err)
	}
}

// 歩く途中で中のものが消えても、本家の _rmtree_safe_fd_step と同じく FileNotFoundError を飛ばして続け、根まで消す (知らせを出さない)。
// 消える時点は、ファイルを消す前・フォルダを読む前・フォルダを消す前の 3 つ。本家は Python 3.15 で、最初の unlink の中で残りの
// ファイルと sub を消しても、知らせを出さずに根を消す (tmp/1077-cont-1120/p4afix3/x.*/vanish.out)。
func TestRemoveTreeSkipsEntriesThatVanish(t *testing.T) {
	cases := map[string]func(root string) treeOps{
		"ファイルを消す前": func(root string) treeOps {
			o := osTree
			o.lstat = func(p string) (fs.FileInfo, error) {
				info, err := os.Lstat(p)
				if p == root+"/a" {
					mustRemoveAll(t, p)
				}
				return info, err
			}
			return o
		},
		"フォルダを読む前": func(root string) treeOps {
			o := osTree
			o.lstat = func(p string) (fs.FileInfo, error) {
				info, err := os.Lstat(p)
				if p == root+"/sub" {
					mustRemoveAll(t, p)
				}
				return info, err
			}
			return o
		},
		"フォルダを消す前": func(root string) treeOps {
			o := osTree
			o.readDir = func(p string) ([]fs.DirEntry, error) {
				entries, err := os.ReadDir(p)
				if p == root+"/sub" {
					mustRemoveAll(t, p)
				}
				return entries, err
			}
			return o
		},
	}
	for name, ops := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir() + "/yomiyasu-check-x"
			if err := os.MkdirAll(root+"/sub/inner", 0o700); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{root + "/a", root + "/b", root + "/sub/x"} {
				if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := ops(root).remove(root, true); err != nil {
				t.Fatalf("消えたものを誤りにした: %v", err)
			}
			if _, err := os.Lstat(root); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("根が残った: %v", err)
			}
		})
	}
}

func mustRemoveAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}
