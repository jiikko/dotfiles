package oracle

// CheckReplayFile は本家 yomiyasu_check.py の verify --replay (と --since・--timeout) を、本家の run が書いた状態フォルダに当てた記録 (testdata/golden/ の中)。
// 形は check-verify.json と同じ。状態フォルダは op snapshot の結果 (keep) で持ち、Go のテストはそれを自分の作業フォルダに戻してから verify を呼ぶ。
// 作り直しの子 (lint と diff) の振る舞いは、run の記録と同じ子の代わり (op pyexe と fakeChild) で決める。
const CheckReplayFile = "check-replay.json"

// CheckReplayFunctions は記録の対象の関数 (main を verify の引数で呼ぶ)。
var CheckReplayFunctions = []string{"main"}

// CheckReplayCases は check-replay.json の case。
func CheckReplayCases() []VisCase {
	cases := []VisCase{replayBasicCase(), replaySinceCase(), replayMismatchCase(), replayChildrenCase(), replayAttemptsCase()}
	for i := range cases {
		scopeRefs(&cases[i])
	}
	return cases
}

// replayBasicCase は作り直しが記録と合う形: 推敲 (原文だけ・資料つき・立場)・新規作成 (資料を原文とみなした・似ていない資料だけ・事実の穴と種類)・
// 語の一覧 (--words・--no-words)・読者・標準入力の最終稿・--source・--timeout の値と誤り・--timeout だけ (作り直さない)・作り直しの前に決まる状態
// (記録が無い・空の最終稿・状態フォルダが無い・複数の記録・最終稿が違う・別の原文)。
func replayBasicCase() VisCase {
	f := newFixtureCase("replay-basic")
	f.write("src.md", rRich).write("final.md", rRich2).write("mat.md", vMaterial).write("comp.md", vComposed).write("far.md", vComposedFar)
	f.write("gaps.md", rGaps).write("other.md", vOther).write("empty.md", "")
	f.write("w/allow.txt", "確かめ\n").write("w/flag.txt", "見張る\t監視する\n入力\n")
	f.write("calque-src.md", "システムが利用者の入力を監視する。\n結果を確認する。\n").write("calque.md", vCalque)
	f.write("cut.md", "<<<yomiyasu:material 議事録>>>\n参加者は15名です。\n<<<yomiyasu:material>>>\n担当は鈴木さんです。\n")
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "r1")
	pycli(f, "0", "run", "--source", "src.md", "--material", "mat.md", "--final", "final.md", "--run-id", "r2", "--stance", "報告", "--reader", "expert")
	pycli(f, "0", "run", "--material", "mat.md", "--final", "comp.md", "--run-id", "c1", "--fact-gaps", "--doc-type", "paper")
	pycli(f, "0", "run", "--material", "mat.md", "--final", "far.md", "--run-id", "c2")
	pycli(f, "0", "run", "--material", "mat.md", "--material", "far.md", "--final", "gaps.md", "--run-id", "c3", "--fact-gaps")
	pycli(f, "0", "run", "--source", "calque-src.md", "--final", "calque.md", "--run-id", "w1", "--words", "w")
	pycli(f, "0", "run", "--source", "calque-src.md", "--final", "calque.md", "--run-id", "w2", "--no-words", "--state-dir", "st2")
	pycli(f, "0", "run", "--source", "src.md", "--materials", "cut.md", "--final", "final.md", "--run-id", "l1", "--state-dir", "st3")
	snapshot(f)
	verifyBoth(f, "--final", "final.md", "--run-id", "r1", "--replay")
	verifyBoth(f, "--final", "final.md", "--run-id", "r2", "--replay")
	verify(f, "--final", "final.md", "--replay")
	verify(f, "--final", "final.md", "--replay", "--json")
	verifyBoth(f, "--final", "comp.md", "--replay")
	verifyBoth(f, "--final", "far.md", "--replay")
	verifyBoth(f, "--final", "gaps.md", "--replay", "--timeout", "30")
	verifyBoth(f, "--final", "calque.md", "--replay")
	verifyBoth(f, "--final", "calque.md", "--replay", "--state-dir", "st2")
	verifyBoth(f, "--final", "final.md", "--replay", "--state-dir", "st3")
	verify(f, "--final", "final.md", "--run-id", "r1", "--replay", "--source", "src.md")
	verify(f, "--final", "final.md", "--run-id", "r1", "--replay", "--source", "other.md")
	verify(f, "--final", "final.md", "--run-id", "r1", "--replay", "--original", "other.md", "--json")
	verifyStdin(f, rRich2, "--final", "-", "--run-id", "r1", "--replay")
	verifyStdin(f, rRich2, "--final", "-", "--run-id", "r2", "--replay", "--json")
	verifyStdin(f, rRich, "--final", "final.md", "--source", "-", "--run-id", "r1", "--replay")
	verify(f, "--final", "src.md", "--replay")
	verify(f, "--final", "other.md", "--replay", "--json")
	verify(f, "--final", "empty.md", "--replay")
	verify(f, "--final", "final.md", "--replay", "--state-dir", "nowhere")
	verify(f, "--final", "comp.md", "--replay", "--run-id", "r1")
	verify(f, "--final", "final.md", "--run-id", "r1", "--timeout", "5")
	verify(f, "--final", "final.md", "--run-id", "r1", "--replay", "--timeout", "1e9")
	verify(f, "--final", "final.md", "--run-id", "r1", "--replay", "--timeout", "2000000")
	for _, bad := range []string{"0", "-1", "x", "", "inf", "nan", "1e-400"} {
		verify(f, "--final", "final.md", "--run-id", "r1", "--replay", "--timeout", bad)
	}
	return f.done()
}

