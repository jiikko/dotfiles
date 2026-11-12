package oracle

import (
	"fmt"
	"strings"
)

func findFolder(cwd, option any) Call { return vkeep(vcall("find_words_folder", cwd, option)) }

// gitFixtureCases は本物の git (git init・git worktree add) で作る 3 つの形: 通常の checkout・linked worktree・git フォルダを別の場所に置いた共有の checkout。
func gitFixtureCases() []VisCase {
	var cases []VisCase
	commit := func(f *fixtureCase, repo string) {
		f.git(repo, "commit", "-q", "--allow-empty", "-m", "init")
	}

	// 通常の checkout: 一覧が checkout の直下にあるとき・上のフォルダにあるとき・無いとき
	f := newFixtureCase("git-plain")
	f.git("", "init", "-q", "-b", "main", f.dir("repo"))
	f.write("repo/.yomiyasu-words/allow.txt", "x\n")
	f.mkdir("repo/sub/deep").mkdir("noroot")
	f.git("", "init", "-q", "-b", "main", f.dir("nowords"))
	f.mkdir("nowords/sub")
	f.call(findFolder(f.dir("repo"), nil)).call(findFolder(f.dir("repo/sub/deep"), nil)).call(findFolder(f.dir("repo/sub/deep/"), nil)).
		call(findFolder(f.dir("repo/./sub/../sub/deep"), nil)).call(findFolder(f.dir("nowords"), nil)).call(findFolder(f.dir("nowords/sub"), nil)).
		call(findFolder(f.dir("repo/missing/dir"), nil)).call(findFolder(nil, nil))
	// 作業ディレクトリを cwd にした相対の cwd
	f.op(Op{Kind: "chdir", Path: f.dir("repo/sub")})
	f.call(findFolder("deep", nil)).call(findFolder(".", nil)).call(findFolder("..", nil)).call(findFolder("", nil)).call(findFolder("../..", nil))
	f.op(Op{Kind: "chdir", Path: RootToken})
	cases = append(cases, f.done())

	// linked worktree: 元の checkout に一覧があるとき・worktree 自身が持つとき・どちらも無いとき・worktree の下のフォルダから
	f = newFixtureCase("git-worktree")
	f.git("", "init", "-q", "-b", "main", f.dir("main"))
	commit(f, "main")
	f.write("main/.yomiyasu-words/flag.txt", "x\n")
	f.git("main", "worktree", "add", "-q", f.dir("wt1"), "-b", "wt1")
	f.git("main", "worktree", "add", "-q", f.dir("wt2"), "-b", "wt2")
	f.write("wt2/.yomiyasu-words/allow.txt", "own\n")
	f.mkdir("wt1/sub/deep")
	f.git("", "init", "-q", "-b", "main", f.dir("bare-main"))
	commit(f, "bare-main")
	f.git("bare-main", "worktree", "add", "-q", f.dir("wt3"), "-b", "wt3")
	f.call(findFolder(f.dir("main"), nil)).call(findFolder(f.dir("wt1"), nil)).call(findFolder(f.dir("wt1/sub/deep"), nil)).call(findFolder(f.dir("wt2"), nil)).
		call(findFolder(f.dir("wt3"), nil)).
		call(vkeep(vcall("resolve_words", f.dir("wt1"), nil, false))).call(vkeep(vcall("resolve_words", f.dir("wt1"), nil, true))).call(vkeep(vcall("resolve_words", f.dir("wt3"), nil, false)))
	cases = append(cases, f.done())

	// 共有の checkout: git フォルダを別の場所 (名前が .git でない) に置き、core.worktree で checkout を指す
	f = newFixtureCase("git-shared")
	f.mkdir("gitdirs")
	f.git("", "init", "-q", "-b", "main", "--separate-git-dir", f.dir("gitdirs/shared.git"), f.dir("co"))
	commit(f, "co")
	f.write("co/.yomiyasu-words/allow.txt", "shared\n")
	f.git("co", "worktree", "add", "-q", f.dir("swt"), "-b", "swt")
	f.mkdir("swt/sub")
	f.git("", "init", "-q", "-b", "main", "--separate-git-dir", f.dir("gitdirs/nowords.git"), f.dir("co2"))
	commit(f, "co2")
	f.git("co2", "worktree", "add", "-q", f.dir("swt2"), "-b", "swt2")
	f.call(findFolder(f.dir("co"), nil)).call(findFolder(f.dir("swt"), nil)).call(findFolder(f.dir("swt/sub"), nil)).call(findFolder(f.dir("co2"), nil)).call(findFolder(f.dir("swt2"), nil)).
		call(vkeep(vcall("resolve_words", f.dir("swt"), nil, false)))
	cases = append(cases, f.done())

	// git フォルダの core.worktree が checkout を指す形 (submodule の git フォルダ .git/modules/<名前> と同じ): 相対の値と絶対の値
	for _, form := range []struct{ name, value string }{{"rel", "../../co"}, {"abs", RootToken + "/git-core-worktree-abs/co"}} {
		f = newFixtureCase("git-core-worktree-" + form.name)
		f.mkdir("gitdirs")
		f.git("", "init", "-q", "-b", "main", "--separate-git-dir", f.dir("gitdirs/g.git"), f.dir("co"))
		commit(f, "co")
		f.git("gitdirs/g.git", "config", "core.worktree", form.value)
		f.write("co/.yomiyasu-words/allow.txt", "shared\n")
		f.git("co", "worktree", "add", "-q", f.dir("swt"), "-b", "swt")
		f.mkdir("swt/sub")
		f.call(findFolder(f.dir("co"), nil)).call(findFolder(f.dir("swt"), nil)).call(findFolder(f.dir("swt/sub"), nil)).call(vkeep(vcall("resolve_words", f.dir("swt/sub"), nil, false)))
		cases = append(cases, f.done())
	}
	return cases
}

