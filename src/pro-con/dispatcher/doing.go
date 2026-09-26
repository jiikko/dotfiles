package dispatcher

// 「PG が今走らせているもの」を集めて store.DoingFile に書く (issue 473)。画面と `pro-con card show` はそれを読むだけ
// (画面が数秒ごとに ps や transcript を重く読まない = 441)。
//
// 集めるのは pro-con が起動した PG の分だけ:
//   - プロセス: 一覧 (claude agents) と記録の両方で pid が一致した PG の session の子孫 (pid の使い回しで外のプロセスの木を出さない)
//   - サブエージェント・道具の呼び出し: その session の transcript の末尾 (live.Transcript の Agents / Calls)
//
// 🚨 PG の外へ抜けたプロセス (nohup … & / setsid で親が launchd に移ったもの) は出ない。cwd で拾うと、人が worktree で開いた shell まで
// PG のものとして出す (pro-con の外の session を出さない = README の s の行と同じ方針)

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// doingEvery は集め直す間隔 (Tick は 3 秒ごと。ps は 1 回 0.04 秒ほどだが、transcript の末尾を PG ごとに読むので毎 Tick は回さない)。
const doingEvery = 10 * time.Second

// doingMax はカード 1 枚に載せるプロセスの上限 (make test の子孫で詳細が埋まらないように)。
const doingMax = 12

// psTimeout は ps 1 回の上限 (普段は 0.04 秒)。
const psTimeout = 5 * time.Second

// Proc はプロセス 1 つ。
type Proc struct {
	PID, PPID int
	Elapsed   time.Duration
	Command   string
}

// PSProcs は本物の ps でプロセスの一覧を読む。
func PSProcs(ctx context.Context) ([]Proc, error) {
	ctx, cancel := context.WithTimeout(ctx, psTimeout) // ps が詰まっても Tick (登録・割り当て) を止めない
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-A", "-ww", "-o", "pid=,ppid=,etime=,command=").Output()
	if err != nil {
		return nil, err
	}
	return parsePS(string(out)), nil
}

func parsePS(out string) []Proc {
	var ps []Proc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		el, ok := parseEtime(f[2])
		if err1 != nil || err2 != nil || !ok {
			continue
		}
		// command は 4 つ目の欄から行末まで (空白を含む)。欄の区切りは Fields と同じ空白で数える (区切りの判定を食い違わせない)
		rest := line
		for range 3 {
			rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
			rest = rest[strings.IndexFunc(rest, unicode.IsSpace):] // Fields が 4 欄以上を返したので、ここでは必ず見つかる
		}
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		ps = append(ps, Proc{PID: pid, PPID: ppid, Elapsed: el, Command: rest})
	}
	return ps
}

// parseEtime は ps の etime ([[dd-]hh:]mm:ss) を読む。
func parseEtime(s string) (time.Duration, bool) {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	secs := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		secs = secs*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(secs)*time.Second, true
}

// processDoings は pid の子孫を木の順 (親の直後に子) に並べる。PG が直接起こしたもの (深さ 0) は古い順。
// Claude Code の Bash は `zsh -c … eval '<コマンド>' …` の shell で包まれるので、その shell は包んだコマンドの文で出し、
// 包んだコマンドそのものの子 (同じ文) は重ねて出さない。
func processDoings(pid int, procs []Proc, now time.Time) []card.Doing {
	kids := map[int][]Proc{}
	for _, p := range procs {
		kids[p.PPID] = append(kids[p.PPID], p)
	}
	var out []card.Doing
	var walk func(parent int, depth int, parentText string)
	walk = func(parent int, depth int, parentText string) {
		cs := kids[parent]
		slices.SortStableFunc(cs, func(a, b Proc) int { return int(b.Elapsed - a.Elapsed) }) // 古い順
		for _, p := range cs {
			text := p.Command
			if s, ok := wrappedCommand(p.Command); ok {
				text = s
			}
			if depth == 0 && isClaudeHelper(p.Command) {
				continue
			}
			d := depth
			if text == parentText { // 包んだ shell が exec せずに起こした同じコマンド
				d--
			} else {
				out = append(out, card.Doing{Kind: card.DoingProcess, Text: text, Since: now.Add(-p.Elapsed), Depth: depth})
			}
			walk(p.PID, d+1, text)
		}
	}
	walk(pid, 0, "")
	for i := len(out) - 1; len(out) > doingMax && i >= 0; i-- { // 後ろの子孫から落とす (PG が直接起こしたものは残す)
		if out[i].Depth > 0 {
			out = slices.Delete(out, i, i+1)
		}
	}
	return out
}

// isClaudeHelper は Claude Code 自身が session の下に起こす補助 (PG が走らせたコマンドではない)。2.1.282 で実測したのは caffeinate だけ。
func isClaudeHelper(command string) bool {
	f := strings.Fields(command)
	return len(f) > 0 && filepath.Base(f[0]) == "caffeinate"
}

