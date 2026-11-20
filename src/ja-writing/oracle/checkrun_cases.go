package oracle

import "strings"

// CheckRunFile は本家 yomiyasu_check.py の run を、時刻・実行 ID・time.monotonic を固定して動かした記録 (testdata/golden/ の中)。
// 形は check-verify.json と同じ。run の標準出力・標準エラー・終了コード (人が読む出力と --json) と、run が書いた状態フォルダの木 (op snapshot) を持つ。
// Go のテストは同じ run を check の Main で動かし、出力を比べ、自分が書いた木を記録の木と比べる (記録の木を戻さない)。
// 逆向きの確かめ: 記録の木の python の欄を Go が書く値 (goize) にしてから本家の verify を当てた出力も持つ。Go のテストは、Go が書いた木が
// 記録の木と python の欄の値の字のほかは同じことと、Go の verify が同じ出力を返すことを見る。
const CheckRunFile = "check-run.json"

// CheckRunFunctions は記録の対象の関数 (main を run と verify の引数で呼ぶ)。
var CheckRunFunctions = []string{"main"}

// goVersionValue・goImplementationValue は goize が記録の python の欄に入れる値 (Go の run が書く値の形。本家の verify は型だけを見る)。
const (
	goVersionValue        = "go1.26.0"
	goImplementationValue = "Go ja-writing 3.1.0"
)

// fakeChild は lint と diff の子の代わり (本家は sys.executable、Go は Runtime.Executable にする)。JA_FAKE_LINT・JA_FAKE_DIFF で振る舞いを選び、
// 空なら本物 (JA_REAL) に渡す。本家の子は「-B <置き場>/yomiyasu_lint.py --json …」、Go の子は「lint --json …」で起こされる。
const fakeChild = `#!/bin/sh
case " $* " in
*yomiyasu_lint.py*|" lint "*) kind=lint; mode=$JA_FAKE_LINT ;;
*) kind=diff; mode=$JA_FAKE_DIFF ;;
esac
case "$mode" in
'') exec "$JA_REAL" "$@" ;;
hang) exec sleep 30 ;;
rc) printf 'boom\nsecond line\n' >&2; exit 4 ;;
kill) kill -9 $$ ;;
garbage) printf 'not json\n' ;;
badutf8) printf '\377\n' ;;
format) printf '{"format": "yomiyasu-other"}\n' ;;
version) printf '{"format": "yomiyasu-%s", "format_version": 99}\n' "$kind" ;;
keys) printf '{"format": "yomiyasu-%s", "format_version": 2, "candidate_count": true, "counts": [], "context": {}}\n' "$kind" ;;
render) printf '{"format": "yomiyasu-lint", "format_version": 2, "candidate_count": 1, "counts": {}, "findings": [5], "context": {"doc_type": null, "reader": null}}\n' ;;
skipped) printf '{"format": "yomiyasu-lint", "format_version": 2, "candidate_count": 0, "counts": {}, "findings": [], "context": {"doc_type": null, "reader": null, "skipped_by_doc_type": {"x": -1}}}\n' ;;
reader) "$JA_REAL" "$@" | sed 's/"reader": null/"reader": "x"/' ;;
touch) printf 'x' >> "$JA_TOUCH"; exec "$JA_REAL" "$@" ;;
remove) rm -f "$JA_TOUCH"; exec "$JA_REAL" "$@" ;;
difffile) cat "$JA_DIFF_JSON" ;;
lintfile) cat "$JA_LINT_JSON" ;;
esac
`

// run は本家の check の main を run の引数で呼び、標準出力・標準エラー・終了コードを記録する。
func run(f *fixtureCase, args ...string) { checkMain(f, append([]string{"run"}, args...)) }

// runStdin は標準入力を content にしてから run を呼ぶ。
func runStdin(f *fixtureCase, content string, args ...string) {
	f.op(Op{Kind: "stdin", Content: content})
	run(f, args...)
}

// goize は記録の python の欄を Go の run が書く値にし、record_digest を求め直す (driver だけが動かす)。mtime は書き換える前の値に戻す
// (verify は記録の mtime で並べるので、書き換えた記録だけが新しくなると Go の木と並びが変わる)。
func goize(f *fixtureCase, records ...string) {
	for _, record := range records {
		edit(f, record, map[string]any{"set": [][]any{set(goVersionValue, "python", "version"), set(goImplementationValue, "python", "implementation")},
			"redigest": true, "keep_mtime": true})
	}
}

// fake は子の代わりを使う (mode は lint と diff の振る舞い。空は本物)。
func fake(f *fixtureCase, lintMode, diffMode string) {
	setenv(f, "JA_FAKE_LINT", lintMode)
	setenv(f, "JA_FAKE_DIFF", diffMode)
}

