package issues

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

// realTempDir は実体のパスで持つ一時ディレクトリ (macOS の /var は /private/var への symlink。
// ResolveLink は解いた実体を返すので、期待値も実体で作る)。
func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestResolveLink(t *testing.T) {
	root := realTempDir(t)
	home := realTempDir(t)
	t.Setenv("HOME", home)
	issue := filepath.Join(root, "issues", "done", "001-x.md")
	writeFile(t, issue)
	writeFile(t, filepath.Join(root, "docs", "spec.md"))
	writeFile(t, filepath.Join(root, "src", "a.go"))
	writeFile(t, filepath.Join(root, "issues", "done", "src", "a.go")) // 同名の別ファイル (基準を取り違えたら当たる)
	writeFile(t, filepath.Join(home, "note.md"))
	writeFile(t, filepath.Join(root, "sp ace.md"))
	outsideDir := realTempDir(t)
	outside := filepath.Join(outsideDir, "secret")
	writeFile(t, outside)
	if err := os.Symlink(outside, filepath.Join(root, "docs", "evil.md")); err != nil {
		t.Fatal(err)
	}
	// ディレクトリの symlink で外へ出る (`d/secret` は外の実体)
	if err := os.Symlink(outsideDir, filepath.Join(root, "d")); err != nil {
		t.Fatal(err)
	}
	// readlink 先の `..` を symlink の後に当てる形: e -> outside/a/b、l -> e/../x。字面で潰すと repo/x (無害) を
	// 判定してしまうので、repo/x も実在させる
	writeFile(t, filepath.Join(outsideDir, "a", "x"))
	if err := os.MkdirAll(filepath.Join(outsideDir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outsideDir, "a", "b"), filepath.Join(root, "e")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "x"))
	if err := os.Symlink("e/../x", filepath.Join(root, "l")); err != nil {
		t.Fatal(err)
	}
	// repo の外から始まり、repo の中の symlink (e) 越しの `..` で外へ抜ける鎖:
	// home/hop -> root/e/../../z。kernel は e を解いてから .. を当てる (= outside/z)。字面で Clean すると
	// root/../z になって repo を通った痕跡ごと消える
	writeFile(t, filepath.Join(outsideDir, "z"))
	writeFile(t, filepath.Join(filepath.Dir(root), "z"))
	if err := os.Symlink(root+"/e/../../z", filepath.Join(home, "hop")); err != nil {
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
	// repo の外 (home) から repo の中の symlink へ入る鎖: home/rules.md -> root/docs/evil.md -> outside
	if err := os.Symlink(filepath.Join(root, "docs", "evil.md"), filepath.Join(home, "chain.md")); err != nil {
		t.Fatal(err)
	}
	// repo の外から repo の中の通常ファイルへ入る鎖 (正当: ~/.claude/rules/x.md の形)
	if err := os.Symlink(filepath.Join(root, "src", "a.go"), filepath.Join(home, "good.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(filepath.Dir(root), "sibling.md")) // repo のすぐ外に実在する通常ファイル
	// repo の外のディレクトリ link が、repo の中の「symlink に差し替えられたディレクトリ」へ入る鎖
	// (~/.claude/skills/forge -> dotfiles/_claude/skills/forge、その forge が PR で外への symlink に)
	if err := os.Symlink(outsideDir, filepath.Join(root, "sk")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "sk"), filepath.Join(home, "skills")); err != nil {
		t.Fatal(err)
	}
	// 正当なディレクトリ link: repo の外から repo の中の通常のディレクトリへ
	if err := os.Symlink(filepath.Join(root, "docs"), filepath.Join(home, "gooddir")); err != nil {
		t.Fatal(err)
	}
	base := LinkBase{File: issue, Project: root, Repos: []string{root}}

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
		{"リンクはファイル基準に無ければプロジェクト root 基準", markdown.LinkDest, "docs/spec.md", "docs/spec.md", 0},
		{"両方の基準に在ればファイル基準を優先", markdown.LinkDest, "src/a.go", "issues/done/src/a.go", 0},
		{"どちらの基準にも無い", markdown.LinkDest, "docs/nope.md", "", 0},
		{"リンクの相対はファイル基準で同名の別ファイル", markdown.LinkDest, "src/a.go", "issues/done/src/a.go", 0},
		{"コードは repo root 基準", markdown.LinkCode, "src/a.go", "src/a.go", 0},
		{"コードの行番号", markdown.LinkCode, "src/a.go:12", "src/a.go", 12},
		{"コードの行:桁", markdown.LinkCode, "src/a.go:12:3", "src/a.go", 12},
		{"コードの ~/", markdown.LinkCode, "~/note.md", "~/note.md", 0},
		{"実在しない", markdown.LinkCode, "src/nope.go", "", 0},
		{"単語 1 つはディレクトリに当てない", markdown.LinkCode, "src", "", 0},
		{"/ を含むディレクトリは止まり先", markdown.LinkCode, "@root/src", "src", 0},
		{"末尾 / 付きのディレクトリ", markdown.LinkCode, "src/", "src", 0},
		{"実在しない絶対パス", markdown.LinkCode, "/nonexistent-glogx/x.go", "", 0},
		{"通常ファイルでもディレクトリでもない", markdown.LinkCode, "/dev/null", "", 0},
		{"空白を含むコードはパスでない", markdown.LinkCode, "cat src/a.go", "", 0},
		{"URL", markdown.LinkDest, "https://example.com/src/a.go", "", 0},
		{"アンカーだけ", markdown.LinkDest, "#sec", "", 0},
		{"制御文字", markdown.LinkCode, "src/a.go\x1b]0;x\x07", "", 0},
		{"行番号 0 は行番号でない", markdown.LinkCode, "src/a.go:0", "", 0},
		{"GitHub 式の #L12", markdown.LinkDest, "../../docs/spec.md#L12-L20", "docs/spec.md", 12},
		{"repo の外を指す symlink は開かない", markdown.LinkCode, "docs/evil.md", "", 0},
		{"repo の中を指す symlink は開く (開くのは実体)", markdown.LinkCode, "docs/alias.md", "docs/spec.md", 0},
		{"相対で repo の外へ出ない", markdown.LinkDest, "../../../outside.md", "", 0},
		{"相対で repo の外の実在ファイルへ出ない", markdown.LinkDest, "../../../sibling.md", "", 0},
		{"~/ は repo に触れない鎖なら書いた場所を信じる (開くのは実体)", markdown.LinkCode, "~/linked.md", "@outside", 0},
		{"絶対パスでも repo の中から外へ出る symlink は開かない", markdown.LinkCode, filepath.Join(root, "docs", "evil.md"), "", 0},
		{"絶対パスのリンクも同じ", markdown.LinkDest, filepath.Join(root, "docs", "evil.md"), "", 0},
		{"絶対パスで repo の中の通常ファイル", markdown.LinkCode, filepath.Join(root, "src", "a.go"), "src/a.go", 0},
		{"外から repo の中の symlink を通って外へ出る鎖", markdown.LinkCode, "~/chain.md", "", 0},
		{"外から repo の中の通常ファイルへ入る鎖は開く (開くのは実体)", markdown.LinkCode, "~/good.md", "src/a.go", 0},
		{"閉じた山括弧の後ろに字", markdown.LinkDest, "<../../docs/spec.md>x", "", 0},
		{"途中のディレクトリの symlink で外へ出ない", markdown.LinkCode, "d/secret", "", 0},
		{"途中のディレクトリの symlink を絶対パスで書いても出ない", markdown.LinkCode, "@root/d/secret", "", 0},
		{"readlink 先の .. を symlink の後で解く", markdown.LinkCode, "l", "", 0},
		{"外から始まる鎖が repo の symlink 越しの .. で外へ抜ける", markdown.LinkCode, "~/hop", "", 0},
		{"外のディレクトリ link から repo の中のディレクトリ link を通って外へ", markdown.LinkCode, "~/skills/secret", "", 0},
		{"外のディレクトリ link から repo の中の通常ディレクトリへは開く", markdown.LinkCode, "~/gooddir/spec.md", "docs/spec.md", 0},
		{"山括弧の中の空白を切らない", markdown.LinkDest, "<../../docs/spec draft.md>", "docs/spec draft.md", 0},
		{"%エスケープで戻る制御文字", markdown.LinkDest, "../../a%1b[31m.md", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dest := strings.ReplaceAll(tc.dest, "@root", root)
			p, line, ok := ResolveLink(tc.kind, dest, base)
			if tc.want == "" {
				if ok {
					t.Fatalf("リンクにしないはずが %q に解決した", p)
				}
				return
			}
			want := filepath.Join(root, tc.want)
			switch {
			case tc.want == "@outside":
				want = outside
			case strings.HasPrefix(tc.want, "~/"):
				want = filepath.Join(home, tc.want[2:])
			}
			if !ok || p != want || line != tc.line {
				t.Fatalf("got (%q, %d, %v) want (%q, %d)", p, line, ok, want, tc.line)
			}
		})
	}
}

