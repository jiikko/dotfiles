package oracle

import (
	"encoding/base64"
	"strings"
)

// 入力の読み (_read_stdin_bytes・_read_named・_path_problem・_read_inputs・_read_target と、標準入力と資料を区切りで分ける
// _split_stdin・_material_marker) の case。_Input と _Target は objects で JSON にして比べる。

// cinput は戻り値に _Input・_Target・bytes を含む呼び出し。
func cinput(fn string, args ...any) Call {
	c := ckeep(ccall(fn, args...))
	c.Objects = true
	return c
}

func setStdin(f *fixtureCase, data string) {
	f.op(Op{Kind: "stdin", BytesB64: base64.StdEncoding.EncodeToString([]byte(data))})
}

func noStdin(f *fixtureCase) { f.op(Op{Kind: "stdin", Name: "none"}) }

// ns は _read_inputs に渡す引数 (argparse の結果と同じ属性)。source と materials は空なら None。
func ns(stdin bool, final string, source string, materials ...string) Namespace {
	n := Namespace{"stdin": stdin, "final": []string{final}, "source": nil, "materials": nil}
	if source != "" {
		n["source"] = []string{source}
	}
	if len(materials) > 0 {
		n["materials"] = materials
	}
	return n
}

// inputFiles は読ませるファイル (名前 → 中身)。
func inputFiles() [][2]string {
	long := strings.Repeat("長", 200)
	return [][2]string{
		{"src.md", "原文です。\n"},
		{"fin.md", "最終稿です。\r\n2 行目\r\n"},
		{"bom.md", "\ufeff本文\r本文\r"},
		{"empty.md", ""},
		{"mixed.md", "a\nb\r\nc\rd"},
		{"bad.md", "ok\xff\n"},
		{"surrogate.md", "\xed\xa0\x80\n"},
		{"mat-plain.md", "資料の本文\n"},
		{"mat-split.md", "<<<yomiyasu:material 出典A>>>\nA の本文\n<<<yomiyasu:material>>>\r\nB の本文\r\n"},
		{"mat-blank-first.md", "\n \t\n<<<yomiyasu:material x>>>\nA\n\n<<<yomiyasu:material y>>>"},
		{"mat-bom.md", "\ufeff<<<yomiyasu:material 出典>>>\nA\n"},
		{"mat-later.md", "先頭の本文\n<<<yomiyasu:material a>>>\nb\n"},
		{"mat-bad.md", "本文\n  <<<YOMIYASU:material>>>\n"},
		{"mat-bad-space.md", "\u3000<<<yomiyasu:material a>>>\nx\n"},
		{"mat-label-200.md", "<<<yomiyasu:material " + long + ">>>\nx\n"},
		{"mat-label-201.md", "<<<yomiyasu:material " + long + "長>>>\nx\n"},
		{"mat-label-arrows.md", "<<<yomiyasu:material a>>>b>>>\nx\n"},
		{"mat-cr.md", "<<<yomiyasu:material a>>>\rA\r<<<yomiyasu:material \tb c >>> \t\rB"},
		{"mat-tab.md", "<<<yomiyasu:material\ta b>>>\nx\n"},
		{"mat-only-marker.md", "<<<yomiyasu:material a>>>\n"},
		{"mat-bad-long.md", "<<<yomiyasu:" + strings.Repeat("あ", 70) + "\n"},
		{"mat-nel.md", "\u0085<<<yomiyasu:material>>>\nx\n"},
		{"mat-vt.md", "<<<yomiyasu:material a>>>\x0b\nx\n"},
		{"dir/x.md", "x"},
		{".hidden.md", "隠れ"},
		{"locked.md", "x"},
	}
}

func checkInputCases() []VisCase {
	return []VisCase{checkInputNamedCase(), checkInputStdinCase(), checkInputArgsCase(), checkInputGoneCase()}
}