func useFakeChild(f *fixtureCase) {
	mode := uint32(0o755)
	f.write("fake.sh", fakeChild)
	f.op(Op{Kind: "chmod", Path: f.dir("fake.sh"), Mode: &mode})
	f.op(Op{Kind: "pyexe", Path: f.dir("fake.sh")})
}

// 入力の本文 (run の case)。
const (
	rRich  = "会議は3月5日に開きます。参加者は12名です。資料は必ず事前に読んでください。\n\n## 背景\n\n予算は未定です。担当は田中さんです。\n"
	rRich2 = "会議は3月6日に開催します。参加者は15名です。資料は事前に読むことができます。\n\n## 経緯\n\n予算は決まりました。担当は佐藤さんと鈴木さんです。\n"
	rGaps  = "この変更は大きな一歩です。処理が大幅に速くなりました。信頼性が向上します。\n"
	rReq   = "見出しは20字以内でまとめてください。会議は3月5日です。\n"
)

// CheckRunCases は check-run.json の case。
func CheckRunCases() []VisCase {
	cases := []VisCase{
		runBasicCase(), runLinkCase(), runComposeCase(), runMaterialsCase(), runStdinCase(), runNoRecordCase(),
		runArgsCase(), runChildrenCase(), runFailOnCase(), runWordsCase(), runDiffsCase(), runDetectorsCase(), runStateCase(),
		runStartFailCase(), runTempdirCase(), runSurrogateCase(),
	}
	for i := range cases {
		scopeRefs(&cases[i])
	}
	return cases
}

// runBasicCase は推敲の run を同じ作業フォルダで重ねる: 1 回目・前回と同じ最終稿・修正後・修正の上限を超えた回・原文から変更なし (改行コードだけの違いも)。
// 最後に、木の python の欄を Go の値にしてから本家の verify を当てる (逆向き)。
func runBasicCase() VisCase {
	f := newFixtureCase("run-basic")
	f.write("src.md", vSource).write("final.md", vFinal).write("same.md", vSource)
	f.write("crlf.md", strings.ReplaceAll(vSource, "\n", "\r\n"))
	chdir(f)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "b1")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "b1", "--json")
	snapshot(f)
	f.write("final.md", vFinal2)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "b1")
	f.write("final.md", vFinal)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "b1", "--json")
	run(f, "--source", "src.md", "--final", "same.md", "--run-id", "b2")
	run(f, "--source", "src.md", "--final", "crlf.md", "--run-id", "b3", "--json")
	run(f, "--source", "src.md", "--final", "crlf.md", "--run-id", "b3")
	snapshot(f)
	var records []string
	for _, r := range []string{"b1/attempt-1", "b1/attempt-2", "b1/attempt-3", "b1/attempt-4", "b2/attempt-1", "b3/attempt-1", "b3/attempt-2"} {
		records = append(records, ".yomiyasu/runs/"+r+".json")
	}
	goize(f, records...)
	verifyBoth(f, "--final", "final.md", "--run-id", "b1")
	verifyBoth(f, "--final", "final.md")
	verify(f, "--final", "same.md", "--run-id", "b2")
	verify(f, "--final", "crlf.md")
	verifyBoth(f, "--final", "final.md", "--run-id", "b1", "--replay")
	verify(f, "--final", "same.md", "--run-id", "b2", "--replay")
	verify(f, "--final", "crlf.md", "--replay")
	return f.done()
}

// runLinkCase は実行 ID を決める道: 新しい実行・同じパスの前の実行につなぐ・6 時間を過ぎたら新しい実行・--new-run・別の原文の --run-id・
// 大文字小文字だけ違う --run-id・別の作業フォルダの記録につながない。
func runLinkCase() VisCase {
	f := newFixtureCase("run-link")
	f.write("src.md", vSource).write("other.md", vOther).write("final.md", vFinal).write("sub/final.md", vFinal).write("sub/src.md", vSource)
	chdir(f)
	run(f, "--source", "src.md", "--final", "final.md")
	clock(f, "2026-01-01T05:59:59Z")
	run(f, "--source", "src.md", "--final", "final.md")
	clock(f, "2026-01-01T12:00:00Z")
	run(f, "--source", "src.md", "--final", "final.md", "--json")
	run(f, "--source", "src.md", "--final", "final.md", "--new-run")
	run(f, "--source", "other.md", "--final", "final.md")
	run(f, "--source", "other.md", "--final", "final.md", "--run-id", "20260101T000000Z-00000001")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "Lk")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "lk")
	snapshot(f)
	chdir(f, "sub")
	run(f, "--source", "src.md", "--final", "final.md", "--state-dir", "../.yomiyasu")
	chdir(f)
	snapshot(f)
	clock(f, "2026-01-01T00:00:00Z")
	return f.done()
}

