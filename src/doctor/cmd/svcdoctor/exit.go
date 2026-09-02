package main

import (
	"doctor/exitcode"
	"doctor/svc"
)

// 終了コードの語彙は 2 本の CLI で共通 (issue 177)。**「検査できなかった」を緑にしない**のが設計。
//
//	0  診断できた + 候補なし
//	1  診断できた + 候補あり
//	2  引数が不正、または診断できなかったものがある (fail-closed)
//	3  実行環境・出力の失敗 (home の解決 / JSON のエンコード)
//
// 2 が 1 より優先する: 「候補が 1 件あった」より「一部を検査できなかった」の方が、
// 呼び出し側が知る必要のある事実 (見えていない候補があるかもしれない)。

// svcExitCode は Report から終了コードを決める。text / -json のどちらの経路でも同じものを通す
// (以前は -json が return で判定を飛ばしていて、JSON を読む側は「診断できず」を exit code で
// 見られなかった: issue 177 (b))。
func svcExitCode(rep svc.Report) int {
	// BrewErr は「brew 台帳が取れなかった」= 診断できず。以前はここを見ておらず、画面に
	// 「🚨 診断できず (brew)」と「壊れた登録は見つかりませんでした」が同時に出て exit 0 だった
	// (issue 177 (a))。
	if rep.Interrupted || rep.StatusErr != "" || rep.BrewErr != "" || len(rep.Undiagnosed) > 0 || len(rep.DirErrs) > 0 {
		return exitcode.Undiagnosed
	}
	if len(rep.Findings) > 0 {
		return exitcode.Findings
	}
	return exitcode.NoFindings
}
