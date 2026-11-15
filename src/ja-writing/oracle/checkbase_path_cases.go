package oracle

import "strings"

// パスと状態フォルダの場所 (_getcwd・_abspath・_disk_path・_case_insensitive・_place・_same_path・_same_file・_home_path・state_dir_for・_state_dir・_state_dir_given) の case。
// 大文字と小文字を区別するファイルシステムは、手元に無いので記録に入らない (本家の _case_insensitive が偽を返す枝は、作業フォルダの外の "/" と数字だけのパスで通す)。

func chdir(f *fixtureCase, rel ...string) { f.op(Op{Kind: "chdir", Path: f.dir(rel...)}) }

func setenv(f *fixtureCase, name, value string) { f.op(Op{Kind: "setenv", Name: name, Value: value}) }

func unsetenv(f *fixtureCase, name string) { f.op(Op{Kind: "unsetenv", Name: name}) }

func checkPathCases() []VisCase {
	cases := checkRunIDCaseCases()
	cases = append(cases, checkAbsPathCase(), checkDiskPathCase(), checkGoneCwdCase(), checkHomePathCase(), checkStateDirCase(), checkStateDirEnvCase())
	return cases
}

func checkAbsPathCase() VisCase {
	f := newFixtureCase("path-abs")
	f.mkdir("sub")
	f.write("sub/file.txt", "x")
	chdir(f)
	f.call(ckeep(ccall("_getcwd")))
	for _, p := range []string{
		"x", "./x", "sub/../x", "sub//file.txt", "/abs/a/../b", "//double/x", "///triple/x", "/", "", ".", "..", "sub/", "/abs/", "~/x", "a/./b/../c", "sub/..", "/a/b/..", "/../x", "x/../../y",
		f.dir("sub") + "/..", "日本/語", "a b", "sub/./file.txt", "./", "a//", "/a//b///c", "//", "/./x",
	} {
		f.call(ckeep(ccall("_abspath", p)))
	}
	return f.done()
}

func checkDiskPathCase() VisCase {
	f := newFixtureCase("path-disk")
	f.mkdir("Dir")
	f.write("Dir/MixedCase.TXT", "x")
	f.write("Dir/Secret.TXT", "x")
	nfd := "Cafe\u0301.txt"
	f.write("Dir/"+nfd, "x")
	f.write("Dir/ÄÖÜ.txt", "x")
	f.write("Dir/Straße.TXT", "x")
	f.write("Dir/digits123", "x")
	f.write("12345", "x")
	f.op(Op{Kind: "symlink", Path: f.dir("link"), Target: f.dir("Dir/MixedCase.TXT")})
	f.op(Op{Kind: "symlink", Path: f.dir("dlink"), Target: f.dir("Dir")})
	f.op(Op{Kind: "symlink", Path: f.dir("rel-link"), Target: "Dir/MixedCase.TXT"})
	f.op(Op{Kind: "symlink", Path: f.dir("dangling"), Target: f.dir("NoSuch/File.TXT")})
	f.op(Op{Kind: "symlink", Path: f.dir("loopA"), Target: f.dir("loopB")})
	f.op(Op{Kind: "symlink", Path: f.dir("loopB"), Target: f.dir("loopA")})
	zero, back := uint32(0), uint32(0o644)
	f.op(Op{Kind: "chmod", Path: f.dir("Dir/Secret.TXT"), Mode: &zero})
	chdir(f)
	paths := []string{
		f.dir("Dir/MixedCase.TXT"), f.dir("dir/mixedcase.txt"), f.dir("DIR/MIXEDCASE.TXT"), f.dir("Dir/../Dir/MixedCase.TXT"), f.dir("Dir/./MixedCase.TXT"), f.dir("Dir//MixedCase.TXT"),
		f.dir("link"), f.dir("rel-link"), f.dir("dlink/MixedCase.TXT"), f.dir("DLINK/mixedcase.txt"), f.dir("Dir"), f.dir("dir"), f.dir("dir/"), f.dir("DIR"),
		f.dir("Dir/" + nfd), f.dir("Dir/Café.txt"), f.dir("dir/CAFÉ.TXT"), f.dir("Dir/äöü.txt"), f.dir("Dir/ÄÖÜ.TXT"), f.dir("Dir/STRASSE.txt"), f.dir("Dir/Straße.TXT"), f.dir("Dir/straße.txt"),
		f.dir("Dir/Secret.TXT"), f.dir("dir/secret.txt"), f.dir("Dir/digits123"), f.dir("12345"),
		f.dir("nope/File"), f.dir("Dir/Missing.TXT"), f.dir("dir/missing.txt"), f.dir("Dir/MixedCase.TXT/extra"), f.dir("dangling"), f.dir("loopA"), f.dir("loopA/x"),
		"/", "/nonexistent-dir-xyz/Q", "/tmp", "/Tmp", "/private", "/PRIVATE/Var",
		"Dir/MixedCase.TXT", "dir/mixedcase.txt", ".", "./Dir", "..",
	}
	for _, p := range paths {
		// シンボリックリンクの輪は stat が失敗して大文字小文字の判定が偽になり、casefold されない (作業フォルダの綴りがそのまま残る)
		fold := func(c Call) Call {
			if strings.Contains(p, "loop") {
				return c
			}
			return cfold(c)
		}
		f.call(ckeep(fold(ccall("_disk_path", p))))
		f.call(ckeep(fold(ccall("_place", p))))
		f.call(ckeep(ccall("_case_insensitive", p)))
	}
	for _, p := range []string{"/", "/1234", "/nonexistent-dir-xyz/Q", f.dir("12345"), f.dir("nope/12345"), f.dir("Dir/digits123"), "", "x"} {
		f.call(ckeep(ccall("_case_insensitive", p)))
	}
	pairs := [][2]string{
		{f.dir("Dir/MixedCase.TXT"), f.dir("dir/mixedcase.txt")}, {f.dir("link"), f.dir("Dir/MixedCase.TXT")}, {f.dir("link"), f.dir("DIR/mixedcase.TXT")}, {f.dir("dlink/MixedCase.TXT"), f.dir("Dir/MixedCase.TXT")},
		{f.dir("Dir/MixedCase.TXT"), f.dir("Dir/Secret.TXT")}, {f.dir("Dir/" + nfd), f.dir("Dir/Café.txt")}, {f.dir("nope/a"), f.dir("nope/a")}, {f.dir("nope/a"), f.dir("nope/b")},
		{f.dir("nope/A"), f.dir("nope/a")}, {f.dir("Dir"), f.dir("Dir/")}, {f.dir("Dir"), f.dir("Dir/.")}, {f.dir("Dir"), f.dir("dlink")}, {f.dir("Dir/MixedCase.TXT"), "Dir/mixedcase.txt"},
		{"Dir/MixedCase.TXT", "dir/MixedCase.txt"}, {f.dir("dangling"), f.dir("NoSuch/File.TXT")}, {f.dir("loopA"), f.dir("loopB")}, {"/", "/."}, {"/tmp", "/private/tmp"}, {"", "."},
	}
	for _, p := range pairs {
		f.call(ckeep(ccall("_same_path", p[0], p[1])))
		f.call(ckeep(ccall("_same_file", p[0], p[1])))
	}
	f.op(Op{Kind: "chmod", Path: f.dir("Dir/Secret.TXT"), Mode: &back})
	return f.done()
}