// runComposeCase は新規作成: 資料を原文とみなす (似かた)・前の回から引き継ぐ・似ていない資料・事実の穴・種類・資料と最終稿が同じ中身。
func runComposeCase() VisCase {
	f := newFixtureCase("run-compose")
	f.write("mat.md", vMaterial).write("comp.md", vComposed).write("far.md", vComposedFar).write("mat2.md", rGaps).write("gaps.md", rGaps)
	f.write("copy.md", vMaterial).write("spaced.md", " 会議は３月５日に開きます。 参加者は12名です。\n")
	chdir(f)
	run(f, "--material", "mat.md", "--final", "comp.md", "--run-id", "c1")
	f.write("comp.md", "会議は3月5日に開きます。どうぞよろしくお願いいたします。来週の打ち合わせでは予算と日程を話し合います。\n")
	run(f, "--material", "mat.md", "--final", "comp.md", "--run-id", "c1")
	run(f, "--material", "mat.md", "--final", "comp.md", "--run-id", "c1", "--json")
	run(f, "--material", "mat.md", "--final", "far.md", "--run-id", "c2", "--fact-gaps")
	run(f, "--material", "mat.md", "--material", "far.md", "--final", "gaps.md", "--run-id", "c3", "--doc-type", "paper", "--fact-gaps", "--json")
	run(f, "--material", "mat.md", "--final", "gaps.md", "--run-id", "c4", "--fact-gaps", "--doc-type", "essay")
	run(f, "--material", "mat.md", "--final", "gaps.md", "--run-id", "c5", "--fact-gaps", "--reader", "expert")
	run(f, "--material", "mat.md", "--final", "copy.md")
	run(f, "--material", "mat.md", "--final", "spaced.md")
	run(f, "--material", "gaps.md", "--material", "mat2.md", "--final", "comp.md")
	snapshot(f)
	goize(f, ".yomiyasu/runs/c1/attempt-1.json", ".yomiyasu/runs/c1/attempt-2.json", ".yomiyasu/runs/c1/attempt-3.json",
		".yomiyasu/runs/c2/attempt-1.json", ".yomiyasu/runs/c3/attempt-1.json")
	verifyBoth(f, "--final", "comp.md")
	verify(f, "--final", "gaps.md", "--run-id", "c3")
	verifyBoth(f, "--final", "comp.md", "--replay")
	verifyBoth(f, "--final", "gaps.md", "--run-id", "c3", "--replay")
	verify(f, "--final", "far.md", "--run-id", "c2", "--replay")
	return f.done()
}

// runMaterialsCase は推敲で資料もある run: 数値と名前の照合・資料に見出しの指定がある・資料の区切りと出典。
func runMaterialsCase() VisCase {
	f := newFixtureCase("run-materials")
	f.write("src.md", rRich).write("final.md", rRich2).write("mat.md", "参加者は15名に増えました。会議は3月6日に移りました。担当は佐藤さんです。予算は決まった見込みです。\n")
	f.write("req.md", rReq)
	f.write("cut.md", "<<<yomiyasu:material 議事録>>>\n参加者は15名です。\n<<<yomiyasu:material>>>\n担当は鈴木さんです。\n")
	chdir(f)
	run(f, "--source", "src.md", "--material", "mat.md", "--final", "final.md", "--run-id", "m1")
	run(f, "--source", "src.md", "--material", "mat.md", "--final", "final.md", "--run-id", "m1", "--json")
	run(f, "--source", "src.md", "--material", "req.md", "--final", "final.md", "--run-id", "m2")
	run(f, "--source", "src.md", "--materials", "cut.md", "--final", "final.md", "--run-id", "m3", "--stance", "報告")
	run(f, "--original", "src.md", "--material", "req.md", "--material", "mat.md", "--final", "final.md", "--run-id", "m4", "--json")
	snapshot(f)
	goize(f, ".yomiyasu/runs/m1/attempt-1.json", ".yomiyasu/runs/m1/attempt-2.json", ".yomiyasu/runs/m3/attempt-1.json")
	verifyBoth(f, "--final", "final.md", "--run-id", "m1")
	verify(f, "--final", "final.md", "--run-id", "m3")
	verifyBoth(f, "--final", "final.md", "--run-id", "m1", "--replay")
	verify(f, "--final", "final.md", "--run-id", "m3", "--replay")
	return f.done()
}

