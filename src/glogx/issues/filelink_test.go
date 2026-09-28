package issues

import (
	"os"
	"path/filepath"
	"testing"

	"tuikit/markdown"
)

func writeFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveLink(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	issue := filepath.Join(root, "issues", "done", "001-x.md")
	writeFile(t, issue)
	writeFile(t, filepath.Join(root, "docs", "spec.md"))
	writeFile(t, filepath.Join(root, "src", "a.go"))
	writeFile(t, filepath.Join(root, "issues", "done", "src", "a.go")) // 同名の別ファイル (基準を取り違えたら当たる)
	writeFile(t, filepath.Join(home, "note.md"))
	writeFile(t, filepath.Join(root, "sp ace.md"))
	outside := filepath.Join(t.TempDir(), "secret")
	writeFile(t, outside)
	if err := os.Symlink(outside, filepath.Join(root, "docs", "evil.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "docs", "spec.md"), filepath.Join(root, "docs", "alias.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "linked.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "docs", "spec draft.md"))
	writeFile(t, filepath.Join(root, "docs", "spec")) // 空白で切ったら当たってしまう別ファイル
	writeFile(t, filepath.Join(root, "a\x1b[31m.md"))
	base := LinkBase{File: issue, Project: root, Repo: root}

	cases := []struct {
		name string
		kind markdown.LinkKind
		dest string
		want string // "" = リンクにしない
		line int
	}{
		{"リンクはファイル基準", markdown.LinkDest, "../../docs/spec.md", "docs/spec.md", 0},
		{"fragment と title を落とす", markdown.LinkDest, `../../docs/spec.md#sec "t"`, "docs/spec.md", 0},
		{"% エスケープを解く", markdown.LinkDest, "../../sp%20ace.md", "sp ace.md", 0},
		{"リンクは repo root 基準にしない", markdown.LinkDest, "docs/spec.md", "", 0},
		{"リンクの相対はファイル基準で同名の別ファイル", markdown.LinkDest, "src/a.go", "issues/done/src/a.go", 0},
		{"コードは repo root 基準", markdown.LinkCode, "src/a.go", "src/a.go", 0},
		{"コードの行番号", markdown.LinkCode, "src/a.go:12", "src/a.go", 12},
		{"コードの行:桁", markdown.LinkCode, "src/a.go:12:3", "src/a.go", 12},
		{"コードの ~/", markdown.LinkCode, "~/note.md", "~/note.md", 0},
		{"実在しない", markdown.LinkCode, "src/nope.go", "", 0},
		{"ディレクトリは開かない", markdown.LinkCode, "src", "", 0},
		{"実在しない絶対パス", markdown.LinkCode, "/nonexistent-glogx/x.go", "", 0},
		{"絶対パスのディレクトリ", markdown.LinkCode, "/tmp", "", 0},
		{"空白を含むコードはパスでない", markdown.LinkCode, "cat src/a.go", "", 0},
		{"URL", markdown.LinkDest, "https://example.com/src/a.go", "", 0},
		{"アンカーだけ", markdown.LinkDest, "#sec", "", 0},
		{"制御文字", markdown.LinkCode, "src/a.go\x1b]0;x\x07", "", 0},
		{"行番号 0 は行番号でない", markdown.LinkCode, "src/a.go:0", "", 0},
		{"GitHub 式の #L12", markdown.LinkDest, "../../docs/spec.md#L12-L20", "docs/spec.md", 12},
		{"repo の外を指す symlink は開かない", markdown.LinkCode, "docs/evil.md", "", 0},
		{"repo の中を指す symlink は開く", markdown.LinkCode, "docs/alias.md", "docs/alias.md", 0},
		{"相対で repo の外へ出ない", markdown.LinkDest, "../../../outside.md", "", 0},
		{"~/ は書いた場所を信じる (symlink の先を問わない)", markdown.LinkCode, "~/linked.md", "~/linked.md", 0},
		{"絶対パスでも repo の中から外へ出る symlink は開かない", markdown.LinkCode, filepath.Join(root, "docs", "evil.md"), "", 0},
		{"絶対パスのリンクも同じ", markdown.LinkDest, filepath.Join(root, "docs", "evil.md"), "", 0},
		{"絶対パスで repo の中の通常ファイル", markdown.LinkCode, filepath.Join(root, "src", "a.go"), "src/a.go", 0},
		{"山括弧の中の空白を切らない", markdown.LinkDest, "<../../docs/spec draft.md>", "docs/spec draft.md", 0},
		{"%エスケープで戻る制御文字", markdown.LinkDest, "../../a%1b[31m.md", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, line, ok := ResolveLink(tc.kind, tc.dest, base)
			if tc.want == "" {
				if ok {
					t.Fatalf("リンクにしないはずが %q に解決した", p)
				}
				return
			}
			want := filepath.Join(root, tc.want)
			if len(tc.want) > 2 && tc.want[:2] == "~/" {
				want = filepath.Join(home, tc.want[2:])
			}
			if !ok || p != want || line != tc.line {
				t.Fatalf("got (%q, %d, %v) want (%q, %d)", p, line, ok, want, tc.line)
			}
		})
	}
}

// 基準が無いと相対パスは解決しない (cwd などの別の基準へ落とさない)。
func TestResolveLinkNoBase(t *testing.T) {
	wd, _ := os.Getwd()
	for _, b := range []LinkBase{{File: "/x/issues/001.md", Project: wd}, {File: "/x/issues/001.md", Repo: wd}} {
		if p, _, ok := ResolveLink(markdown.LinkCode, "go.mod", b); ok {
			t.Fatalf("基準が欠けた %+v で %q に解決した", b, p)
		}
	}
}

// インラインコードの基準は issue ディレクトリの親 (root/app1/issues なら root/app1)。repo root ではない。
func TestBodyCodeBaseIsProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main.go"))         // repo root の同名ファイル (当たってはいけない)
	writeFile(t, filepath.Join(root, "app1", "src", "main.go")) // app1 の本物
	issue := filepath.Join(root, "app1", "issues", "done", "010-x.md")
	writeFileContent(t, issue, "`src/main.go`\n")
	iss := &Issue{Path: issue, Dir: filepath.Join(root, "app1", "issues")}
	body, err := iss.ReadBody()
	if err != nil {
		t.Fatal(err)
	}
	fl := body.FileLinks(80, root)
	if len(fl) != 1 || fl[0].Path != filepath.Join(root, "app1", "src", "main.go") {
		t.Fatalf("基準が app1 でない: %+v", fl)
	}
}

