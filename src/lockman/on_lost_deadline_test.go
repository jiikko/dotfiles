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

			setupErr := make(chan error, 1)
			go func() {
				_, orig, err := waitForLockToken(l)
				if err != nil {
					setupErr <- err
					return
				}
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
				time.Sleep(tc.block)
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
				setupErr <- nil
			}()

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