// runStdinCase は標準入力の run: --stdin の区切り・資料の区切りと出典・--final -・区切りの書き損じ・標準入力の資料。
func runStdinCase() VisCase {
	f := newFixtureCase("run-stdin")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	runStdin(f, "<<<yomiyasu:original>>>\n"+vSource+"<<<yomiyasu:final>>>\n"+vFinal, "--stdin", "--run-id", "s1")
	runStdin(f, "<<<yomiyasu:original>>>\n"+vSource+"<<<yomiyasu:final>>>\n"+vFinal, "--stdin", "--json")
	runStdin(f, "<<<yomiyasu:material 会議の案内>>>\n"+vMaterial+"<<<yomiyasu:final>>>\n"+vComposed, "--stdin")
	runStdin(f, "<<<yomiyasu:material 会議の案内>>>\n"+vMaterial+"<<<yomiyasu:final>>>\n"+vComposed, "--stdin", "--state-dir", "st")
	runStdin(f, "<<<yomiyasu:original>>>\n"+vSource+"<<<yomiyasu:Final>>>\n"+vFinal, "--stdin")
	runStdin(f, vFinal, "--source", "src.md", "--final", "-", "--run-id", "s2")
	runStdin(f, vSource, "--source", "-", "--final", "final.md", "--run-id", "s3")
	runStdin(f, vMaterial, "--material", "-", "--final", "final.md")
	runStdin(f, "\n \n", "--source", "src.md", "--final", "-")
	runStdin(f, vFinal, "--stdin", "--final", "final.md")
	snapshot(f)
	goize(f, ".yomiyasu/runs/s1/attempt-1.json", ".yomiyasu/runs/s2/attempt-1.json")
	verifyStdin(f, vFinal, "--final", "-", "--run-id", "s1")
	verifyStdin(f, vFinal, "--final", "-", "--run-id", "s2", "--json")
	verifyStdin(f, vFinal, "--final", "-", "--run-id", "s1", "--replay")
	verifyStdin(f, vFinal, "--final", "-", "--run-id", "s2", "--replay", "--json")
	return f.done()
}

// runNoRecordCase は記録を書かない run: --no-record・書けない状態フォルダ (拒否)・状態フォルダの場所がファイル・環境変数で決めた状態フォルダ。
func runNoRecordCase() VisCase {
	f := newFixtureCase("run-norecord")
	f.write("src.md", vSource).write("final.md", vFinal).write("blocker", "x")
	f.mkdir("ro")
	chdir(f)
	run(f, "--source", "src.md", "--final", "final.md", "--no-record")
	run(f, "--source", "src.md", "--final", "final.md", "--no-record", "--json")
	run(f, "--source", "src.md", "--final", "final.md", "--no-record", "--run-id", "n1")
	mode := uint32(0o555)
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &mode})
	run(f, "--source", "src.md", "--final", "final.md", "--state-dir", "ro/st")
	run(f, "--source", "src.md", "--final", "final.md", "--state-dir", "ro/st", "--json")
	run(f, "--source", "src.md", "--final", "final.md", "--state-dir", "blocker/st")
	open := uint32(0o755)
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &open})
	setenv(f, "YOMIYASU_STATE_DIR", f.dir("envst"))
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "e1")
	runStdin(f, vFinal, "--source", "src.md", "--final", "-", "--run-id", "e2")
	setenv(f, "YOMIYASU_SESSION", "sess-1")
	setenv(f, "CODEX_THREAD_ID", "${CODEX_THREAD_ID}")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "e3")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "e4", "--session", "given")
	unsetenv(f, "YOMIYASU_STATE_DIR")
	snapshot(f)
	return f.done()
}

// runSurrogateCase は、OS から UTF-8 として読めないバイトで受けた環境変数と引数 (本家は os.fsdecode で単独のサロゲートを持つ str にする) の出口:
// 記録の JSON (YOMIYASU_SESSION・CODEX_THREAD_ID・--session)、誤りの文 (標準エラーは backslashreplace)、標準出力 (strict。状態フォルダのパスが案内に入る
// run・verify は UnicodeEncodeError の内部エラーで rc 3)。最後に木の python の欄を Go の値にしてから本家の verify を当てる (逆向き)。
func runSurrogateCase() VisCase {
	f := newFixtureCase("run-surrogate")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	setenv(f, "YOMIYASU_SESSION", surrogates(0xDCFF)+"a")
	setenv(f, "CODEX_THREAD_ID", "b"+surrogates(0xDCED, 0xDCA0, 0xDC80))
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "s1")
	unsetenv(f, "CODEX_THREAD_ID")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "s2", "--session", "x"+surrogates(0xDCFF)+"y"+surrogates(0xDCED, 0xDCB3, 0xDCBF), "--json")
	unsetenv(f, "YOMIYASU_SESSION")
	for _, v := range ArgvSurrogates {
		for _, args := range [][]string{
			{"--fail-on", v.Value},
			{"--timeout", v.Value},
			{"--stance", v.Value},
			{"--doc-type", v.Value},
			{"--reader", v.Value},
			{"--run-id", v.Value},
			{"--words", v.Value},
			{"--material", v.Value},
			{"--state-dir", v.Value},
			{"--state-dir", v.Value, "--json"},
			{v.Value},
		} {
			run(f, append([]string{"--source", "src.md", "--final", "final.md"}, args...)...)
		}
		run(f, "--source", v.Value, "--final", "final.md")
		run(f, "--source", "src.md", "--final", v.Value)
		checkMain(f, []string{v.Value})
		for _, args := range [][]string{
			{"--run-id", v.Value},
			{"--since", v.Value},
			{"--timeout", v.Value, "--replay"},
			{"--source", v.Value},
			{"--state-dir", v.Value},
			{"--state-dir", v.Value, "--json"},
		} {
			verify(f, append([]string{"--final", "final.md"}, args...)...)
		}
		verify(f, "--final", v.Value)
	}
	snapshot(f)
	goize(f, ".yomiyasu/runs/s1/attempt-1.json", ".yomiyasu/runs/s2/attempt-1.json")
	verifyBoth(f, "--final", "final.md", "--run-id", "s1")
	verifyBoth(f, "--final", "final.md", "--run-id", "s2", "--replay")
	return f.done()
}