// replaySinceCase は --since と --replay の組: 記録の created_at (2026-01-01T00:00:00Z) と同じ時刻・後の時刻・前の時刻を、時差・書き方
// (小文字の t と z・空白・コロンなしの時差・秒より細かい部分) を変えて渡す。後の時刻なら stale_record で作り直さない。受けない値は 2。
func replaySinceCase() VisCase {
	f := newFixtureCase("replay-since")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "s1")
	snapshot(f)
	for _, since := range []string{
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:00.999Z", "2026-01-01T00:00:01Z", "2025-12-31T23:59:59Z",
		"2026-01-01T09:00:00+09:00", "2026-01-01T09:00:01+0900", "2025-12-31T15:00:00-09:00", "2025-12-31T15:00:01-0900",
		"2026-01-01t00:00:00z", "2026-01-01 00:00:00Z", "2026-01-01T00:00:00,5+00:00", "2026-01-01T00:00:00-00:00",
		" 2026-01-01T00:00:00Z ", "2026-01-01T00:00:00+23:59", "2026-01-01T00:00:00-23:59",
	} {
		verify(f, "--final", "final.md", "--replay", "--since", since)
	}
	verify(f, "--final", "final.md", "--replay", "--since", "2026-01-01T00:00:00Z", "--json")
	verify(f, "--final", "final.md", "--replay", "--since", "2026-01-01T00:00:01Z", "--json")
	verify(f, "--final", "final.md", "--since", "2026-01-01T00:00:00Z", "--timeout", "3")
	for _, bad := range []string{
		"2026-01-01T00:00:00", "2026-01-01", "2026-13-01T00:00:00Z", "2026-02-30T00:00:00Z", "2026-01-01T24:00:00Z",
		"2026-01-01T00:00:60Z", "2026-01-01T00:00:00+24:00", "2026-01-01T00:00:00+0960", "", "x", "２０２６-01-01T00:00:00Z",
	} {
		verify(f, "--final", "final.md", "--replay", "--since", bad)
	}
	return f.done()
}

