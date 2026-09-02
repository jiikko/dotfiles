// Package exitcode は 2 本の CLI (diskdoctor / svcdoctor) で共通の終了コード語彙。
//
// **「検査できなかった」を緑にしない**のが設計。語彙をここ 1 箇所に置くのは、以前 2 本の
// main.go が独立に数字を決めていて非対称になっていたため (issue 177)。定数を各 CLI に
// コピーすると、片方だけ変えても機械的には検出できない (別 package なので参照が切れている。
// 敵対レビュー 2026-09-03 で、svcdoctor 側の値を 9 に変えてもテストが全部 green のまま
// 通ることを実測した)。
package exitcode

const (
	// NoFindings は診断できた + 候補なし。
	NoFindings = 0
	// Findings は診断できた + 候補あり。
	Findings = 1
	// Undiagnosed は引数が不正、または診断できなかったものがある。
	// **Findings より優先する**: 「候補が 1 件あった」より「一部を検査できなかった」の方が、
	// 呼び出し側が知る必要のある事実 (見えていない候補があるかもしれない)。
	Undiagnosed = 2
	// EnvFailure は実行環境・出力の失敗 (home の解決 / JSON のエンコード)。
	EnvFailure = 3
)
