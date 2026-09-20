package main

import "time"

// leaseTracker は「自分の lease が生きていると言い切れる限界」を持つ小さな状態機械 (issue 385)。
//
// 🚨 **切り出した目的は分割ではなく、期限の"値"を単体で検査できるようにすること**
// (敵対レビュー 385 の 2 周目 P2-A)。以前は期限を決める箇所が `runWith` の中に 3 つ散っており
// (①起点に ttl を足す 2 箇所 ②呼び出し側の述語 ③境界の述語)、単体で固定できていたのは③だけ
// だった。**同じ幅 (ttl の 44%) の猶予を①②へ移した変異は統合テストで全緑**で通る = fail-open を
// 入れても誰も気づかない状態だった。1 つの型に寄せると、どこへ猶予を入れても
// `lease_tracker_test.go` の単体テストが落ちる。
//
// 契約:
//   - 期限は **打刻より前の時刻**を起点にする (保守側 = 最も早い期限)。打刻はサーバが行うので
//     こちらからは「呼ぶ前」と「返った後」しか見えず、遅く見積もると**他者が正当に引き継げる
//     時刻を過ぎてから昇格する**ことになる
//   - 時刻は**引数で受け取る** (内部で `time.Now()` を読まない)。読むと「いつを起点にしたか」が
//     テストから観測できなくなり、上の変異がまた無検査へ戻る
type leaseTracker struct {
	ttl      time.Duration
	deadline time.Time
	// pending は「判定不能を報告済みで、まだ昇格していない」。
	pending bool
}

// newLeaseTracker は取得直後の状態を作る。start は **Acquire を呼ぶ前**の時刻。
func newLeaseTracker(start time.Time, ttl time.Duration) *leaseTracker {
	return &leaseTracker{ttl: ttl, deadline: start.Add(ttl)}
}

// renewed は更新が成功したことを記録する。start は **Renew を呼ぶ前**の時刻。
// 期限を進め、保留していた昇格を解く。
func (t *leaseTracker) renewed(start time.Time) {
	t.deadline = start.Add(t.ttl)
	t.pending = false
}

// expired は「保守側の見積もりで、自分の lease はもう生きていない」。
//
// 🚨 **猶予を足さないこと**。ここへ猶予を足すのは「他者が正当に引き継げる時刻を過ぎても
// 子を走らせ続ける」= 明示的な fail-open で、判定不能で昇格を保留する設計 ((b')) が
// fail-open でないと言い切れる根拠を壊す。境界は `deadline` ちょうどで真。
func (t *leaseTracker) expired(now time.Time) bool { return !now.Before(t.deadline) }

// indeterminate は「更新が判定不能だった」を記録し、**いま昇格すべきか**を返す。
//
// 期限を過ぎていれば true (他者が正当に引き継げる時刻を過ぎているので即昇格)、
// まだなら保留して false を返す。
func (t *leaseTracker) indeterminate(now time.Time) bool {
	if t.expired(now) {
		return true
	}
	t.pending = true
	return false
}

// dueForEscalation は「保留した昇格を、いま撃つべきか」。tick ごとに呼ぶ。
func (t *leaseTracker) dueForEscalation(now time.Time) bool { return t.pending && t.expired(now) }
