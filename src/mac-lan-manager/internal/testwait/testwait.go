// Package testwait はテストの「条件が成り立つまで刻みを回して待つ」を 1 つに持つ。本番からは使わない。
// lanmgr と setup のテストに 8 通りの写しがあり、上限で Fatal せず黙って先へ進むものが混ざっていた (後の assert が別の文で落ち、原因が読めない)。
package testwait

import (
	"testing"
	"time"
)

// Until は cond が成り立つまで step (1 周ごとの刻み。Advance・結果の取り込み。nil なら何もしない) を回し、limit を超えたら what を添えて
// Fatal する。周の間は 1ms 眠る (Gosched で回すと、-race と並列のテストの下で上限まで CPU を焼く)。
func Until(t testing.TB, what string, limit time.Duration, step func(), cond func() bool) {
	t.Helper()
	// 上限は実時間で測る (テストが止まったときの安全網。Model の「今」ではないので注入した時計を使わない)
	for deadline := time.Now().Add(limit); !cond(); { //nolint:forbidigo // ハングの上限 (実時間)
		if time.Now().After(deadline) { //nolint:forbidigo // ハングの上限 (実時間)
			t.Fatalf("%s が %v 待っても成り立たない", what, limit)
		}
		if step != nil {
			step()
		}
		time.Sleep(time.Millisecond) // sleep-ok: tick: 条件のポーリングの刻み (待ちの実体は cond)
	}
}
