package card

import (
	"fmt"
	"slices"
	"strings"
)

// 見積もりのポイント (issue 490)。PM が積むとき (`card plan --points`) に付け、ボードのカードの右上に出す。

// PointScale は見積もりのポイントに使える値。0 は「見積もり無し」。
var PointScale = []int{1, 2, 3, 5, 8}

// CheckPoints は見積もりのポイントが使える値か (0 = 付けない は通す)。
func CheckPoints(p int) error {
	if p == 0 || slices.Contains(PointScale, p) {
		return nil
	}
	return fmt.Errorf("ポイントは %v のどれか (%d は使えない)", PointScale, p)
}

// PointsMeaning はポイントの意味の正本。画面の ? の表・`pro-con card` の使い方・PM の指示書 (pm-guide.md の役目 3) がこれを出す
// (文面を写さない。直すのはここだけ)。
var PointsMeaning = []string{
	"相対の大きさであって、時間の見積もりではない。付けるのは PM が積むとき (plan --points)。付けないカードは出さない",
	"1 = 数行の変更とテスト 1 本",
	"2 = 1 か所の変更と、そのテストを数本",
	"3 = 1 つの判断を変えて、周辺のテストも直す",
	"5 = 判断を複数変える・画面の見本を人に選んでもらう",
	"8 = 設計から要る。積む前に分けられないかを先に考える大きさ",
}

// PointsMeaningText は PointsMeaning を、行ごとに prefix を付けて改行で繋ぐ。
func PointsMeaningText(prefix string) string {
	return prefix + strings.Join(PointsMeaning, "\n"+prefix)
}

// PointsLabel はカードの右上と詳細に出す見積もり (「3pt」)。見積もり無しは空。
func PointsLabel(c Card) string {
	if c.Points == 0 {
		return ""
	}
	return fmt.Sprintf("%dpt", c.Points)
}
