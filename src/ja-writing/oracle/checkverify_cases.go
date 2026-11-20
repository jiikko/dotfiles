package oracle

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// CheckVerifyFile は本家 yomiyasu_check.py の verify (--replay を除く) を、本家の run が書いた状態フォルダに当てた記録 (testdata/golden/ の中)。
// 形は check-base.json と同じ (case ごとの呼び出しの digest の列)。状態フォルダは op snapshot の結果 (keep) で持ち、Go のテストはそれを自分の作業フォルダに戻してから verify を呼ぶ。
const CheckVerifyFile = "check-verify.json"

// CheckVerifyFunctions は記録の対象の関数 (main を verify の引数で呼ぶ)。
var CheckVerifyFunctions = []string{"main"}

// 入力の本文 (推敲の原文と最終稿・別の原文・資料・新規作成の最終稿)。
const (
	vSource      = "今日は良い天気です。散歩に行きました。\n"
	vFinal       = "今日は良い天気でした。散歩に行った。\n"
	vFinal2      = "今日は良い天気でした。公園まで散歩に行った。\n"
	vSourceB     = "明日は雨が降ります。家で本を読みます。\n"
	vOther       = "全く別の文章です。\n"
	vMaterial    = "会議は3月5日に開きます。参加者は12名です。\n"
	vComposed    = "会議は3月5日に開きます。参加者は12名です。よろしくお願いします。\n"
	vComposedFar = "来週の打ち合わせでは予算を話し合います。\n"
	vCalque      = "システムが利用者の入力を見張る。\n結果の確かめをする。\n"
	zeros64      = "0000000000000000000000000000000000000000000000000000000000000000"
)

// pycli は本家の check の main を呼ぶ (driver だけが動かす。rc は受ける終了コードを「,」でつないだもの)。
func pycli(f *fixtureCase, rc string, args ...string) {
	f.op(Op{Kind: "pycli", Args: args, Value: rc})
}

// clock は本家の check の時刻を固定する。
func clock(f *fixtureCase, iso string) { f.op(Op{Kind: "clock", Value: iso}) }

// edit は rel の JSON を書き換える (driver だけが動かす。指示の形は call_function.py の冒頭の op pyedit)。
func edit(f *fixtureCase, rel string, spec map[string]any) {
	b, err := json.Marshal(spec)
	if err != nil {
		panic(err)
	}
	f.op(Op{Kind: "pyedit", Path: f.dir(rel), Value: string(b)})
}

// copyFile は from を rel へバイト列のまま写す (driver だけが動かす。Go のテストは後の snapshot の木を使う)。
func copyFile(f *fixtureCase, from, rel string) {
	edit(f, rel, map[string]any{"copy_from": f.dir(from)})
}

// set は edit の set の 1 組 (鍵の道と値)。
func set(value any, steps ...any) []any { return []any{steps, value} }

// redigest は記録の 1 か所を値に替え、record_digest を求め直す。
func redigest(f *fixtureCase, record string, pairs ...[]any) {
	edit(f, record, map[string]any{"set": pairs, "redigest": true})
}

// rename は dir からの相対の glob に当たるファイルの名前の old を new に替える (driver だけが動かす。glob ごとに 1 つは当たらないと止める)。
func rename(f *fixtureCase, dir, old, new string, globs ...string) {
	b, err := json.Marshal(map[string]any{"globs": globs, "old": old, "new": new})
	if err != nil {
		panic(err)
	}
	f.op(Op{Kind: "rename", Path: f.dir(dir), Value: string(b)})
}

// renameAttempt は状態フォルダ state の実行 runID の回 from の記録・レポート・索引の名前を回 to にする (記録の中身は替えない)。
func renameAttempt(f *fixtureCase, state, runID, from, to string) {
	rename(f, state+"/runs/"+runID, "attempt-"+from+".", "attempt-"+to+".", "attempt-"+from+".*")
	rename(f, state+"/index", runID+"."+from, runID+"."+to,
		"by-final/*/"+runID+"."+from, "by-text/*/"+runID+"."+from, "by-path/*/*."+runID+"."+from)
}

// moveAttempt は renameAttempt に加えて、記録の中のレポートの道と attempt を to にし、record_digest を求め直す (読める記録のまま回を移す)。
func moveAttempt(f *fixtureCase, state, runID, from, to string) {
	record := state + "/runs/" + runID + "/attempt-" + from + ".json"
	prefix := "runs/" + runID + "/attempt-"
	edit(f, record, map[string]any{"raw": [][2]string{{prefix + from + ".", prefix + to + "."}}})
	redigest(f, record, set(json.Number(to), "attempt"))
	renameAttempt(f, state, runID, from, to)
}

// snapshot は作業フォルダの木を記録に残す (Go のテストは、これを作業フォルダに戻す)。
func snapshot(f *fixtureCase) {
	f.call(Call{Op: &Op{Kind: "snapshot", Path: f.dir()}, Digest: true, Keep: true})
}

// snapshotTies は snapshot と同じだが、組ごとに、パターン (fnmatch。作業フォルダからの相対パス) に当たるファイルの mtime を同じ値にする
// (mtime が 1 秒単位のファイルシステムで同じ秒に書いた場面。並べ方の同点を記録に出す)。
func snapshotTies(f *fixtureCase, groups ...[]string) {
	b, err := json.Marshal(map[string]any{"ties": groups})
	if err != nil {
		panic(err)
	}
	f.call(Call{Op: &Op{Kind: "snapshot", Path: f.dir(), Value: string(b)}, Digest: true, Keep: true})
}

// verify は本家の verify を呼び、標準出力・標準エラー・終了コードを記録する。
func verify(f *fixtureCase, args ...string) {
	f.call(Call{Module: CheckModule, Function: "main", Args: []any{append([]string{"verify"}, args...)}, Digest: true, Keep: true, Capture: true})
}

// verifyBoth は verify を、人が読む出力と --json の両方で呼ぶ (JSON は digest だけを残す。詳しい食い違いは JSON にしか出ない)。
func verifyBoth(f *fixtureCase, args ...string) {
	verify(f, args...)
	f.call(Call{Module: CheckModule, Function: "main", Args: []any{append(append([]string{"verify"}, args...), "--json")}, Digest: true, Capture: true})
}

// verifyStdin は標準入力を content にしてから verify を呼ぶ。
func verifyStdin(f *fixtureCase, content string, args ...string) {
	f.op(Op{Kind: "stdin", Content: content})
	verify(f, args...)
}

// sha256Hex は本文の UTF-8 の SHA-256 (状態フォルダの写しの名前)。
func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// firstRecord は状態フォルダ state の実行 runID の 1 回目の記録のパス。
func firstRecord(state, runID string) string { return state + "/runs/" + runID + "/attempt-1.json" }

