package oracle

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// 本家の道具は cwd から上へ .git・.yomiyasu-words・.yomiyasu を探す。使い捨ての dir がそれらの下に作られるなら、記録を取らずに止まる。
func TestNewSandboxRefusesDirUnderUpstreamMarker(t *testing.T) {
	// 守る名前は実装の一覧を回さずここに直に書く (一覧から名前が消えても落ちるように)。
	for _, marker := range []string{".git", ".yomiyasu-words", ".yomiyasu"} {
		t.Run(marker, func(t *testing.T) {
			top := t.TempDir()
			if err := os.Mkdir(filepath.Join(top, marker), 0o755); err != nil {
				t.Fatal(err)
			}
			tmp := filepath.Join(top, "a", "b")
			if err := os.MkdirAll(tmp, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", tmp)
			sb, err := newSandbox()
			if err == nil {
				sb.remove()
				t.Fatalf("%s の下の TMPDIR で sandbox が作れた", marker)
			}
			realTop, rerr := filepath.EvalSymlinks(top)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if !strings.Contains(err.Error(), filepath.Join(realTop, marker)) {
				t.Errorf("エラーが拾われるパスを示さない: %v", err)
			}
			if left, _ := filepath.Glob(filepath.Join(tmp, "yomiyasu-oracle-*")); len(left) != 0 {
				t.Errorf("断った sandbox の dir が残った: %v", left)
			}
		})
	}
}

// .git は worktree では dir でなくファイル。ファイルでも拾われる。
func TestNewSandboxRefusesGitFile(t *testing.T) {
	top := t.TempDir()
	if err := os.WriteFile(filepath.Join(top, ".git"), []byte("gitdir: /x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", top)
	if sb, err := newSandbox(); err == nil {
		sb.remove()
		t.Fatal(".git のファイルの下の TMPDIR で sandbox が作れた")
	}
}

func TestMarkerAboveIsEmptyForCleanTree(t *testing.T) {
	clean := t.TempDir()
	// t.TempDir の上 (os.TempDir) に偶然 marker があると検査にならないので、その場合は飛ばす
	if above, err := markerAbove(clean); err != nil || above != "" {
		t.Skipf("この環境の一時領域の上に %q がある (%v)", above, err)
	}
	t.Setenv("TMPDIR", clean)
	sb, err := newSandbox()
	if err != nil {
		t.Fatal(err)
	}
	sb.remove()
}

// TMPDIR が symlink で、実体が .git と .yomiyasu-words のある dir の下にあるとき、字面の親には marker が無い。
func TestNewSandboxRefusesSymlinkedTmpdirUnderMarker(t *testing.T) {
	for _, marker := range []string{".git", ".yomiyasu-words", ".yomiyasu"} {
		t.Run(marker, func(t *testing.T) {
			repo := t.TempDir()
			if err := os.Mkdir(filepath.Join(repo, marker), 0o755); err != nil {
				t.Fatal(err)
			}
			real := filepath.Join(repo, "tmp")
			if err := os.Mkdir(real, 0o755); err != nil {
				t.Fatal(err)
			}
			clean := t.TempDir()
			link := filepath.Join(clean, "link")
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			if above, err := markerAbove(clean); err != nil || above != "" {
				t.Skipf("この環境の一時領域の上に %q がある (%v)", above, err)
			}
			t.Setenv("TMPDIR", link)
			sb, err := newSandbox()
			if err == nil {
				sb.remove()
				t.Fatalf("symlink の TMPDIR (実体は %s の下) で sandbox が作れた", marker)
			}
			if left, _ := filepath.Glob(filepath.Join(real, "yomiyasu-oracle-*")); len(left) != 0 {
				t.Errorf("断った sandbox の dir が残った: %v", left)
			}
		})
	}
}

func writeSrcTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// 実行する木は、検証したファイルだけを写した新しい dir。置き場の __pycache__ (SHA-256 を見ない .pyc) や一覧に無い .py を import させない。
func TestStageTreeCopiesOnlyVerifiedFiles(t *testing.T) {
	files, sums, bodies := fakeFiles()
	src := Tree{Root: t.TempDir()}
	writeSrcTree(t, src.Root, bodies)
	writeSrcTree(t, src.Root, map[string]string{"a/__pycache__/one.cpython-315.pyc": "偽の bytecode", "a/evil.py": "raise SystemExit(1)\n"})
	dst := Tree{Root: filepath.Join(t.TempDir(), "run")}
	if err := stageTree(src, dst, files, sums); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := filepath.WalkDir(dst.Root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dst.Root, path)
			got = append(got, filepath.ToSlash(rel))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/one.py", "b/two.txt"}; !slices.Equal(got, want) {
		t.Fatalf("写したファイル %v、期待 %v (置き場の __pycache__ や一覧に無い .py を写してはいけない)", got, want)
	}
	for rel, body := range bodies {
		if have, _ := os.ReadFile(dst.Path(rel)); string(have) != body {
			t.Errorf("%s の中身が違う: %q", rel, have)
		}
	}
}

func TestStageTreeRefusesTamperedAndMissingFiles(t *testing.T) {
	files, sums, bodies := fakeFiles()
	src := Tree{Root: t.TempDir()}
	writeSrcTree(t, src.Root, bodies)
	writeSrcTree(t, src.Root, map[string]string{"b/two.txt": "改ざん\n"})
	err := stageTree(src, Tree{Root: t.TempDir()}, files, sums)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("SHA-256 が合わない置き場は断るはず: %v", err)
	}
	if err := stageTree(Tree{Root: t.TempDir()}, Tree{Root: t.TempDir()}, files, sums); err == nil {
		t.Fatal("置き場にファイルが無いのに写せた")
	}
}