// replayMismatchCase は作り直しが記録と合わない形と、作り直しの前に止まる形。レポートのファイル (lint・diff・資料との照合の .json と .txt) を
// 値を変えずに書き換え、記録のハッシュと record_digest を求め直す (件数の照らしは通り、作り直しで食い違う)。事実の穴の記録を書き換える。
// 記録の tools の SHA-256 (道具・語の一覧) を変える (tool_changed で作り直さない)。立場に NUL を入れる (子を起動できない内部エラー)。
func replayMismatchCase() VisCase {
	f := newFixtureCase("replay-mismatch")
	f.write("src.md", rRich).write("final.md", rRich2).write("mat.md", vMaterial).write("comp.md", vComposed).write("gaps.md", rGaps)
	chdir(f)
	files := map[string][][2]string{
		"lint.json": {{`"format":`, `"format" :`}}, "diff.json": {{`"format":`, `"format" :`}}, "materials.json": {{`{"`, `{ "`}},
		"lint.txt": {{"\n", " \n"}}, "diff.txt": {{"\n", " \n"}}, "materials.txt": {{"\n", " \n"}},
	}
	order := []string{"lint.json", "lint.txt", "diff.json", "diff.txt", "materials.json", "materials.txt"}
	for i, name := range order {
		state := "m" + string(rune('a'+i))
		pycli(f, "0", "run", "--source", "src.md", "--material", "mat.md", "--final", "final.md", "--run-id", "x", "--state-dir", state)
		edit(f, state+"/runs/x/attempt-1."+name, map[string]any{"raw": files[name]})
		edit(f, firstRecord(state, "x"), map[string]any{"rehash": true, "redigest": true})
	}
	// 2 つのレポートを書き換えた (食い違いを全部並べる)
	pycli(f, "0", "run", "--source", "src.md", "--material", "mat.md", "--final", "final.md", "--run-id", "x", "--state-dir", "mz")
	edit(f, "mz/runs/x/attempt-1.lint.json", map[string]any{"raw": files["lint.json"]})
	edit(f, "mz/runs/x/attempt-1.materials.txt", map[string]any{"raw": files["materials.txt"]})
	edit(f, firstRecord("mz", "x"), map[string]any{"rehash": true, "redigest": true})
	// 事実の穴の記録を書き換えた
	pycli(f, "0", "run", "--material", "mat.md", "--final", "gaps.md", "--run-id", "g", "--fact-gaps", "--state-dir", "fg")
	redigest(f, firstRecord("fg", "g"), set("x", "fact_gaps", "items", 0, "need"))
	// tools の SHA-256 を変えた (道具・語の一覧)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "t", "--state-dir", "tl")
	redigest(f, firstRecord("tl", "t"), set(zeros64, "tools", "yomiyasu_lint.py", "sha256"))
	pycli(f, "0", "run", "--material", "mat.md", "--final", "gaps.md", "--run-id", "t", "--fact-gaps", "--state-dir", "tg")
	redigest(f, firstRecord("tg", "t"), set(zeros64, "tools", "references/lists/fact-gaps.txt", "sha256"))
	// 立場に NUL を入れた (diff の子を起動できない。本家の Popen は起動の前に ValueError。内部エラーの文は伏せるので TestReplayCannotRunChildren が比べる)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "n", "--stance", "報告", "--state-dir", "sn")
	redigest(f, firstRecord("sn", "n"), set("a\x00b", "options", "stance"))
	snapshot(f)
	for _, state := range []string{"ma", "mb", "mc", "md", "me", "mf", "mz", "fg", "tl", "tg"} {
		final := "final.md"
		if state == "fg" || state == "tg" {
			final = "gaps.md"
		}
		verifyBoth(f, "--final", final, "--state-dir", state, "--replay")
	}
	verify(f, "--final", "final.md", "--state-dir", "ma")
	verifyBoth(f, "--final", "final.md", "--state-dir", "sn", "--replay")
	return f.done()
}

