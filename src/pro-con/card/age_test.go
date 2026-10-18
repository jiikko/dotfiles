package card

import (
	"testing"
	"time"
)

// 経過時間の書式。境目ごとに固定する。
func TestAge(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5秒"},
		{59 * time.Second, "59秒"},
		{time.Minute, "1分"},
		{5*time.Minute + 30*time.Second, "5分"},
		{59 * time.Minute, "59分"},
		{time.Hour, "1時間0分"},
		{time.Hour + 5*time.Minute, "1時間5分"},
		{23*time.Hour + 59*time.Minute, "23時間59分"},
		{24 * time.Hour, "1日0時間"},
		{25 * time.Hour, "1日1時間"},
		{74 * time.Hour, "3日2時間"},
	} {
		if got := Age(tc.d); got != tc.want {
			t.Errorf("Age(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
