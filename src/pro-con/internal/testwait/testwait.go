// Package testwait は pro-con のテストが「何かが起きる」のを待つ helper (issue 777)。
// 秒数で待たず、条件を上限つきで見ながら待つ (_claude/rules/avoid-wall-clock-assertions.md)。テストからだけ使う。
package testwait

import (
	"testing"
	"time"
)

// Limit は Until の上限。負荷の高い CI でも成り立つ条件が間に合う長さにしてある (縮めると遅いマシンで flaky になる)。
const Limit = 10 * time.Second

// Until は cond が成り立つまで待つ。Limit を過ぎても成り立たなければ what を出してテストを落とす。
func Until(t testing.TB, what string, cond func() bool) {
	t.Helper()
	if !Poll(Limit, cond) {
		t.Fatalf("%s (%s 待っても成立しない)", what, Limit)
	}
}

// Poll は cond が成り立つまで最長 limit 待ち、成り立ったかを返す (落とすかは呼ぶ側が決める)。
func Poll(limit time.Duration, cond func() bool) bool {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) { // sleep-ok: tick: 条件を見ながら刻む待ちの刻み
		if cond() {
			return true
		}
	}
	return cond()
}
