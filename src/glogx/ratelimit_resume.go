package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// ratelimitStateTTL は記憶の有効期限。issues / doctor と同じ値 — 根拠も同じで、C-g のトグル
// 感覚 (閉じてすぐ開き直す) に効かせ、時間が空いた起動では素の画面から始める。
const ratelimitStateTTL = issuesStateTTL

// ratelimitScreen は閉じたときのダッシュボードの画面。ダッシュボードはタブもカーソルも
// 持たないので、覚えるのは保存時刻だけ (TTL 判定に要る)。
type ratelimitScreen struct {
	SavedAt time.Time `json:"saved_at"`
}

func ratelimitStatePath() (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ratelimit-screen.json"), nil
}

func saveRatelimitScreen(s ratelimitScreen) error {
	path, err := ratelimitStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// 🚨 素の os.WriteFile を使わないこと (issue 219 / 286)。writeAtomic は temp + rename で、
	// write / Close / rename の 3 分岐すべてで temp を掃除する。素の WriteFile は O_TRUNC なので
	// 書き込み中に落ちると**途中まで書けた JSON** が残り、次回起動の復元が黙って失敗する。
	// 規律は cache_write_discipline_test.go がソース走査で強制する。
	return writeAtomic(path, data)
}

func removeRatelimitScreen() {
	if path, err := ratelimitStatePath(); err == nil {
		_ = os.Remove(path)
	}
}

// loadRatelimitScreen は「復元するか」を返す。読めない / 壊れている / 期限切れ / 未来の時刻は
// すべて「復元しない」に倒す (勝手に全画面を出さない方が安全側。doctor 側と同じ規律)。
func loadRatelimitScreen(now time.Time) bool {
	path, err := ratelimitStatePath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var s ratelimitScreen
	if err := json.Unmarshal(data, &s); err != nil {
		return false
	}
	return !s.SavedAt.IsZero() && now.Sub(s.SavedAt) < ratelimitStateTTL && !s.SavedAt.After(now)
}
