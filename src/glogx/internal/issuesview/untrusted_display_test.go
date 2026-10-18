package issuesview

import (
	"glogx/internal/testenv"
	"glogx/issues"
	"testing"
)

// n の確認モーダルに載る issue ファイル名は issues/ 直下の実ファイル名 (Rel は同一性のため
// 生のまま保持される契約)。表示に出すここで無害化する回帰テスト。
func TestMarkNextBoxSanitizesFilename(t *testing.T) {
	v := &IssuesView{}
	v.markNext = issuesMarkConfirm{active: true, targets: []*issues.Issue{
		{Rel: "036-bug-" + testenv.OSC8 + "0;PWNED" + testenv.ST8 + testenv.CSI8 + "2J.md"},
	}}
	testenv.AssertSanitizedAndVisible(t, "markNext 確認モーダル", v.markNextBox(80, false), "036-bug-", ".md")
}
