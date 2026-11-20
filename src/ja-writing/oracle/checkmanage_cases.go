package oracle

import (
	"encoding/json"
	"strings"
)

// CheckManageFile は本家 yomiyasu_check.py の start・end・export と登録 (armed/・ロック・管理の上限) を、時刻・実行 ID・time.monotonic を
// 固定して動かした記録 (testdata/golden/ の中)。形は check-run.json と同じ: main の標準出力・標準エラー・終了コードと、状態フォルダの木
// (op snapshot)。1078 の hook が呼ぶ本家の関数 (armed_entries・armed_update・start_run・verify_quick・run_known_texts・latest_final_path・
// text_sha256_of) を driver で直接呼んだ戻り値と例外も持つ。
// Go のテストは同じ呼び出しを check の Main と公開の関数で動かし、出力と戻り値を比べ、自分が書いた木を記録の木と比べる。
// 逆向き: 木の記録の python の欄を Go の値にした (goize) 後に、本家の verify・export・end・run を当てた出力も持つ。
const CheckManageFile = "check-manage.json"

// CheckManageFunctions は記録の対象の関数。
var CheckManageFunctions = []string{"main", "armed_entries", "armed_update", "start_run", "verify_quick", "run_known_texts", "latest_final_path", "text_sha256_of"}

// ArmedChange は armed_update に渡す change (driver の {"$armed_change"}。登録の鍵の道に値を入れ、Write が偽なら書かせない)。
type ArmedChange struct {
	Set   [][]any `json:"set"`
	Write bool    `json:"write"`
}

func (c ArmedChange) MarshalJSON() ([]byte, error) {
	type plain ArmedChange
	return json.Marshal(map[string]any{"$armed_change": plain(c)})
}

// start・end・export は本家の check の main をその subcommand で呼ぶ。
func start(f *fixtureCase, args ...string)  { checkMain(f, append([]string{"start"}, args...)) }
func end(f *fixtureCase, args ...string)    { checkMain(f, append([]string{"end"}, args...)) }
func export(f *fixtureCase, args ...string) { checkMain(f, append([]string{"export"}, args...)) }

// both は人が読む出力と --json の両方で呼ぶ (--json は digest だけを残す)。
func both(f *fixtureCase, sub string, args ...string) {
	checkMain(f, append([]string{sub}, args...))
	f.call(Call{Module: CheckModule, Function: "main", Args: []any{Argv(append(append([]string{sub}, args...), "--json"))}, Digest: true, Capture: true})
}

// hook は 1078 の hook が呼ぶ本家の関数を直接呼ぶ (戻り値と例外を残す)。
func hook(f *fixtureCase, fn string, args []any, kwargs map[string]any) {
	f.call(Call{Module: CheckModule, Function: fn, Args: args, Kwargs: kwargs, Digest: true, Keep: true})
}

// utime は rel の mtime を今から seconds 秒ずらす。
func utime(f *fixtureCase, rel, seconds string) {
	f.op(Op{Kind: "utime", Path: f.dir(rel), Value: seconds})
}

// CheckManageCases は check-manage.json の case。
func CheckManageCases() []VisCase {
	cases := []VisCase{
		manageFlowCase(), manageLimitCase(), manageReserveCase(), manageStartCase(), manageLockCase(), manageExportCase(),
		manageEndCase(), manageSameFileCase(), manageArgsCase(), manageHookCase(), manageSurrogateCase(),
	}
	for i := range cases {
		scopeRefs(&cases[i])
	}
	return cases
}

