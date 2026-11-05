package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// 閾値。どれも docs/macos-health-check.md の「閾値は仮」を引き継ぐ (実測で外れたら直す)。
const (
	runawayCPU     = 90.0             // 2 回とも %CPU がこれ以上
	runawayMinAge  = 10 * time.Minute // 起動したばかりの山 (ビルド・インデックス) を外す
	zombieWarnAge  = time.Hour        // これより古いゾンビは、親が回収し損ねている
	growthMinKB    = 200 * 1024       // 2 回の間に増えた量がこれ以上
	growthMinRatio = 0.10             //   かつ 1 回目の 10% 以上
	pressureBad    = 10               // memory_pressure の空き % がこれ未満 = 異常
	pressureWarn   = 25               //                               これ未満 = 注意
	kallocBad      = 15_000_000       // data.kalloc.1024 の inuse (issue 500。パニックは約 2,100 万)
	recentDiag     = 7 * 24 * time.Hour
)

func (s *scanner) proc(ctx context.Context) []Check {
	cs := []Check{}
	snap, err := s.snapshot(ctx)
	if err != nil {
		cs = append(cs, unknown("proc.ps", "プロセスの一覧", err.Error()))
	} else {
		cs = append(cs, runaway(snap), zombies(snap.Second), memGrowth(snap), topRSS(snap.Second))
	}
	cs = append(cs, s.memPressure(ctx), s.swap(ctx), s.kalloc(ctx), s.resourceDiag())
	return cs
}

// runaway は 2 回とも CPU を張り付かせている、起動から時間の経ったプロセス。
func runaway(snap *psSnapshot) Check {
	c := Check{ID: "proc.runaway", Label: "CPU を張り付かせ続けるプロセス"}
	second := snap.secondByPID()
	var hits []string
	for _, p := range snap.First {
		q, ok := second[p.PID]
		if !ok || q.Exe != p.Exe || p.zombie() {
			continue
		}
		if p.CPU >= runawayCPU && q.CPU >= runawayCPU && q.Elapsed >= runawayMinAge {
			hits = append(hits, fmt.Sprintf("%s (pid %d, %.0f%%, 起動から%s)", q.Name(), q.PID, q.CPU, fmtDur(q.Elapsed)))
		}
	}
	if len(hits) == 0 {
		c.Value, c.Status = "なし", StatusOK
		return c
	}
	c.Value, c.Status = strings.Join(hits, " / "), StatusWarn
	c.Note = fmt.Sprintf("%s あけて 2 回読み、両方で %.0f%% 以上だったもの", snap.Interval, runawayCPU)
	c.Fix = "sample <pid> 3 で何をしているか見る。アプリなら再起動、要らないものなら kill <pid> (pid を名指しする。pkill は使わない)"
	return c
}

// zombies はゾンビを親ごとにまとめる。ゾンビ自身は kill できないので、直す相手は親。
func zombies(ps []proc) Check {
	c := Check{ID: "proc.zombie", Label: "ゾンビプロセス"}
	byPID := make(map[int]proc, len(ps))
	for _, p := range ps {
		byPID[p.PID] = p
	}
	type parent struct {
		n      int
		oldest time.Duration
	}
	parents := map[int]*parent{}
	total := 0
	for _, p := range ps {
		if !p.zombie() {
			continue
		}
		total++
		pp := parents[p.PPID]
		if pp == nil {
			pp = &parent{}
			parents[p.PPID] = pp
		}
		pp.n++
		pp.oldest = max(pp.oldest, p.Elapsed)
	}
	if total == 0 {
		c.Value, c.Status = "なし", StatusOK
		return c
	}
	ppids := make([]int, 0, len(parents))
	for k := range parents {
		ppids = append(ppids, k)
	}
	slices.SortFunc(ppids, func(a, b int) int { return parents[b].n - parents[a].n })
	var parts []string
	stale := false
	for _, k := range ppids {
		pp := parents[k]
		name := "不明"
		if q, ok := byPID[k]; ok {
			name = q.Name()
		}
		parts = append(parts, fmt.Sprintf("%s (pid %d) の下に %d 個 (最古 %s)", name, k, pp.n, fmtDur(pp.oldest)))
		stale = stale || pp.oldest >= zombieWarnAge
	}
	c.Value = fmt.Sprintf("%d 個: %s", total, strings.Join(parts, " / "))
	// 再起動を勧めてはいけない親。tmux のサーバは本番の全セッションを持ち、launchd・WindowServer・loginwindow はログインそのもの
	var keep []string
	for _, k := range ppids {
		if q, ok := byPID[k]; ok && (k == 1 || slices.Contains(noRestartParents, q.Name())) {
			keep = append(keep, q.Name())
		}
	}
	if !stale {
		c.Status, c.Note = StatusInfo, "どれも 1 時間以内 (親が回収する前の一瞬のこともある)"
		return c
	}
	c.Status = StatusWarn
	c.Note = "ゾンビは資源をほぼ食わないが、親が子を回収し損ねている印。溜まり続けるとプロセス数の上限に近づく"
	c.Fix = "親のアプリ・プロセスを再起動する (ゾンビ自身は kill できない)"
	if len(keep) > 0 {
		c.Fix = "親のアプリを再起動する (ゾンビ自身は kill できない)。ただし " + strings.Join(keep, "・") +
			" は再起動の対象にしない (tmux のサーバは全セッションを失い、launchd・WindowServer・loginwindow はログインごと落ちる)。増え続けるときだけ、次に再起動する機会に消える"
	}
	return c
}