// 大文字小文字を変えて書いても repo の中と判定する (APFS は区別しないので、字面で比べると迂回される)。
func TestResolveLinkCaseInsensitivePathStillChecked(t *testing.T) {
	root := realTempDir(t)
	outside := filepath.Join(realTempDir(t), "secret")
	writeFile(t, outside)
	writeFile(t, filepath.Join(root, "docs", "ok.md"))
	if err := os.Symlink(outside, filepath.Join(root, "docs", "evil.md")); err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(root)
	if _, err := os.Stat(filepath.Join(upper, "docs", "ok.md")); err != nil {
		t.Skipf("このファイルシステムは大文字小文字を区別する (この迂回は起きない): %v", err)
	}
	base := LinkBase{File: filepath.Join(root, "issues", "001.md"), Project: root, Repos: []string{root}}
	if p, _, ok := ResolveLink(markdown.LinkCode, filepath.Join(upper, "docs", "evil.md"), base); ok {
		t.Fatalf("大文字で書いた repo 内の symlink から外へ出た: %q", p)
	}
	if _, _, ok := ResolveLink(markdown.LinkCode, filepath.Join(upper, "docs", "ok.md"), base); !ok {
		t.Fatal("大文字で書いた repo 内の通常ファイルが開けない")
	}
}