// manageFlowCase は start → run → run → verify → export → end の流れ。最後に木の python の欄を Go の値にしてから、本家の verify・export・end を当てる (逆向き)。
func manageFlowCase() VisCase {
	f := newFixtureCase("manage-flow")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	both(f, "start", "--source", "src.md", "--final", "final.md", "--run-id", "a1")
	snapshot(f)
	run(f, "--source", "src.md", "--final", "final.md")
	run(f, "--source", "src.md", "--final", "final.md", "--json")
	f.write("final.md", vFinal2)
	run(f, "--source", "src.md", "--final", "final.md")
	snapshot(f)
	both(f, "verify", "--final", "final.md")
	both(f, "export", "--final", "final.md")
	export(f, "--final", "final.md", "--run-id", "a1")
	snapshot(f)
	goize(f, ".yomiyasu/runs/a1/attempt-1.json", ".yomiyasu/runs/a1/attempt-2.json", ".yomiyasu/runs/a1/attempt-3.json")
	both(f, "verify", "--final", "final.md")
	both(f, "export", "--final", "final.md")
	both(f, "end", "--source", "src.md")
	end(f, "--run-id", "a1")
	both(f, "start", "--source", "src.md")
	run(f, "--source", "src.md", "--final", "final.md")
	snapshot(f)
	return f.done()
}

// manageLimitCase は管理の上限: run の回数 (--max-runs)・期限 (--deadline-min)・上限の後の run (unverified のまま)・--new-run と --no-record
// (登録を見ない)・--run-id で登録の実行につなぐ・hook が unverified にした登録 (理由が null・別の理由・str でない理由)。
func manageLimitCase() VisCase {
	f := newFixtureCase("manage-limit")
	f.write("src.md", vSource).write("final.md", vFinal).write("src2.md", vSourceB).write("final2.md", vOther)
	chdir(f)
	start(f, "--source", "src.md", "--run-id", "l1", "--max-runs", "1")
	run(f, "--source", "src.md", "--final", "final.md")
	both(f, "run", "--source", "src.md", "--final", "final.md")
	run(f, "--source", "src.md", "--final", "final.md", "--new-run")
	run(f, "--source", "src.md", "--final", "final.md", "--no-record")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "l1")
	snapshot(f)
	start(f, "--source", "src2.md", "--run-id", "l2", "--deadline-min", "1")
	clock(f, "2026-01-01T00:01:00Z")
	run(f, "--source", "src2.md", "--final", "final2.md")
	clock(f, "2026-01-01T00:01:01Z")
	both(f, "run", "--source", "src2.md", "--final", "final2.md")
	run(f, "--source", "src2.md", "--final", "final2.md", "--run-id", "l2")
	snapshot(f)
	clock(f, "2026-01-01T00:00:00Z")
	f.write("src3.md", vRichSource).write("final3.md", vFinal)
	// 登録のセッションは、run に --session も YOMIYASU_SESSION も無ければ記録の host.session に入る
	f.write("src4.md", "四つめの原文です。\n").write("final4.md", "四つめの最終稿です。\n")
	start(f, "--source", "src4.md", "--run-id", "l4", "--session", "armed-sess")
	run(f, "--source", "src4.md", "--final", "final4.md")
	run(f, "--source", "src4.md", "--final", "final4.md", "--session", "given")
	for _, reason := range []any{nil, "stop_limit", 5} {
		writeArmed(f, "l3", "src3.md", vRichSource, map[string]any{"status": "unverified", "status_reason": reason})
		both(f, "run", "--source", "src3.md", "--final", "final3.md")
	}
	snapshot(f)
	return f.done()
}

// vRichSource は別の原文 (manage-limit の 3 つ目の登録)。
const vRichSource = "会議は3月5日に開きます。参加者は12名です。\n"

