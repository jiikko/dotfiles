package upgrade

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// tree は一時ディレクトリに repo の形 (src/pro-con/{go.mod,pro-con} と bin/lib/go_autobuild.zsh) を作る。
func tree(t *testing.T) (root, exe string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "src", "pro-con")
	for _, d := range []string{dir, filepath.Join(root, "bin", "lib")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(dir, "go.mod"), "module pro-con\n")
	write(t, filepath.Join(root, "bin", "lib", "go_autobuild.zsh"), "# shim\n")
	exe = filepath.Join(dir, BinaryName)
	write(t, exe, "old")
	return root, exe
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect(t *testing.T) {
	root, exe := tree(t)
	link := filepath.Join(root, "bin-pro-con")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	src, err := Detect(link)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(exe)
	realRoot, _ := filepath.EvalSymlinks(root)
	if src.Exe != real || src.Dir != filepath.Dir(real) || src.Shim != filepath.Join(realRoot, "bin", "lib", "go_autobuild.zsh") {
		t.Fatalf("見つけたものが違う: %+v", src)
	}
}

// ソースのディレクトリから起動していると確かめられないものは無効: 名前が違う / go.mod が無い / shim が辿れない。
func TestDetectRefuses(t *testing.T) {
	root, exe := tree(t)
	other := filepath.Join(filepath.Dir(exe), "other")
	write(t, other, "x")
	lone := filepath.Join(t.TempDir(), BinaryName)
	write(t, lone, "x")
	for name, p := range map[string]string{"名前": other, "go.mod": lone} {
		if _, err := Detect(p); !errors.Is(err, ErrNoSource) {
			t.Fatalf("%s: 無効のはず: %v", name, err)
		}
	}
	if err := os.Remove(filepath.Join(root, "bin", "lib", "go_autobuild.zsh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Detect(exe); !errors.Is(err, ErrNoSource) {
		t.Fatalf("shim が無いのに有効: %v", err)
	}
}

// Spawn は shim の go_autobuild_spawn_if_stale を、パスを位置引数で渡して呼ぶ (スクリプトへ文字列連結しない)。
// rc=0 は起動した、rc=1 は「要らない / backoff」(エラーではない)、それ以外 (実行できない等) はエラー。
func TestSpawnAsksShim(t *testing.T) {
	_, exe := tree(t)
	src, _ := Detect(exe)
	var got []string
	ok, err := src.Spawn(context.Background(), func(_ context.Context, name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	})
	want := []string{"zsh", "-c", `source "$1"; go_autobuild_spawn_if_stale "$2" "$3"`, "zsh", src.Shim, src.Dir, BinaryName}
	if !ok || err != nil || !slices.Equal(got, want) {
		t.Fatalf("呼び方が違う:\n got %q (%v %v)\nwant %q", got, ok, err, want)
	}
	rc1 := exec.Command("sh", "-c", "exit 1").Run()
	if ok, err := src.Spawn(context.Background(), func(context.Context, string, ...string) error { return rc1 }); ok || err != nil {
		t.Fatalf("rc=1 は「要らない」でエラーではない: %v %v", ok, err)
	}
	missing := errors.New(`exec: "zsh": executable file not found`)
	if ok, err := src.Spawn(context.Background(), func(context.Context, string, ...string) error { return missing }); ok || err == nil {
		t.Fatalf("実行できないのに「要らない」と区別しない: %v %v", ok, err)
	}
}

// Replaced は起動したときのバイナリと今のファイルが違うか。rename での差し替えも、同じファイルの書き換えも見分ける。
func TestReplaced(t *testing.T) {
	_, exe := tree(t)
	src, _ := Detect(exe)
	start, _ := os.Stat(src.Exe)
	if r, err := src.Replaced(start); err != nil || r {
		t.Fatalf("何も変えていないのに差し替え扱い: %v %v", r, err)
	}
	tmp := src.Exe + ".new"
	write(t, tmp, "old") // 中身も大きさも同じ新しいファイル (shim の mv -f と同じ形)
	if err := os.Rename(tmp, src.Exe); err != nil {
		t.Fatal(err)
	}
	if r, _ := src.Replaced(start); !r {
		t.Fatal("rename で差し替えたのに気づかない")
	}
}

func TestFailedSince(t *testing.T) {
	_, exe := tree(t)
	src, _ := Detect(exe)
	start := time.Now()
	stamp := filepath.Join(src.Dir, ".autobuild.failed")
	write(t, stamp, "fp")
	old := start.Add(-time.Hour)
	_ = os.Chtimes(stamp, old, old)
	if src.FailedSince(start) {
		t.Fatal("起動より前の失敗の記録を拾った")
	}
	later := start.Add(time.Minute)
	_ = os.Chtimes(stamp, later, later)
	if !src.FailedSince(start) {
		t.Fatal("起動より後の失敗の記録を拾わない")
	}
}

func TestSaveLoadAndVersion(t *testing.T) {
	dir := t.TempDir()
	p, err := Save(dir, "", State{UI: []byte(`{"tab":"x"}`), Backend: []byte(`{"n":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	st, err := Load(p)
	if err != nil || string(st.UI) != `{"tab":"x"}` || string(st.Backend) != `{"n":1}` {
		t.Fatalf("往復しない: %+v %v", st, err)
	}
	// 引き継いだファイルへは同じパスで上書きする (溜めない)
	p2, err := Save(dir, p, State{UI: []byte(`{"tab":"y"}`)})
	if err != nil || p2 != p {
		t.Fatalf("同じパスへ上書きしない: %s %v", p2, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("ファイルが増えた: %v", entries)
	}
	write(t, p, `{"version":99}`)
	if _, err := Load(p); !errors.Is(err, ErrVersion) {
		t.Fatalf("版の違いを見分けない: %v", err)
	}
}

// Exec は状態のファイルを環境変数で渡す。前の値は置き換え、shim の印 (GO_AUTOBUILD_PENDING) は落とす。
func TestExecPassesEnv(t *testing.T) {
	var argv0 string
	var argv, env []string
	fn := func(a string, v, e []string) error { argv0, argv, env = a, v, e; return errors.New("exec しない") }
	in := []string{"HOME=/h", ResumeEnv + "=/old", "GO_AUTOBUILD_PENDING=1"}
	if err := Exec("/s/pro-con", []string{"x"}, in, "/state.json", fn); err == nil {
		t.Fatal("exec の失敗が返らない")
	}
	if argv0 != "/s/pro-con" || !slices.Equal(argv, []string{"/s/pro-con", "x"}) {
		t.Fatalf("argv が違う: %s %v", argv0, argv)
	}
	if !slices.Equal(env, []string{"HOME=/h", ResumeEnv + "=/state.json"}) {
		t.Fatalf("env が違う: %v", env)
	}
	if slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "GO_AUTOBUILD_PENDING=") }) {
		t.Fatal("shim の印が残った")
	}
}
