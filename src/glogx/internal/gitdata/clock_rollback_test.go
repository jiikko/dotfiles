package gitdata

import (
	"testing"
	"time"
)

// 永続キャッシュの鮮度判定は「未来の時刻」を fresh にしない。
//
// 🚨 これらの時刻は **disk 上の JSON から読む**ので、時計を戻した / NTP が大きく補正した /
// 別マシンのキャッシュを持ってきた、のいずれかで未来になりうる。`now.Sub(x) < TTL` は
// 未来だと負になって**永久に真**を返し、古い値を使い続ける。しかも silent で、
// 「なぜか更新されない」としか見えない (doctor トーストが実際にこれで沈黙した)。
//
// 同じ形が **8 箇所中 4 箇所だけガード済み**という非対称を作らない。
func TestPersistedCacheRejectsFutureTimestamps(t *testing.T) {
	now := time.Now()

	t.Run("cacheEntry.fresh", func(t *testing.T) {
		e := cacheEntry{FetchedAt: now.Add(time.Hour), State: StateSuccess}
		if e.fresh(now) {
			t.Error("FetchedAt が 1 時間後なのに fresh 扱い (時計を戻すと古い CI 状態を永久に使う)")
		}
		ok := cacheEntry{FetchedAt: now, State: StateSuccess}
		if !ok.fresh(now) {
			t.Error("現在時刻の entry が fresh でない (ガードが広すぎる)")
		}
	})
}