// stateSet は 1 つの case の中で、状態フォルダ st<n> に実行 <prefix><n> の run を 1 回ずつ作り、作った順に覚える。
type stateSet struct {
	f      *fixtureCase
	prefix string
	args   []string // run の入力の引数 (実行 ID と状態フォルダの前まで)
	states []string
}

// add は run を作り、1 回目の記録のパスを返す。final を渡すと args の最後の値 (最終稿) をそれにする。
func (s *stateSet) add(n string, final ...string) string {
	args := append([]string{"run"}, s.args...)
	if len(final) > 0 {
		args[len(args)-1] = final[0]
	}
	state := "st" + n
	pycli(s.f, "0", append(args, "--run-id", s.prefix+n, "--state-dir", state)...)
	s.states = append(s.states, state)
	return firstRecord(state, s.prefix+n)
}

// keep は手で作った状態フォルダを覚える。
func (s *stateSet) keep(state string) { s.states = append(s.states, state) }

// CheckVerifyCases は check-verify.json の case。
func CheckVerifyCases() []VisCase {
	cases := []VisCase{
		verifyBasicCase(), verifyChangedCase(), verifyAttemptsCase(), verifyAmbiguousCase(), verifyPlaceCase(),
		verifyInvalidCase(), verifyChecksCase(), verifyToolsCase(), verifyComposeCase(), verifyStdinCase(),
		verifyContextCase(), verifyWordsCase(), verifyEdgesCase(), verifyArgsCase(),
		verifyTiesCase(), verifySummaryEdgesCase(), verifyHugeAttemptCase(), verifyAttemptOrderCase(),
	}
	for i := range cases {
		scopeRefs(&cases[i])
	}
	return cases
}

// verifyBasicCase は 1 回の run の後の照らし: 合う・JSON・原文の指定・実行 ID・状態フォルダの指定・空・標準入力・別の場所に写した最終稿・原文を渡した。
func verifyBasicCase() VisCase {
	f := newFixtureCase("verify-basic")
	f.write("src.md", vSource).write("final.md", vFinal).write("other-src.md", vOther).write("copy.md", vFinal)
	f.write("empty.md", "").write("blank.md", " \n\n")
	chdir(f)
	verify(f, "--final", "final.md") // 状態フォルダがまだ無い
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "b1")
	snapshot(f)
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	verify(f, "--final", f.dir("final.md"))
	verify(f, "--final", "final.md", "--source", "src.md")
	verify(f, "--final", "final.md", "--source", "other-src.md")
	verify(f, "--final", "final.md", "--source", "other-src.md", "--json")
	verify(f, "--final", "final.md", "--original", "src.md")
	verify(f, "--final", "final.md", "--run-id", "b1")
	verify(f, "--final", "final.md", "--run-id", "b1", "--json")
	verify(f, "--final", "final.md", "--state-dir", "nowhere")
	verify(f, "--final", "final.md", "--state-dir", "nowhere", "--json")
	verify(f, "--final", "final.md", "--state-dir", ".yomiyasu")
	verify(f, "--final", "empty.md")
	verify(f, "--final", "blank.md", "--json")
	verify(f, "--final", "copy.md")
	verify(f, "--final", "copy.md", "--json")
	verify(f, "--final", "src.md")
	verify(f, "--final", "src.md", "--json")
	verify(f, "--final", "other-src.md")
	verifyStdin(f, vFinal, "--final", "-")
	verifyStdin(f, vFinal, "--final", "-", "--json")
	verifyStdin(f, "\ufeff"+vFinal+"\n\n", "--final", "-")
	verifyStdin(f, vOther, "--final", "-")
	verifyStdin(f, vFinal, "--final", "-", "--source", "src.md")
	verifyStdin(f, vSource, "--final", "final.md", "--source", "-")
	verifyStdin(f, "", "--final", "-")
	verify(f, "--final", "final.md", "--since", "2025-12-31T23:59:59Z")
	verify(f, "--final", "final.md", "--since", "2026-01-01T09:00:00+09:00")
	verify(f, "--final", "final.md", "--since", "2026-01-01T00:00:01Z")
	verify(f, "--final", "final.md", "--since", "2026-01-01T00:00:01Z", "--json")
	verify(f, "--final", "final.md", "--timeout", "5")
	return f.done()
}

// verifyChangedCase は最終稿を書き換えた後: 本文・改行コードだけ・BOM と末尾の改行だけ・実行 ID つき・別のファイル・索引の欠け。
func verifyChangedCase() VisCase {
	f := newFixtureCase("verify-changed")
	f.write("src.md", vSource).write("final.md", vFinal).write("other.md", vOther)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "c1")
	snapshot(f)
	f.write("final.md", vFinal2)
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	verify(f, "--final", "final.md", "--run-id", "c1")
	verify(f, "--final", "other.md", "--run-id", "c1")
	verify(f, "--final", "other.md", "--run-id", "c1", "--json")
	verify(f, "--final", "final.md", "--run-id", "nothing")
	verify(f, "--final", "final.md", "--run-id", "C1")
	verify(f, "--final", "final.md", "--run-id", "bad/id")
	verify(f, "--final", "final.md", "--run-id", "con")
	f.write("final.md", strings.ReplaceAll(vFinal, "\n", "\r\n"))
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	f.write("final.md", "\ufeff"+vFinal)
	verify(f, "--final", "final.md")
	f.write("final.md", vFinal+"\n\n")
	verify(f, "--final", "final.md", "--json")
	verifyStdin(f, vFinal2, "--final", "-", "--source", "src.md")
	// 最終稿の索引だけが無い: by-path から見つける
	f.write("final.md", vFinal)
	f.op(Op{Kind: "remove", Path: f.dir(".yomiyasu/index/by-final")})
	snapshot(f)
	verify(f, "--final", "final.md")
	f.op(Op{Kind: "remove", Path: f.dir(".yomiyasu/index/by-path")})
	verify(f, "--final", "final.md")
	return f.done()
}

// verifyAttemptsCase は同じ作業フォルダで複数回起動した木: 直した 2 回目・前の回の本文に戻した (後の回が別の本文)・同じ秒の別の実行。
func verifyAttemptsCase() VisCase {
	f := newFixtureCase("verify-attempts")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md")
	f.write("final.md", vFinal2)
	clock(f, "2026-01-01T00:10:00Z")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md")
	snapshot(f)
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	f.write("final.md", vFinal)
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	// 同じ本文で 3 回目 (同じ実行)。前の回の本文と同じ最終稿は、最新の回で見つかる
	clock(f, "2026-01-01T00:20:00Z")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md")
	// 同じ秒に新しい実行 (書いた時刻で並べる)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--new-run")
	snapshot(f)
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	// --run-id の実行に、同じ本文の回が 2 つある (大きい回を選ぶ)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "a7", "--state-dir", "st7")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "a7", "--state-dir", "st7")
	snapshot(f)
	verifyBoth(f, "--final", "final.md", "--state-dir", "st7", "--run-id", "a7")
	clock(f, "2026-01-01T00:00:00Z")
	return f.done()
}

