package main

import (
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
type ratelimitScreen struct {
	SavedAt time.Time `json:"saved_at"`
}

var ratelimitMemory = screenMemory[ratelimitScreen]{file: "ratelimit-screen.json", savedAt: func(s ratelimitScreen) time.Time { return s.SavedAt }}

func saveRatelimitScreen(s ratelimitScreen) error { return ratelimitMemory.save(s) }

func removeRatelimitScreen() { ratelimitMemory.remove() }

// loadRatelimitScreen は「復元するか」を返す (読めない・壊れている・期限切れ・未来の時刻は「復元しない」。screenMemory.load)。
func loadRatelimitScreen(now time.Time) bool {
	_, ok := ratelimitMemory.load(now)
	return ok
}
