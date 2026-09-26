package store

import (
	"time"

	"pro-con/agents"
)

// SeenFile は dispatcher が tick で取った session の一覧と、pro-con が起動した session の出力の末尾 (issue 502 / 503)。
// 画面はこれが新しければ使い、自分では `claude agents --json` (1 回 CPU 約 0.3 秒) も transcript (1 回 5.7 ms / 2.9 MB) も読まない。
const SeenFile = "seen.json"

// Seen は SeenFile の中身。
type Seen struct {
	At       time.Time           `json:"at"` // 一覧を取った時刻
	Sessions []agents.Session    `json:"sessions"`
	Logs     map[string][]string `json:"logs,omitempty"` // 短い session id → PG の出力の末尾 (古い順)
	// Err は dispatcher が一覧を取れなかった理由 (空でなければ Sessions は空。画面は使わずに自分で読み、取れなければその理由を出す)
	Err string `json:"err,omitempty"`
}

func SaveSeen(dir string, s Seen) error { return saveJSON(dir, SeenFile, s) }

// LoadSeen は読む。無ければ zero (dispatcher がまだ書いていない)。
func LoadSeen(dir string) (Seen, error) { return loadJSON[Seen](dir, SeenFile) }