// writeArmed は登録 armed/<実行ID>.json を、start が書く形の値に override を重ねて書く (op write なので Go のテストも同じものを書く。
// driver だけが動かす pyedit を使わない)。原文は改行で終わる、BOM の無い文 (text_sha256 が sha256 と同じ)。
func writeArmed(f *fixtureCase, runID, sourceRel, sourceText string, override map[string]any) {
	digest := sha256Hex(sourceText)
	entry := map[string]any{
		"format": "yomiyasu-armed", "format_version": 1, "run_id": runID, "created_at": "2026-01-01T00:00:00Z",
		"deadline": "2026-01-01T00:30:00Z", "by": "start", "host": nil, "session_id": nil, "bound_at": nil, "cwd": f.dir(),
		"targets": map[string]any{"source": map[string]any{"path": f.dir(sourceRel), "sha256": digest, "text_sha256": digest,
			"snapshot": "inputs/" + digest + ".bin"}, "final": nil},
		"limits": map[string]any{"max_stop_blocks": 2, "max_runs": 3, "deadline_min": 30}, "counters": map[string]any{"stop_blocks": 0},
		"status": "active", "status_reason": nil, "verified": nil, "notified": false, "log": []any{},
	}
	for k, v := range override {
		entry[k] = v
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // <<ROOT>> の印を \u003c の形にしない (op write の中身の印は字のまま置き換える)
	if err := enc.Encode(entry); err != nil {
		panic(err)
	}
	f.write(".yomiyasu/armed/"+runID+".json", b.String())
}

// manageReserveCase は予約の後始末と、中身の無い予約の数え方: run が記録を書けない (状態フォルダの inputs がファイル) と確保した回を消す・
// 10 分以内の空の予約は回数に数える・古い空の予約は数えない・中身のある回は古くても数える。
func manageReserveCase() VisCase {
	f := newFixtureCase("manage-reserve")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	start(f, "--source", "src.md", "--run-id", "r1", "--max-runs", "2")
	f.op(Op{Kind: "remove", Path: f.dir(".yomiyasu/inputs")})
	f.write(".yomiyasu/inputs", "x")
	both(f, "run", "--source", "src.md", "--final", "final.md")
	snapshot(f)
	f.op(Op{Kind: "remove", Path: f.dir(".yomiyasu/inputs")})
	f.mkdir(".yomiyasu/inputs")
	f.write(".yomiyasu/runs/r1/attempt-7.json", "")
	f.write(".yomiyasu/runs/r1/attempt-8.json", "")
	utime(f, ".yomiyasu/runs/r1/attempt-8.json", "-601")
	run(f, "--source", "src.md", "--final", "final.md")
	snapshot(f)
	run(f, "--source", "src.md", "--final", "final.md")
	f.op(Op{Kind: "remove", Path: f.dir(".yomiyasu/runs/r1/attempt-7.json")})
	utime(f, ".yomiyasu/runs/r1/attempt-9.json", "-3600")
	run(f, "--source", "src.md", "--final", "final.md")
	snapshot(f)
	// 1 回までの登録で、ほかの run の中身の無い予約 (10 分以内) があれば上限、10 分を過ぎた予約は数えない (op snapshot は mtime を昔にそろえるので、予約は後に書く)
	f.write("src2.md", vSourceB)
	start(f, "--source", "src2.md", "--run-id", "r2", "--max-runs", "1")
	f.write(".yomiyasu/runs/r2/attempt-1.json", "")
	run(f, "--source", "src2.md", "--final", "final.md")
	utime(f, ".yomiyasu/runs/r2/attempt-1.json", "-601")
	run(f, "--source", "src2.md", "--final", "final.md")
	snapshot(f)
	return f.done()
}

// manageStartCase は start: 同じ原文の二重の start (前の登録を superseded で終える)・すでにある実行 ID (登録と runs/ のどちら)・大文字小文字だけ違う実行 ID・
// --host と --session (結び付けた時刻)・環境変数のセッション・語の一覧 (--json の words)・上限の境・別の原文の登録は終えない。
func manageStartCase() VisCase {
	f := newFixtureCase("manage-start")
	f.write("src.md", vSource).write("other.md", vOther)
	f.write(".yomiyasu-words/allow.txt", "確かめ\n")
	chdir(f)
	start(f, "--source", "src.md")
	start(f, "--source", "other.md")
	both(f, "start", "--source", "src.md", "--no-words")
	start(f, "--source", "src.md", "--run-id", "s1", "--host", "codex", "--session", "sess", "--max-stop-blocks", "0", "--max-runs", "10",
		"--deadline-min", "240", "--final", "out/final.md")
	start(f, "--source", "src.md", "--run-id", "s1")
	start(f, "--source", "src.md", "--run-id", "S1")
	f.mkdir(".yomiyasu/runs/s2")
	start(f, "--source", "src.md", "--run-id", "s2")
	setenv(f, "YOMIYASU_SESSION", "env-sess")
	both(f, "start", "--source", "src.md", "--run-id", "s3", "--host", "claude-code", "--max-stop-blocks", " 5 ", "--max-runs", "+1", "--deadline-min", "１")
	setenv(f, "YOMIYASU_SESSION", "${X}")
	start(f, "--source", "src.md", "--run-id", "s4", "--host", "codex")
	unsetenv(f, "YOMIYASU_SESSION")
	start(f, "--source", "src.md", "--run-id", "s5", "--session", "")
	start(f, "--source", "src.md", "--run-id", "s6", "--final", "")
	snapshot(f)
	hook(f, "armed_entries", []any{f.dir(".yomiyasu")}, nil)
	return f.done()
}

// manageLockCase はロック: 使用中のロック (時間切れで rc 3。待つ時計を進める)・古いロック (取り直す)・ロックの名前がフォルダ・start の 2 つ目のロック。
func manageLockCase() VisCase {
	f := newFixtureCase("manage-lock")
	f.write("src.md", vSource).write("final.md", vFinal)
	chdir(f)
	start(f, "--source", "src.md", "--run-id", "k1")
	f.op(Op{Kind: "monotonic", Value: "step"}) // 待つ時計を呼ぶたびに進める (止めた時計では使用中のロックを待ち終わらない)
	f.write(".yomiyasu/armed/k1.lock", "")
	end(f, "--run-id", "k1")
	run(f, "--source", "src.md", "--final", "final.md")
	f.write(".yomiyasu/armed/k2.lock", "")
	start(f, "--source", "src.md", "--run-id", "k2")
	snapshot(f)
	utime(f, ".yomiyasu/armed/k1.lock", "-60")
	both(f, "end", "--run-id", "k1")
	f.op(Op{Kind: "remove", Path: f.dir(".yomiyasu/armed/k2.lock")})
	f.mkdir(".yomiyasu/armed/k2.lock")
	start(f, "--source", "src.md", "--run-id", "k2")
	f.op(Op{Kind: "monotonic"})
	snapshot(f)
	return f.done()
}

// manageExportCase は export: 記録が無い・照合が合う (拡張子の形: .md・無い・2 つ・長すぎる・記号)・同じ写しがある・違う中身の写しがある・
// --run-id・原文が合わない・原文を読めない・最終稿が変わった・最終稿が無い・書き出し先に書けない。
func manageExportCase() VisCase {
	f := newFixtureCase("manage-export")
	f.write("src.md", vSource).write("final.md", vFinal).write("other.md", vOther)
	for _, name := range exportNames {
		f.write(name, vFinal+name+"\n")
	}
	chdir(f)
	both(f, "export", "--final", "final.md")
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "e1")
	for _, name := range exportNames {
		run(f, "--source", "src.md", "--final", name, "--run-id", "e1")
		export(f, "--final", name)
	}
	both(f, "export", "--final", "final.md")
	export(f, "--final", "final.md")
	copied := ".yomiyasu/exports/e1/attempt-1-" + sha256Hex(vFinal)[:12] + ".md"
	writable := uint32(0o644)
	f.op(Op{Kind: "chmod", Path: f.dir(copied), Mode: &writable})
	f.write(copied, "違う中身\n")
	export(f, "--final", "final.md", "--run-id", "e1")
	export(f, "--final", "final.md", "--source", "other.md")
	export(f, "--final", "final.md", "--source", "missing.md")
	export(f, "--final", "missing.md")
	export(f, "--final", "final.md", "--run-id", "nope")
	snapshot(f)
	f.write("final.md", vFinal2)
	both(f, "export", "--final", "final.md")
	f.write("final.md", vFinal)
	f.op(Op{Kind: "remove", Path: f.dir(copied)})
	ro := uint32(0o555)
	f.op(Op{Kind: "chmod", Path: f.dir(".yomiyasu/exports/e1"), Mode: &ro})
	export(f, "--final", "final.md")
	rw := uint32(0o755)
	f.op(Op{Kind: "chmod", Path: f.dir(".yomiyasu/exports/e1"), Mode: &rw})
	snapshot(f)
	return f.done()
}

