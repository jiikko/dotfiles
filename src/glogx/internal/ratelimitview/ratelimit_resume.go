package ratelimitview

import (
	"glogx/internal/app"
	"time"
)

// ratelimit ダッシュボード (R) を開いたまま glogx を閉じたら、次の起動でそれを開き直す。
//
// なぜ: glogx は tmux の `bind -n C-g` popup で**トグルとして**開閉する。トグルなら「閉じる」は
// 「隠す」に近いので、開き直したときにダッシュボードが消えていると往復のたびに R を押し直す
// ことになる (ユーザー要望 2026-09-05)。doctor の記憶 (doctor_resume.go) と同じ理由・同じ作法。
//
// 🚨 **数字は持ち越さない。** 覚えるのは「開いていた」ことだけで、Snapshot は usageOverlay の
// ディスクキャッシュ (TTL つき) が担う。混ぜると画面の復元が取得経路の信頼境界に巻き込まれる。

// ratelimitScreen は閉じたときのダッシュボードの画面。ダッシュボードはタブもカーソルも
// 持たないので、覚えるのは保存時刻だけ (TTL 判定に要る)。
type RatelimitScreen struct {
	SavedAt time.Time `json:"saved_at"`
}

var ratelimitMemory = app.ScreenMemory[RatelimitScreen]{File: "ratelimit-screen.json", SavedAt: func(s RatelimitScreen) time.Time { return s.SavedAt }}

func SaveRatelimitScreen(s RatelimitScreen) error { return ratelimitMemory.Save(s) }

func RemoveRatelimitScreen() { ratelimitMemory.Remove() }

// loadRatelimitScreen は「復元するか」を返す (読めない・壊れている・期限切れ・未来の時刻は「復元しない」。app.ScreenMemory.load)。
func LoadRatelimitScreen(now time.Time) bool {
	_, ok := ratelimitMemory.Load(now)
	return ok
}

// RememberScreen は終了時の記憶。ダッシュボードを出していれば保存し、🚨 出していなければ消す (issues / doctor と同じ) —
// 残すと、一覧を見て閉じた次の起動で 2 回前のダッシュボードが蘇る。保存の失敗は終了を妨げない。
func (d *RatelimitDash) RememberScreen(now time.Time) {
	if d.Visible() {
		_ = SaveRatelimitScreen(RatelimitScreen{SavedAt: now})
		return
	}
	RemoveRatelimitScreen()
}
