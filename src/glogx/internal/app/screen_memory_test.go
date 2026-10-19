package app_test

import (
	"os"
	"testing"
	"time"

	"glogx/internal/app"
	"glogx/internal/testenv"
)

type memo struct {
	N       int       `json:"n"`
	SavedAt time.Time `json:"saved_at"`
}

// ScreenMemory.Load の拒否の規則 (全画面で共通): 無い・壊れた JSON・保存時刻なし・期限切れ (ちょうど TTL を含む)・未来の時刻は
// 「記憶なし」。TTL の手前なら返す。画面ごとのテストは照合と SavedAt の配線を見る (規則の正本はここ)。
func TestScreenMemoryLoadRejectsBadState(t *testing.T) {
	now := testenv.IsolateScreenMemory(t)
	m := app.ScreenMemory[memo]{File: "test-screen.json", SavedAt: func(v memo) time.Time { return v.SavedAt }}
	if _, ok := m.Load(now); ok {
		t.Fatal("ファイルが無いのに記憶を返した")
	}
	for _, c := range []struct {
		name string
		v    memo
		ok   bool
	}{
		{"TTL の手前", memo{N: 1, SavedAt: now.Add(-app.ScreenMemoryTTL + time.Second)}, true},
		{"ちょうど TTL", memo{N: 1, SavedAt: now.Add(-app.ScreenMemoryTTL)}, false},
		{"TTL 超過", memo{N: 1, SavedAt: now.Add(-app.ScreenMemoryTTL - time.Second)}, false},
		{"未来の時刻", memo{N: 1, SavedAt: now.Add(time.Second)}, false},
		{"保存時刻なし", memo{N: 1}, false},
	} {
		if err := m.Save(c.v); err != nil {
			t.Fatal(err)
		}
		if got, ok := m.Load(now); ok != c.ok || (ok && got.N != 1) {
			t.Errorf("%s: Load = %+v, %v (want ok=%v)", c.name, got, ok, c.ok)
		}
	}
	path, err := m.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Load(now); ok {
		t.Error("壊れた JSON から記憶を返した")
	}
	m.Remove()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Remove の後もファイルが残っている: %v", err)
	}
}