// checkGoneCwdCase は作業ディレクトリを消した後の呼び出し。絶対パスの引数は通り、作業ディレクトリを読む呼び出しは誤りになる。
func checkGoneCwdCase() VisCase {
	f := newFixtureCase("path-gone-cwd")
	f.mkdir("gone")
	f.mkdir("keep")
	chdir(f, "gone")
	f.op(Op{Kind: "remove", Path: f.dir("gone")})
	f.call(ckeep(ccall("_getcwd")))
	for _, p := range []string{"x", "", ".", "/x/../y", f.dir("keep") + "/../z", "//a"} {
		f.call(ckeep(ccall("_abspath", p)))
	}
	for _, p := range []string{"x", "/x", f.dir("keep"), f.dir("keep/missing")} {
		f.call(ckeep(cfold(ccall("_disk_path", p))))
		f.call(ckeep(cfold(ccall("_place", p))))
	}
	f.call(ckeep(ccall("_same_path", "/a", "/a")))
	f.call(ckeep(ccall("_same_path", "a", "/a")))
	f.call(ckeep(ccall("_same_file", "/a", "/a")))
	f.call(ckeep(ccall("_same_file", f.dir("keep"), f.dir("keep")+"/")))
	f.call(ckeep(ccall("_state_dir", nil)))
	f.call(ckeep(ccall("_state_dir", "")))
	f.call(ckeep(ccall("_state_dir", "/abs/./st/../x")))
	f.call(ckeep(ccall("_state_dir", "rel")))
	f.call(ckeep(ccall("_state_dir", f.dir("keep/st"))))
	f.call(ckeep(ccall("state_dir_for", f.dir("keep"), nil)))
	f.call(ckeep(ccall("state_dir_for", f.dir("keep"), "opt")))
	f.call(ckeep(ccall("state_dir_for", "rel", nil)))
	f.call(ckeep(ccall("state_dir_for", "rel", "opt")))
	f.call(ckeep(ccall("state_dir_for", "/", nil)))
	return f.done()
}