// checkInputNamedCase はファイルを 1 つずつ読む (_read_named・_read_target・_path_problem)。
func checkInputNamedCase() VisCase {
	f := newFixtureCase("input-named")
	zero, back := uint32(0), uint32(0o644)
	files := inputFiles()
	for _, file := range files {
		f.write(file[0], file[1])
	}
	f.op(Op{Kind: "chmod", Path: f.dir("locked.md"), Mode: &zero})
	f.op(Op{Kind: "symlink", Path: f.dir("link.md"), Target: f.dir("src.md")})
	f.op(Op{Kind: "symlink", Path: f.dir("out.md"), Target: "/etc/hosts"})
	chdir(f)
	names := []string{"missing.md", "dir", "src.md/", "locked.md", "link.md", "/dev/null", f.dir("src.md")}
	for _, file := range files {
		names = append(names, file[0])
	}
	stdin := Bytes("標準入力\r\n")
	for _, name := range names {
		for _, role := range []string{"source", "material", "final"} {
			f.call(cinput("_read_named", role, 0, name, stdin))
		}
		f.call(cinput("_read_target", name, stdin))
	}
	for _, data := range []string{"標準入力\r\n", "", "\xff", "\ufeffx", "\xed\xa0\x80"} {
		f.call(cinput("_read_named", "final", 0, "-", Bytes(data)))
		f.call(cinput("_read_named", "material", 2, "-", Bytes(data)))
		f.call(cinput("_read_target", "-", Bytes(data)))
	}
	for _, command := range []string{"run", "verify", "start", "export"} {
		for _, paths := range [][]any{
			{nil}, {"-"}, {"src.md", nil, "-"}, {".hidden.md"}, {"out.md"}, {"/etc/hosts"}, {"missing.md"}, {"dir"},
			{"src.md", ".hidden.md", "/etc/hosts"}, {"link.md"}, {"../input-named/src.md"}, {"dir/../.hidden.md"},
		} {
			f.call(ckeep(ccall("_path_problem", command, paths)))
		}
	}
	f.op(Op{Kind: "chmod", Path: f.dir("locked.md"), Mode: &back})
	return f.done()
}

// stdinTexts は --stdin で渡す本文 (区切りの行で分ける)。
func stdinTexts() []string {
	return []string{
		"<<<yomiyasu:original>>>\n原文\n<<<yomiyasu:final>>>\n最終稿\n",
		"\ufeff<<<yomiyasu:original>>>\r\n原文\r\n<<<yomiyasu:final>>>\r\n最終稿",
		"<<<yomiyasu:material 出典A>>>\r資料A\r<<<yomiyasu:material>>>\r資料B\r<<<yomiyasu:final>>>\r最終稿\r",
		"\n \t\n<<<yomiyasu:material>>>\n資料\n<<<yomiyasu:final>>>\n最終\n",
		"本文\n<<<yomiyasu:final>>>\nx\n",
		"<<<yomiyasu:original>>>\nx\n",
		"<<<yomiyasu:original>>>\nx\n<<<yomiyasu:final>>>\na\n<<<yomiyasu:final>>>\nb\n",
		"<<<yomiyasu:original>>>\nx\n<<<yomiyasu:original>>>\ny\n<<<yomiyasu:final>>>\nb\n",
		"<<<yomiyasu:final>>>\nx\n",
		"",
		"<<<yomiyasu:original>>>\nx\n<<<yomiyasu:Final>>>\ny\n",
		"<<<yomiyasu:original>>>\nx\n<<<yomiyasu:final>>> \ny\n",
		"<<<yomiyasu:original>>>\nx\n\u3000<<<yomiyasu:final>>>\ny\n",
		"<<<yomiyasu:original>>>\nx\n\u2028<<<yomiyasu:final>>>\ny\n",
		"<<<yomiyasu:original>>>\nx\n\x1c<<<yomiyasu:final>>>\ny\n",
		"<<<yomiyasu:original>>>\nx\n<<<yomiyasu:" + strings.Repeat("い", 60) + "\n",
		"<<<yomiyasu:original>>>\nx\n<<<yomiyasu:" + strings.Repeat("い", 47) + "\n",
		"<<<yomiyasu:original>>>\nx\n<<<yomiyasu:\xff\xfe>>>\n",
		"<<<yomiyasu:original>>>\nx\na<<<yomiyasu:final>>>\n<<<yomiyasu:final>>>\ny",
		"<<<yomiyasu:original>>>\n\xff\n<<<yomiyasu:final>>>\ny\n",
		"<<<yomiyasu:original>>>\nx\n<<<yomiyasu:final>>>\n\xed\xa0\x80\n",
		"<<<yomiyasu:material " + strings.Repeat("長", 201) + ">>>\nx\n<<<yomiyasu:final>>>\ny\n",
		"<<<yomiyasu:material a>>>b>>>\nx\n<<<yomiyasu:final>>>\ny\n",
		"<<<yomiyasu:material\t出典 >>>\t \nx\n<<<yomiyasu:final>>>\r\n\r\n",
		"<<<YOMIYASU:FINAL>>>\nx\n",
		"<<<yomiyasu:original>>><<<yomiyasu:final>>>\n",
	}
}

