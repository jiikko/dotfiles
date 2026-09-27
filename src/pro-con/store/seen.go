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
	// Keep は dispatcher が次に一覧を取るまでの最長の間 (暇な間は一覧を間引く。issue 559)。0 は間引いていない (SeenFresh)
	Keep time.Duration `json:"keep,omitempty"`
}

// SeenFresh は間引いていない一覧を今の様子として使う古さの上限。dispatcher の tick (3 秒) と、一覧の取得の上限
// (agents.Timeout = 10 秒) を足した余裕。
const SeenFresh = 15 * time.Second

// seenKeepMax は Keep の上限 (壊れた値で、画面がいつまでも自分で読まなくなるのを防ぐ)。
const seenKeepMax = 2 * time.Minute

// Fresh は now の時点で、この一覧を今の様子として使ってよいか。古ければ画面が自分で読む。
// 未来の時刻 (時計が戻った) と、dispatcher が一覧を取れなかった印 (Err) は古いとみなす。
// 間引いた一覧 (Keep) は、次の一覧の取得までの間に、間引かないときと同じ余裕を足した間だけ使う。
func (s Seen) Fresh(now time.Time) bool {
	age := now.Sub(s.At)
	return !s.At.IsZero() && s.Err == "" && age >= 0 && age <= SeenFresh+min(max(s.Keep, 0), seenKeepMax)
}

func SaveSeen(dir string, s Seen) error { return saveJSON(dir, SeenFile, s) }

// LoadSeen は読む。無ければ zero (dispatcher がまだ書いていない)。
func LoadSeen(dir string) (Seen, error) { return loadJSON[Seen](dir, SeenFile) }