// site を読ませない (-S) と、cwd・user site・.pyc の書き込みを止める引数が、起動の形に入っていること。
// 外すと、system site の sitecustomize が driver より前に走って誤った記録を正解にできる (Python を動かさない検査の代わりに、引数そのものを固定する)。
func TestPythonFlagsKeepSiteAndCwdOut(t *testing.T) {
	for _, f := range []string{"-P", "-s", "-S", "-B"} {
		if !slices.Contains(pythonFlags, f) {
			t.Errorf("python3 の起動の引数に %s が無い: %v", f, pythonFlags)
		}
	}
	if slices.Contains(pythonFlags, "-I") {
		t.Error("-I は PYTHONHASHSEED を無視するので使わない")
	}
}

// driver を Runner を通さずに repo の中で起こすと、本家の check が状態フォルダを上の .git の所に作る。
// call_function.py は作業根かその上に印があれば、指示を読む前に止まる。作業根の中のサブフォルダの印 (fixture の .git) では止まらない。
func TestCallFunctionRefusesUnderUpstreamMarker(t *testing.T) {
	r, err := NewRunner(Tree{})
	if err != nil {
		t.Fatal(err)
	}
	src, err := drivers.ReadFile("py/call_function.py")
	if err != nil {
		t.Fatal(err)
	}
	// run は top/<cwd> を作業根にして driver を起こす。driver は top の外の drivers に置く
	run := func(t *testing.T, top, cwd string) RawResult {
		t.Helper()
		sb := sandbox{root: top, cwd: filepath.Join(top, cwd), home: filepath.Join(top, "home"), drivers: filepath.Join(top, "drivers")}
		for _, d := range []string{sb.cwd, sb.home, sb.drivers} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		path := filepath.Join(sb.drivers, "call_function.py")
		if err := os.WriteFile(path, src, 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := r.exec(t.Context(), sb, []byte("{}"), path)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	const refusal = "作業根かその上に"
	// 印の場所: 作業根そのもの (repo の根で起こす)・2 段上・4 段上 (tmp/<作業>/<run>/cwd の形)
	places := map[string]string{"作業根": "w", "2 段上": "a/w", "4 段上": "a/b/c/w"}
	for _, marker := range []string{".git", ".yomiyasu-words", ".yomiyasu"} {
		for where, cwd := range places {
			t.Run(marker+"/"+where, func(t *testing.T) {
				top := t.TempDir()
				// 作業根のほかは印を top の直下に置き、作業根との間を空ける (段数を確かめる)
				markerDir := top
				if where == "作業根" {
					markerDir = filepath.Join(top, cwd)
				}
				if err := os.MkdirAll(filepath.Join(markerDir, marker), 0o755); err != nil {
					t.Fatal(err)
				}
				res := run(t, top, cwd)
				if res.RC == 0 || !strings.Contains(res.Stderr, refusal) || !strings.Contains(res.Stderr, marker) {
					t.Fatalf("%s が%sにあるのに driver が止まらない: rc=%d stderr=%q", marker, where, res.RC, res.Stderr)
				}
			})
		}
	}
	t.Run("作業根の中のサブフォルダの印では止まらない", func(t *testing.T) {
		top := t.TempDir()
		if above, err := markerAbove(top); err != nil || above != "" {
			t.Skipf("t.TempDir の上に %s があり、対照にならない", above)
		}
		if err := os.MkdirAll(filepath.Join(top, "w", "repo", ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		res := run(t, top, "w")
		if strings.Contains(res.Stderr, refusal) {
			t.Fatalf("作業根の中の .git で止まった: %q", res.Stderr)
		}
		// 指示 {} には scripts_dir が無いので、止める処理を通り過ぎた先で KeyError になる
		if !strings.Contains(res.Stderr, "scripts_dir") {
			t.Fatalf("止める処理の先まで進んでいない: rc=%d stderr=%q", res.RC, res.Stderr)
		}
	})
}