// verifyAmbiguousCase は同じ最終稿に原文の違う記録・資料の集まりの違う記録が複数あるとき。
func verifyAmbiguousCase() VisCase {
	f := newFixtureCase("verify-ambiguous")
	f.write("src.md", vSource).write("srcb.md", vSourceB).write("other.md", vOther).write("final.md", vFinal)
	f.write("mat.md", vMaterial).write("m/src.md", vSource).write("m/final.md", vFinal).write("m/mat.md", vMaterial)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "a1")
	pycli(f, "0", "run", "--source", "srcb.md", "--final", "final.md", "--run-id", "a2")
	chdir(f, "m")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "m1")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--material", "mat.md", "--run-id", "m2")
	chdir(f)
	snapshot(f)
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	verify(f, "--final", "final.md", "--source", "src.md")
	verify(f, "--final", "final.md", "--source", "srcb.md")
	verify(f, "--final", "final.md", "--source", "other.md")
	verify(f, "--final", "final.md", "--run-id", "a1")
	verifyStdin(f, vFinal, "--final", "-")
	chdir(f, "m")
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	verify(f, "--final", "final.md", "--source", "src.md")
	verify(f, "--final", "final.md", "--run-id", "m2")
	chdir(f)
	return f.done()
}

// verifyPlaceCase は検査ずみの最終稿を別の原文の記録があるパスへ写したとき (ambiguous) と、続けて直して前の最終稿の場所へ写したとき (さかのぼって同じ流れ)。
func verifyPlaceCase() VisCase {
	f := newFixtureCase("verify-place")
	f.write("a.md", vSource).write("b.md", vSourceB).write("fa.md", vFinal).write("fb.md", vFinal2)
	f.write("orig.md", vOther).write("doc.md", vSource).write("new.md", vFinal)
	chdir(f)
	pycli(f, "0", "run", "--source", "a.md", "--final", "fa.md", "--run-id", "p1")
	pycli(f, "0", "run", "--source", "b.md", "--final", "fb.md", "--run-id", "p2")
	f.write("fb.md", vFinal) // fa.md の最終稿を、別の原文の記録がある fb.md へ写す
	// 続けて直す流れ: orig → doc.md、doc.md → new.md、new.md を doc.md へ写す
	pycli(f, "0", "run", "--source", "orig.md", "--final", "doc.md", "--run-id", "q0", "--state-dir", "st")
	pycli(f, "0", "run", "--source", "doc.md", "--final", "new.md", "--run-id", "q1", "--state-dir", "st")
	f.write("doc.md", vFinal)
	snapshot(f)
	verify(f, "--final", "fb.md")
	verify(f, "--final", "fb.md", "--json")
	verify(f, "--final", "fb.md", "--source", "a.md")
	verify(f, "--final", "fa.md")
	verify(f, "--final", "doc.md", "--state-dir", "st")
	verify(f, "--final", "doc.md", "--state-dir", "st", "--json")
	return f.done()
}

// rewriteStates は推敲 (資料なし) の run を st<n> に作る stateSet。
func rewriteStates(f *fixtureCase) *stateSet {
	return &stateSet{f: f, prefix: "r", args: []string{"--source", "src.md", "--final", "final.md"}}
}

// verifyInvalidCase は形の正しくない記録: 読めない・format・形・場所・digest・最終稿のハッシュ・写しとレポートの欠け・件数の食い違い・--run-id で読めない回。
func verifyInvalidCase() VisCase {
	f := newFixtureCase("verify-invalid")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	s := rewriteStates(f)
	var r string
	r = s.add("1")
	edit(f, r, map[string]any{"raw": [][2]string{{`"format":`, `"format"`}}})
	r = s.add("2")
	redigest(f, r, set("other-format", "format"))
	r = s.add("3")
	redigest(f, r, set("1", "format_version"))
	r = s.add("4")
	redigest(f, r, set(true, "format_version"))
	r = s.add("5")
	redigest(f, r, set("x", "mode"))
	r = s.add("6")
	redigest(f, r, set("zz", "run_id"))
	r = s.add("7")
	redigest(f, r, set(2, "attempt"))
	r = s.add("8")
	edit(f, r, map[string]any{"set": []any{set(3, "fix_count")}})
	r = s.add("9")
	redigest(f, r, set(zeros64, "inputs", "final", "sha256"), set("inputs/"+zeros64+".bin", "inputs", "final", "snapshot"))
	s.add("10")
	f.op(Op{Kind: "remove", Path: f.dir("st10/inputs/" + sha256Hex(vSource) + ".bin")})
	s.add("11")
	f.op(Op{Kind: "remove", Path: f.dir("st11/runs/r11/attempt-1.lint.json")})
	s.add("12")
	f.write("st12/runs/r12/attempt-1.diff.txt", "x")
	r = s.add("13")
	redigest(f, r, set("runs/r13/attempt-9.lint.json", "checks", 0, "report"))
	r = s.add("14")
	redigest(f, r, set(9, "checks", 0, "summary", "candidate_count"))
	r = s.add("15")
	redigest(f, r, set(1, "result", "meaning_risk_count"))
	r = s.add("16")
	redigest(f, r, set(7, "checks", 1, "summary", "hunk_count"))
	r = s.add("17")
	redigest(f, r, set([]any{map[string]any{"rule": "calque", "word": "x", "lines": []any{1}}}, "checks", 0, "summary", "kept_marks"))
	r = s.add("18")
	edit(f, "st18/runs/r18/attempt-1.lint.json", map[string]any{"set": []any{set("pr", "context", "doc_type")}})
	edit(f, r, map[string]any{"rehash": true, "redigest": true})
	r = s.add("19")
	edit(f, "st19/runs/r19/attempt-1.lint.json", map[string]any{"set": []any{set(5, "findings")}})
	edit(f, r, map[string]any{"rehash": true, "redigest": true})
	r = s.add("20")
	edit(f, "st20/runs/r20/attempt-1.diff.json", map[string]any{"set": []any{set("x", "full_diff")}})
	edit(f, r, map[string]any{"rehash": true, "redigest": true})
	r = s.add("21")
	edit(f, "st21/runs/r21/attempt-1.lint.json", map[string]any{"raw": [][2]string{{"{", "["}}})
	edit(f, r, map[string]any{"rehash": true, "redigest": true})
	r = s.add("22")
	redigest(f, r, set("16.0.0", "python", "unicode_version"))
	r = s.add("23")
	redigest(f, r, set("Go ja-writing", "python", "implementation"), set("go1.26.0", "python", "version"))
	r = s.add("24")
	redigest(f, r, set("other/x.json", "checks", 0, "report"))
	s.add("25")
	f.write("st25/runs/r25/attempt-1.lint.json", "{}")
	r = s.add("26")
	redigest(f, r, set(map[string]any{"found": true, "dir": "/skill", "skill_md_sha256": zeros64, "version": "3.1.0"}, "skill"))
	r = s.add("27")
	edit(f, "st27/runs/r27/attempt-1.diff.json", map[string]any{"set": []any{set(map[string]any{"candidate_count": 0, "level": "none"}, "stiffness")}})
	edit(f, r, map[string]any{"rehash": true, "redigest": true})
	r = s.add("28")
	edit(f, "st28/runs/r28/attempt-1.lint.json", map[string]any{"set": []any{set([]any{5, "x", map[string]any{"rule": []any{}}}, "findings")}})
	edit(f, r, map[string]any{"rehash": true, "redigest": true})
	r = s.add("30")
	edit(f, r, map[string]any{"delete": []any{[]any{"checks", 0, "summary", "kept_marks"}}, "redigest": true})
	r = s.add("31")
	// 件数を同じ値の float にする (json.Marshal は 0.0 を 0 と書くので、字を置き換える)
	edit(f, "st31/runs/r31/attempt-1.lint.json", map[string]any{"raw": [][2]string{{`"candidate_count": 0,`, `"candidate_count": 0.0,`}}})
	edit(f, r, map[string]any{"rehash": true, "redigest": true})
	r = s.add("29")
	edit(f, "st29/runs/r29/attempt-1.lint.json", map[string]any{"set": []any{set([]any{map[string]any{"rule": "calque", "message": 5}}, "findings")}})
	edit(f, r, map[string]any{"rehash": true, "redigest": true})
	snapshot(f)
	for _, state := range s.states {
		verifyBoth(f, "--final", "final.md", "--state-dir", state)
	}
	verify(f, "--final", "final.md", "--state-dir", "st1", "--run-id", "r1")
	verify(f, "--final", "final.md", "--state-dir", "st1", "--run-id", "r1", "--json")
	verify(f, "--final", "final.md", "--state-dir", "st9", "--run-id", "r9")
	verify(f, "--final", "final.md", "--state-dir", "st6", "--run-id", "r6")
	verify(f, "--final", "final.md", "--state-dir", "st6", "--source", "src.md")
	verify(f, "--final", "final.md", "--state-dir", "st2", "--since", "2030-01-01T00:00:00Z")
	return f.done()
}

