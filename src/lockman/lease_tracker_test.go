package main

import (
	"testing"
	"time"
)

// `leaseTracker` は **期限の"値"そのもの**を固定する (issue 385 / 敵対レビュー 2 周目 P2-A)。
//
// 🚨 切り出す前は、期限を決める箇所が `runWith` の中に 3 つ散っていて、単体で固定できていたのは
// 境界の述語だけだった。**同じ幅 (ttl の 44%) の猶予を「起点」や「呼び出し側」へ移した変異は
// 統合テストで全緑**で通る = fail-open を入れても誰も気づかない状態だった。
// ここでは ①起点から期限を作る ②境界 ③判定不能の扱い の 3 つを、**具体的な数値**で固定する
// (production と同じ式を書いて期待値を作らない)。
func TestLeaseTracker(t *testing.T) {
	// 起点は「1 時 0 分 0 秒」、TTL は 10 秒 → 期限は「1 時 0 分 10 秒」
	start := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	const ttl = 10 * time.Second
	at := func(sec int, nsec int) time.Time {
		return time.Date(2026, 9, 20, 1, 0, sec, nsec, time.UTC)
	}

	t.Run("期限は起点 + TTL", func(t *testing.T) {
		lt := newLeaseTracker(start, ttl)
		for _, tc := range []struct {
			name string
			now  time.Time
			want bool
		}{
			{"9.999999999 秒 (期限の 1ns 前)", at(9, 999999999), false},
			{"ちょうど 10 秒", at(10, 0), true},
			{"10 秒 + 1ns", at(10, 1), true},
			// 🚨 猶予を足す変異が通ってしまう幅。どこ (起点 / 述語) へ入れてもここで落ちる
			{"14 秒 (猶予 4 秒を足す変異が通る幅)", at(14, 0), true},
		} {
			if got := lt.expired(tc.now); got != tc.want {
				t.Errorf("%s: expired=%v (期待 %v)", tc.name, got, tc.want)
			}
		}
	})

	t.Run("更新の成功で期限が進む。起点は渡した時刻", func(t *testing.T) {
		lt := newLeaseTracker(start, ttl)
		// 🚨 **渡した時刻**を起点にすること。内部で `time.Now()` を読む実装だと、
		// 「いつを起点にしたか」がテストから観測できず、M4 (更新が返った時刻を起点にする) の
		// ような遅い見積もりが無検査になる。ここでは 4 秒前の時刻を渡して、期限が
		// 「4 秒前 + 10 秒 = 6 秒後」になることを見る
		lt.renewed(at(4, 0))
		if lt.expired(at(13, 999999999)) {
			t.Errorf("期限が進んでいない (4 秒の時点で更新したので 14 秒までは生きている)")
		}
		if !lt.expired(at(14, 0)) {
			t.Errorf("期限が進みすぎている (4 秒 + TTL 10 秒 = 14 秒で期限切れのはず)")
		}
	})

	t.Run("判定不能: 期限前は保留、期限後は即昇格", func(t *testing.T) {
		lt := newLeaseTracker(start, ttl)
		if lt.indeterminate(at(5, 0)) {
			t.Errorf("期限前 (5 秒) なのに即昇格を要求した")
		}
		if !lt.dueForEscalation(at(10, 0)) {
			t.Errorf("保留したまま期限 (10 秒) を過ぎたのに昇格しない")
		}
		lt2 := newLeaseTracker(start, ttl)
		if !lt2.indeterminate(at(10, 0)) {
			t.Errorf("期限 (10 秒) を過ぎてからの判定不能は即昇格のはず")
		}
	})

	t.Run("期限前は昇格しない / 更新の成功で保留が解ける", func(t *testing.T) {
		lt := newLeaseTracker(start, ttl)
		if lt.dueForEscalation(at(11, 0)) {
			t.Errorf("判定不能を報告していないのに昇格した (期限を過ぎただけでは昇格しない)")
		}
		lt.indeterminate(at(5, 0)) // 保留
		if lt.dueForEscalation(at(9, 0)) {
			t.Errorf("保留中でも期限前 (9 秒) は昇格しない")
		}
		lt.renewed(at(8, 0)) // 復旧
		if lt.dueForEscalation(at(11, 0)) {
			t.Errorf("更新が成功したのに保留が解けていない (8 秒 + 10 秒 = 18 秒まで生きている)")
		}
		if !lt.expired(at(18, 0)) {
			t.Errorf("期限が 8 秒 + TTL 10 秒 = 18 秒になっていない")
		}
	})
}
