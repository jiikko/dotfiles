package card

import (
	"fmt"
	"slices"
	"time"
)

// カードの重さ (issue 490): 見積もり = ポイント (PM が積むときに付ける) と、実際 = 作業中だった時間の合計。

// PointScale は見積もりのポイントに使える値 (相対の大きさ)。0 は「見積もり無し」。
var PointScale = []int{1, 2, 3, 5, 8}

// CheckPoints は見積もりのポイントが使える値か (0 = 付けない は通す)。
func CheckPoints(p int) error {
	if p == 0 || slices.Contains(PointScale, p) {
		return nil
	}
	return fmt.Errorf("ポイントは %v のどれか (%d は使えない)", PointScale, p)
}

// Working は作業中だった時間に数える状態か。issue 455 の「動いている」と同じ定義にする: 作業中の列に居て、
// テストの係の結果を待っていない (待っている間はボードで暗く出す = spinner.go の waiting と表裏)。
// 質問待ち・レビュー待ち・分解済み (再開待ちを含む) は数えない。落ちて自動の再開を待っている間 (DeadSince) は列が作業中のままなので数える。
func Working(c Card) bool { return c.State == Running && !c.AwaitsRun() }

// SettleWork は Working の切り替わりを作業中の時間へ反映する。記録の書き手 (store の Apply / Update) が書く前に全カードへ呼ぶ
// (列を動かす口ごとに足さない。どの口から Working が変わっても取りこぼさない)。
func (c *Card) SettleWork(now time.Time) {
	switch working := Working(*c); {
	case working && c.WorkFrom.IsZero():
		c.WorkFrom = now
	case !working && !c.WorkFrom.IsZero():
		c.Worked += max(now.Sub(c.WorkFrom), 0)
		c.WorkFrom = time.Time{}
	}
}

// WorkedAt は now までの作業中の時間の合計 (今作業中なら今の分も足す)。
func (c Card) WorkedAt(now time.Time) time.Duration {
	if c.WorkFrom.IsZero() {
		return c.Worked
	}
	return c.Worked + max(now.Sub(c.WorkFrom), 0)
}

// EffortLine は詳細 (画面の enter と `pro-con card show`) に出す重さの 1 行。見積もりも作業中の時間も無ければ空 (行を出さない)。
func EffortLine(c Card, now time.Time, dur func(time.Duration) string) string {
	worked := c.WorkedAt(now)
	if c.Points == 0 && worked == 0 {
		return ""
	}
	est := "なし"
	if c.Points != 0 {
		est = fmt.Sprintf("%dpt", c.Points)
	}
	line := "重さ: 見積もり " + est + " / 作業中だった時間 " + dur(worked)
	if !c.WorkFrom.IsZero() {
		line += " (今も作業中)"
	}
	return line
}
