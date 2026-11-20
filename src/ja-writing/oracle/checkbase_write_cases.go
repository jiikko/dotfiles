package oracle

import "strings"

// 状態フォルダへの書き込み (_ensure_state・_write_atomic・_place_new_file と、書けなかったときの _write_problem)・記録の読み
// (_read_record_status・_read_record)・errno の文 (os.strerror) の case。書き込みの結果は、呼んだ後の木 (op tree) で比べる。
// 本物のファイルシステムで起こせない失敗 (名前が 100 回重なる・link を作れない・作ったフォルダに書けない) は記録に入らない (check の単体のテスト)。

// tree は作業フォルダの rel から下の木を比べる操作 (一時ファイルの名前は乱数なので伏せる)。
func tree(f *fixtureCase, rel string) {
	f.call(Call{Op: &Op{Kind: "tree", Path: f.dir(rel)}, Digest: true, Keep: true, Temporaries: true})
}

// cwrite は書き込みの関数の呼び出し。例外になったら name で覚え、一時ファイルの名前を伏せる。
func cwrite(name, fn string, args ...any) Call {
	c := ckeep(ccall(fn, args...))
	c.Save, c.SaveRaised, c.Temporaries = name, true, true
	return c
}

// problem は name で覚えた例外から、書けなかった理由の文を作る呼び出し。
func problem(state, name string) Call {
	return ckeep(ccall("_write_problem", state, Ref(name)))
}

func checkWriteCases() []VisCase {
	return []VisCase{checkEnsureStateCase(), checkEnsureStateFailureCase(), checkWriteFileCase(), checkRecordReadCase(), checkStrerrorCase()}
}

func checkEnsureStateCase() VisCase {
	f := newFixtureCase("write-ensure-state")
	f.write("ignore-only/.yomiyasu/.gitignore", "x")
	f.write("state-only/.yomiyasu/state.json", "x")
	f.mkdir("bare/.yomiyasu")
	f.mkdir("given/.yomiyasu")
	f.mkdir("broken-link/.yomiyasu")
	f.op(Op{Kind: "symlink", Path: f.dir("broken-link/.yomiyasu/.gitignore"), Target: "missing"})
	f.mkdir("elsewhere")
	f.op(Op{Kind: "symlink", Path: f.dir("linked/.yomiyasu"), Target: f.dir("elsewhere")})
	chdir(f)
	for _, c := range []struct {
		state        string
		defaultPlace bool
	}{
		{"new/.yomiyasu", true}, {"new-given/a/b/st", false}, {"bare/.yomiyasu", true}, {"given/.yomiyasu", false},
		{"ignore-only/.yomiyasu", true}, {"state-only/.yomiyasu", true}, {"broken-link/.yomiyasu", true}, {"linked/.yomiyasu", true},
	} {
		f.call(cwrite("e", "_ensure_state", f.dir(c.state), c.defaultPlace))
		tree(f, strings.Split(c.state, "/")[0])
	}
	tree(f, "elsewhere")
	// 2 回目は何も変えない
	f.call(cwrite("e", "_ensure_state", f.dir("new/.yomiyasu"), true))
	tree(f, "new")
	// 相対パスの状態フォルダ
	f.call(cwrite("e", "_ensure_state", "rel/.yomiyasu", true))
	tree(f, "rel")
	return f.done()
}

// checkEnsureStateFailureCase は状態フォルダを用意できないとき。例外と、書けなかった理由の文と、残った木。
func checkEnsureStateFailureCase() VisCase {
	f := newFixtureCase("write-ensure-state-failure")
	readOnly, back := uint32(0o500), uint32(0o755)
	f.write("file/.yomiyasu", "x")
	f.op(Op{Kind: "symlink", Path: f.dir("dangling/.yomiyasu"), Target: "nowhere"})
	f.write("parent-file", "x")
	f.mkdir("ro")
	f.mkdir("ro-state/.yomiyasu")
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &readOnly})
	f.op(Op{Kind: "chmod", Path: f.dir("ro-state/.yomiyasu"), Mode: &readOnly})
	chdir(f)
	for i, c := range []struct {
		state        string
		defaultPlace bool
	}{
		{"file/.yomiyasu", true}, {"dangling/.yomiyasu", true}, {"parent-file/.yomiyasu", true}, {"parent-file/deep/.yomiyasu", false},
		{"ro/.yomiyasu", true}, {"ro/deep/x/.yomiyasu", true}, {"ro-state/.yomiyasu", true},
	} {
		name := "e" + strings.Repeat("x", i)
		f.call(cwrite(name, "_ensure_state", f.dir(c.state), c.defaultPlace))
		f.call(problem(f.dir(c.state), name))
		tree(f, strings.Split(c.state, "/")[0])
	}
	// 指定した場所なら、書けないフォルダでも何も書かずに通る
	f.call(cwrite("given", "_ensure_state", f.dir("ro-state/.yomiyasu"), false))
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &back})
	f.op(Op{Kind: "chmod", Path: f.dir("ro-state/.yomiyasu"), Mode: &back})
	return f.done()
}

