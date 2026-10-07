// Package proctree はプロセスの子孫を集めて止める。プロセスグループの全員と、ppid でたどれる子孫
// (setsid / setpgid でグループを抜けた子も、親が生きていれば木に居る) を凍らせてから集め、TERM → 猶予 → KILL。
// 使う側: src/runtimeout (時間の上限付き実行) / src/zundamon-kaisetsu (mermaid の描画。puppeteer は Chrome を別のグループで起こす)。
// 設計の経緯と敵対的レビューは issue 640 / 649。
package proctree

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 🚨 絶対パスで呼ぶ。テスト (tests/bin/test_go_autobuild.sh) は PATH を絞った状態で道具を起動する。
// var なのは ps が使えない経路をテストで作るため
var PSPath = "/bin/ps"

type proc struct {
	pid, ppid, pgid int
	zombie          bool
}

// listProcs は全プロセスの pid / ppid / pgid を返す。ps が使えなければ nil (呼び出し側はグループ宛てだけで止める)
func listProcs() []proc {
	out, err := exec.Command(PSPath, "-Ao", "pid=,ppid=,pgid=,stat=").Output()
	if err != nil {
		return nil
	}
	var ps []proc
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		pgid, e3 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		ps = append(ps, proc{pid: pid, ppid: ppid, pgid: pgid, zombie: strings.HasPrefix(f[3], "Z")})
	}
	return ps
}

// Target は止める対象。Root は木の根 (自分が起こした子)。Group は根を専用グループ (pgid = Root) に置いたか。
// 🚨 Group が false のとき、根は呼び出し元のグループに居る。グループ宛て (kill(-pgid)) を撃つと呼び出し元ごと止めるので、
// 個別の pid にだけ撃つ。
type Target struct {
	Root  int
	Group bool
}

// members は root の子孫を返す (値はグループの外に居るか = 個別に撃つ必要があるか)。
// group なら root のグループの全員も含める。木でたどるのは、setsid / setpgid でグループを抜けた子を拾うため
// (親が生きていれば木に居る)
func (t Target) members(ps []proc) map[int]bool {
	self := os.Getpid()
	set := map[int]bool{t.Root: !t.Group}
	if t.Group {
		for _, p := range ps {
			if p.pgid == t.Root {
				set[p.pid] = false
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, p := range ps {
			if _, in := set[p.pid]; in {
				continue
			}
			if _, parentIn := set[p.ppid]; parentIn {
				set[p.pid] = !t.Group || p.pgid != t.Root
				changed = true
			}
		}
	}
	delete(set, self)
	delete(set, 1)
	delete(set, 0)
	return set
}

// send はグループ宛てに 1 回 (group のときだけ)、グループの外の子孫へ個別に 1 回ずつ送る。
// 🚨 グループの中の子へ個別にも送らない: 同じシグナルが 2 回届き、2 回目の INT を強制終了と読むプログラムがある
func (t Target) send(set map[int]bool, sig syscall.Signal) {
	if t.Group {
		_ = syscall.Kill(-t.Root, sig)
	}
	for pid, outside := range set {
		if outside {
			_ = syscall.Kill(pid, sig)
		}
	}
	if !t.Group {
		// ps が使えず set が空でも、直接の子には届ける
		if _, in := set[t.Root]; !in {
			_ = syscall.Kill(t.Root, sig)
		}
	}
}

// Stop は Root とその子孫を止め、止め終えるまで返らない (最長で grace + ps 数回分)。
// bin/mutate-verify の stop_tree を移したもの: 凍らせてから集め直し、TERM → 猶予 → KILL。
// 凍らせるのは、親が先に死んで子が init へ付け替わると木から外れて取りこぼすため。
// 🚨 凍らせた pid は累積して持つ。集め直した一覧で置き換えると、凍らせた後に init へ付け替わった子が一覧から外れ、
// TERM も CONT も受けずに T のまま残る (mutate-verify の red team 2 周目で実測)。
// 止められないもの: 時間切れより前に親を離れて init の子になり、グループも抜けたもの (daemon 化した子・tmux -L のサーバ等)。
func (t Target) Stop(grace time.Duration) {
	all := map[int]bool{} // pid → グループの外に居るか (集めた時点)
	for range 10 {
		ps := listProcs()
		if ps == nil {
			break
		}
		var fresh []int
		for pid, outside := range t.members(ps) {
			if _, seen := all[pid]; !seen {
				all[pid] = outside
				fresh = append(fresh, pid)
			}
		}
		if len(fresh) == 0 {
			break
		}
		for _, pid := range fresh {
			_ = syscall.Kill(pid, syscall.SIGSTOP)
		}
	}
	// root は ps が使えなくても必ず入れる (-f ではグループ宛てに撃てないので、入っていないと KILL まで進まずに返り、
	// 呼び出し側が子の終了を無期限に待つ)。
	// 🚨 集める前に入れない: 入れると root が「まだ集めていないもの」から外れて凍らず、集めている間も fork を続けられる
	//    (setsid した子が逃げる。red team 2 周目で 6 回中 13〜20 個残った)
	if _, in := all[t.Root]; !in {
		all[t.Root] = !t.Group
	}
	// グループ宛ても撃つ: ps が使えなかった・集めた後に fork した、の保険
	t.send(all, syscall.SIGTERM)
	t.send(all, syscall.SIGCONT)

	end := time.Now().Add(grace)
	for {
		alive := aliveOf(all)
		if len(alive) == 0 && !t.groupExists() {
			return
		}
		if !time.Now().Before(end) {
			if t.Group {
				_ = syscall.Kill(-t.Root, syscall.SIGKILL)
			}
			for _, pid := range alive {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Forward は受けたシグナルを子孫へそのまま伝える (凍らせない。子に後始末の機会を渡す)
func (t Target) Forward(sig syscall.Signal) {
	var set map[int]bool
	if ps := listProcs(); ps != nil {
		set = t.members(ps)
	}
	t.send(set, sig)
}

// aliveOf は集めた pid のうち、まだ生きている (zombie でない) ものを返す
func aliveOf(all map[int]bool) []int {
	ps := listProcs()
	if ps == nil {
		// ps が使えないなら kill(0) で見る (zombie も生きている扱いになるが、KILL を撃つだけなので害は無い)
		var alive []int
		for pid := range all {
			if syscall.Kill(pid, 0) == nil {
				alive = append(alive, pid)
			}
		}
		return alive
	}
	var alive []int
	for _, p := range ps {
		if _, in := all[p.pid]; in && !p.zombie {
			alive = append(alive, p.pid)
		}
	}
	return alive
}

// groupExists は root のグループにまだ (zombie でない) 誰かが居るか (group でなければ常に false)
func (t Target) groupExists() bool {
	if !t.Group {
		return false
	}
	ps := listProcs()
	if ps == nil {
		return syscall.Kill(-t.Root, 0) == nil
	}
	for _, p := range ps {
		if p.pgid == t.Root && !p.zombie {
			return true
		}
	}
	return false
}