// verifyChecksCase は必要な検査が欠けた記録: 時間切れ・内部エラー・target の違い・項目が無い・要らない検査の名前。
func verifyChecksCase() VisCase {
	f := newFixtureCase("verify-checks")
	f.write("src.md", vSource).write("final.md", vFinal).write("mat.md", vMaterial)
	chdir(f)
	stopped := func(status string, i int) []any {
		return []any{
			set(status, "checks", i, "status"), set(nil, "checks", i, "exit_code"), set(nil, "checks", i, "report"),
			set(nil, "checks", i, "report_sha256"), set(nil, "checks", i, "text"), set(nil, "checks", i, "text_sha256"),
			set(nil, "checks", i, "summary"), set("止めた", "checks", i, "error"),
		}
	}
	s := rewriteStates(f)
	var r string
	r = s.add("1")
	edit(f, r, map[string]any{"set": stopped("timeout", 1), "redigest": true})
	r = s.add("2")
	edit(f, r, map[string]any{"set": stopped("error", 0), "redigest": true})
	r = s.add("3")
	edit(f, r, map[string]any{"set": append(stopped("timeout", 0), stopped("error", 1)...), "redigest": true})
	r = s.add("4")
	redigest(f, r, set(zeros64, "checks", 0, "target", "final"))
	r = s.add("5")
	edit(f, r, map[string]any{"delete": []any{[]any{"checks", 1}}, "redigest": true})
	r = s.add("6")
	redigest(f, r, set([]any{"lint", "diff", "materials"}, "required_checks"))
	r = s.add("7")
	edit(f, r, map[string]any{"set": stopped("skipped", 1), "redigest": true})
	r = s.add("8")
	edit(f, r, map[string]any{"set": append(stopped("timeout", 1), set(zeros64, "checks", 0, "target", "final")), "redigest": true})
	// 資料つきの推敲 (materials の検査と照合の件数)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--material", "mat.md", "--run-id", "rm", "--state-dir", "stm")
	s.keep("stm")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--material", "mat.md", "--run-id", "rn", "--state-dir", "stn")
	redigest(f, firstRecord("stn", "rn"), set(99, "materials_summary", "from_materials"))
	s.keep("stn")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--material", "mat.md", "--run-id", "ro", "--state-dir", "sto")
	edit(f, firstRecord("sto", "ro"), map[string]any{"set": stopped("timeout", 2), "redigest": true})
	s.keep("sto")
	snapshot(f)
	for _, state := range s.states {
		verifyBoth(f, "--final", "final.md", "--state-dir", state)
	}
	return f.done()
}

// verifyToolsCase は記録のあとで道具と語の一覧が変わったとき (スクリプトだけ・一覧だけ・両方・fact-gaps)。
func verifyToolsCase() VisCase {
	f := newFixtureCase("verify-tools")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	s := rewriteStates(f)
	var r string
	const hedges, nominal = "references/lists/hedges.txt", "references/lists/nominalization.txt"
	r = s.add("1")
	redigest(f, r, set(zeros64, "tools", "yomiyasu_check.py", "sha256"))
	r = s.add("2")
	redigest(f, r, set(zeros64, "tools", "yomiyasu_lint.py", "sha256"))
	r = s.add("3")
	redigest(f, r, set(zeros64, "tools", hedges, "sha256"))
	r = s.add("4")
	redigest(f, r, set(zeros64, "tools", hedges, "sha256"), set(zeros64, "tools", nominal, "sha256"))
	r = s.add("5")
	redigest(f, r, set(zeros64, "tools", "yomiyasu_diff.py", "sha256"), set(zeros64, "tools", hedges, "sha256"))
	r = s.add("6")
	edit(f, r, map[string]any{"delete": []any{[]any{"tools", hedges}}, "redigest": true})
	r = s.add("7")
	redigest(f, r, set(nil, "tools", hedges, "sha256"))
	r = s.add("8")
	redigest(f, r, set("9.9.9", "tools", "yomiyasu_check.py", "version"))
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--fact-gaps", "--run-id", "rf", "--state-dir", "stf")
	s.keep("stf")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--fact-gaps", "--run-id", "rg", "--state-dir", "stg")
	redigest(f, firstRecord("stg", "rg"), set(zeros64, "tools", "references/lists/fact-gaps.txt", "sha256"))
	s.keep("stg")
	r = s.add("9")
	redigest(f, r, set(map[string]any{"version": nil, "sha256": zeros64}, "tools", "references/lists/fact-gaps.txt"))
	snapshot(f)
	for _, state := range s.states {
		verifyBoth(f, "--final", "final.md", "--state-dir", state)
	}
	return f.done()
}