// replayChildrenCase は作り直しの子が最後まで動かない形 (子の代わり): 時間切れ (片方・両方・時間切れと誤りが重なる)・終了コード・シグナル・
// JSON の形の誤り・人が読む形を作れない・中身の違うレポート・起動できない (rc 3)・出力の無い子。新規作成で diff の無い記録は lint だけを起こす。
func replayChildrenCase() VisCase {
	f := newFixtureCase("replay-children")
	f.write("src.md", vSource).write("final.md", vFinal).write("mat.md", vMaterial).write("far.md", vComposedFar)
	f.write("diff.json", craftedDiff).write("lint.json", craftedLint).write("rsrc.md", rRich).write("rfinal.md", rRich2)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "k1")
	pycli(f, "0", "run", "--material", "mat.md", "--final", "far.md", "--run-id", "k2")
	pycli(f, "0", "run", "--source", "src.md", "--material", "mat.md", "--final", "final.md", "--run-id", "k3", "--state-dir", "st3")
	pycli(f, "0", "run", "--source", "rsrc.md", "--material", "mat.md", "--final", "rfinal.md", "--run-id", "k4", "--state-dir", "st4")
	snapshot(f)
	useFakeChild(f)
	setenv(f, "JA_DIFF_JSON", f.dir("diff.json"))
	setenv(f, "JA_LINT_JSON", f.dir("lint.json"))
	for _, modes := range [][2]string{
		{"hang", "hang"}, {"hang", ""}, {"", "hang"}, {"hang", "rc"}, {"rc", ""}, {"", "rc"}, {"kill", ""}, {"", "kill"}, {"kill", "kill"},
		{"garbage", ""}, {"", "badutf8"}, {"format", ""}, {"version", ""}, {"", "version"}, {"keys", ""}, {"", "keys"}, {"render", ""},
		{"skipped", ""}, {"reader", ""}, {"", "reader"}, {"lintfile", "difffile"},
	} {
		fake(f, modes[0], modes[1])
		timeout := "60"
		switch {
		case modes[0] == "hang" && modes[1] == "hang":
			timeout = "0.3"
		case modes[0] == "hang" || modes[1] == "hang":
			// 片方だけが止まらないときは、もう片方が終わるのを待てる秒数にする
			timeout = "3"
		}
		verify(f, "--final", "final.md", "--run-id", "k1", "--replay", "--timeout", timeout)
	}
	// 資料つきの推敲で diff を作り直せない: 照合の .txt は比べない (diff の「元の文にない語」を使うため)。k4 は、diff が無いと
	// 「資料にない語」の行が変わる記録 (比べると食い違う)
	for _, mode := range []string{"garbage", "rc", "render"} {
		fake(f, "", mode)
		verifyBoth(f, "--final", "final.md", "--state-dir", "st3", "--replay")
		verifyBoth(f, "--final", "rfinal.md", "--state-dir", "st4", "--replay")
	}
	fake(f, "rc", "")
	verify(f, "--final", "final.md", "--run-id", "k1", "--replay", "--json")
	verifyBoth(f, "--final", "far.md", "--replay")
	fake(f, "hang", "")
	verifyBoth(f, "--final", "far.md", "--replay", "--timeout", "0.3")
	fake(f, "", "")
	verify(f, "--final", "final.md", "--run-id", "k1", "--replay", "--timeout", "0.000001")
	verify(f, "--final", "final.md", "--run-id", "k1", "--replay", "--timeout", "0.000001", "--json")
	f.op(Op{Kind: "pyexe", Path: f.dir("missing.sh")})
	verify(f, "--final", "final.md", "--run-id", "k1", "--replay")
	verify(f, "--final", "far.md", "--replay", "--json")
	f.write("plain.sh", "#!/bin/sh\n")
	f.op(Op{Kind: "pyexe", Path: f.dir("plain.sh")})
	verify(f, "--final", "final.md", "--run-id", "k1", "--replay")
	verify(f, "--final", "final.md", "--run-id", "k1", "--replay", "--json")
	f.op(Op{Kind: "pyexe"})
	return f.done()
}

// replayAttemptsCase は回が複数・partial の回・壊れた記録: 直した 2 回目 (最新の回を作り直す)・前の回の本文 (後の回が別の本文の注記つき)・
// 子が終わらなかった回 (stopped で作り直さない)・読めない記録・形の違う記録・写しの欠け。
func replayAttemptsCase() VisCase {
	f := newFixtureCase("replay-attempts")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "a1")
	f.write("final.md", vFinal2)
	clock(f, "2026-01-01T00:10:00Z")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "a1")
	// 子が終わらなかった回 (partial の記録)
	useFakeChild(f)
	fake(f, "rc", "")
	pycli(f, "1,3", "run", "--source", "src.md", "--final", "final.md", "--run-id", "p1", "--state-dir", "stp")
	fake(f, "", "")
	f.op(Op{Kind: "pyexe"})
	// 読めない記録・形の違う記録・写しの欠け
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "b1", "--state-dir", "stb")
	f.write("stb/runs/b1/attempt-1.json", "{")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "b2", "--state-dir", "sts")
	edit(f, firstRecord("sts", "b2"), map[string]any{"set": []any{set("x", "options")}})
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "b3", "--state-dir", "stc")
	f.op(Op{Kind: "remove", Path: f.dir("stc/inputs/" + sha256Hex(vSource) + ".bin")})
	clock(f, "2026-01-01T00:00:00Z")
	snapshot(f)
	verifyBoth(f, "--final", "final.md", "--replay")
	verify(f, "--final", "final.md", "--replay", "--since", "2026-01-01T00:10:00Z")
	verify(f, "--final", "final.md", "--replay", "--since", "2026-01-01T00:10:01Z")
	f.write("final.md", vFinal)
	verifyBoth(f, "--final", "final.md", "--replay")
	verify(f, "--final", "final.md", "--replay", "--since", "2026-01-01T00:00:01Z")
	f.write("final.md", vFinal2)
	for _, state := range []string{"stp", "stb", "sts", "stc"} {
		verifyBoth(f, "--final", "final.md", "--state-dir", state, "--replay")
	}
	return f.done()
}
