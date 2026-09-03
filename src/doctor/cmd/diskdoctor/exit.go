package main

import (
	"doctor/disk"
	"doctor/exitcode"
)

// 終了コードの語彙は 2 本の CLI で共通 (issue 177)。意味は cmd/svcdoctor/exit.go と同じ。
//
//	0  診断できた + 候補なし
//	1  診断できた + 候補あり
//	2  引数が不正、または走査できなかったエントリがある (fail-closed)
//	3  実行環境・出力の失敗 (JSON のエンコード)

// diskExitCode は Report から終了コードを決める。以前は「一覧を出せたか」だけを見ており、
// 候補があっても 0 を返していた (svcdoctor は 1 を返すので非対称だった: issue 177 (c))。
func diskExitCode(rep disk.Report) int {
	if rep.Partial {
		return exitcode.Undiagnosed
	}
	found := false
	for _, r := range rep.Results {
		switch {
		case r.Status == disk.StatusFailed:
			return exitcode.Undiagnosed
		case r.Status == disk.StatusOK && len(r.Failures) > 0:
			// エントリ全体は走査できたが一部の Item が読めなかった。合計に入っていないので
			// 「見えていない候補があるかもしれない」= 診断できず側に倒す
			return exitcode.Undiagnosed
		case r.Status == disk.StatusOK && r.Entry.Unverified != "" && len(r.Items) == 0:
			// 🚨 **検出条件そのものが未実測**のエントリが 0 件でも「きれい」と言わない
			// (issue 236 の P3-4)。人間向け出力と UI は「0 件ですが『候補なし』では
			// ありません」と言うのに、rc だけが NoFindings に丸めていた。
			// report.go / doctor_view.go が守っている false green の規律を rc でも守る
			return exitcode.Undiagnosed
		case r.Status == disk.StatusOK && len(r.Items) > 0:
			found = true
		}
	}
	if found {
		return exitcode.Findings
	}
	return exitcode.NoFindings
}
