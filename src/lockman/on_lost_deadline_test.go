package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// 🚨 **既定 (`--on-lost=kill`) の経路を通る lease 生存テスト** (issue 385)。
//
// 385 以前は「最初の判定不能で即昇格」だったので、**一過性の詰まりで殺さなくてよい子を
// 殺していた**。381 がループ側を「判定不能でも更新を続ける」へ変えたのに、昇格側が
// 即殺すままだったため、既定では 381 の便益がほとんど回収されていなかった。
//
// 実測 A-B (2026-09-20。ttl=900ms / tick=300ms / io-timeout=200ms、詰まりの長さを変えて):
//
//	詰まり  旧 (即昇格)                新 (期限まで保留)
//	300ms   子が死ぬ (510ms)           子が完走 (3.02s) / lease 保持
//	500ms   子が死ぬ (710ms)           子が完走 (3.02s) / lease 保持
//	700ms   子が死ぬ (710ms)           子が死ぬ (910ms) ← 期限を過ぎたので従来どおり昇格
//
// このテストは 1 行目と 3 行目を固定する (2 本目が「常に昇格しない」への退行を止める)。
//
// 🚨 lease 生存系のテストが**全部 `onLostKill=false`** だったことが 385 の「テストの穴」節。
// 既定の経路を通るのは「本当に喪失した」列だけで、ここが埋まっていなかった。
// blockSpec は「いつから / どれだけ」詰まらせるか。複数回の詰まりを作るために使う。
type blockSpec struct {
	after    time.Duration // 直前の復旧 (最初は lock 取得) からの待ち
	duration time.Duration
}

// blockRenews は lock を FIFO で覆って更新を詰まらせ、指定の時間が経ったら復旧させる、を
// spec の回数だけ繰り返す。setup そのものの失敗は返り値の chan で返す (assert 以前の話)。
func blockRenews(l *Locker, specs []blockSpec) <-chan error {
	setupErr := make(chan error, 1)
	go func() {
		_, orig, err := waitForLockToken(l)
		if err != nil {
			setupErr <- err
			return
		}
		for _, spec := range specs {
			time.Sleep(spec.after)
			// ① 詰まらせる (FIFO を被せる)
			fifo := l.lockPath() + ".fifo"
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				setupErr <- err
				return
			}
			if err := os.Rename(fifo, l.lockPath()); err != nil {
				setupErr <- err
				return
			}
			time.Sleep(spec.duration)
			// ② 復旧させる。**順番が要る**: 先に FIFO を退かして本物を戻し、
			//    その後で FIFO へ書いて詰まっている読み手を解放する
			//    (逆順だと、解放された Renew の open がまた FIFO に当たって詰まる)
			if err := os.Rename(l.lockPath(), fifo); err != nil {
				setupErr <- err
				return
			}
			real := l.lockPath() + ".real"
			if err := os.WriteFile(real, orig, 0o600); err != nil {
				setupErr <- err
				return
			}
			if err := os.Rename(real, l.lockPath()); err != nil {
				setupErr <- err
				return
			}
			if f, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
				_, _ = f.Write(orig)
				_ = f.Close()
			}
			_ = os.Remove(fifo)
		}
		setupErr <- nil
	}()
	return setupErr
}