// exportNames は写しの拡張子の形を試す最終稿の名前 (拡張子が無い・2 つ・長すぎる・記号・点で終わる)。
var exportNames = []string{"plain", "a.tar.gz", "x.abcdefghijk", "x.m-d", "x."}

// manageEndCase は end: 登録が無い (状態フォルダが無い・armed/ が無い)・実行 ID で・原文で (別の綴りのパス)・終えた登録をもう一度・壊れた登録は飛ばす。
func manageEndCase() VisCase {
	f := newFixtureCase("manage-end")
	f.write("src.md", vSource).write("sub/src.md", vSourceB)
	chdir(f)
	both(f, "end", "--run-id", "n1")
	f.mkdir(".yomiyasu")
	end(f, "--source", "src.md")
	start(f, "--source", "src.md", "--run-id", "n1")
	start(f, "--source", "sub/src.md", "--run-id", "n2")
	f.write(".yomiyasu/armed/n3.json", "{")
	f.write(".yomiyasu/armed/bad name.json", "{}")
	both(f, "end", "--source", "./sub/../src.md")
	end(f, "--source", "src.md")
	end(f, "--run-id", "n2")
	end(f, "--run-id", "n2")
	snapshot(f)
	return f.done()
}

// manageSameFileCase は原文と最終稿が同じファイルの run: start の写しを原文にする (--run-id なし・あり)・6 時間を過ぎた start・別の作業フォルダの start・
// start が無い・--new-run と --no-record (写しを使わず 2)・写しが消えた・start.json の digest が合わない・原文を UTF-8 として読めない。
func manageSameFileCase() VisCase {
	f := newFixtureCase("manage-same")
	f.write("doc.md", vSource).write("doc2.md", vSourceB).write("sub/x.md", "x\n")
	chdir(f)
	start(f, "--source", "doc.md", "--run-id", "m1")
	f.write("doc.md", vFinal)
	both(f, "run", "--source", "doc.md", "--final", "doc.md")
	run(f, "--source", "doc.md", "--final", "doc.md", "--run-id", "m1")
	run(f, "--source", "doc.md", "--final", "doc.md", "--new-run")
	run(f, "--source", "doc.md", "--final", "doc.md", "--no-record")
	snapshot(f)
	end(f, "--run-id", "m1")
	run(f, "--source", "doc.md", "--final", "doc.md")
	clock(f, "2026-01-01T06:00:01Z")
	run(f, "--source", "doc.md", "--final", "doc.md")
	run(f, "--source", "doc.md", "--final", "doc.md", "--run-id", "m1")
	clock(f, "2026-01-01T00:00:00Z")
	start(f, "--source", "doc2.md", "--run-id", "m2")
	f.write("doc2.md", vOther)
	f.op(Op{Kind: "remove", Path: f.dir(".yomiyasu/inputs/" + sha256Hex(vSourceB) + ".bin")})
	run(f, "--source", "doc2.md", "--final", "doc2.md")
	start(f, "--source", "doc.md", "--run-id", "m3")
	f.write("doc.md", vFinal2)
	f.write(".yomiyasu/runs/m3/start.json", `{"format": "yomiyasu-check-start", "record_digest": "sha256:`+zeros64+`"}`+"\n")
	run(f, "--source", "doc.md", "--final", "doc.md")
	chdir(f, "sub")
	start(f, "--source", "x.md", "--run-id", "m4", "--state-dir", "../.yomiyasu")
	chdir(f)
	f.write("sub/x.md", "y\n")
	run(f, "--source", "sub/x.md", "--final", "sub/x.md")
	f.writeBytes("bad.md", []byte{0xff, '\n'})
	run(f, "--source", "bad.md", "--final", "bad.md")
	snapshot(f)
	return f.done()
}