func checkHomePathCase() VisCase {
	f := newFixtureCase("path-home")
	f.mkdir("home")
	chdir(f)
	values := []string{"~", "~/", "~/a", "~/a//b/.", "~/a/../b", "~/日本語", "~user/x", "~x", "x~/y", "", "a", "/abs", "~~", "~/ ", " ~/a", "~ /a", "~\\a", "./~/a"}
	homes := []string{f.dir("home"), "/", "", "  ", "\t/\n", "~weird", "~", f.dir("home") + "//", f.dir("home") + "/./sub/", "/a//b/", "/home dir/with space", "relative/home", "/日本/語", "///", "//x"}
	for _, h := range homes {
		setenv(f, "HOME", h)
		for _, v := range values {
			f.call(ckeep(ccall("_home_path", v)))
		}
	}
	return f.done()
}

func checkStateDirCase() VisCase {
	f := newFixtureCase("path-state")
	f.mkdir("home")
	f.mkdir("repo1/.git")
	f.mkdir("repo1/sub/deep")
	f.write("repo2/.git", "gitdir: ../elsewhere\n")
	f.mkdir("repo2/wt")
	f.mkdir("norepo/a/b")
	f.op(Op{Kind: "symlink", Path: f.dir("repo3/.git"), Target: f.dir("missing-git")})
	f.mkdir("repo3/x")
	f.mkdir("outer/.git")
	f.mkdir("outer/inner/.git")
	f.mkdir("outer/inner/x")
	f.mkdir("日本語/.git")
	f.mkdir("日本語/下")
	f.mkdir("with space/.git")
	setenv(f, "HOME", f.dir("home"))
	chdir(f)
	cwds := []string{f.dir("repo1"), f.dir("repo1/sub/deep"), f.dir("repo1/sub/deep/"), f.dir("repo1/sub/../sub"), f.dir("repo2/wt"), f.dir("norepo/a/b"), f.dir("norepo"), f.dir("repo3/x"), f.dir("outer/inner/x"), f.dir("outer/inner"), f.dir("outer"),
		f.dir("日本語/下"), f.dir("with space"), f.dir("repo1/missing/deeper"), "repo1/sub", "repo1", ".", "", "/", f.dir("repo1") + "//sub", "norepo/a/b/.."}
	for _, cwd := range cwds {
		f.call(ckeep(ccall("state_dir_for", cwd, nil)))
	}
	options := []any{"", "custom", "/abs/st", "~/st", "~", "~/", "~x/st", "./st", "../st", "a//b", "a/./b/", "/abs/../x", "日本語", " ", "with space", f.dir("outer/st"), "${X}/st", "~/a/../b"}
	for _, opt := range options {
		for _, cwd := range []string{f.dir("repo1/sub/deep"), f.dir("norepo/a/b"), "repo1/sub"} {
			f.call(ckeep(ccall("state_dir_for", cwd, opt)))
		}
		f.call(ckeep(ccall("_state_dir_given", opt)))
	}
	// _state_dir と _state_dir_given は作業ディレクトリが決める
	for _, rel := range []string{"repo1/sub/deep", "norepo/a/b", "outer/inner/x", "repo3/x", "日本語/下"} {
		chdir(f, rel)
		f.call(ckeep(ccall("_state_dir", nil)))
		f.call(ckeep(ccall("_state_dir_given", nil)))
		for _, opt := range options {
			f.call(ckeep(ccall("_state_dir", opt)))
		}
	}
	return f.done()
}

// checkStateDirEnvCase は環境変数 YOMIYASU_STATE_DIR の値ごとの状態フォルダ。空白だけと、展開されなかった ${…} を含む値は無いものとして扱う。
func checkStateDirEnvCase() VisCase {
	f := newFixtureCase("path-state-env")
	f.mkdir("home")
	f.mkdir("repo/.git")
	f.mkdir("repo/sub")
	setenv(f, "HOME", f.dir("home"))
	chdir(f, "repo/sub")
	envs := []string{"envstate", "  ", "", "\t\n", "${CLAUDE_PROJECT_DIR}/x", "x${", "$HOME/x", "~/e", "~", "/abs/env", " keepspaces ", "a//b", "../up", "日本語", "/abs/./env/../x", "{HOME}"}
	for _, e := range envs {
		setenv(f, stateDirEnvName, e)
		f.call(ckeep(ccall("_state_dir_given", nil)))
		f.call(ckeep(ccall("_state_dir_given", "")))
		f.call(ckeep(ccall("_state_dir_given", "opt")))
		f.call(ckeep(ccall("_state_dir", nil)))
		f.call(ckeep(ccall("_state_dir", "opt")))
		f.call(ckeep(ccall("state_dir_for", f.dir("repo/sub"), nil)))
		f.call(ckeep(ccall("state_dir_for", f.dir("repo/sub"), "opt")))
		f.call(ckeep(ccall("state_dir_for", "x", nil)))
	}
	unsetenv(f, stateDirEnvName)
	f.call(ckeep(ccall("_state_dir_given", nil)))
	f.call(ckeep(ccall("_state_dir", nil)))
	return f.done()
}

const stateDirEnvName = "YOMIYASU_STATE_DIR"