func TestDefaultKillWaitsForLeaseDeadline(t *testing.T) {
	for _, tc := range []struct {
		name         string
		block        time.Duration // 詰まりの長さ
		wantFinished bool          // 子が完走するか
	}{
		// 期限 (ttl=900ms) より十分手前で復旧する = 殺す理由が無い
		{"一過性 (期限より前に復旧)", 500 * time.Millisecond, true},
		// 期限を過ぎても判定不能のまま = 他者が正当に引き継げるので従来どおり昇格する
		{"期限を過ぎる (fail-closed のまま)", 1500 * time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, err := NewLocker(t.TempDir(), testIOTimeout) // io-timeout = 200ms
			if err != nil {
				t.Fatalf("NewLocker: %v", err)
			}
			const ttl = 900 * time.Millisecond // tick = 300ms
			mark := filepath.Join(t.TempDir(), "child-finished")

			setupErr := blockRenews(l, []blockSpec{{duration: tc.block}})

			// 子は 3 秒走ってからマーカーを書く。**完走したかはマーカーで見る**
			// (`kill(pid, 0)` はゾンビにも成功するので生死の判定に使えない。issue 384)
			rc := boundedInt(t, "runWith (既定の kill)", func() int {
				return runWith(l, ttl, "", true, // ← 既定 = --on-lost kill
					[]string{"sh", "-c", fmt.Sprintf("sleep 3; : > %q", mark)})
			})
			if err := <-setupErr; err != nil {
				t.Fatalf("前提が作れていない: %v", err)
			}
			_, markErr := os.Stat(mark)
			finished := markErr == nil
			if finished != tc.wantFinished {
				if tc.wantFinished {
					t.Fatalf("一過性の詰まりで子を殺している (rc=%d)。"+
						"lease の期限より前に復旧しているので、殺す理由が無い", rc)
				}
				t.Fatalf("lease の期限を過ぎても昇格していない (rc=%d, 子が完走した)。"+
					"他者が正当に引き継げる時刻を過ぎており、二重実行になる", rc)
			}
			// 判定不能だった窓があった事実は消えないので、どちらの腕でも rc は 125
			// (091:398-399 / issue 381 の sticky な outcome を据え置いている)
			if rc != exitWithInvalid {
				t.Errorf("rc=%d (期待 %d = 判定不能)", rc, exitWithInvalid)
			}
		})
	}
}

// 🚨 **更新が成功したら lease の期限も進むこと** (issue 385)。
//
// 「判定不能では期限まで昇格を保留する」だけでは足りない: 期限を**進めない**と、
// 2 回目の一過性の詰まりが来たとき「最初の期限はとうに過ぎている」ので即昇格してしまい、
// **更新が成功して lease が新しくなっているのに子を殺す**。
//
// この形は 1 回だけ詰まらせるテストでは**構造的に観測できない** (1 回目は期限より手前で
// 判定するので、期限を進めるかどうかが結果を変えない)。実測 2026-09-20: 期限を進めない
// 変異は、1 回詰まりのテスト 2 本では緑のまま通った。
//
// 時間の設計 (ttl=1500ms / tick=500ms / io-timeout=200ms):
//
//	400-800ms   1 回目の詰まり  → 700ms に判定不能。期限 (1500ms) は未到達なので保留
//	1000ms      更新が成功      → 期限が 2500ms へ進む (ここを消すのが変異)
//	1200-1600ms 2 回目の詰まり  → 1700ms に判定不能。正しければ期限 2500ms まで保留、
//	                              進めていなければ「期限切れ」と読んで即昇格 = 子が死ぬ
func TestLeaseDeadlineAdvancesOnSuccessfulRenew(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout) // io-timeout = 200ms
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 1500 * time.Millisecond // tick = 500ms
	mark := filepath.Join(t.TempDir(), "child-finished")

	setupErr := blockRenews(l, []blockSpec{
		{after: 400 * time.Millisecond, duration: 400 * time.Millisecond},
		{after: 400 * time.Millisecond, duration: 400 * time.Millisecond},
	})

	rc := boundedInt(t, "runWith (既定の kill / 詰まり 2 回)", func() int {
		return runWith(l, ttl, "", true, // ← 既定 = --on-lost kill
			[]string{"sh", "-c", fmt.Sprintf("sleep 3; : > %q", mark)})
	})
	if err := <-setupErr; err != nil {
		t.Fatalf("前提が作れていない: %v", err)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Fatalf("2 回目の一過性の詰まりで子を殺している (rc=%d)。"+
			"更新が成功したのに lease の期限が古いままで、「期限切れ」と読んでいる", rc)
	}
	if rc != exitWithInvalid {
		t.Errorf("rc=%d (期待 %d = 判定不能)", rc, exitWithInvalid)
	}
}