// noRestartParents は、ゾンビの親でも再起動を勧めない ucomm。
var noRestartParents = []string{"tmux", "launchd", "WindowServer", "loginwindow", "kernel_task"}

// memGrowth は 2 回の読み取りの間に常駐量が大きく増えたプロセス (漏れの疑い。短い間隔では確定しない)。
func memGrowth(snap *psSnapshot) Check {
	c := Check{ID: "proc.memgrowth", Label: fmt.Sprintf("メモリが増え続けている疑い (%s の間)", snap.Interval)}
	second := snap.secondByPID()
	var hits []string
	for _, p := range snap.First {
		q, ok := second[p.PID]
		if !ok || q.Exe != p.Exe {
			continue
		}
		d := q.RSSKB - p.RSSKB
		if d >= growthMinKB && float64(d) >= float64(p.RSSKB)*growthMinRatio {
			hits = append(hits, fmt.Sprintf("%s (pid %d) %s → %s", q.Name(), q.PID, fmtMB(p.RSSKB), fmtMB(q.RSSKB)))
		}
	}
	if len(hits) == 0 {
		c.Value, c.Status = "なし", StatusOK
		c.Note = "短い間隔の 2 点なので、ゆっくり漏れるものは見えない。疑わしいアプリは -interval を伸ばして測り直す"
		return c
	}
	c.Value, c.Status = strings.Join(hits, " / "), StatusWarn
	c.Note = "一時的な山 (読み込み・ビルド) でも当たる。同じものが測り直しでも増えていれば漏れを疑う"
	c.Fix = "mac-healthcheck -categories proc -interval 60s で測り直す。続けて増えるならアプリを再起動し、leaks <pid> で見る"
	return c
}

// topRSS は常駐量の大きいプロセスの上位 (判定しない)。
func topRSS(ps []proc) Check {
	sorted := slices.Clone(ps)
	slices.SortFunc(sorted, func(a, b proc) int { return int(b.RSSKB - a.RSSKB) })
	parts := make([]string, 0, min(5, len(sorted)))
	for _, p := range sorted[:min(5, len(sorted))] {
		parts = append(parts, fmt.Sprintf("%s %s", p.Name(), fmtMB(p.RSSKB)))
	}
	return Check{ID: "proc.top", Label: "常駐量の大きいプロセス (上位 5)", Value: strings.Join(parts, " / "), Status: StatusInfo}
}

func (s *scanner) memPressure(ctx context.Context) Check {
	const id, label = "proc.pressure", "メモリの逼迫 (空きの割合)"
	out, stderr, rc, err := s.cmd(ctx, 10*time.Second, "/usr/bin/memory_pressure")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("memory_pressure", rc, err, stderr))
	}
	pct, ok := parsePressure(out)
	if !ok {
		return unknown(id, label, "memory_pressure の出力に System-wide memory free percentage の行が無い")
	}
	c := Check{ID: id, Label: label, Value: fmt.Sprintf("空き %d%%", pct), Status: StatusOK}
	switch {
	case pct < pressureBad:
		c.Status = StatusBad
	case pct < pressureWarn:
		c.Status = StatusWarn
	}
	if c.Status != StatusOK {
		c.Note = "空きの割合はすぐ返せるキャッシュを含む。これが低いのは本当に足りていない"
		c.Fix = "上位のアプリを閉じる。続くなら搭載メモリが用途に足りていない (Apple Silicon は増設できないので買い替えの材料になる)"
	}
	return c
}

func parsePressure(out string) (int, bool) {
	const key = "System-wide memory free percentage:"
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), key); ok {
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), "%"))
			return n, err == nil
		}
	}
	return 0, false
}

