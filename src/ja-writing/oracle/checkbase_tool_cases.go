package oracle

// 記録の tools の欄 (_tool_versions・_list_sha256・_skill_info) の case。
// 道具と一覧を読む場所は本家の module の _HERE で、builtins.setattr で作業フォルダの中の置き場に差し替えて走らせる (走らせた後は元に戻す)。
// 差し替える前の本家は、固定した commit の本物の道具と一覧を読む (Go の Released の値と同じになる)。

const (
	toolHere = "_HERE"
)

// toolFiles は fixture の道具 4 本の名前。
var toolFileNames = []string{"yomiyasu_check.py", "yomiyasu_lint.py", "yomiyasu_diff.py", "markdown_visibility.py"}

func checkToolCases() []VisCase {
	cases := []VisCase{checkToolRealCase()}
	cases = append(cases, checkToolFixtureCases()...)
	return cases
}

func checkToolRealCase() VisCase {
	tools := rawJSON(`{"references/lists/hedges.txt":{"version":null,"sha256":"abc"},"references/lists/feelings.txt":{"version":null,"sha256":null},` +
		`"references/lists/empty.txt":{},"scalar":5,"list":[1],"nothing":null,"text":"s"}`)
	calls := []Call{
		ckeep(ccall("_tool_versions")), ckeep(ccall("_tool_versions", false)), ckeep(ccall("_tool_versions", true)), ckeep(ccall("_skill_info")),
	}
	for _, key := range []string{"references/lists/hedges.txt", "references/lists/feelings.txt", "references/lists/empty.txt", "scalar", "list", "nothing", "text", "missing", ""} {
		calls = append(calls, ckeep(ccall("_list_sha256", tools, key)))
	}
	calls = append(calls, ckeep(ccall("_list_sha256", rawJSON(`{}`), "references/lists/hedges.txt")))
	return VisCase{Name: "check/tools-real", Calls: calls}
}

// toolsCase は _HERE を f の scripts フォルダに差し替えて calls を走らせる case にする。
func toolsCase(f *fixtureCase, calls ...Call) VisCase {
	mod, here := Ref("mod@"+f.name), Ref("here@"+f.name)
	f.call(Call{Module: "builtins", Function: "__import__", Args: []any{CheckModule}, Save: "mod@" + f.name, Quiet: true, Digest: true})
	f.call(Call{Module: CheckModule, Function: toolHere, Get: true, Save: "here@" + f.name, Quiet: true, Digest: true})
	f.call(Call{Module: "builtins", Function: "setattr", Args: []any{mod, toolHere, f.dir("scripts")}, Digest: true})
	for _, c := range calls {
		f.call(c)
	}
	f.call(Call{Module: "builtins", Function: "setattr", Args: []any{mod, toolHere, here}, Digest: true})
	return f.done()
}

func toolsFixture(name string, tools map[string]string, lists map[string]string) *fixtureCase {
	f := newFixtureCase(name)
	f.mkdir("scripts")
	f.mkdir("references/lists")
	for n, content := range tools {
		f.write("scripts/"+n, content)
	}
	for n, content := range lists {
		f.write("references/lists/"+n+".txt", content)
	}
	return f
}

func toolsAll(version string) map[string]string {
	m := map[string]string{}
	for _, n := range toolFileNames {
		m[n] = "\"\"\"" + n + "\"\"\"\n__version__ = \"" + version + n[:3] + "\"\n"
	}
	return m
}

func toolsLists(names ...string) map[string]string {
	m := map[string]string{}
	for _, n := range names {
		m[n] = n + "\n日本語\n"
	}
	return m
}

var allListNames = []string{"nominalization", "intensifiers", "feelings", "hedges", "katakana-terms", "thin-evaluation", "renyo-nouns", "fact-gaps"}