// 🚨 **「失敗した更新では期限を進めない」を固定する** (敵対レビュー 385 の P1-2)。
//
// `case err := <-renewCh` の `break` を外すと、**失敗した更新でも期限が ttl ぶん前進し、
// `pendingIndeterminate` も解ける**。更新が 1 回も成功していないのに昇格が来なくなるので、
// lease は実際に死に、他者が正当に引き継いで**無制限の二重実行**になる。
//
// 既存のテストがこれを守れなかったのは、詰まりを FIFO で作ると `case <-renewExpired` へ
// 落ちるので、**`renewCh` がエラーを返す列が 1 本も無かった**から。実在する速い失敗は
// `errUnreadableLock` (lock の中身を読めない。issue 383 が「健全な保持者の Renew の一瞬でも
// 出る」と記録している状態) なので、それで列を作る。
//
// 実測 A-B (2026-09-20 / ttl=900ms / io-timeout=200ms / 子は sleep 3):
//
//	現行        918ms で昇格、子は完走しない
//	break 削除  3.03s (昇格ゼロ) で子が完走 = lease は死んでいるのに走り続ける
func TestFailedRenewDoesNotAdvanceLeaseDeadline(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 900 * time.Millisecond
	mark := filepath.Join(t.TempDir(), "child-finished")

	setupErr := make(chan error, 1)
	go func() {
		// 取得できたら中身を壊す。以後の Renew は errUnreadableLock を**即座に**返す
		// (FIFO と違って詰まらないので、renewCh のエラー枝を通る)。
		if _, _, err := waitForLockToken(l); err != nil {
			setupErr <- err
			return
		}
		setupErr <- os.WriteFile(l.lockPath(), []byte("{ torn"), 0o600)
	}()

	// 🚨 **肯定側の観測を 1 つ持つ** (敵対レビュー 385 の 2 周目 P3-C)。合否が「marker が無い」+
	// 「rc == 125」だけだと**純粋な否定**になり、列が成立しなくても緑になる。実測: argv を
	// 存在しないバイナリに替えると `cmd.Start()` が失敗して即 125 を返し、更新も昇格も期限も
	// 一度も通らないのに PASS (0.02s。正常時は 0.91s)。子が走り始めたことを固定する。
	started := filepath.Join(filepath.Dir(mark), "child-started")
	rc := boundedInt(t, "runWith (中身を読めない lock)", func() int {
		return runWith(l, ttl, "", true, // ← 既定 = --on-lost kill
			[]string{"sh", "-c", fmt.Sprintf(": > %q; sleep 3; : > %q", started, mark)})
	})
	if err := <-setupErr; err != nil {
		t.Fatalf("前提が作れていない: %v", err)
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatalf("子が走り始めていない (rc=%d)。この列は成立しておらず、何も検査していない", rc)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatalf("更新が 1 回も成功していないのに子が完走した (rc=%d)。"+
			"失敗した更新で lease の期限を進めている = 昇格が永久に来ない", rc)
	}
	if rc != exitWithInvalid {
		t.Errorf("rc=%d (期待 %d = 判定不能)", rc, exitWithInvalid)
	}
}

// 🚨 **期限の境界に猶予を足さないこと** (敵対レビュー 385 の P2-1)。
//
// 猶予を足すのは「他者が正当に引き継げる時刻を過ぎても子を走らせ続ける」= 明示的な fail-open で、
// (b') が fail-open でないと言い切れる唯一の根拠を壊す。**統合テストでは捕まらない**
// (実測: 400ms = ttl の 44% の猶予を足す変異が、パッケージ全体で緑のまま通った) ので、
// 境界そのものを単体で固定する。
func TestLeaseHasExpiredBoundary(t *testing.T) {
	deadline := time.Now()
	for _, tc := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"期限の 1ns 前", deadline.Add(-time.Nanosecond), false},
		{"期限ちょうど", deadline, true},
		{"期限の 1ns 後", deadline.Add(time.Nanosecond), true},
		{"期限の 400ms 後 (猶予を足す変異が通ってしまう幅)", deadline.Add(400 * time.Millisecond), true},
	} {
		if got := leaseHasExpired(tc.now, deadline); got != tc.want {
			t.Errorf("%s: leaseHasExpired=%v (期待 %v)", tc.name, got, tc.want)
		}
	}
}
