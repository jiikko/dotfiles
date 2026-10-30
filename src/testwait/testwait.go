// Package testwait はテストの「条件が成り立つまで上限つきで待つ」を 1 つに持つ。テストからだけ使う。
// 秒数で待たず条件を見て待つ (_claude/rules/avoid-wall-clock-assertions.md)。上限を超えたら黙って先へ進まず落とし、
// そのときの状態 (待った時間・呼び出し側の診断・このテストのプロセスの子孫・goroutine) を出す。
// CI の混んだ runner でだけ起きる止まりは、落ちた 1 回のログしか手掛かりが無いので、止まった時点の状態をその場で残す。
package testwait

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Limit は Until の既定の上限。負荷の高い CI でも成り立つ条件が間に合う長さにしてある (縮めると遅いマシンで flaky になる)。
const Limit = 10 * time.Second

const defaultTick = 10 * time.Millisecond

// goroutineBytes は時間切れのときに出す goroutine の一覧の上限 (-race や並列のテストでは全部だと数 MB になる)。
const goroutineBytes = 32 << 10

type config struct {
	limit time.Duration
	tick  time.Duration
	step  func()
	diag  func() string
}

// Option は Until と Poll の待ち方を変える。
type Option func(*config)

// Within は上限を d にする (既定は Limit)。
func Within(d time.Duration) Option { return func(c *config) { c.limit = d } }

// Tick は条件を見る間隔を d にする (既定 10ms)。条件が外部コマンドを起こすなど重いときに広げる。
func Tick(d time.Duration) Option { return func(c *config) { c.tick = d } }

// Step は 1 周ごとに条件の前に回す処理 (Advance・結果の取り込み)。nil なら何もしない。
func Step(f func()) Option { return func(c *config) { c.step = f } }

// Diag は時間切れのときに出す、呼び出し側だけが知っている状態 (着いた段・相手の出力など)。
func Diag(f func() string) Option { return func(c *config) { c.diag = f } }

func newConfig(opts []Option) config {
	c := config{limit: Limit, tick: defaultTick}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Until は cond が成り立つまで待つ。上限を超えたら what と診断を出してテストを落とす。
func Until(t testing.TB, what string, cond func() bool, opts ...Option) {
	t.Helper()
	c := newConfig(opts)
	start := time.Now()
	if wait(c, cond) {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s が %v 待っても成り立たない (待ち始めから %v)", what, c.limit, time.Since(start).Round(time.Millisecond))
	if c.diag != nil {
		fmt.Fprintf(&b, "\n--- 呼び出し側の診断\n%s", strings.TrimRight(c.diag(), "\n"))
	}
	b.WriteString("\n" + Snapshot())
	t.Fatalf("%s", b.String())
}

// Poll は cond が成り立つまで最長 limit 待ち、成り立ったかを返す (落とすかは呼ぶ側が決める。落とすなら Snapshot を添える)。
func Poll(limit time.Duration, cond func() bool, opts ...Option) bool {
	c := newConfig(opts)
	c.limit = limit
	return wait(c, cond)
}

func wait(c config, cond func() bool) bool {
	// 上限は実時間で測る (テストが止まったときの安全網。テストが注入した時計ではない)
	deadline := time.Now().Add(c.limit)
	// 条件を先に見る (成り立っているなら step を回さない。step の Advance は状態を進めるので、余分に回すと待つ前の状態が変わる)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		if c.step != nil {
			c.step()
		}
		time.Sleep(c.tick) // sleep-ok: tick: 条件を見ながら刻む待ちの刻み (待ちの実体は cond)
	}
}

// Snapshot はこのテストのプロセスの子孫と goroutine の一覧を返す (時間切れの診断用)。
func Snapshot() string {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	stacks := string(buf[:n])
	if len(stacks) > goroutineBytes {
		stacks = stacks[:goroutineBytes] + fmt.Sprintf("\n… (全 %d bytes のうち先頭 %d bytes)", n, goroutineBytes)
	}
	return "--- このテストのプロセスの子孫 (STAT の T は止まっている、+ は端末の前面のグループ)\n" + ProcessTree(os.Getpid()) +
		"--- goroutine\n" + stacks
}

// ProcessTree は pid とその子孫の ps の行 (pid ppid pgid tpgid stat etime command) を返す。取れなければ理由を返す。
func ProcessTree(pid int) string {
	out, err := exec.Command("ps", "-Ao", "pid=,ppid=,pgid=,tpgid=,stat=,etime=,command=").Output()
	if err != nil {
		return fmt.Sprintf("(ps が失敗: %v)\n", err)
	}
	rows := map[int]string{}
	kids := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		p, err1 := strconv.Atoi(f[0])
		pp, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		rows[p] = line
		kids[pp] = append(kids[pp], p)
	}
	var b strings.Builder
	var walk func(p int)
	walk = func(p int) {
		if row, ok := rows[p]; ok {
			b.WriteString(row + "\n")
		}
		for _, k := range kids[p] {
			walk(k)
		}
	}
	walk(pid)
	if b.Len() == 0 {
		return fmt.Sprintf("(pid %d が ps に出ない)\n", pid)
	}
	return b.String()
}
