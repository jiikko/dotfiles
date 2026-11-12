package oracle

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadCorpusRejectsEmptyDirectory(t *testing.T) {
	if _, err := LoadCorpus(t.TempDir()); err == nil {
		t.Fatal("入力が 0 件の集まりを緑にしてはいけない")
	}
}

func TestLoadCorpusRejectsInvalidUTF8(t *testing.T) {
	dir := writeTree(t, map[string]string{"bad.md": "\xff\xfe"})
	if _, err := LoadCorpus(dir); err == nil {
		t.Fatal("UTF-8 でない入力は拒否する")
	}
}

func TestLoadCorpusCountsLinesLikePythonSplit(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"empty.md": "",
		"crlf.md":  "a\r\nb\r\n",
		"one.md":   "x",
		"sub/n.md": "\n\n",
	})
	c, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"empty.md": 1, "crlf.md": 3, "one.md": 1, "sub/n.md": 3}
	if len(c.Files) != len(want) {
		t.Fatalf("ファイル数 %d", len(c.Files))
	}
	for i, f := range c.Files {
		if f.Lines != want[f.Path] {
			t.Errorf("%s: 行数 %d、期待 %d", f.Path, f.Lines, want[f.Path])
		}
		if i > 0 && c.Files[i-1].Path >= f.Path {
			t.Errorf("Path の昇順でない: %s の後に %s", c.Files[i-1].Path, f.Path)
		}
	}
	if got := c.Units(); got != 4+1+3+1+3 {
		t.Errorf("単位の数 %d", got)
	}
}

func TestUnitKey(t *testing.T) {
	if UnitKey("a/b.md", -1) != "a/b.md#doc" || UnitKey("a/b.md", 7) != "a/b.md#7" {
		t.Fatal("単位の名前の形が変わっている")
	}
}
