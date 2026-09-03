package runner

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ctx の期限で子を殺した後、pipe を継承した孫が残っていても Run が戻る (WaitDelay)。
// 孫 (bg の sleep) は stdout を継承しているので、WaitDelay が無いと孫の 20 秒まで戻らない。
func TestExecReturnsAfterTimeoutEvenIfGrandchildHoldsPipe(t *testing.T) {
	t0 := time.Now()
	_, _, _, err := WithTimeout(context.Background(), Exec, time.Second, "sh", "-c", "sleep 20 & exec sleep 20")
	elapsed := time.Since(t0)
	if err == nil {
		t.Fatal("timeout なのに err が nil")
	}
	// 判定は「孫の 20 秒より桁で短いか」(成功側 ~1+2 秒 / 失敗側 20 秒。実測で 6 倍以上の差)
	if elapsed > 10*time.Second {
		t.Fatalf("孫が pipe を握っている間 Run が戻らない: %s", elapsed)
	}
}

// cancel で孫まで死ぬ (プロセスグループ)。孫が pipe に書かなくても残らない。
//
// ⚠️ **判定を時計に依存させない** (issue 212)。旧版は「300ms 待って cancel し、2 秒後に marker が
// 無ければ合格」だったので、**孫が fork される前に cancel が届いた run では何も検査していない**
// (marker が無いのは当たり前で、緑の側から観測できない)。CI が高負荷のときだけ vacuous になる形。
// 今の形は「孫が自分の pid を書く」→「読めるまで待つ (= 孫が確かに生まれた)」→「cancel」→
// 「その pid が死ぬまで待つ」の 3 段で、**孫の生存という状態**で判定する。
// 孫が生まれなければ pid が読めず、合格でも不合格でもなく **判定不能として落ちる**
// (_claude/rules/adversarial-review-own-safeguards.md: 判定できなかったを緑にしない)。
func TestExecKillsGrandchildOnCancel(t *testing.T) {
	pidFile := t.TempDir() + "/grandchild.pid"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// 孫の pid は **親から見た `$!`** で取る。⚠️ サブシェルの中の `$$` は POSIX では
		// **起動シェル (= 直接の子) の pid** で、孫の pid ではない (実測 2026-09-03)。
		// `$$` で書くと、この検査は孫ではなく**直接の子**を見ることになり、
		// `Kill(-pgid)` を `Kill(pid)` に退行させても緑のまま通る (変異検証で実際に素通りした)。
		// 孫は stdout を閉じて pipe を握らない (WaitDelay 頼みにしない)。親は exec sleep なので、
		// 孫が死ぬ経路はプロセスグループ経由だけ
		_, _, _, _ = Exec(ctx, "sh", "-c",
			"(exec sleep 30) >/dev/null 2>&1 & echo $! > "+pidFile+"; exec sleep 30")
		close(done)
	}()

	// 孫が生まれた証拠 (pid) を待つ。取れなければ判定不能で落とす
	pid := waitForPID(t, pidFile, 10*time.Second)
	if !processAlive(pid) {
		t.Fatalf("判定不能: 孫 (pid=%d) が cancel の前に既に死んでいる", pid)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel 後に Exec が戻らない")
	}

	// 孫が死ぬこと。ポーリングは「死ぬまでの上限」であって合否の基準ではない
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("孫 (pid=%d) が cancel 後も生きている (プロセスグループごと殺していない)", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForPID は孫が書いた pid を読めるまで待つ。読めなければ **判定不能として落とす**
// (「孫が生まれなかった」を合格にしない)。
func waitForPID(t *testing.T, path string, limit time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			if pid, cerr := strconv.Atoi(strings.TrimSpace(string(b))); cerr == nil && pid > 0 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("判定不能: 孫の pid を %s から読めない (孫が生まれていない = 何も検査できていない)", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// processAlive は pid が生きているかを signal 0 で見る。
// ⚠️ ゾンビ (親が wait していない) にも 0 は通るが、ここでは親 (sh) がプロセスグループごと
// 殺されるため孫は再親付けされて init が回収する。生存判定として十分
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