type handVariant struct {
	name string
	// common は共有の git フォルダの置き場 (checkout の .git か、名前が .git でない gd)。
	commonIsDotGit bool
	config         string
	configWorktree string
	commondir      *string
	gitFile        string // 空なら "gitdir: <worktree の gitdir>"
	noCheckout     bool
}

func strPtr(s string) *string { return &s }

// handCases は git の出力の形を手で書いた fixture (config・config.worktree・commondir・.git ファイルの読み方)。
func handCases() []VisCase {
	checkout := RootToken + "/%s/co"
	variants := []handVariant{
		{name: "core-abs", config: "[core]\n\tworktree = " + checkout + "\n"},
		{name: "core-rel", config: "[core]\n\tworktree = ../co\n"},
		{name: "core-rel-dotdot-out", config: "[core]\n\tworktree = ../../co\n"},
		{name: "core-quoted", config: "[core]\n\tworktree = \"" + checkout + "\"\n"},
		{name: "core-upper", config: "[CORE]\n\tWorkTree=" + checkout + "\n"},
		{name: "core-kelvin", config: "[core]\n\twor\U0000212atree = " + checkout + "\n"},
		{name: "core-crlf", config: "[core]\r\n\tworktree = " + checkout + "\r\n"},
		{name: "core-bom", config: "\U0000feff[core]\n\tworktree = " + checkout + "\n"},
		{name: "core-comment", config: "; c\n# c\n[core]\n\t; worktree = /nope\n\tworktree = " + checkout + "\n"},
		{name: "core-last-wins", config: "[core]\n\tworktree = /nope\n\tworktree = " + checkout + "\n"},
		{name: "core-empty-value", config: "[core]\n\tworktree =\n"},
		{name: "core-empty-value-dotgit", commonIsDotGit: true, config: "[core]\n\tworktree =\n"},
		{name: "core-no-equals", config: "[core]\n\tworktree\n"},
		{name: "core-section-sub", config: "[core \"x\"]\n\tworktree = " + checkout + "\n"},
		{name: "core-section-other", config: "[remote]\n\tworktree = " + checkout + "\n[core]\n\tbare = true\n"},
		{name: "core-nonexistent", config: "[core]\n\tworktree = " + RootToken + "/nowhere\n"},
		{name: "core-bad-utf8", config: "[core]\n\tname = \xff\xfe\n\tworktree = " + checkout + "\n"},
		{name: "core-bad-utf8-in-value", config: "[core]\n\tworktree = " + checkout + "\xe3\x81\n"},
		{name: "wt-config", configWorktree: "[core]\n\tworktree = " + checkout + "\n"},
		{name: "wt-config-overrides", config: "[core]\n\tworktree = /nope\n", configWorktree: "[core]\n\tworktree = " + checkout + "\n"},
		{name: "wt-config-then-config", config: "[core]\n\tworktree = " + checkout + "\n", configWorktree: "[core]\n\tbare = true\n"},
		{name: "dotgit-plain", commonIsDotGit: true, config: "[core]\n\tbare = false\n"},
		{name: "dotgit-noconfig", commonIsDotGit: true},
		{name: "dotgit-bare-true", commonIsDotGit: true, config: "[core]\n\tbare = true\n"},
		{name: "dotgit-bare-yes", commonIsDotGit: true, config: "[core]\n\tbare = YES\n"},
		{name: "dotgit-bare-on", commonIsDotGit: true, config: "[core]\n\tbare = on\n"},
		{name: "dotgit-bare-one", commonIsDotGit: true, config: "[core]\n\tbare = 1\n"},
		{name: "dotgit-bare-false", commonIsDotGit: true, config: "[core]\n\tbare = false\n"},
		{name: "dotgit-bare-later-false", commonIsDotGit: true, config: "[core]\n\tbare = true\n\tbare = false\n"},
		{name: "dotgit-bare-kelvin", commonIsDotGit: true, config: "[core]\n\tbare = tru\U0000212a\n"},
		{name: "dotgit-worktree-overrides", commonIsDotGit: true, config: "[core]\n\tworktree = " + checkout + "\n\tbare = true\n"},
		{name: "nodotgit-nocore", config: "[user]\n\tname = x\n"},
		{name: "nodotgit-noconfig"},
		{name: "commondir-missing", config: "[core]\n\tworktree = " + checkout + "\n", commondir: nil},
		{name: "commondir-empty", config: "[core]\n\tworktree = " + checkout + "\n", commondir: strPtr("")},
		{name: "commondir-spaces", config: "[core]\n\tworktree = " + checkout + "\n", commondir: strPtr("  ../..  \n")},
		{name: "commondir-abs", config: "[core]\n\tworktree = " + checkout + "\n", commondir: strPtr(RootToken + "/%s/gd")},
		{name: "commondir-bad-utf8", config: "[core]\n\tworktree = " + checkout + "\n", commondir: strPtr("../..\xff")},
		{name: "gitfile-empty", config: "[core]\n\tworktree = " + checkout + "\n", gitFile: "gitdir:"},
		{name: "gitfile-garbage", config: "[core]\n\tworktree = " + checkout + "\n", gitFile: "hello"},
		{name: "gitfile-nocolon", config: "[core]\n\tworktree = " + checkout + "\n", gitFile: "gitdir " + RootToken + "/%s/gd/worktrees/w1"},
		{name: "gitfile-spaces", config: "[core]\n\tworktree = " + checkout + "\n", gitFile: "gitdir:   " + RootToken + "/%s/gd/worktrees/w1  \n\n"},
		{name: "gitfile-relative", config: "[core]\n\tworktree = " + checkout + "\n", gitFile: "gitdir: ../gd/worktrees/w1"},
		{name: "gitfile-bad-utf8", config: "[core]\n\tworktree = " + checkout + "\n", gitFile: "gitdir: \xff"},
		{name: "gitfile-nonexistent", config: "[core]\n\tworktree = " + checkout + "\n", gitFile: "gitdir: " + RootToken + "/nowhere"},
		{name: "nocheckout", config: "[core]\n\tbare = true\n", noCheckout: true},
	}
	var cases []VisCase
	for _, v := range variants {
		f := newFixtureCase("hand-" + v.name)
		gd := f.dir("gd")
		if v.commonIsDotGit {
			gd = f.dir("co/.git")
		}
		gitdir := gd + "/worktrees/w1"
		sub := func(s string) string { return strings.ReplaceAll(s, "%s", f.name) }
		if !v.noCheckout {
			f.write("co/.yomiyasu-words/allow.txt", "w\n")
		}
		f.op(Op{Kind: "mkdir", Path: gitdir})
		commondir := "../.."
		if v.commondir != nil {
			commondir = sub(*v.commondir)
		}
		if v.commondir != nil || !strings.HasPrefix(v.name, "commondir-missing") {
			f.op(Op{Kind: "write", Path: gitdir + "/commondir", Content: commondir})
		}
		if v.config != "" {
			f.op(Op{Kind: "write", Path: gd + "/config", Content: sub(v.config)})
		}
		if v.configWorktree != "" {
			f.op(Op{Kind: "write", Path: gd + "/config.worktree", Content: sub(v.configWorktree)})
		}
		gitFile := v.gitFile
		if gitFile == "" {
			gitFile = "gitdir: " + gitdir
		}
		f.op(Op{Kind: "write", Path: f.dir("wt/.git"), Content: sub(gitFile)})
		f.mkdir("wt/sub")
		f.call(findFolder(f.dir("wt"), nil)).call(findFolder(f.dir("wt/sub"), nil))
		cases = append(cases, f.done())
	}
	// .git がフォルダ・.git が無い・一覧が cwd にある
	f := newFixtureCase("hand-misc")
	f.mkdir("a/.git").mkdir("a/sub").mkdir("b/sub").write("b/.yomiyasu-words/allow.txt", "x\n").mkdir("c/.yomiyasu-words").mkdir("c/.git/.yomiyasu-words")
	f.write("d/.yomiyasu-words", "a file, not a folder\n").mkdir("d/sub").mkdir("e/.git").write("e/.yomiyasu-words/allow.txt", "inner\n").mkdir("e/sub/.git").mkdir("e/sub/deeper")
	f.write("g/.git", "gitdir: "+RootToken+"/nowhere\n").mkdir("g/sub")
	for _, p := range []string{"a", "a/sub", "b", "b/sub", "c", "d", "d/sub", "e", "e/sub", "e/sub/deeper", "g", "g/sub"} {
		f.call(findFolder(f.dir(p), nil))
	}
	cases = append(cases, f.done())
	return cases
}

