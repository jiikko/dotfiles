package card

import (
	"fmt"
	"time"
)

// Age は経過時間を人が読む長さで出す (CLI と画面で同じ書式。issue 772 で 2026-10-15 にユーザーが選んだ案C):
// 1 分未満は秒 / 1 時間未満は分 / 1 日未満は時間 + 分 / それ以上は日 + 時間。空白は入れない (例: 5秒 / 5分 / 1時間5分 / 3日2時間)。
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d秒", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%d分", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d時間%d分", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
	return fmt.Sprintf("%d日%d時間", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
}

// RoleProgress は役の進み具合 (RoleStep) に、役が今扱っている最後の道具とその経過を足した 1 行 (例: PG 実装 ▸ Bash 5分前)。
// CLI (pro-con card) と画面の詳細が同じ行を出す。
func (c Card) RoleProgress(r Roles, ss []RoleState, now time.Time) string {
	p := c.RoleStep(r, ss)
	if last, at, ok := c.LastCall(r, ss); ok {
		p += " ▸ " + last + " " + Age(now.Sub(at)) + "前"
	}
	return p
}

// StatusLine はカードの状態の 1 行 (状態と経過・担当・repo・session)。CLI と画面の詳細が同じ行を出す。空の欄の見せ方は呼び出し側が決めて渡す。
func (c Card) StatusLine(now time.Time, assignee, repo, session string) string {
	return fmt.Sprintf("状態: %s  担当: %s  repo: %s  session: %s", c.State.SinceText(Age(now.Sub(c.Since))), assignee, repo, session)
}