// runTempdirCase は一時フォルダの親の選び方 (tempfile.gettempdir): TMPDIR が無いフォルダ・ファイル・書けないフォルダのとき、次の候補
// (TEMP・TMP・/tmp) に移って、--no-record の run と、状態フォルダに書けずに一時フォルダへ移る run が最後まで動く。
func runTempdirCase() VisCase {
	f := newFixtureCase("run-tempdir")
	f.write("src.md", vSource).write("final.md", vFinal).write("afile", "x").write("blocker", "x")
	f.mkdir("ro").mkdir("tmp-ok")
	chdir(f)
	mode := uint32(0o555)
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &mode})
	for _, dir := range []string{f.dir("missing"), f.dir("afile"), f.dir("ro")} {
		setenv(f, "TMPDIR", dir)
		run(f, "--source", "src.md", "--final", "final.md", "--no-record")
		run(f, "--source", "src.md", "--final", "final.md", "--state-dir", "blocker/st")
	}
	setenv(f, "TMPDIR", f.dir("missing"))
	setenv(f, "TEMP", f.dir("afile"))
	setenv(f, "TMP", f.dir("tmp-ok"))
	run(f, "--source", "src.md", "--final", "final.md", "--no-record", "--json")
	open := uint32(0o755)
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &open})
	snapshot(f)
	return f.done()
}

// runArgsCase は使い方と入力の誤り (どれも何も書かずに 2) と -h。
func runArgsCase() VisCase {
	f := newFixtureCase("run-args")
	f.write("src.md", vSource).write("final.md", vFinal).write("empty.md", " \n\n").write(".hidden.md", vFinal)
	f.writeBytes("bad.md", []byte{0xff, 0xfe, '\n'})
	f.write("words/allow.txt", "語\n")
	chdir(f)
	for _, args := range [][]string{
		{"-h"},
		{"--bogus"},
		{"--final"},
		{"--source", "src.md"},
		{"--final", "final.md"},
		{"--final", "final.md", "--final", "src.md", "--source", "src.md"},
		{"--final", "final.md", "--source", "src.md", "--source", "src.md"},
		{"--final", "-", "--source", "-"},
		{"--stdin", "--final", "final.md"},
		{"--source", "src.md", "--final", "final.md", "--run-id", "a", "--new-run"},
		{"--source", "src.md", "--final", "final.md", "--state-dir", " "},
		{"--source", "src.md", "--final", "final.md", "--fail-on", "nope"},
		{"--source", "src.md", "--final", "final.md", "--fail-on", ","},
		{"--source", "src.md", "--final", "final.md", "--doc-type", "nope"},
		{"--source", "src.md", "--final", "final.md", "--reader", "nope"},
		{"--source", "src.md", "--final", "final.md", "--timeout", "0"},
		{"--source", "src.md", "--final", "final.md", "--timeout", ""},
		{"--source", "src.md", "--final", "final.md", "--run-id", "bad/id"},
		{"--source", "src.md", "--final", "final.md", "--words", "words", "--no-words"},
		{"--source", "src.md", "--final", "final.md", "--words", "nowhere"},
		{"--source", "src.md", "--final", "final.md", "--stance", "nope"},
		{"--source", "src.md", "--final", "final.md", "--stance", " "},
		{"--source", "src.md", "--final", ".hidden.md"},
		{"--source", "src.md", "--final", "../x.md"},
		{"--source", "src.md", "--final", "missing.md"},
		{"--source", "src.md", "--final", "bad.md"},
		{"--source", "src.md", "--final", "src.md"},
		{"--source", "src.md", "--final", "empty.md"},
		{"--source", "src.md", "--final", "final.md", "--json", "extra"},
		{"--source=src.md", "--final=final.md", "--no-record", "--doc-type", "PR", "--reader", "一般"},
	} {
		run(f, args...)
	}
	return f.done()
}

