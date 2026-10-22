package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 小さな木で 1 サイクル回し、段が空振りしていない (フォルダが開く・タイルが開く・diff が出る・ライブ更新の合図が来る) ことと、
// 結果のファイルが揃うことを確かめる。台本が filer のキーの変更で黙って何もしなくなったら、ここで落ちる。
func TestProbeRunsEveryPhase(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"treefiler/filer/model.go", "treefiler/README.md", "a/b/c.txt", "zz.txt"} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeFile(p, strings.Repeat("render line\n", 400)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir()) // run が差し替える。テストの後で戻すために先に張る
	out := t.TempDir()
	o := options{root: root, out: out, file: "treefiler/filer/model.go", md: "treefiler/README.md", query: "tr",
		cycles: 1, width: 120, height: 40, live: 20, settle: 30 * time.Second}
	if err := run(o); err != nil {
		t.Fatal(err)
	}
	steps, err := os.ReadFile(filepath.Join(out, "steps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(steps), "\n"), "\n")[1:] {
		f := strings.Split(line, "\t")
		state[f[1]] = f[14] + " | " + f[15]
	}
	for phase, want := range map[string]string{
		"explode":        "opened=4 ",
		"move":           "cursor=zz.txt ",    // 列 (a, treefiler, zz.txt) の最後。j / k が効かなければ l の先の a に残る
		"search":         "cursor=treefiler ", // 列の最後から tr で treefiler へ。検索が効かなければ zz.txt に残る
		"busy-close":     "閉じた時に裏の処理が走っていた",
		"collapse":       "opened=1 ",
		"tile-code":      "tiles=[treefiler/filer/model.go]",
		"tile-md":        "tiles=[treefiler/README.md]",
		"live-tile-diff": "tiles=[code.go(diff)]",
		"live-open":      "cursor=big/f0",
		"live-touch":     "取り込んだ後の動き=true",
	} {
		if !strings.Contains(state[phase], want) {
			t.Errorf("%s の後の状態 = %q, want %q を含む", phase, state[phase], want)
		}
	}
	for _, f := range []string{"cycles.tsv", "summary.txt"} {
		if fi, err := os.Stat(filepath.Join(out, f)); err != nil || fi.Size() == 0 {
			t.Errorf("%s が無いか空: %v", f, err)
		}
	}
}