// FileLinks は実在するものだけを出現順で返し、JumpLines は行数を変えずに強調する。
func TestBodyFileLinks(t *testing.T) {
	root := t.TempDir()
	issue := filepath.Join(root, "issues", "001-x.md")
	writeFile(t, filepath.Join(root, "src", "a.go"))
	writeFile(t, filepath.Join(root, "docs", "b.md"))
	src := "# t\n\n`src/a.go` と `src/none.go` と [仕様](../docs/b.md) と `src/a.go:3`\n"
	writeFileContent(t, issue, src)
	iss := &Issue{Path: issue, Dir: filepath.Join(root, "issues")}
	body, err := iss.ReadBody()
	if err != nil {
		t.Fatal(err)
	}
	fl := body.FileLinks(80, root)
	if len(fl) != 3 {
		t.Fatalf("開けるリンク %d 個 (want 3): %+v", len(fl), fl)
	}
	if fl[0].Dest != "src/a.go" || fl[1].Path != filepath.Join(root, "docs", "b.md") || fl[2].Line != 3 {
		t.Fatalf("並び・解決が想定と違う: %+v", fl)
	}
	if fl[1].Index <= fl[0].Index+1 {
		t.Fatalf("存在しないリンクを飛ばしたのに添字が詰まっている (Index は markdown.Link の添字): %+v", fl)
	}
	plain := body.Lines(80, true)
	jl := body.JumpLines(80, true, root, 1)
	if len(plain) != len(jl) {
		t.Fatalf("強調で行数が変わった: %d != %d", len(plain), len(jl))
	}
}

func writeFileContent(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