// runChildrenCase は子が最後まで動かない run (partial): 時間切れ・終了コード・シグナル・JSON の形の誤り・人が読む形を作れない・context の食い違い・
// 途中で最終稿が変わった。--timeout の秒数を短くした本物の子も通す。
func runChildrenCase() VisCase {
	f := newFixtureCase("run-children")
	f.write("src.md", vSource).write("final.md", vFinal).write("mat.md", vMaterial).write("comp.md", vComposed)
	chdir(f)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "k0", "--timeout", "0.000001")
	useFakeChild(f)
	for i, modes := range [][2]string{
		{"hang", "hang"}, {"rc", ""}, {"", "rc"}, {"kill", ""}, {"garbage", ""}, {"", "badutf8"}, {"format", ""},
		{"version", ""}, {"", "version"}, {"keys", ""}, {"", "keys"}, {"render", ""}, {"skipped", ""}, {"reader", ""},
		{"", "reader"},
	} {
		fake(f, modes[0], modes[1])
		id := "k" + string(rune('a'+i))
		timeout := "60"
		if modes[0] == "hang" {
			timeout = "0.3"
		}
		run(f, "--source", "src.md", "--final", "final.md", "--run-id", id, "--timeout", timeout)
	}
	fake(f, "rc", "")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "kr", "--json")
	run(f, "--material", "mat.md", "--final", "comp.md", "--run-id", "kt")
	run(f, "--source", "src.md", "--final", "final.md", "--no-record")
	fake(f, "", "rc")
	run(f, "--source", "src.md", "--material", "mat.md", "--final", "final.md", "--run-id", "ku", "--json")
	run(f, "--source", "src.md", "--material", "mat.md", "--final", "final.md", "--run-id", "ku")
	run(f, "--material", "mat.md", "--final", "comp.md", "--run-id", "kx")
	fake(f, "remove", "")
	f.write("gone.md", vFinal)
	setenv(f, "JA_TOUCH", f.dir("gone.md"))
	run(f, "--source", "src.md", "--final", "gone.md", "--run-id", "ky")
	fake(f, "touch", "")
	setenv(f, "JA_TOUCH", f.dir("final.md"))
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "kv")
	f.write("final.md", vFinal)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "kw", "--json")
	fake(f, "", "")
	f.write("final.md", vFinal)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "kr")
	f.op(Op{Kind: "pyexe", Path: f.dir("missing.sh")})
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "kz")
	f.write("plain.sh", "#!/bin/sh\n")
	f.op(Op{Kind: "pyexe", Path: f.dir("plain.sh")})
	run(f, "--source", "src.md", "--final", "final.md", "--no-record")
	f.op(Op{Kind: "pyexe"})
	snapshot(f)
	goize(f, ".yomiyasu/runs/kb/attempt-1.json", ".yomiyasu/runs/kr/attempt-1.json", ".yomiyasu/runs/kr/attempt-2.json")
	verifyBoth(f, "--final", "final.md", "--run-id", "kb")
	verifyBoth(f, "--final", "final.md", "--run-id", "kr")
	verify(f, "--final", "final.md", "--run-id", "kb", "--replay")
	verifyBoth(f, "--final", "final.md", "--run-id", "kr", "--replay")
	return f.done()
}

// runFailOnCase は --fail-on (意味が変わる恐れ・warn・error。当たれば記録を残して 1)。
func runFailOnCase() VisCase {
	f := newFixtureCase("run-failon")
	f.write("src.md", rRich).write("final.md", rRich2).write("mat.md", vMaterial).write("comp.md", "会議は3月7日に開きます。参加者は30名です。\n")
	chdir(f)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "f1", "--fail-on", "meaning-risk")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "f1", "--fail-on", "WARN,error", "--fail-on", "meaning_risk", "--json")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "f2", "--fail-on", "error", "--doc-type", "pr")
	run(f, "--material", "mat.md", "--final", "comp.md", "--run-id", "f3", "--fail-on", "meaning-risk")
	run(f, "--source", "src.md", "--final", "final.md", "--no-record", "--fail-on", "meaning-risk")
	// 意味が変わる恐れがちょうど 1 件・lint が error だけ (warn は 0 件) の境
	f.write("one.md", "参加者は12名です。\n").write("one2.md", "参加者は15名です。\n").write("bold.md", "これは**「重要」**です。\n")
	run(f, "--source", "one.md", "--final", "one2.md", "--run-id", "f4", "--fail-on", "meaning-risk")
	run(f, "--source", "one.md", "--final", "bold.md", "--run-id", "f5", "--fail-on", "warn")
	snapshot(f)
	return f.done()
}

