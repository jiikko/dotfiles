package main

import (
	"testing"
	"time"

	"ratelimit/usage"
)

// usageSnapFixture は本物の /usage 出力を usage.Parse に通して作る。手組みの Snapshot だと
// 「実際に Fetch が返す形」と食い違っても気づけない: usage.Snapshot / Window は json タグを
// 持たずフィールド名で符号化され、ResetAt は time.Time。ここが往復で壊れると症状は
// 「永久に cache miss = 毎起動 claude を起こし続ける」で、どこも失敗しないまま劣化する。
func usageSnapFixture(t *testing.T) *usage.Snapshot {
	t.Helper()
	const realResult = `You are currently using your subscription to power your Claude Code usage

Current session: 2% used · resets Jul 22 at 3:09am (Asia/Tokyo)
Current week (all models): 29% used · resets Jul 24 at 8am (Asia/Tokyo)
Current week (Fable): 48% used · resets Jul 24 at 8am (Asia/Tokyo)

What's contributing to your limits usage?
Last 24h · 875 requests · 7 sessions`
	snap, err := usage.Parse(realResult, time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("fixture の Parse に失敗: %v", err)
	}
	snap.Version = "2.1.216"
	return snap
}