// optionCases は --words の値の読み方 (相対・絶対・~・空・フォルダでない・cwd が読めない)。
func optionCases() []VisCase {
	f := newFixtureCase("option")
	f.write("repo/.yomiyasu-words/allow.txt", "x\n").mkdir("repo/sub").write("other/words/flag.txt", "y\n").write("home/words/allow.txt", "h\n").write("file.txt", "x").mkdir("plain")
	f.git("", "init", "-q", "-b", "main", f.dir("repo2"))
	opt := func(cwd any, option string) Call { return findFolder(cwd, option) }
	cwd := f.dir("repo/sub")
	for _, o := range []string{f.dir("other/words"), "../../other/words", "../../other/./words", "../..//other//words", "./../../other/words", "../.yomiyasu-words", ".", "..", "other", f.dir("file.txt"), "../../file.txt", "", " ", "\t", "\U00003000", f.dir("nowhere"), "nowhere", f.dir("other/words/"), f.dir("other/./words"), f.dir("other/../other/words"), "//" + strings.TrimPrefix(f.dir("other/words"), "/"), "\x00", f.dir("other\x00")} {
		f.call(opt(cwd, o))
	}
	for _, o := range []string{f.dir("other/words"), "other", "../x", ""} {
		f.call(opt(nil, o))
	}
	f.op(Op{Kind: "chdir", Path: cwd}).call(opt("", "../../other/words")).call(opt("..", "../other/words")).op(Op{Kind: "chdir", Path: RootToken})
	// ~ の展開 (HOME を変えて)
	home := f.dir("home")
	f.op(Op{Kind: "setenv", Name: "HOME", Value: home})
	for _, o := range []string{"~", "~/", "~/words", "~/words/", "~/./words", "~//words", "~/../home/words", "~x", "~x/words", "x~", "~\\words", "~ /words"} {
		f.call(opt(cwd, o))
	}
	for _, h := range []string{"", " ", "/", "//", "///", "/ ", " /", home + "/", home + "/.", home + "//", "/./", ".", "..", "home", "rel/home", "~", "~x", home + "/./", f.dir("nowhere"), "\U00003000" + home} {
		f.op(Op{Kind: "setenv", Name: "HOME", Value: h})
		f.call(opt(cwd, "~")).call(opt(cwd, "~/words")).call(opt(cwd, "~/")).call(opt(nil, "~/words"))
	}
	f.op(Op{Kind: "restoreenv"})
	return []VisCase{f.done()}
}