// runWordsCase は語の一覧を使う run (残した指摘の語・使う語・flag-patterns.txt・--no-words)。
func runWordsCase() VisCase {
	f := newFixtureCase("run-words")
	f.write("src.md", "システムが利用者の入力を監視する。\n結果を確認する。\n").write("final.md", vCalque)
	f.write(".yomiyasu-words/allow.txt", "確かめ\n").write(".yomiyasu-words/flag.txt", "見張る\t監視する\n入力\n")
	f.write("w2/flag.txt", "見張る\n").write("w2/flag-patterns.txt", "結果\t結果の\\S+\n")
	chdir(f)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "w1")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "w1", "--json")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "w2", "--words", "w2")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "w3", "--no-words")
	var flagWords, manyLines []string
	for i := range 11 {
		word := "語" + string(rune('あ'+i))
		flagWords = append(flagWords, word)
		manyLines = append(manyLines, "ここに"+word+"を置く。")
	}
	f.write("w3/flag.txt", strings.Join(flagWords, "\n")+"\n")
	f.write("many.md", strings.Repeat("システムが入力を見張る。\n", 7)+strings.Join(manyLines, "\n")+"\n")
	run(f, "--source", "src.md", "--final", "many.md", "--run-id", "w4", "--words", "w3")
	snapshot(f)
	goize(f, ".yomiyasu/runs/w1/attempt-1.json")
	verifyBoth(f, "--final", "final.md", "--run-id", "w1")
	verify(f, "--final", "final.md", "--run-id", "w2")
	verifyBoth(f, "--final", "final.md", "--run-id", "w1", "--replay")
	verify(f, "--final", "final.md", "--run-id", "w2", "--replay")
	return f.done()
}

// runDiffsCase は直した箇所の節の形: 11 か所以上・長い行 (60 字を超える窓)・空白だけ・段落の数の違い・改行コードと BOM だけの違い・足しただけ・消しただけ。
func runDiffsCase() VisCase {
	f := newFixtureCase("run-diffs")
	var many, many2 []string
	for i := range 12 {
		many = append(many, "段落"+string(rune('A'+i))+"の文です。\n")
		many2 = append(many2, "段落"+string(rune('A'+i))+"の文でした。\n")
	}
	long := strings.Repeat("長い文の前の部分です、", 4) + "ここで変わります。" + strings.Repeat("後ろの部分が続きます。", 4) + "\n"
	long2 := strings.Repeat("長い文の前の部分です、", 4) + "ここで変えました。" + strings.Repeat("後ろの部分が続きます。", 4) + "\n"
	f.write("many.md", strings.Join(many, "\n")).write("many2.md", strings.Join(many2, "\n"))
	f.write("long.md", long).write("long2.md", long2)
	f.write("ws.md", "文の一つめです。\n").write("ws2.md", "文の一つめです。 \n")
	f.write("para.md", "一つめの段落です。\n\n二つめの段落です。\n").write("para2.md", "一つめの段落です。二つめの段落です。\n\n## 見出し\n\n三つめを足しました。\n")
	f.write("bom.md", vSource).write("bom2.md", "\ufeff"+vSource)
	f.write("add.md", vSource).write("add2.md", vSource+"\n最後に一文を足しました。\n")
	f.write("del2.md", "今日は良い天気です。\n")
	f.write("pre.md", strings.Repeat("あ", 70)+"\n").write("pre2.md", strings.Repeat("あ", 70)+"いうえお。\n")
	f.write("blank.md", strings.Repeat("あ", 27)+"〔空行〕"+strings.Repeat("い", 29)+"X\n").write("blank2.md", strings.Repeat("あ", 27)+"〔空行〕"+strings.Repeat("い", 29)+"Y\n")
	f.write("cut.md", strings.Repeat("い", 5)+"X"+strings.Repeat("う", 51)+"〔空行〕"+strings.Repeat("え", 5)+"\n")
	f.write("cut2.md", strings.Repeat("い", 5)+"Y"+strings.Repeat("う", 51)+"〔空行〕"+strings.Repeat("え", 5)+"\n")
	f.write("same.md", "本文です。〔改行〕次の文です。\n").write("same2.md", "本文です。\n次の文です。\n")
	f.write("stop.md", strings.Repeat("前の文です。", 3)+strings.Repeat("か", 50)+"X\n").write("stop2.md", strings.Repeat("前の文です。", 3)+strings.Repeat("か", 50)+"Y\n")
	chdir(f)
	for i, pair := range [][2]string{{"many.md", "many2.md"}, {"long.md", "long2.md"}, {"ws.md", "ws2.md"}, {"para.md", "para2.md"},
		{"bom.md", "bom2.md"}, {"add.md", "add2.md"}, {"add.md", "del2.md"}, {"pre.md", "pre2.md"}, {"blank.md", "blank2.md"},
		{"cut.md", "cut2.md"}, {"same.md", "same2.md"}, {"stop.md", "stop2.md"}} {
		run(f, "--source", pair[0], "--final", pair[1], "--run-id", "d"+string(rune('a'+i)))
	}
	run(f, "--source", "long.md", "--final", "long2.md", "--run-id", "db", "--json")
	snapshot(f)
	return f.done()
}