// manageArgsCase は start・export・end の使い方と入力の誤り (どれも何も書かずに 2) と -h、start が状態フォルダに書けない (rc 3)。
func manageArgsCase() VisCase {
	f := newFixtureCase("manage-args")
	f.write("src.md", vSource).write("final.md", vFinal).write(".hidden.md", vSource)
	f.writeBytes("bad.md", []byte{0xff, '\n'})
	f.write("big.md", strings.Repeat("あ", 2*1024*1024/3+1))
	f.write("words/allow.txt", "語\n")
	f.mkdir("dir.md").write("blocker", "x")
	chdir(f)
	for _, args := range [][]string{
		{"start", "-h"}, {"export", "--help"}, {"end", "-h"},
		{"start"}, {"start", "--source", "src.md", "--host", "x"}, {"start", "--source", "src.md", "--max-runs", "x"},
		{"start", "--source", "src.md", "--max-runs", "3", "--max-runs", "zz", "--bogus"}, {"start", "--bogus", "--source", "src.md"},
		{"start", "--max-runs", "x", "-h"}, {"start", "-h", "--max-runs", "x"}, {"start", "--source"},
		{"start", "--source", "src.md", "--max-runs", "0"}, {"start", "--source", "src.md", "--max-runs", "11"},
		{"start", "--source", "src.md", "--deadline-min", "241"}, {"start", "--source", "src.md", "--max-stop-blocks", "-1"},
		{"start", "--source", "src.md", "--max-runs", "1" + strings.Repeat("0", 30)},
		{"start", "--source", "src.md", "--deadline-min", "1_0"}, {"start", "--source", "src.md", "--deadline-min", "1__0"},
		{"start", "--source", "-"}, {"start", "--source", "src.md", "--final", "-"}, {"start", "--source", "../src.md"},
		{"start", "--source", ".hidden.md"}, {"start", "--source", "src.md", "--final", "../out.md"},
		{"start", "--source", "missing.md"}, {"start", "--source", "dir.md"}, {"start", "--source", "bad.md"}, {"start", "--source", "big.md"},
		{"start", "--source", "src.md", "--words", "words", "--no-words"}, {"start", "--source", "src.md", "--words", "nowhere"},
		{"start", "--source", "src.md", "--state-dir", " "}, {"start", "--source", "src.md", "--run-id", "bad/id"},
		{"start", "--source", "src.md", "--state-dir", "blocker/st"},
		{"start", "--source", "src.md", "--json", "extra"},
		{"export"}, {"export", "--final", "-"}, {"export", "--final", "final.md", "--source", "-"}, {"export", "--final", "../x.md"},
		{"export", "--final", "final.md", "--run-id", ".x"}, {"export", "--final", "dir.md"}, {"export", "--final", "bad.md"},
		{"export", "--final", "final.md", "--state-dir", ""},
		{"end"}, {"end", "--run-id", "a", "--source", "b"}, {"end", "--source", "b", "--run-id", "a"}, {"end", "--run-id", "a", "--run-id", "b"},
		{"end", "--bogus", "--run-id", "a"}, {"end", "--run-id", "a", "--source"}, {"end", "--run-id", "-x"}, {"end", "--source", "-"},
		{"end", "--json=1", "--run-id", "a"},
	} {
		checkMain(f, args)
	}
	ro := uint32(0o555)
	f.mkdir("ro")
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &ro})
	both(f, "start", "--source", "src.md", "--state-dir", "ro/st")
	rw := uint32(0o755)
	f.op(Op{Kind: "chmod", Path: f.dir("ro"), Mode: &rw})
	snapshot(f)
	return f.done()
}