// verifyComposeCase は新規作成: 資料を原文とみなした記録・みなさない記録・前の回から引き継いだ記録と、その印を書き換えた記録・Unicode のバージョンの違い・道具の違い。
func verifyComposeCase() VisCase {
	f := newFixtureCase("verify-compose")
	f.write("mat.md", vMaterial).write("near.md", vComposed).write("far.md", vComposedFar).write("mat2.md", vSourceB)
	chdir(f)
	s := &stateSet{f: f, prefix: "c", args: []string{"--material", "mat.md", "--final", "near.md"}}
	finalsLate := map[string]string{}
	untreat := map[string]any{"delete": []any{[]any{"inputs", "materials", 0, "treated_as_source"}, []any{"inputs", "materials", 0, "treated_by"}}, "redigest": true}
	s.add("1")
	s.add("2", "far.md")
	edit(f, s.add("3"), untreat)
	redigest(f, s.add("4"), set("16.0.0", "python", "unicode_version"))
	redigest(f, s.add("5"), set(zeros64, "tools", "yomiyasu_check.py", "sha256"))
	redigest(f, s.add("6", "far.md"), set(true, "inputs", "materials", 0, "treated_as_source"), set("similarity", "inputs", "materials", 0, "treated_by"))
	redigest(f, s.add("7"), set("previous_attempt", "inputs", "materials", 0, "treated_by"))
	s.add("8")
	f.op(Op{Kind: "remove", Path: f.dir("st8/inputs/" + sha256Hex(vComposed) + ".bin")})
	edit(f, s.add("9"), map[string]any{"delete": []any{[]any{"python", "unicode_version"}}, "redigest": true})
	s.add("13")
	f.write("st13/inputs/"+sha256Hex(vMaterial)+".bin", vOther)
	// 前の回で原文とみなした資料を引き継ぐ: 1 回目は near、2 回目は far に書き換えた同じファイル
	f.write("inherit.md", vComposed)
	s.add("10", "inherit.md")
	f.write("inherit.md", vComposedFar)
	pycli(f, "0", "run", "--material", "mat.md", "--final", "inherit.md", "--state-dir", "st10")
	f.write("inherit2.md", vComposed)
	s.add("11", "inherit2.md")
	f.write("inherit2.md", vComposedFar)
	pycli(f, "0", "run", "--material", "mat.md", "--final", "inherit2.md", "--state-dir", "st11")
	edit(f, "st11/runs/c11/attempt-2.json", untreat)
	// 前の回が読めない記録 (st14) と、前の回が推敲の記録 (st15) の 2 回目
	for _, n := range []string{"14", "15"} {
		f.write("inherit"+n+".md", vComposed)
		s.add(n, "inherit"+n+".md")
		f.write("inherit"+n+".md", vComposedFar)
		pycli(f, "0", "run", "--material", "mat.md", "--final", "inherit"+n+".md", "--state-dir", "st"+n)
		finalsLate["st"+n] = "inherit" + n + ".md"
	}
	edit(f, firstRecord("st14", "c14"), map[string]any{"raw": [][2]string{{"{", "["}}})
	edit(f, firstRecord("st15", "c15"), map[string]any{"set": []any{set("rewrite", "mode")}})
	// 資料の写しが UTF-8 でない (ハッシュは合う)
	bad := string([]byte{0x80, 'a'})
	r := s.add("16")
	f.writeBytes("st16/inputs/"+sha256Hex(bad)+".bin", []byte(bad))
	redigest(f, r, set(sha256Hex(bad), "inputs", "materials", 0, "sha256"),
		set("inputs/"+sha256Hex(bad)+".bin", "inputs", "materials", 0, "snapshot"))
	// 資料 2 つ (原文とみなさない)
	pycli(f, "0", "run", "--material", "mat.md", "--material", "mat2.md", "--final", "far.md", "--run-id", "c12", "--state-dir", "st12")
	s.keep("st12")
	snapshot(f)
	finals := map[string]string{"st2": "far.md", "st6": "far.md", "st10": "inherit.md", "st11": "inherit2.md", "st12": "far.md"}
	for k, v := range finalsLate {
		finals[k] = v
	}
	finalOf := func(state string) string {
		if v, ok := finals[state]; ok {
			return v
		}
		return "near.md"
	}
	for _, state := range s.states {
		verifyBoth(f, "--final", finalOf(state), "--state-dir", state)
	}
	verify(f, "--final", "near.md", "--state-dir", "st1", "--source", "mat.md")
	verify(f, "--final", "near.md", "--state-dir", "st1", "--source", "far.md")
	verify(f, "--final", "far.md", "--state-dir", "st2", "--source", "mat.md")
	verify(f, "--final", "inherit.md", "--state-dir", "st10", "--run-id", "c10")
	return f.done()
}

// verifyStdinCase は標準入力で検査した記録 (--stdin の区切り) と、標準入力の最終稿で照らすとき。
func verifyStdinCase() VisCase {
	f := newFixtureCase("verify-stdin")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	f.op(Op{Kind: "stdin", Content: "<<<yomiyasu:original>>>\n" + vSource + "<<<yomiyasu:final>>>\n" + vFinal})
	pycli(f, "0", "run", "--stdin", "--run-id", "s1")
	f.op(Op{Kind: "stdin", Content: "<<<yomiyasu:material>>>\n" + vMaterial + "<<<yomiyasu:final>>>\n" + vComposed})
	pycli(f, "0", "run", "--stdin", "--run-id", "s2", "--state-dir", "st2")
	// by-path の索引に、名前の形が違うものと、stat できない (壊れたリンクの) ものを混ぜる
	byPath := ".yomiyasu/index/by-path/" + sha256Hex("stdin:"+sha256Hex(vSource))
	f.write(byPath+"/junk", "")
	f.op(Op{Kind: "symlink", Path: f.dir(byPath + "/20260101T000000Z.s9.1"), Target: "missing"})
	snapshot(f)
	verifyStdin(f, vFinal, "--final", "-")
	verifyStdin(f, vFinal, "--final", "-", "--json")
	verify(f, "--final", "final.md")
	verify(f, "--final", "final.md", "--json")
	verifyStdin(f, vFinal2, "--final", "-", "--source", "src.md")
	verifyStdin(f, vFinal2, "--final", "-", "--source", "src.md", "--json")
	verifyStdin(f, vFinal2, "--final", "-")
	verifyStdin(f, vComposed, "--final", "-", "--state-dir", "st2")
	verifyStdin(f, vFinal2, "--final", "-", "--state-dir", "st2")
	verifyStdin(f, strings.ReplaceAll(vFinal, "\n", "\r\n"), "--final", "-")
	return f.done()
}