func (s *scanner) swap(ctx context.Context) Check {
	const id, label = "proc.swap", "スワップの使用量"
	out, stderr, rc, err := s.cmd(ctx, 5*time.Second, "/usr/sbin/sysctl", "-n", "vm.swapusage")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("sysctl", rc, err, stderr))
	}
	used := ""
	if _, rest, ok := strings.Cut(out, "used = "); ok {
		used, _, _ = strings.Cut(rest, " ")
	}
	if used == "" {
		return unknown(id, label, "vm.swapusage に used の欄が無い")
	}
	return Check{ID: id, Label: label, Value: used, Status: StatusInfo, Note: "単独では判定しない (メモリの逼迫と併せて見る)"}
}

// kalloc はカーネルの zone data.kalloc.1024 の inuse。ps にも出ないカーネルのリークで、溜まり切るとパニックする。
func (s *scanner) kalloc(ctx context.Context) Check {
	const id, label = "proc.kalloc", "カーネルのメモリリーク (data.kalloc.1024)"
	out, stderr, rc, err := s.cmd(ctx, 10*time.Second, "/usr/bin/zprint")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("zprint", rc, err, stderr))
	}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 7 || f[0] != "data.kalloc.1024" {
			continue
		}
		n, err := strconv.Atoi(f[6])
		if err != nil {
			break
		}
		c := Check{ID: id, Label: label, Value: fmt.Sprintf("%d 個", n), Status: StatusOK}
		if n >= kallocBad {
			c.Status = StatusBad
			c.Note = "約 2,100 万個でカーネルパニックになる (issue 500)"
			c.Fix = "再起動する。増え方は kernel-alloc-watch で記録する"
		}
		return c
	}
	return unknown(id, label, "zprint の出力に data.kalloc.1024 の行が無い")
}

// resourceDiag は OS が書いた資源超過・応答停止の記録 (直近 7 日) をプロセス名でまとめる。
func (s *scanner) resourceDiag() Check {
	c := Check{ID: "proc.resource", Label: "OS が記録した CPU・書き込み・応答停止の超過 (直近 7 日)"}
	since := s.o.Now().Add(-recentDiag)
	counts := map[string]int{}
	read := 0
	for _, dir := range s.o.DiagDirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		read++
		for _, e := range ents {
			name := e.Name()
			if !strings.HasSuffix(name, ".diag") && !strings.HasSuffix(name, ".hang") && !strings.HasSuffix(name, ".spin") {
				continue
			}
			if fi, err := e.Info(); err != nil || fi.ModTime().Before(since) {
				continue
			}
			counts[diagProcName(name)+" ("+diagKind(filepath.Join(dir, name))+")"]++
		}
	}
	if read == 0 {
		return unknown(c.ID, c.Label, "DiagnosticReports を読めない")
	}
	if len(counts) == 0 {
		c.Value, c.Status = "なし", StatusOK
		return c
	}
	c.Value, c.Status = joinCounts(counts), StatusWarn
	c.Note = "OS 自身が張り付きに気づいて書く記録。同じ張り付きが続いても毎回は書かれない"
	c.Fix = "中身 (~/Library/Logs/DiagnosticReports/ と /Library/Logs/DiagnosticReports/) の Event: (cpu usage / disk writes など) と、その下の超過の行を見る。アプリなら再起動・更新"
	return c
}

// diagKind は記録の種類。macOS 27 の資源超過の記録は名前が素の <プロセス>_<日時>_<マシン>.diag のものと
// .cpu_resource.diag のものが混ざっており、種類は中身の "Event:" の行 (cpu usage / disk writes / wakeups) にしか無い。
// 読めなければ拡張子で代える (数え漏らさない)。
func diagKind(path string) string {
	switch {
	case strings.HasSuffix(path, ".hang"):
		return "応答停止"
	case strings.HasSuffix(path, ".spin"):
		return "応答の遅れ"
	}
	f, err := os.Open(path)
	if err != nil {
		return "種類不明"
	}
	defer func() { _ = f.Close() }() // 読むだけなので、閉じる失敗で判定は変わらない
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	for line := range strings.SplitSeq(string(buf[:n]), "\n") {
		if v, ok := strings.CutPrefix(line, "Event:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return "種類不明"
}

// diagProcName は "<プロセス名>_<日時>_<マシン名>.cpu_resource.diag" や "<名前>-<日時>.ips" からプロセス名を取る。
// マシン名はレポートに出さない。
func diagProcName(file string) string {
	base := filepath.Base(file)
	if i := strings.Index(base, "_20"); i > 0 {
		return base[:i]
	}
	if i := strings.Index(base, "-20"); i > 0 {
		return base[:i]
	}
	base, _, _ = strings.Cut(base, ".")
	return base
}

// joinCounts は件数の多い順に "名前 ×N" を並べる。
func joinCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if counts[a] != counts[b] {
			return counts[b] - counts[a]
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s ×%d", k, counts[k])
	}
	return strings.Join(parts, " / ")
}