// runDetectorsCase は 1 行目と冒頭の一覧の、規則の候補以外の見直し候補: 読点の増加・見出しの形と勢い・構造の行番号・敬語 (mail)・資料の見出しの指定・
// 規則の候補が無く見直し候補だけがある diff (子の代わりが出す JSON)・findings の無い lint の候補の数・事実の穴が 10 件を超える。
func runDetectorsCase() VisCase {
	f := newFixtureCase("run-detectors")
	f.write("a3.md", "今日は晴れで外は暖かいです。\n").write("b3.md", "今日は、晴れで、外は、暖かいです。\n")
	f.write("a7.md", "ご確認ください。\n").write("b7.md", "確認して。\n")
	f.write("a10.md", "# 設定の修正\n\n本文です。\n\n# 結果\n\n本文です。\n").write("b10.md", "# 設定を直しました。\n\n本文です。\n")
	f.write("r10.md", "見出しは短くまとめてください。\n")
	f.write("a11.md", "# 一\n\n本文\n\n## 二\n\n本文\n").write("b11.md", "## 一\n\n本文\n\n### 二\n\n本文\n")
	f.write("a12.md", "# 設定を直しました。\n\n本文です。\n").write("b12.md", "# 設定の修正\n\n本文です。\n")
	f.write("a13.md", "田中さんが資料を確認します。\n").write("b13.md", "田中さんと佐藤さんが資料を確認します。\n")
	var gaps []string
	for i := range 12 {
		gaps = append(gaps, "項目"+string(rune('A'+i))+"の処理が大幅に速くなりました。")
	}
	f.write("gaps.md", strings.Join(gaps, "\n")+"\n").write("mat.md", vMaterial)
	f.write("diff.json", craftedDiff).write("lint.json", craftedLint)
	chdir(f)
	run(f, "--source", "a3.md", "--final", "b3.md", "--run-id", "t3", "--doc-type", "article")
	run(f, "--source", "a7.md", "--final", "b7.md", "--run-id", "t7", "--doc-type", "mail")
	run(f, "--source", "a10.md", "--final", "b10.md", "--material", "r10.md", "--run-id", "t10")
	run(f, "--source", "a10.md", "--final", "b10.md", "--run-id", "t10b")
	run(f, "--source", "a11.md", "--final", "b11.md", "--run-id", "t11")
	run(f, "--source", "a12.md", "--final", "b12.md", "--run-id", "t12", "--material", "r10.md")
	run(f, "--source", "a12.md", "--final", "b12.md", "--run-id", "t12b", "--json")
	run(f, "--source", "a13.md", "--final", "b13.md", "--run-id", "t13")
	run(f, "--material", "mat.md", "--final", "gaps.md", "--run-id", "t14", "--fact-gaps")
	useFakeChild(f)
	setenv(f, "JA_DIFF_JSON", f.dir("diff.json"))
	setenv(f, "JA_LINT_JSON", f.dir("lint.json"))
	fake(f, "", "difffile")
	run(f, "--source", "a12.md", "--final", "b12.md", "--run-id", "t15")
	run(f, "--source", "a12.md", "--final", "b12.md", "--run-id", "t15b", "--json")
	fake(f, "lintfile", "")
	run(f, "--source", "a12.md", "--final", "b12.md", "--run-id", "t16")
	fake(f, "", "")
	f.op(Op{Kind: "pyexe"})
	snapshot(f)
	return f.done()
}

// runStateCase は状態フォルダの中の想定外のもの: 前の最終稿を原文に渡す・読めない前の記録・実行のフォルダの場所がファイル・
// index がファイル (記録を書いた後で索引を書けない)・読めない写し。
func runStateCase() VisCase {
	f := newFixtureCase("run-state")
	f.write("src.md", vSource).write("final.md", vFinal).write("final2.md", vFinal2)
	chdir(f)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "g0")
	run(f, "--source", "final.md", "--final", "final2.md", "--run-id", "g1")
	f.write(".yomiyasu/runs/g2/attempt-1.json", "{")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "g2")
	f.write(".yomiyasu/runs/g3", "x")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "g3")
	unreadable := uint32(0)
	snapshotFile := ".yomiyasu/inputs/" + sha256Hex(vSource) + ".bin"
	f.op(Op{Kind: "chmod", Path: f.dir(snapshotFile), Mode: &unreadable})
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "g4")
	snapshot(f)
	f.op(Op{Kind: "remove", Path: f.dir(".yomiyasu/index")})
	f.write(".yomiyasu/index", "x")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "g5")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "g5", "--json")
	snapshot(f)
	return f.done()
}

// runStartFailCase は subcommand を省いた run と、subcommand の前の -h・--version。
func runStartFailCase() VisCase {
	f := newFixtureCase("run-top")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	checkMain(f, []string{"--source", "src.md", "--final", "final.md", "--run-id", "o1"})
	checkMain(f, []string{"-h"})
	checkMain(f, []string{"--version"})
	snapshot(f)
	return f.done()
}