// wordsFileVariants は load_words に通す語の一覧のフォルダ 1 つ分の中身 (相対パス → 中身。中身が []byte のときはそのバイト列)。
func wordsFileVariants() []struct {
	name  string
	files map[string]any
} {
	manyLines := func(n int, line string) string { return strings.Repeat(line+"\n", n) }
	type V = struct {
		name  string
		files map[string]any
	}
	m := func(kv ...any) map[string]any {
		out := map[string]any{}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	return []V{
		{"empty-folder", m()},
		{"allow-only", m("allow.txt", "a\nb\n")},
		{"flag-only", m("flag.txt", "x\n")},
		{"patterns-only", m("flag-patterns.txt", "n1\tab\n")},
		{"all-three", m("allow.txt", "a\n", "flag.txt", "x\ty\n", "flag-patterns.txt", "n\tab\n")},
		{"empty-files", m("allow.txt", "", "flag.txt", "", "flag-patterns.txt", "")},
		{"comments-blank", m("allow.txt", "# c\n\n  \n a \n#b\n\t#c\n d\t\n\U00003000e\U00003000\n")},
		{"allow-bom-crlf", m("allow.txt", "\U0000feffa\r\nb\r\n\r\nc")},
		{"allow-cr", m("allow.txt", "a\rb\rc\r")},
		{"allow-mixed-eol", m("allow.txt", "a\r\nb\nc\rd")},
		{"allow-no-trailing-newline", m("allow.txt", "a\nb")},
		{"allow-ja", m("allow.txt", "\U00003059\U00003054\U00003044\n\U000030c7\U000030fc\U000030bf\n")},
		{"allow-10000", m("allow.txt", manyLines(10000, "a"))},
		{"allow-10001", m("allow.txt", manyLines(10001, "a"))},
		{"allow-10000-blank-tail", m("allow.txt", manyLines(10000, "a")+"\n")},
		{"allow-10001-blank-lines", m("allow.txt", manyLines(10001, ""))},
		{"allow-bad-utf8-first", m("allow.txt", []byte("\xff\nb\n"))},
		{"allow-bad-utf8-line3-lf", m("allow.txt", []byte("a\nb\n\xe3\x81\nd\n"))},
		{"allow-bad-utf8-line3-crlf", m("allow.txt", []byte("a\r\nb\r\n\xe3\x81\r\nd\r\n"))},
		{"allow-bad-utf8-line3-cr", m("allow.txt", []byte("a\rb\r\xe3\x81\rd\r"))},
		{"allow-bad-utf8-end", m("allow.txt", []byte("a\nb\n\xe3"))},
		{"allow-surrogate-bytes", m("allow.txt", []byte("a\n\xed\xa0\x80\n"))},
		{"flag-bad-utf8", m("allow.txt", "a\n", "flag.txt", []byte("x\n\xff"))},
		{"flag-replacements", m("flag.txt", "x\ty\n a \t b \nsame\tsame\n\tindented\nt\tu\nt\tv\nt\n#c\n")},
		{"flag-dup-replacement-then-none", m("flag.txt", "t\tu\nt\n")},
		{"flag-three-columns", m("flag.txt", "a\tb\tc\n")},
		{"flag-three-columns-trailing-tab", m("flag.txt", "a\tb\t\n")},
		{"flag-replacement-80", m("flag.txt", "a\t"+strings.Repeat("\U00003042", 80)+"\n")},
		{"flag-replacement-81", m("flag.txt", "a\t"+strings.Repeat("\U00003042", 81)+"\n")},
		{"flag-replacement-81-line3", m("flag.txt", "a\nb\nc\t"+strings.Repeat("x", 81)+"\n")},
		{"flag-tab-line-error-after-ok", m("flag.txt", "a\tb\nc\td\te\n")},
		{"flag-empty-word", m("flag.txt", "\t\n\tx\n")},
		{"flag-10001", m("flag.txt", manyLines(10001, "a"))},
		{"patterns-valid", m("flag-patterns.txt", "# c\n\nn1\tab\nn2\t[a-c]{1,3}x\n  n3  \t  \\d{2}x  \n")},
		{"patterns-fullwidth-space-kept", m("flag-patterns.txt", "n\tab\U00003000\n")},
		{"patterns-no-tab", m("flag-patterns.txt", "n1 ab\n")},
		{"patterns-no-tab-line2", m("flag-patterns.txt", "n1\tab\nn2 cd\n")},
		{"patterns-empty-name", m("flag-patterns.txt", "\tab\n")},
		{"patterns-space-name", m("flag-patterns.txt", "  \tab\n")},
		{"patterns-name-40", m("flag-patterns.txt", strings.Repeat("\U00003042", 40)+"\tab\n")},
		{"patterns-name-41", m("flag-patterns.txt", strings.Repeat("\U00003042", 41)+"\tab\n")},
		{"patterns-empty-pattern", m("flag-patterns.txt", "n\t\n")},
		{"patterns-space-pattern", m("flag-patterns.txt", "n\t \t\n")},
		{"patterns-bad-escape", m("flag-patterns.txt", "n\t\\x41\n")},
		{"patterns-bad-escape-line2", m("flag-patterns.txt", "ok\tab\nbad\ta*\n")},
		{"patterns-compile-error", m("flag-patterns.txt", "n\t(?<=a|bc)d\n")},
		{"patterns-class-nested", m("flag-patterns.txt", "n\t[[a]x\n")},
		{"patterns-tab-in-pattern", m("flag-patterns.txt", "n\ta\tb\n")},
		{"patterns-500", m("flag-patterns.txt", numberedPatterns(500, "ab"))},
		{"patterns-501", m("flag-patterns.txt", numberedPatterns(501, "ab"))},
		{"patterns-cost-512", m("flag-patterns.txt", "n1\ta{0,7}b{0,7}c{0,7}d\n")},
		{"patterns-cost-513-total", m("flag-patterns.txt", "n1\ta{0,7}b{0,7}c{0,7}d\nn2\te\n")},
		{"patterns-cost-total-over-line2", m("flag-patterns.txt", "n1\ta?b?c?d?e?f?g?h?i\nn2\tx|y|z|w\nn3\ta{0,7}b{0,7}c{0,7}d\n")},
		{"patterns-cost-single-over", m("flag-patterns.txt", "n1\ta{0,7}b{0,7}c{0,8}d\n")},
		{"patterns-bad-utf8", m("flag-patterns.txt", []byte("n\tab\xff\n"))},
		{"patterns-10001-lines", m("flag-patterns.txt", manyLines(10001, "#c"))},
		{"patterns-bom-crlf", m("flag-patterns.txt", "\U0000feffn\tab\r\nm\tcd\r\n")},
		{"patterns-lookbehind", m("flag-patterns.txt", "n\t(?<=ab)c\n")},
		{"patterns-japanese", m("flag-patterns.txt", "\U00003059\U00003054\U00003044\t\U00003059\U00003054\U00003044\n")},
		{"allow-is-dir", m("allow.txt/x", "a\n")},
		{"flag-is-dir", m("flag.txt/x", "a\n")},
		{"patterns-is-dir", m("flag-patterns.txt/x", "a\n")},
		{"allow-name-with-dir-later", m("allow.txt", "a\n", "flag.txt/x", "b\n")},
	}
}

func numberedPatterns(n int, body string) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "n%d\t%s\n", i, body)
	}
	return b.String()
}