// verifyContextCase は種類と読者・残した指摘の語・意味が変わる恐れの件数の出る記録。
func verifyContextCase() VisCase {
	f := newFixtureCase("verify-context")
	f.write("src.md", vSource).write("final.md", vFinal).write("calque.md", vCalque).write("calque-src.md", vCalque+"追記。\n")
	f.write("num-src.md", "参加者は12名です。\n").write("num.md", "参加者は13名です。\n")
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--doc-type", "pr", "--reader", "expert", "--run-id", "x1", "--state-dir", "st1")
	pycli(f, "0", "run", "--source", "calque-src.md", "--final", "calque.md", "--run-id", "x2", "--state-dir", "st2")
	pycli(f, "0", "run", "--final", "calque.md", "--material", "src.md", "--run-id", "x3", "--state-dir", "st3")
	pycli(f, "0,1", "run", "--source", "num-src.md", "--final", "num.md", "--run-id", "x4", "--state-dir", "st4")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--doc-type", "mail", "--run-id", "x5", "--state-dir", "st5")
	redigest(f, firstRecord("st5", "x5"), set("pr", "options", "doc_type"))
	pycli(f, "0", "run", "--source", "calque-src.md", "--final", "calque.md", "--run-id", "x6", "--state-dir", "st6")
	redigest(f, firstRecord("st6", "x6"), set([]any{map[string]any{"rule": "calque", "word": "見張る", "lines": []any{1.0}}}, "checks", 0, "summary", "kept_marks"))
	snapshot(f)
	verify(f, "--final", "final.md", "--state-dir", "st1")
	verify(f, "--final", "final.md", "--state-dir", "st1", "--json")
	verify(f, "--final", "calque.md", "--state-dir", "st2")
	verify(f, "--final", "calque.md", "--state-dir", "st2", "--json")
	verify(f, "--final", "calque.md", "--state-dir", "st3")
	verify(f, "--final", "calque.md", "--state-dir", "st3", "--json")
	verify(f, "--final", "num.md", "--state-dir", "st4")
	verify(f, "--final", "num.md", "--state-dir", "st4", "--json")
	verify(f, "--final", "final.md", "--state-dir", "st5")
	verify(f, "--final", "calque.md", "--state-dir", "st6")
	return f.done()
}

// verifyWordsCase は語の一覧 (--words) を使った記録と、残した指摘の語が多い (10 語・5 行を超える) 記録。
func verifyWordsCase() VisCase {
	f := newFixtureCase("verify-words")
	var lines []string
	for _, verb := range []string{"データを拾う", "要求を引き込む", "設定を抱える", "入力を見張る", "資源を食う", "変更を受け止める", "エラーを飲み込む", "手順を覚える", "数を足す", "利用者を助ける"} {
		lines = append(lines, "システムが"+verb+"。")
	}
	for range 5 {
		lines = append(lines, "システムが入力を見張る。")
	}
	many := strings.Join(append(lines, "入力が来る。"), "\n") + "\n"
	f.write("src.md", many+"追記。\n").write("many.md", many).write("w/flag.txt", "入力\t打ち込み）の候補です\n")
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "many.md", "--words", "w", "--run-id", "w1", "--state-dir", "st1")
	pycli(f, "0", "run", "--source", "src.md", "--final", "many.md", "--no-words", "--run-id", "w2", "--state-dir", "st2")
	pycli(f, "0", "run", "--source", "src.md", "--final", "many.md", "--words", "w", "--run-id", "w3", "--state-dir", "st3")
	redigest(f, firstRecord("st3", "w3"), set(nil, "options", "words"))
	snapshot(f)
	for _, state := range []string{"st1", "st2", "st3"} {
		verifyBoth(f, "--final", "many.md", "--state-dir", state)
	}
	return f.done()
}