// checkWriteFileCase は入れ替えて書く _write_atomic と、まだ何も無いときだけ置く _place_new_file。
func checkWriteFileCase() VisCase {
	f := newFixtureCase("write-file")
	readOnly, back := uint32(0o500), uint32(0o755)
	data := Bytes("data\n")
	f.write("w/old.json", "old")
	f.mkdir("w/taken")
	f.write("p/first", "first")
	f.op(Op{Kind: "symlink", Path: f.dir("p/dangling"), Target: "missing"})
	f.op(Op{Kind: "symlink", Path: f.dir("w/link.json"), Target: f.dir("w/old.json")})
	f.mkdir("ro")
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &readOnly})
	chdir(f)
	for _, rel := range []string{"w/new.json", "w/old.json", "w/link.json"} {
		f.call(cwrite("w", "_write_atomic", f.dir(rel), data))
	}
	for i, rel := range []string{"w/taken", "w/none/x.json", "ro/x.json"} {
		name := "w" + strings.Repeat("x", i)
		f.call(cwrite(name, "_write_atomic", f.dir(rel), data))
		f.call(problem(f.dir("state"), name))
	}
	tree(f, "w")
	for _, rel := range []string{"p/new", "p/first", "p/dangling"} {
		f.call(cwrite("p", "_place_new_file", f.dir(rel), data))
	}
	for i, rel := range []string{"p/none/x", "ro/x"} {
		name := "p" + strings.Repeat("x", i)
		f.call(cwrite(name, "_place_new_file", f.dir(rel), data))
		f.call(problem(f.dir("state"), name))
	}
	tree(f, "p")
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &back})
	tree(f, "ro")
	return f.done()
}

// recordFiles は記録として読ませるファイル (名前 → 中身)。
func recordFiles() [][2]string {
	deep := strings.Repeat(`{"a": `, 300) + "1" + strings.Repeat("}", 300)
	deepList := `{"a": ` + strings.Repeat("[", 300) + strings.Repeat("]", 300) + "}"
	return [][2]string{
		{"ok.json", `{"a": 1, "b": [1, 2.5, "x", null, true, false], "c": {"d": {}}}`},
		{"empty.json", ""},
		{"space.json", " \n"},
		{"newline.json", "{}\n"},
		{"list.json", "[1]"},
		{"string.json", `"x"`},
		{"number.json", "1"},
		{"null.json", "null"},
		{"broken.json", `{"a":`},
		{"trailing.json", "{} x"},
		{"two.json", "{}{}"},
		{"bom.json", "\ufeff{}"},
		{"utf16.json", "\xff\xfe{\x00}\x00"},
		{"bytes.json", "{\"a\": \"\xff\"}"},
		{"nan.json", `{"a": NaN}`},
		{"inf.json", `{"a": Infinity, "b": -Infinity, "c": 1e400}`},
		{"bigint.json", `{"a": 123456789012345678901234567890, "b": -0, "c": -0.0, "d": 1E2}`},
		{"dup.json", `{"a": 1, "a": 2}`},
		{"surrogate.json", `{"a": "\ud800", "b": "\ud83d\ude00"}`},
		{"escaped.json", `{"\u3042": "\u00e9\n\t\"\\"}`},
		{"control.json", "{\"a\": \"x\ty\"}"},
		{"deep.json", deep},
		{"deep-list.json", deepList},
		{"keys.json", `{"b": 1, "a": 2, "": 3, "10": 4, "9": 5}`},
	}
}

func checkRecordReadCase() VisCase {
	f := newFixtureCase("record-read")
	zero, back := uint32(0), uint32(0o644)
	files := recordFiles()
	for _, file := range files {
		f.write(file[0], file[1])
	}
	f.mkdir("folder")
	f.write("parent", "x")
	f.write("locked.json", "{}")
	f.op(Op{Kind: "chmod", Path: f.dir("locked.json"), Mode: &zero})
	f.op(Op{Kind: "symlink", Path: f.dir("link.json"), Target: f.dir("ok.json")})
	f.op(Op{Kind: "symlink", Path: f.dir("dangling.json"), Target: f.dir("missing.json")})
	chdir(f)
	paths := []string{"missing.json", "folder", "parent/x.json", "locked.json", "link.json", "dangling.json", "missing/x.json"}
	for _, file := range files {
		paths = append(paths, file[0])
	}
	for _, rel := range paths {
		// NaN と無限大を含む結果は driver の出力の JSON (Go の encoding/json で読む) に書けないので、digest だけを持つ
		keep := ckeep
		if rel == "nan.json" || rel == "inf.json" {
			keep = func(c Call) Call { return c }
		}
		f.call(keep(ccall("_read_record_status", f.dir(rel))))
		f.call(keep(ccall("_read_record", f.dir(rel))))
	}
	f.call(ckeep(ccall("_read_record_status", "ok.json")))
	f.call(ckeep(ccall("_read_record_status", "../record-read/ok.json")))
	f.op(Op{Kind: "chmod", Path: f.dir("locked.json"), Mode: &back})
	return f.done()
}

// checkStrerrorCase は errno の文 (Go の visibility の表と比べる)。表の外の errno も含める。
func checkStrerrorCase() VisCase {
	var calls []Call
	for n := range 121 {
		calls = append(calls, Call{Module: "os", Function: "strerror", Args: []any{n}, Digest: true, Keep: true})
	}
	return VisCase{Name: "check/strerror", Calls: calls}
}