func wordsCases() ([]VisCase, error) {
	cases := gitFixtureCases()
	cases = append(cases, handCases()...)
	cases = append(cases, optionCases()...)

	f := newFixtureCase("words-load")
	variants := wordsFileVariants()
	for _, v := range variants {
		f.mkdir(v.name)
		names := make([]string, 0, len(v.files))
		for n := range v.files {
			names = append(names, n)
		}
		sortStrings(names)
		for _, rel := range names {
			switch body := v.files[rel].(type) {
			case string:
				f.write(v.name+"/"+rel, body)
			case []byte:
				f.writeBytes(v.name+"/"+rel, body)
			}
		}
	}
	big := map[string]bool{"allow-10000": true, "allow-10000-blank-tail": true, "patterns-500": true, "flag-10001": true}
	for _, v := range variants {
		c := vcall("load_words", f.dir(v.name))
		if !big[v.name] {
			c = vkeep(c)
		}
		f.call(c)
	}
	// フォルダの字面の違い (末尾の /・. と ..・空・相対・存在しない・フォルダでない・長すぎる名前)
	for _, p := range []string{f.dir("allow-only") + "/", f.dir("allow-only/."), f.dir("x/../allow-only"), f.dir("./allow-only"), f.dir("allow-only//"), "//" + strings.TrimPrefix(f.dir("allow-only"), "/"), f.dir("nonexistent"), f.dir("allow-only/allow.txt"), "", "allow-only", f.dir(strings.Repeat("d", 300)), f.dir("allow-only") + "\x00"} {
		f.call(vkeep(vcall("load_words", p)))
	}
	f.op(Op{Kind: "chdir", Path: f.dir("all-three")}).call(vkeep(vcall("load_words", "."))).call(vkeep(vcall("load_words", ""))).call(vkeep(vcall("load_words", "../allow-only")))
	f.op(Op{Kind: "chdir", Path: RootToken})
	// シンボリックリンク: 輪・壊れたリンク・フォルダへのリンク
	f.mkdir("links")
	f.op(Op{Kind: "symlink", Path: f.dir("links/loop/allow.txt"), Target: "allow.txt"})
	f.op(Op{Kind: "symlink", Path: f.dir("links/broken/allow.txt"), Target: "nowhere"})
	f.op(Op{Kind: "symlink", Path: f.dir("links/ok/allow.txt"), Target: f.dir("allow-only/allow.txt")})
	f.op(Op{Kind: "symlink", Path: f.dir("links/dirlink"), Target: f.dir("all-three")})
	f.op(Op{Kind: "write", Path: f.dir("links/unreadable/allow.txt"), Content: "a\n"})
	f.op(Op{Kind: "chmod", Path: f.dir("links/unreadable/allow.txt"), Mode: modePtr(0)})
	f.op(Op{Kind: "write", Path: f.dir("links/unreadable-dir/allow.txt"), Content: "a\n"})
	f.op(Op{Kind: "chmod", Path: f.dir("links/unreadable-dir"), Mode: modePtr(0)})
	for _, p := range []string{"links/loop", "links/broken", "links/ok", "links/dirlink", "links/unreadable", "links/unreadable-dir"} {
		f.call(vkeep(vcall("load_words", f.dir(p))))
	}
	f.op(Op{Kind: "chmod", Path: f.dir("links/unreadable-dir"), Mode: modePtr(0o755)})
	// words_context と words_label (前の結果を Ref で渡す)
	for i, name := range []string{"all-three", "allow-only", "flag-only", "patterns-only", "empty-files", "patterns-valid"} {
		w, c := fmt.Sprintf("w%d", i), fmt.Sprintf("c%d", i)
		f.call(vsave(vkeep(vcall("load_words", f.dir(name))), w))
		f.call(vsave(vkeep(vcall("words_context", Ref(w))), c))
		f.call(vkeep(vcall("words_label", Ref(c))))
	}
	// resolve_words: 無効・--words・cwd から探す・cwd が無い
	f.call(vkeep(vcall("resolve_words", f.dir("all-three"), nil, false))).call(vkeep(vcall("resolve_words", f.dir("all-three"), nil, true))).
		call(vkeep(vcall("resolve_words", f.dir("all-three"), f.dir("allow-only"), false))).call(vkeep(vcall("resolve_words", f.dir("all-three"), f.dir("nonexistent"), false))).
		call(vkeep(vcall("resolve_words", nil, nil, false))).call(vkeep(vcall("resolve_words", nil, "x", false))).call(vkeep(vcall("resolve_words", f.dir("all-three"), f.dir("allow-bad-utf8-first"), false))).
		call(vkeep(vcall("resolve_words", f.dir("all-three"), f.dir("allow-bad-utf8-first"), true))).call(vkeep(vcall("resolve_words", f.dir("all-three"), "", false)))
	cases = append(cases, f.done())
	return cases, nil
}