// verifyEdgesCase は索引と記録の端の形: 索引の名前の形が違う・壊れたリンクの索引・形の分からない記録が同じ最終稿にある・
// --run-id の回の最終稿が dict でない・ハッシュでない・標準入力の最終稿と --run-id・by-path の最新の記録が読めない・
// 読めない runs・標準入力が無い。
func verifyEdgesCase() VisCase {
	f := newFixtureCase("verify-edges")
	f.write("src.md", vSource).write("final.md", vFinal).write("other.md", vOther).write("mat.md", vMaterial).write("near.md", vComposed)
	f.write("fc.md", vFinal2).write("srcb.md", vSourceB)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e1")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e2", "--state-dir", "st2")
	finalKey := ".yomiyasu/index/by-final/" + sha256Hex(vFinal)
	f.write(finalKey+"/junk", "")
	f.write(finalKey+"/e9.notanumber", "")
	f.op(Op{Kind: "symlink", Path: f.dir(finalKey + "/e8.1"), Target: "missing"})
	// 同じ最終稿に、資料が list でない記録 (e3) と、原文が dict でない記録 (e4)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e3", "--state-dir", "st3")
	edit(f, firstRecord("st3", "e3"), map[string]any{"set": []any{set("x", "inputs", "materials")}})
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e4", "--state-dir", "st3")
	edit(f, firstRecord("st3", "e4"), map[string]any{"set": []any{set(nil, "inputs", "source")}})
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e5", "--state-dir", "st3")
	// 新規作成で資料が list でない記録だけがある最終稿
	pycli(f, "0", "run", "--material", "mat.md", "--final", "near.md", "--run-id", "e6", "--state-dir", "st6")
	edit(f, firstRecord("st6", "e6"), map[string]any{"set": []any{set("x", "inputs", "materials")}})
	// --run-id の回の最終稿が dict でない (e7) と、ハッシュでない (e10)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e7", "--state-dir", "st7")
	edit(f, firstRecord("st7", "e7"), map[string]any{"set": []any{set("x", "inputs", "final")}})
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e10", "--state-dir", "st10")
	edit(f, firstRecord("st10", "e10"), map[string]any{"set": []any{set("x", "inputs", "final", "sha256")}})
	// by-path の最新の記録が読めない場所へ、検査ずみの最終稿を写す
	pycli(f, "0", "run", "--source", "srcb.md", "--final", "fc.md", "--run-id", "e11", "--state-dir", "st11")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e12", "--state-dir", "st11")
	edit(f, firstRecord("st11", "e11"), map[string]any{"raw": [][2]string{{"{", "["}}})
	f.write("fc.md", vFinal)
	// 原文を --final に渡したとき、runs に読めない記録がある・runs を読めない
	f.write("st2/runs/e2/attempt-5.json", "{")
	f.write("st2/runs/zz/attempt-1.json", "[]")
	f.write("st2/runs/a0/attempt-1.json", "[]")
	f.write("st2/runs/a1/attempt-1.json", `{"inputs": 1}`)
	// by-path の最新の記録の原文が分からない場所へ写す: 原文が null の推敲 (e14) と、資料が空の新規作成 (e16)
	f.write("fc2.md", vFinal2).write("fc3.md", vComposedFar)
	pycli(f, "0", "run", "--source", "srcb.md", "--final", "fc2.md", "--run-id", "e14", "--state-dir", "st14")
	edit(f, firstRecord("st14", "e14"), map[string]any{"set": []any{set(nil, "inputs", "source")}})
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e15", "--state-dir", "st14")
	pycli(f, "0", "run", "--material", "mat.md", "--final", "fc3.md", "--run-id", "e16", "--state-dir", "st15")
	edit(f, firstRecord("st15", "e16"), map[string]any{"set": []any{set([]any{}, "inputs", "materials")}})
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e17", "--state-dir", "st15")
	f.write("fc2.md", vFinal).write("fc3.md", vFinal)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "e13", "--state-dir", "st13")
	snapshot(f)
	none := uint32(0)
	f.op(Op{Kind: "chmod", Path: f.dir("st13/runs"), Mode: &none})
	verifyBoth(f, "--final", "final.md")
	verifyBoth(f, "--final", "final.md", "--state-dir", "st3")
	verifyBoth(f, "--final", "final.md", "--state-dir", "st3", "--source", "src.md")
	verifyBoth(f, "--final", "near.md", "--state-dir", "st6", "--source", "mat.md")
	verifyBoth(f, "--final", "final.md", "--state-dir", "st7", "--run-id", "e7")
	verifyBoth(f, "--final", "final.md", "--state-dir", "st10", "--run-id", "e10")
	verifyStdin(f, vOther, "--final", "-", "--run-id", "e1")
	verifyStdin(f, vOther, "--final", "-", "--run-id", "e1", "--json")
	verifyBoth(f, "--final", "fc.md", "--state-dir", "st11")
	verifyBoth(f, "--final", "src.md", "--state-dir", "st2")
	verifyBoth(f, "--final", "src.md", "--state-dir", "st13")
	verifyBoth(f, "--final", "fc2.md", "--state-dir", "st14")
	verifyBoth(f, "--final", "fc3.md", "--state-dir", "st15")
	f.op(Op{Kind: "stdin", Name: "none"})
	verify(f, "--final", "-")
	back := uint32(0o700)
	f.op(Op{Kind: "chmod", Path: f.dir("st13/runs"), Mode: &back})
	return f.done()
}

// verifyArgsCase は引数の誤り (argparse・実行 ID・時刻・秒数・パス・読めない最終稿) と --version と -h。
func verifyArgsCase() VisCase {
	f := newFixtureCase("verify-args")
	f.write("final.md", vFinal).write(".hidden.md", vFinal).write("d/x.md", vFinal).writeBytes("latin.md", []byte{0xff, 0xfe, 'a'})
	f.mkdir("folder.md")
	f.write("../verify-args-outside.md", vFinal)
	chdir(f)
	for _, args := range [][]string{
		{}, {"--json"}, {"--final"}, {"--final", "final.md", "--bogus"}, {"--final", "final.md", "extra"},
		{"--final", "final.md", "--state-dir", ""}, {"--final", "final.md", "--state-dir", "  "}, {"--final", "-", "--source", "-"},
		{"--final", "final.md", "--since", "yesterday"}, {"--final", "final.md", "--since", ""}, {"--final", "final.md", "--since", "2026-01-01T00:00:00+24:00"},
		{"--final", "final.md", "--timeout", "0"}, {"--final", "final.md", "--timeout", "x"}, {"--final", "final.md", "--timeout", "-1"},
		{"--final", "final.md", "--run-id", "-x"}, {"--final", "final.md", "--run-id", ""},
		{"--final", ".hidden.md"}, {"--final", "../verify-args-outside.md"}, {"--final", "final.md", "--source", ".hidden.md"},
		{"--final", "missing.md"}, {"--final", "folder.md"}, {"--final", "final.md/"}, {"--final", "final.md", "--source", "missing.md"},
		{"--final", "latin.md"}, {"--final", "final.md", "--replay", "--state-dir", "nowhere"}, {"--final=final.md"},
		{"--final", "final.md", "--json=1"}, {"--fin", "final.md"}, {"-h"}, {"--final", "final.md", "-h"}, {"--help"},
		{"--final", "d/x.md"},
	} {
		verify(f, args...)
	}
	f.op(Op{Kind: "stdin", BytesB64: base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe})})
	verify(f, "--final", "-")
	f.call(Call{Module: CheckModule, Function: "main", Args: []any{[]string{"--version"}}, Digest: true, Keep: true, Capture: true})
	return f.done()
}

// verifyTiesCase は「最新」の並べ方の同点: created_at と書いた時刻と回が同じ 2 つの実行 (索引の名前の順で最初のもの)、
// 同じ実行の 2 回 (回の大きい方)、by-path の索引の時刻と書いた時刻と回が同じ 2 つの実行 (実行 ID の大きい方)。
func verifyTiesCase() VisCase {
	f := newFixtureCase("verify-ties")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "t1", "--state-dir", "st")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "t2", "--state-dir", "st")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "u1", "--state-dir", "su")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "u1", "--state-dir", "su")
	snapshotTies(f, []string{"st/runs/t*/attempt-1.json"}, []string{"su/runs/u1/attempt-*.json"})
	verifyBoth(f, "--final", "final.md", "--state-dir", "st")
	verifyBoth(f, "--final", "final.md", "--state-dir", "su")
	// 最終稿の索引が無い: by-path の同点から選ぶ
	f.op(Op{Kind: "remove", Path: f.dir("st/index/by-final")})
	snapshotTies(f, []string{"st/runs/t*/attempt-1.json"}, []string{"st/index/by-path/*/*.t[12].1"})
	verifyBoth(f, "--final", "final.md", "--state-dir", "st")
	return f.done()
}