// manageHookCase は 1078 の hook が呼ぶ関数: armed_entries (無い・壊れた登録・名前の合わない登録・ロックのファイル)・start_run (cwd・by・host・
// session・上限・最終稿・誤り)・armed_update (書く・書かない・読めない登録・締め切り)・verify_quick・run_known_texts・latest_final_path・text_sha256_of。
func manageHookCase() VisCase {
	f := newFixtureCase("manage-hook")
	f.write("src.md", vSource).write("final.md", vFinal).write("other.md", vOther)
	f.writeBytes("bad.md", []byte{0xff, '\n'})
	state := f.dir(".yomiyasu")
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	hook(f, "armed_entries", []any{state}, nil)
	hook(f, "start_run", []any{state, "src.md"}, map[string]any{"cwd": f.dir(), "by": "hook:codex:UserPromptSubmit", "host": "codex",
		"session": "sess-h", "run_id": "h1", "default_place": true})
	hook(f, "start_run", []any{state, "other.md"}, map[string]any{"cwd": f.dir(), "final_path": "final.md", "run_id": "h2",
		"limits": map[string]any{"max_stop_blocks": 1, "max_runs": 2, "deadline_min": 3}})
	for _, kwargs := range []map[string]any{
		{"limits": map[string]any{"max_stop_blocks": 2, "max_runs": 3}},
		{"limits": map[string]any{"max_stop_blocks": true, "max_runs": 3, "deadline_min": 30}},
		{"limits": map[string]any{"max_stop_blocks": 2, "max_runs": 3, "deadline_min": json.Number("3.0")}},
		{"final_path": "-"},
		{"run_id": "bad/id"},
		{"run_id": "H1"},
		{"run_id": "h1"},
	} {
		kwargs["cwd"] = f.dir()
		hook(f, "start_run", []any{state, "src.md"}, kwargs)
	}
	hook(f, "start_run", []any{state, "-"}, map[string]any{"cwd": f.dir()})
	hook(f, "start_run", []any{state, "missing.md"}, map[string]any{"cwd": f.dir()})
	hook(f, "start_run", []any{state, "bad.md"}, map[string]any{"cwd": f.dir()})
	hook(f, "armed_entries", []any{state}, nil)
	f.write(".yomiyasu/armed/h9.json", "{}")
	f.write(".yomiyasu/armed/h8.json", "[]")
	f.write(".yomiyasu/armed/x.lock", "")
	hook(f, "armed_entries", []any{state}, nil)
	writeArmed(f, "h7", "src.md", vSource, map[string]any{"limits": map[string]any{"max_stop_blocks": 2, "max_runs": "3", "deadline_min": 30}})
	writeArmed(f, "h6", "src.md", vSource, map[string]any{"status": "nope"})
	writeArmed(f, "h5", "src.md", vSource, map[string]any{"counters": map[string]any{"stop_blocks": true}})
	hook(f, "armed_entries", []any{state}, nil)
	hook(f, "armed_update", []any{state, "h2", ArmedChange{Set: [][]any{{[]any{"counters", "stop_blocks"}, 1}}, Write: true}, 2000.0}, nil)
	hook(f, "armed_update", []any{state, "h2", ArmedChange{Set: [][]any{{[]any{"notified"}, true}}, Write: false}, nil}, nil)
	hook(f, "armed_update", []any{state, "h9", ArmedChange{Write: true}, nil}, nil)
	hook(f, "armed_update", []any{state, "nope", ArmedChange{Write: true}, nil}, nil)
	hook(f, "armed_update", []any{state, "bad/id", ArmedChange{Write: true}, nil}, nil)
	hook(f, "armed_update", []any{state, "h2", ArmedChange{Write: true}, 999.0}, nil)
	hook(f, "armed_update", []any{state, "h2", ArmedChange{Write: true}, 1000.0}, nil)
	hook(f, "armed_update", []any{state, "h1", ArmedChange{Set: [][]any{{[]any{"status"}, "nope"}}, Write: true}, nil}, nil)
	hook(f, "armed_entries", []any{state}, nil)
	hook(f, "verify_quick", []any{state}, map[string]any{"final_path": "final.md"})
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "h3")
	f.write("final.md", vFinal2)
	run(f, "--source", "src.md", "--final", "final.md", "--run-id", "h3")
	for _, kwargs := range []map[string]any{
		{"final_path": "final.md"},
		{"final_path": "final.md", "run_id": "h3", "since": "2026-01-01T00:00:00Z"},
		{"final_path": "final.md", "since": "2026-01-01T00:00:01Z"},
		{"final_path": "final.md", "since": "nope"},
		{"final_bytes": Bytes(vFinal2)},
		{"final_bytes": Bytes(vFinal), "run_id": "h3"},
		{"final_path": f.dir("final.md"), "final_bytes": Bytes(vFinal)},
		{"final_bytes": Bytes([]byte{0xff})},
		{"final_bytes": Bytes("")},
		{"final_path": "missing.md"},
		{},
		{"final_path": "final.md", "run_id": "bad/id"},
		{"final_path": "final.md", "run_id": "H3"},
	} {
		hook(f, "verify_quick", []any{state}, kwargs)
	}
	hook(f, "verify_quick", []any{f.dir("nowhere")}, map[string]any{"final_path": "final.md"})
	for _, runID := range []string{"h1", "h3", "nope", "bad/id"} {
		hook(f, "run_known_texts", []any{state, runID}, nil)
		hook(f, "latest_final_path", []any{state, runID}, nil)
	}
	f.write(".yomiyasu/runs/h3/attempt-9.json", `{"inputs": {"final": {"path": 5, "text_sha256": "t"}, "source": "x"}}`)
	hook(f, "run_known_texts", []any{state, "h3"}, nil)
	hook(f, "latest_final_path", []any{state, "h3"}, nil)
	f.write(".yomiyasu/runs/h3/attempt-10.json", `{"inputs": {"final": {"path": "p"}}}`)
	hook(f, "latest_final_path", []any{state, "h3"}, nil)
	f.write(".yomiyasu/runs/h3/attempt-11.json", `{"inputs": []}`)
	hook(f, "latest_final_path", []any{state, "h3"}, nil)
	for _, data := range []string{vSource, "\ufeffa\r\nb", "", "\xff"} {
		hook(f, "text_sha256_of", []any{Bytes(data)}, nil)
	}
	snapshot(f)
	return f.done()
}