func modePtr(m uint32) *uint32 { return &m }

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// pathCases は absolute_path・input_path_problem・current_directory (作業フォルダで chdir して、相対パスと symlink を通す)。
func pathCases() []VisCase {
	f := newFixtureCase("paths")
	f.write("cwd/a.txt", "a").write("cwd/sub/b.txt", "b").write("cwd/.hidden", "h").write("cwd/.d/x.txt", "x").write("cwd/sub/.e/y.txt", "y").write("cwd/sub/.f", "z").write("outside/o.txt", "o").write("cwd/.git/config", "g")
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/link-out"), Target: f.dir("outside")})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/link-in"), Target: f.dir("cwd/sub")})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/link-hidden"), Target: f.dir("cwd/.d")})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/link-rel"), Target: "sub/b.txt"})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/loop"), Target: "loop"})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/loop-a"), Target: "loop-b"})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/loop-b"), Target: "loop-a"})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/dangling"), Target: "nowhere"})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/up"), Target: ".."})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/dotlink"), Target: ".d/x.txt"})
	f.op(Op{Kind: "symlink", Path: f.dir("cwd/sub/back"), Target: "../a.txt"})
	f.op(Op{Kind: "symlink", Path: f.dir("alias"), Target: f.dir("cwd")})
	f.op(Op{Kind: "chdir", Path: f.dir("cwd")})
	f.call(vkeep(vcall("current_directory")))
	inputs := []string{"-", "a.txt", "./a.txt", "sub/b.txt", "sub/../a.txt", "sub/./b.txt", "missing.txt", "sub/missing/x", "a.txt/x", ".hidden", "./.hidden", ".d/x.txt", "sub/.e/y.txt", "sub/.f", ".git/config", ".", "./", "..", "../outside/o.txt", "../cwd/a.txt", "../cwd/.hidden", "..//cwd//a.txt", "", " ", "a.txt ", "link-out/o.txt", "link-out", "link-in/b.txt", "link-hidden/x.txt", "link-rel", "loop", "loop/x", "loop-a", "loop-a/x", "dangling", "dangling/x", "up/cwd/a.txt", "up/outside/o.txt", "dotlink", "sub/back", f.dir("cwd/a.txt"), f.dir("cwd/.hidden"), f.dir("cwd"), f.dir("cwd") + "/", f.dir("outside/o.txt"), f.dir("alias/a.txt"), f.dir("alias/.hidden"), strings.Replace(f.dir("cwd/a.txt"), "/cwd/", "/CWD/", 1), strings.Replace(f.dir("cwd/.hidden"), "/cwd/", "/CWD/", 1), strings.Replace(f.dir("cwd/sub/b.txt"), "/paths/", "/PATHS/", 1), f.dir("alias"), "/", "/etc/hosts", "/nonexistent/x", "//", "a\x00b", "\x00", "-a", "--", "~", "~/a.txt", "\U00003042.txt", "sub//b.txt", "sub/", "sub/b.txt/", "a.txt/", "..a", "...", "sub/..e"}
	for _, in := range inputs {
		f.call(vkeep(vcall("input_path_problem", in)))
	}
	abs := []string{"", ".", "..", "a", "./a/../b", "/", "//", "///a", "//a//b/../c", "a/", "/a/b/", "~", "a\x00b", "../..", "/..", "/../a", "//..", "a/./b//c/../d", "\U00003042/\U00003044", "  ", ".hidden", "..."}
	for _, in := range abs {
		f.call(vkeep(vcall("absolute_path", in)))
	}
	f.op(Op{Kind: "chdir", Path: f.dir("cwd/sub")})
	f.call(vkeep(vcall("current_directory")))
	for _, in := range []string{"b.txt", "../a.txt", ".e/y.txt", "../.hidden", "back", "../../outside/o.txt", "..", "../.."} {
		f.call(vkeep(vcall("input_path_problem", in))).call(vkeep(vcall("absolute_path", in)))
	}
	// cwd が symlink の先: 実パスでの比較
	f.op(Op{Kind: "chdir", Path: f.dir("alias")})
	f.call(vkeep(vcall("current_directory")))
	for _, in := range []string{"a.txt", ".hidden", "sub/b.txt", "../outside/o.txt", f.dir("alias/a.txt"), f.dir("cwd/a.txt"), f.dir("alias/.d/x.txt"), "link-out/o.txt"} {
		f.call(vkeep(vcall("input_path_problem", in))).call(vkeep(vcall("absolute_path", in)))
	}
	// cwd が消えたとき
	f.mkdir("gone")
	f.op(Op{Kind: "chdir", Path: f.dir("gone")}).op(Op{Kind: "remove", Path: f.dir("gone")})
	f.call(vkeep(vcall("current_directory"))).call(vkeep(vcall("input_path_problem", "a.txt"))).call(vkeep(vcall("input_path_problem", "-"))).call(vkeep(vcall("input_path_problem", f.dir("cwd/a.txt")))).
		call(vkeep(vcall("input_path_problem", "a\x00b"))).call(vkeep(vcall("absolute_path", "a"))).call(vkeep(vcall("absolute_path", "/a/b/../c"))).
		call(vkeep(vcall("find_words_folder", nil, nil))).call(vkeep(vcall("resolve_words", nil, nil, false)))
	f.op(Op{Kind: "chdir", Path: RootToken})
	return []VisCase{f.done()}
}