func checkToolFixtureCases() []VisCase {
	versions := func() []Call {
		return []Call{ckeep(ccall("_tool_versions")), ckeep(ccall("_tool_versions", true)), ckeep(ccall("_skill_info"))}
	}
	var cases []VisCase
	cases = append(cases, toolsCase(toolsFixture("tools-full", toolsAll("1."), toolsLists(allListNames...)), versions()...))

	missing := toolsAll("2.")
	delete(missing, "yomiyasu_check.py")
	cases = append(cases, toolsCase(toolsFixture("tools-missing", missing, toolsLists("nominalization", "intensifiers", "feelings", "hedges", "katakana-terms", "thin-evaluation")), versions()...))

	// バージョンの行の読み方: 引用符・空白・行の位置・同じ行が 2 つ・字下げ・行末の改行 (CRLF)・行末の注釈・空の値・引用符の不揃い
	forms := map[string]string{
		"yomiyasu_check.py":      "__version__ = '2.0'\n",
		"yomiyasu_lint.py":       "x = 1\n__version__   =   \"3\"  \n",
		"yomiyasu_diff.py":       "no version here\n    __version__ = \"indented\"\n",
		"markdown_visibility.py": "    __version__ = \"indented\"\n__version__ = \"first\"\n__version__ = \"second\"\n",
	}
	cases = append(cases, toolsCase(toolsFixture("tools-version-forms", forms, toolsLists(allListNames...)), versions()...))
	edge := map[string]string{
		"yomiyasu_check.py":      "__version__ = \"crlf\"\r\nz = 1\r\n",
		"yomiyasu_lint.py":       "__version__ = \"c\" # comment\n__version__ = \"\"\n__version__ = \"a'\n",
		"yomiyasu_diff.py":       "\ufeff__version__ = \"bom\"\n__VERSION__ = \"upper\"\n__version__ = \"日本 語\"\n",
		"markdown_visibility.py": "__version__ = \"a b\"\t\n",
	}
	cases = append(cases, toolsCase(toolsFixture("tools-version-edge", edge, toolsLists(allListNames...)), versions()...))
	bad := toolsFixture("tools-version-bytes", toolsAll("3."), toolsLists(allListNames...))
	bad.op(Op{Kind: "write", Path: bad.dir("scripts/yomiyasu_check.py"), BytesB64: "/19fdmVyc2lvbl9fID0gIngiCl9fdmVyc2lvbl9fID0gInkiCg=="})
	cases = append(cases, toolsCase(bad, versions()...))

	// 読めない・フォルダ・無い
	f := toolsFixture("tools-unreadable", toolsAll("4."), toolsLists(allListNames...))
	zero := uint32(0)
	back := uint32(0o644)
	f.op(Op{Kind: "chmod", Path: f.dir("scripts/yomiyasu_diff.py"), Mode: &zero})
	f.op(Op{Kind: "chmod", Path: f.dir("references/lists/feelings.txt"), Mode: &zero})
	f.op(Op{Kind: "remove", Path: f.dir("scripts/yomiyasu_lint.py")})
	f.mkdir("scripts/yomiyasu_lint.py")
	f.op(Op{Kind: "remove", Path: f.dir("references/lists/hedges.txt")})
	f.mkdir("references/lists/hedges.txt")
	c := toolsCase(f, versions()...)
	c.Calls = append([]Call{}, c.Calls...)
	// 読めなくした 2 つのファイルを戻す (後始末の rmtree のため)。restoreenv の前に入れる
	restore := []Call{vop(Op{Kind: "chmod", Path: f.dir("scripts/yomiyasu_diff.py"), Mode: &back}), vop(Op{Kind: "chmod", Path: f.dir("references/lists/feelings.txt"), Mode: &back})}
	last := len(c.Calls) - 1
	c.Calls = append(append(append([]Call{}, c.Calls[:last]...), restore...), c.Calls[last])
	cases = append(cases, c)

	notDir := newFixtureCase("tools-references-is-file")
	notDir.mkdir("scripts")
	for n, content := range toolsAll("5.") {
		notDir.write("scripts/"+n, content)
	}
	notDir.write("references", "file")
	cases = append(cases, toolsCase(notDir, versions()...))

	noLists := newFixtureCase("tools-no-lists")
	noLists.mkdir("scripts")
	noLists.mkdir("references")
	for n, content := range toolsAll("6.") {
		noLists.write("scripts/"+n, content)
	}
	cases = append(cases, toolsCase(noLists, versions()...))

	// scripts フォルダが無い: 道具は null・null、一覧は ../references の手前で ENOENT になり「無い」
	noScripts := newFixtureCase("tools-no-scripts")
	noScripts.mkdir("references/lists")
	for n, content := range toolsLists(allListNames...) {
		noScripts.write("references/lists/"+n+".txt", content)
	}
	cases = append(cases, toolsCase(noScripts, versions()...))
	return cases
}