func TestExpandBraces(t *testing.T) {
	cases := map[string][]string{
		"a/b.{sql,tsv,meta}": {"a/b.sql", "a/b.tsv", "a/b.meta"},
		"a/{x,y}/c":          {"a/x/c", "a/y/c"},
		"a/b.go":             {"a/b.go"},
		"a/{x}":              {"a/{x}"},      // カンマが無いものは展開しない
		"{a,b}{c,d}":         {"{a,b}{c,d}"}, // 2 組以上は展開しない
	}
	for in, want := range cases {
		if got := ExpandBraces(in); !slices.Equal(got, want) {
			t.Errorf("ExpandBraces(%q) = %q want %q", in, got, want)
		}
	}
}

// 基準が無いと相対パスは解決しない (cwd などの別の基準へ落とさない)。
func TestResolveLinkNoBase(t *testing.T) {
	wd, _ := os.Getwd()
	for _, b := range []LinkBase{{File: "/x/issues/001.md", Project: wd}, {File: "/x/issues/001.md", Repos: []string{wd}}} {
		if p, _, ok := ResolveLink(markdown.LinkCode, "go.mod", b); ok {
			t.Fatalf("基準が欠けた %+v で %q に解決した", b, p)
		}
	}
}

// インラインコードの基準は issue ディレクトリの親 (root/app1/issues なら root/app1)。repo root ではない。
func TestBodyCodeBaseIsProject(t *testing.T) {
	root := realTempDir(t)
	writeFile(t, filepath.Join(root, "src", "main.go"))         // repo root の同名ファイル (当たってはいけない)
	writeFile(t, filepath.Join(root, "app1", "src", "main.go")) // app1 の本物
	issue := filepath.Join(root, "app1", "issues", "done", "010-x.md")
	writeFileContent(t, issue, "`src/main.go`\n")
	iss := &Issue{Path: issue, Dir: filepath.Join(root, "app1", "issues")}
	body, err := iss.ReadBody()
	if err != nil {
		t.Fatal(err)
	}
	fl := body.FileLinks(80, []string{root})
	if len(fl) != 1 || fl[0].Path != filepath.Join(root, "app1", "src", "main.go") {
		t.Fatalf("基準が app1 でない: %+v", fl)
	}
}

// FileLinks は実在するものだけを出現順で返し、JumpLines は行数を変えずに強調する。
func TestBodyFileLinks(t *testing.T) {
	root := realTempDir(t)
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
	fl := body.FileLinks(80, []string{root})
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
	jl := body.JumpLines(80, true, []string{root}, 1)
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

// 同じ repo の別 checkout に置かれた symlink を通って外へ出るのも止める (Repos に全 checkout を入れる)。
func TestResolveLinkOtherCheckoutSymlink(t *testing.T) {
	a, b := realTempDir(t), realTempDir(t) // 同じ repo の 2 つの checkout に見立てる
	secret := filepath.Join(realTempDir(t), "id")
	writeFile(t, secret)
	for _, r := range []string{a, b} {
		if err := os.MkdirAll(filepath.Join(r, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, filepath.Join(r, "docs", "x.md")); err != nil {
			t.Fatal(err)
		}
	}
	base := LinkBase{File: filepath.Join(b, "issues", "001.md"), Project: b, Repos: []string{b, a}}
	if p, _, ok := ResolveLink(markdown.LinkCode, filepath.Join(a, "docs", "x.md"), base); ok {
		t.Fatalf("別 checkout の symlink から外へ出た: %q", p)
	}
}

// WorktreeRoots は本体と worktree の両方を返す (本物の git で確かめる)。
func TestWorktreeRoots(t *testing.T) {
	// 🚨 hook から起動されたときに継承した GIT_DIR / GIT_WORK_TREE は -C / Dir より優先されるので外す
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		t.Setenv(k, "") // 終了時に元へ戻す登録 (t.Setenv が持つ)
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	main := realTempDir(t)
	wt := filepath.Join(realTempDir(t), "wt")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = main
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x")
	git("worktree", "add", "-q", "--detach", wt)
	got := WorktreeRoots(main)
	if !slices.Contains(got, main) || !slices.Contains(got, wt) {
		t.Fatalf("WorktreeRoots = %q (want %q と %q を含む)", got, main, wt)
	}
	if got := WorktreeRoots(t.TempDir()); len(got) != 1 {
		t.Fatalf("git 管理外では root だけ: %q", got)
	}
}