// manageSurrogateCase は OS から UTF-8 として読めないバイトで受けた引数と環境変数 (本家は os.fsdecode で単独のサロゲートにする) の出口:
// 登録の JSON (--session・YOMIYASU_SESSION)・誤りの文 (標準エラーは backslashreplace)・標準出力 (strict)。
func manageSurrogateCase() VisCase {
	f := newFixtureCase("manage-surrogate")
	f.write("src.md", vSource)
	chdir(f)
	setenv(f, "YOMIYASU_SESSION", surrogates(0xDCFF)+"a")
	both(f, "start", "--source", "src.md", "--run-id", "u1")
	unsetenv(f, "YOMIYASU_SESSION")
	start(f, "--source", "src.md", "--run-id", "u2", "--session", "x"+surrogates(0xDCED, 0xDCB3, 0xDCBF))
	for _, v := range ArgvSurrogates {
		for _, args := range [][]string{
			{"start", "--source", v.Value}, {"start", "--source", "src.md", "--run-id", v.Value}, {"start", "--source", "src.md", "--host", v.Value},
			{"start", "--source", "src.md", "--max-runs", v.Value}, {"start", "--source", "src.md", "--state-dir", v.Value},
			{"start", "--source", "src.md", "--state-dir", v.Value, "--json"}, {"start", "--source", "src.md", "--final", v.Value},
			{"export", "--final", v.Value}, {"export", "--final", "src.md", "--source", v.Value}, {"end", "--source", v.Value},
			{"end", "--run-id", v.Value},
		} {
			checkMain(f, args)
		}
	}
	snapshot(f)
	return f.done()
}
