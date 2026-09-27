package store

import (
	"testing"
	"time"
)

// 画面が dispatcher の一覧を今の様子として使う境目: 間引いていなければ SeenFresh、間引いていれば次の取得までの間 (Keep) を足す (issue 559)。
func TestSeenFresh(t *testing.T) {
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		keep  time.Duration
		age   time.Duration
		err   string
		fresh bool
	}{
		{"間引いていない・新しい", 0, SeenFresh - time.Second, "", true},
		{"間引いていない・古い", 0, SeenFresh + time.Second, "", false},
		{"間引いた・次の取得の前", 30 * time.Second, SeenFresh + 30*time.Second - time.Second, "", true},
		{"間引いた・次の取得を過ぎた", 30 * time.Second, SeenFresh + 30*time.Second + time.Second, "", false},
		{"壊れた Keep は上限で切る", time.Hour, SeenFresh + seenKeepMax + time.Second, "", false},
		{"未来 (時計が戻った)", 30 * time.Second, -time.Minute, "", false},
		{"一覧を取れなかった", 30 * time.Second, time.Second, "claude agents が終わらない", false},
	} {
		s := Seen{At: at, Keep: tc.keep, Err: tc.err}
		if got := s.Fresh(at.Add(tc.age)); got != tc.fresh {
			t.Errorf("%s: Fresh = %v (%v のはず)", tc.name, got, tc.fresh)
		}
	}
	if (Seen{}).Fresh(at) {
		t.Error("まだ書いていない一覧 (zero) を新しいと読んだ")
	}
}