// verifySummaryEdgesCase は記録とレポートの照らしの端: 資料を逆の順で渡した 2 つの実行 (同じ資料の集まり)・diff の
// fail_on_meaning_risk_count が真偽 (数に入れない)・created_at が時刻でない記録 (「最新」の並べ方で空にする)・report が runs/ で始まらない。
func verifySummaryEdgesCase() VisCase {
	f := newFixtureCase("verify-summary-edges")
	f.write("src.md", vSource).write("final.md", vFinal).write("a.md", vMaterial).write("b.md", vSourceB)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--material", "a.md", "--material", "b.md", "--run-id", "m1", "--state-dir", "sm")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--material", "b.md", "--material", "a.md", "--run-id", "m2", "--state-dir", "sm")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "b1", "--state-dir", "sb")
	edit(f, "sb/runs/b1/attempt-1.diff.json", map[string]any{"set": []any{set(true, "fail_on_meaning_risk_count")}})
	edit(f, firstRecord("sb", "b1"), map[string]any{"set": []any{set(true, "checks", 1, "summary", "fail_on_meaning_risk_count")}, "rehash": true, "redigest": true})
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "c1", "--state-dir", "sc")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "c2", "--state-dir", "sc")
	redigest(f, firstRecord("sc", "c2"), set("zzzz", "created_at"))
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "d1", "--state-dir", "sd")
	redigest(f, firstRecord("sd", "d1"), set("./runs/d1/attempt-1.diff.json", "checks", 1, "report"), set(99, "checks", 1, "summary", "candidate_count"))
	snapshot(f)
	for _, state := range []string{"sm", "sb", "sc", "sd"} {
		verifyBoth(f, "--final", "final.md", "--state-dir", state)
	}
	return f.done()
}

// verifyHugeAttemptCase は回の番号が int64 に収まらない記録: --run-id の最大の回・最終稿の索引が指す回・あとの回で別の本文を検査した回。
func verifyHugeAttemptCase() VisCase {
	const huge = "99999999999999999999"
	f := newFixtureCase("verify-huge-attempt")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "p1", "--state-dir", "sp")
	copyFile(f, firstRecord("sp", "p1"), "sp/runs/p1/attempt-"+huge+".json")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "q1", "--state-dir", "sq")
	copyFile(f, firstRecord("sq", "q1"), "sq/runs/q1/attempt-"+huge+".json")
	f.write("sq/index/by-final/"+sha256Hex(vFinal)+"/q1."+huge, "")
	// 2 回目 (別の本文) の記録を、int64 に収まらない回の名前へ移す
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "n1", "--state-dir", "sn")
	f.write("final.md", vFinal2)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "n1", "--state-dir", "sn")
	copyFile(f, "sn/runs/n1/attempt-2.json", "sn/runs/n1/attempt-"+huge+".json")
	f.op(Op{Kind: "remove", Path: f.dir("sn/runs/n1/attempt-2.json")})
	f.write("final.md", vFinal)
	snapshotTies(f, []string{"sq/runs/q1/attempt-*.json"})
	verifyBoth(f, "--final", "final.md", "--state-dir", "sp", "--run-id", "p1")
	verifyBoth(f, "--final", "final.md", "--state-dir", "sp")
	verifyBoth(f, "--final", "final.md", "--state-dir", "sq")
	verifyBoth(f, "--final", "final.md", "--state-dir", "sn")
	return f.done()
}

// verifyAttemptOrderCase は回の番号を字の順と大きさの順が食い違う番号 (9 と 10) へ移した記録: あとの回で別の本文を検査した回・
// --run-id の最大の回・同点の並べ方 (同じ実行の 2 回・by-path の索引)・前の回で原文とみなした資料の引き継ぎ。名前の先頭に 0 を付けた回
// (本家は 0 を落とした名前のファイルを探す) と、int64 に収まらない回へ記録の attempt とレポートまで移した読める記録も。
func verifyAttemptOrderCase() VisCase {
	const huge = "99999999999999999999"
	f := newFixtureCase("verify-attempt-order")
	f.write("src.md", vSource).write("final.md", vFinal).write("mat.md", vMaterial).write("near.md", vComposed)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "n1", "--state-dir", "sn")
	f.write("final.md", vFinal2)
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "n1", "--state-dir", "sn")
	moveAttempt(f, "sn", "n1", "2", "10")
	moveAttempt(f, "sn", "n1", "1", "9")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "t1", "--state-dir", "st")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "t1", "--state-dir", "st")
	moveAttempt(f, "st", "t1", "2", "10")
	moveAttempt(f, "st", "t1", "1", "9")
	pycli(f, "0", "run", "--material", "mat.md", "--final", "near.md", "--run-id", "c1", "--state-dir", "sc")
	f.write("near.md", vComposedFar)
	pycli(f, "0", "run", "--material", "mat.md", "--final", "near.md", "--run-id", "c1", "--state-dir", "sc")
	moveAttempt(f, "sc", "c1", "2", "10")
	moveAttempt(f, "sc", "c1", "1", "9")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "z1", "--state-dir", "sz")
	renameAttempt(f, "sz", "z1", "1", "01")
	pycli(f, "0", "run", "--source", "src.md", "--final", "final.md", "--run-id", "h1", "--state-dir", "sh")
	moveAttempt(f, "sh", "h1", "1", huge)
	f.write("final.md", vFinal)
	snapshotTies(f, []string{"st/runs/t1/attempt-*.json"})
	verifyBoth(f, "--final", "final.md", "--state-dir", "sn")
	verifyBoth(f, "--final", "final.md", "--state-dir", "sn", "--run-id", "n1")
	f.write("final.md", vOther)
	verifyBoth(f, "--final", "final.md", "--state-dir", "sn", "--run-id", "n1")
	f.write("final.md", vFinal2)
	verifyBoth(f, "--final", "final.md", "--state-dir", "st")
	verifyBoth(f, "--final", "final.md", "--state-dir", "st", "--run-id", "t1")
	verifyBoth(f, "--final", "near.md", "--state-dir", "sc")
	verifyBoth(f, "--final", "near.md", "--state-dir", "sc", "--run-id", "c1")
	// sz と sh の記録の最終稿は vFinal2
	verifyBoth(f, "--final", "final.md", "--state-dir", "sz")
	verifyBoth(f, "--final", "final.md", "--state-dir", "sz", "--run-id", "z1")
	verifyBoth(f, "--final", "final.md", "--state-dir", "sh")
	verifyBoth(f, "--final", "final.md", "--state-dir", "sh", "--run-id", "h1")
	f.op(Op{Kind: "remove", Path: f.dir("st/index/by-final")})
	snapshotTies(f, []string{"st/runs/t1/attempt-*.json"}, []string{"st/index/by-path/*/*.t1.*"})
	verifyBoth(f, "--final", "final.md", "--state-dir", "st")
	clock(f, "2026-01-01T00:00:00Z")
	return f.done()
}