// wrappedCommand は Claude Code の Bash が包んだ shell の `eval '<コマンド>'` からコマンドを取り出す (2.1.282 で実測した形)。
// 形が違えば ok = false (そのまま出す)。
func wrappedCommand(command string) (string, bool) {
	const open, close = "eval '", "' < /dev/null"
	i := strings.Index(command, open)
	if i < 0 {
		return "", false
	}
	rest := command[i+len(open):]
	j := strings.LastIndex(rest, close)
	if j < 0 {
		return "", false
	}
	return strings.Join(strings.Fields(strings.ReplaceAll(rest[:j], `'\''`, `'`)), " "), true
}

// jobKinds は Claude Code がその session について書く ~/.claude/jobs/<短い id>/state.json の、走っている裏の作業の種類
// (inFlight.kinds。local_bash / local_agent。2.1.282 で実測)。読めなければ ok = false。
func jobKinds(jobsDir, id string) (kinds []string, ok bool) {
	if jobsDir == "" || id == "" || strings.ContainsAny(id, `/\`) {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(jobsDir, id, "state.json"))
	if err != nil {
		return nil, false
	}
	var st struct {
		InFlight *struct {
			Kinds []string `json:"kinds"`
		} `json:"inFlight"`
	}
	if json.Unmarshal(data, &st) != nil || st.InFlight == nil {
		return nil, false
	}
	return st.InFlight.Kinds, true
}

// collectDoing は doingEvery ごとに、PG が今走らせているものを集めて書く。書けなくても Tick は続ける (見えないだけで割り当ては止めない)。
func (d *Dispatcher) collectDoing(ctx context.Context, now time.Time, ss []agents.Session) {
	if d.Procs == nil || (!d.doingAt.IsZero() && now.Sub(d.doingAt) < doingEvery) {
		return
	}
	d.doingAt = now
	st, err := store.Load(d.Dir)
	if err != nil {
		return
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return
	}
	out := store.Doing{At: now, Cards: map[string][]card.Doing{}}
	procs, perr := d.Procs(ctx)
	if perr != nil {
		out.Err = perr.Error()
	}
	for _, c := range st.Cards {
		if c.Session == "" || c.State == card.Done {
			continue
		}
		o, ok := owned(c, reg)
		if !ok {
			continue
		}
		if !liveIn(ss, c.Session, o.PID) { // 一覧と記録で pid が合わない = 今動いていると確かめられない session は、transcript の残りも出さない
			continue
		}
		var ds []card.Doing
		if perr == nil && isClaude(procs, o.PID) {
			ds = processDoings(o.PID, procs, now)
		}
		ds = append(ds, d.transcriptDoings(o, perr == nil)...)
		if len(ds) > 0 {
			out.Cards[c.ID] = ds
		}
	}
	_ = store.SaveDoing(d.Dir, out)
}

// isClaude は ps の一覧で pid が claude のプロセスか。一覧 (ss) は Tick の頭に取ったもので、ps はその後なので、
// その間に PG が落ちて pid が使い回されたら、外のプロセスの子孫を出してしまう。同じ ps の中で pid の中身を確かめる。
func isClaude(procs []Proc, pid int) bool {
	for _, p := range procs {
		if p.PID == pid {
			f := strings.Fields(p.Command)
			return len(f) > 0 && filepath.Base(f[0]) == "claude"
		}
	}
	return false
}

// liveIn は短い id の session が一覧に居て、pid が記録と一致するか (一致しない pid の子孫は出さない)。
func liveIn(ss []agents.Session, id string, pid int) bool {
	for _, s := range ss {
		if s.ID == id && pid != 0 && s.PID == pid {
			return true
		}
	}
	return false
}

// transcriptDoings は transcript の末尾から、裏のサブエージェントと結果待ちの道具の呼び出しを出す。
// Bash の呼び出しはプロセスとして出ているので、プロセスを読めたときは重ねない。
// サブエージェントは jobs の state.json が「走っていない」と言えば出さない (終わりの知らせの形が変わって読めなくても残り続けない)。
func (d *Dispatcher) transcriptDoings(o live.Owned, procsOK bool) []card.Doing {
	if d.Transcript == nil || o.SessionID == "" {
		return nil
	}
	t, err := d.Transcript(o.SessionID)
	if err != nil {
		return nil
	}
	var out []card.Doing
	kinds, known := jobKinds(d.JobsDir, o.ID)
	if !known || slices.Contains(kinds, "local_agent") {
		for _, a := range t.Agents {
			out = append(out, card.Doing{Kind: card.DoingAgent, Text: orName(a.Text, "(説明なし)"), Since: a.At})
		}
		if known && len(t.Agents) == 0 { // 起こしたのが transcript の末尾より前
			out = append(out, card.Doing{Kind: card.DoingAgent, Text: "(transcript の末尾に起こした記録が無い)", Since: t.LastAt})
		}
	}
	for _, c := range t.Calls {
		if c.Name == "Bash" && procsOK {
			continue
		}
		text := c.Name
		if c.Text != "" {
			text += ": " + c.Text
		}
		out = append(out, card.Doing{Kind: card.DoingTool, Text: text, Since: c.At})
	}
	return out
}

func orName(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}