// checkInputStdinCase は --stdin の本文の分け方と標準入力の読み (_split_stdin・_read_stdin_bytes・--stdin の _read_inputs)。
func checkInputStdinCase() VisCase {
	f := newFixtureCase("input-stdin")
	for _, text := range stdinTexts() {
		f.call(cinput("_split_stdin", Bytes(text), true))
		setStdin(f, text)
		f.call(cinput("_read_inputs", ns(true, "-", "")))
	}
	for _, data := range []string{"", "x\r\n", "\xff"} {
		setStdin(f, data)
		f.call(cinput("_read_stdin_bytes"))
	}
	noStdin(f)
	f.call(cinput("_read_stdin_bytes"))
	f.call(cinput("_read_inputs", ns(true, "-", "")))
	f.call(cinput("_read_inputs", ns(false, "-", "")))
	for _, bare := range []string{
		"<<<yomiyasu:material>>>", "<<<yomiyasu:material a>>>", "<<<yomiyasu:material  a b >>> \t", "<<<yomiyasu:material \t>>>",
		"<<<yomiyasu:material>>>x", " <<<yomiyasu:material>>>", "<<<yomiyasu:materials>>>", "<<<yomiyasu:material a>>>b>>>",
		"<<<yomiyasu:material " + strings.Repeat("長", 200) + ">>>", "<<<yomiyasu:material " + strings.Repeat("長", 201) + ">>>",
		"<<<yomiyasu:material \u2028>>>", "<<<yomiyasu:material a\x0b>>>", "<<<yomiyasu:material \xff>>>", "<<<yomiyasu:material a>>>\r",
	} {
		f.call(cinput("_material_marker", Bytes(bare)))
	}
	return f.done()
}

// checkInputArgsCase はファイルと - を並べた _read_inputs (資料の区切り・読む順・誤りの文)。
func checkInputArgsCase() VisCase {
	f := newFixtureCase("input-args")
	for _, file := range inputFiles() {
		f.write(file[0], file[1])
	}
	chdir(f)
	materials := []string{}
	for _, file := range inputFiles() {
		if strings.HasPrefix(file[0], "mat-") {
			materials = append(materials, file[0])
		}
	}
	for _, m := range materials {
		f.call(cinput("_read_inputs", ns(false, "fin.md", "", m)))
	}
	stdin := "<<<yomiyasu:material a>>>\nA\n"
	for _, n := range []Namespace{
		ns(false, "fin.md", "src.md"),
		ns(false, "fin.md", "", "mat-split.md", "mat-plain.md", "mat-cr.md"),
		ns(false, "fin.md", "src.md", "mat-plain.md", "mat-bad.md"),
		ns(false, "-", "src.md"),
		ns(false, "fin.md", "-"),
		ns(false, "fin.md", "", "-", "mat-split.md"),
		ns(false, "-", "-"),
		ns(false, "dir", "src.md"),
		ns(false, "/dev/null", "src.md"),
		ns(false, "fin.md", "missing.md"),
		ns(false, "fin.md", "src.md", "bad.md"),
		ns(false, "bad.md", "src.md"),
		ns(false, "fin.md", "bad.md", "missing.md"),
		ns(false, "missing.md", "src.md", "mat-bad.md"),
		ns(false, "/dev/null", "", "/dev/null"),
	} {
		setStdin(f, stdin)
		f.call(cinput("_read_inputs", n))
	}
	setStdin(f, "\xff")
	f.call(cinput("_read_inputs", ns(false, "-", "src.md")))
	f.call(cinput("_read_inputs", ns(false, "fin.md", "", "-")))
	return f.done()
}

// checkInputGoneCase は作業フォルダが消えたときの _path_problem (ファイルを渡したときだけ作業フォルダを読む)。
func checkInputGoneCase() VisCase {
	f := newFixtureCase("input-gone")
	f.mkdir("gone")
	chdir(f, "gone")
	f.op(Op{Kind: "remove", Path: f.dir("gone")})
	for _, paths := range [][]any{{nil}, {"-"}, {"a.md"}, {nil, "-", "/etc/hosts"}} {
		f.call(ckeep(ccall("_path_problem", "run", paths)))
	}
	f.call(cinput("_read_named", "final", 0, "a.md", nil))
	f.call(cinput("_read_target", "a.md", nil))
	return f.done()
}
