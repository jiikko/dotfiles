package main

// pro-con ps — 役ごとのプロセスの一覧 (issue 456。読むだけ)。dispatcher / PM / PG / テストの係 / 画面を、pid・起動からの経過・状態・
// 担当のカード・今のコマンドで並べる。
//
// 🚨 出すのは pro-con が起動したものだけ: dispatcher は lock の pid、PM と PG は起動の記録 (sessions.json)、テストの係はカードの実行の印
// (`pro-con-run:<RunID>`)、画面は presence の数。pro-con の外の session は名前も本数も出さない (README の s の行と同じ)。
// 🚨 状態の置き場に何も書かない (dispatcher の lock も取らない: 取ると、その間に起動した dispatcher が「既に動いている」で抜ける)。
// 生きているかは `ps` を 1 回だけ読んで決める (claude agents は最大 10 秒かかり本物の claude を起こすので読まない。busy / idle は出さない)。
// 要約の係 (haiku) は dispatcher の中の短い子プロセスで印を持たないので、ここには出さない。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/dispatcher"
	"pro-con/live"
	"pro-con/relay"
	"pro-con/store"
)

const psUsage = `usage: pro-con ps [--json]   (pro-con が起動したプロセスを役ごとに出す。読むだけ)`

// Proc は一覧の 1 行 (設定画面のプロセスのタブと同じ型)。
type Proc = backend.Proc

// procLister は動いているプロセスの pid → コマンド行 (ps を 1 回。テストが差し替える)。
type procLister func(ctx context.Context) (map[int]string, error)

func execProcs(ctx context.Context) (map[int]string, error) {
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	procs := map[int]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if pid, err := strconv.Atoi(f[0]); err == nil {
			procs[pid] = strings.Join(f[1:], " ")
		}
	}
	return procs, sc.Err()
}

func runPS(args []string, dir string, now func() time.Time, list procLister, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con ps", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, psUsage)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	procs, err := list(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con ps:", err)
		return 1
	}
	rows, warns := collectProcs(dir, now(), procs)
	for _, w := range warns {
		_, _ = fmt.Fprintln(stderr, "pro-con ps:", w)
	}
	if *asJSON {
		data, _ := json.MarshalIndent(rows, "", "  ")
		_, _ = fmt.Fprintln(stdout, string(data))
	} else {
		for _, p := range rows {
			pid := "-"
			if p.PID > 0 {
				pid = strconv.Itoa(p.PID)
			}
			age := "-"
			if p.Age > 0 {
				age = p.Age.Round(time.Second).String()
			}
			state := p.State
			if p.Mismatch != "" {
				state = "食い違い: " + p.Mismatch
			}
			_, _ = fmt.Fprintf(stdout, "%-10s  %-7s  %-9s  %-14s  %-6s  %-12s  %s\n",
				p.Role, pid, age, state, orDashCLI(p.Card), orDashCLI(p.Session), orDashCLI(p.Command))
		}
	}
	if len(warns) > 0 {
		return 1
	}
	return 0
}

// collectProcs は状態の置き場と procs から役ごとの行を組む (読むだけ)。読めなかったものは warns に出す (0 本と区別する)。
func collectProcs(dir string, now time.Time, procs map[int]string) (rows []Proc, warns []string) {
	alive := func(pid int) bool { _, ok := procs[pid]; return pid > 0 && ok }

	// dispatcher: lock のファイルの pid (Lock が書く)。pid が生きていても別のプロセスかもしれないので、コマンド行に dispatcher を含むものだけ
	ds, ticked, err := store.LoadDispatcherState(dir)
	if err != nil {
		warns = append(warns, "dispatcher の様子を読めない: "+err.Error())
	}
	d := Proc{Role: "dispatcher", State: backend.ProcStopped}
	if b, err := os.ReadFile(filepath.Join(dir, dispatcher.LockFile)); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && alive(pid) && strings.Contains(procs[pid], "dispatcher") {
			d.PID, d.State, d.Command = pid, "動いている", procs[pid]
			if ticked {
				d.State += fmt.Sprintf(" (最後の Tick %s 前)", now.Sub(ds.Tick).Round(time.Second))
			}
		}
	}
	rows = append(rows, d)
	// 見張り (issue 475): dispatcher と同じく、lock のファイルの pid でコマンド行に monitor を含むものだけ
	mon := Proc{Role: "見張り", State: backend.ProcStopped}
	if b, err := os.ReadFile(filepath.Join(dir, dispatcher.MonitorLockFile)); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && alive(pid) && strings.Contains(procs[pid], " monitor") {
			mon.PID, mon.State, mon.Command = pid, "動いている", procs[pid]
		}
	}
	rows = append(rows, mon)

	st, err := store.Load(dir)
	cardsOK := err == nil // 読めなければ食い違いを付けない (どの PG も「片付け済みなのに動いている」に見える)
	if err != nil {
		warns = append(warns, "カードの記録を読めない: "+err.Error())
	}
	cards := map[string]card.Card{}
	for _, c := range st.Cards {
		cards[c.ID] = c
	}
	reg, err := live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if err != nil {
		warns = append(warns, "起動の記録を読めない: "+err.Error())
	}
	sort.SliceStable(reg, func(i, j int) bool {
		return roleOrder(reg[i].CardID) < roleOrder(reg[j].CardID)
	})
	for _, o := range reg {
		p := Proc{Role: "PG", PID: o.PID, Card: o.CardID, Session: o.ID, State: backend.ProcStopped}
		if !o.StartedAt.IsZero() {
			p.Age = now.Sub(o.StartedAt)
		}
		switch o.CardID {
		case dispatcher.PMCardID:
			p.Role, p.Card = "PM", ""
		case dispatcher.IntegratorCardID:
			p.Role, p.Card = "取り込み", ""
		}
		c, known := cards[o.CardID]
		if alive(o.PID) && strings.Contains(procs[o.PID], "claude") { // pid が別のプロセスに使い回されたものを数えない (dispatcher の行と同じ)
			p.State = "動いている"
			if known {
				p.State = c.State.Label()
				if c.Exec.Active() {
					p.Command = c.Exec.Command
				}
			}
		}
		if p.Role == "PG" && cardsOK {
			p.Mismatch = mismatch(p.State != backend.ProcStopped, c, known)
		}
		rows = append(rows, p)
	}

	// テストの係: 実行の印 (RunID) がコマンド行に載っているプロセス
	for _, c := range st.Cards {
		e := c.Exec
		if !e.Active() {
			continue
		}
		p := Proc{Role: "テストの係", Card: c.ID, Command: e.Command, State: backend.ProcStopped}
		if !e.Since.IsZero() {
			p.Age = now.Sub(e.Since)
		}
		if e.RunID != "" {
			for pid, cmd := range procs {
				if strings.Contains(cmd, dispatcher.RunMark+e.RunID) && (p.PID == 0 || pid < p.PID) { // 同じ印の子孫があれば親 (小さい pid) を出す
					p.PID, p.State = pid, "実行中"
				}
			}
		}
		rows = append(rows, p)
	}

	// 画面: 画面の中継 (relay) の印。🚨 presence は数えない (Count は落ちた画面の印を消し、数えるための一瞬の flock も
	// 最後の画面の Leave に「ほかにも開いている」と見せる)。relay の印は別のファイルで、pro-con screen と同じ読み方
	screens, err := relay.List(dir)
	if err != nil {
		warns = append(warns, "開いている画面を読めない: "+err.Error())
	}
	for _, s := range screens {
		if s.Open {
			rows = append(rows, Proc{Role: "画面", State: "開いている", Session: s.ID})
		}
	}
	return rows, warns
}

// mismatch はカードと PG の session の食い違いの文 (無ければ空)。known はカードが記録にあるか (無ければ完了して書庫へ移したか削除した)。
// 食い違いは issue 497 の 2 つだけ: 作業中なのに止まっている / 完了 (か片付け済み) なのに動いている。
// 質問待ち・レビュー待ちなどで止まっているのは食い違いにしない (dispatcher を止めた後に全部が並ぶと、497 が消したい雑音に戻る)。
func mismatch(alive bool, c card.Card, known bool) string {
	switch {
	case alive && !known:
		return "動いている (カードは片付け済み)"
	case alive && c.State == card.Done:
		return "動いている (カードは完了)"
	case !alive && known && c.State == card.Running:
		return "止まっている (カードは" + c.State.Label() + ")"
	}
	return ""
}

// roleOrder は一覧の並び (PM → 取り込みの係 → PG)。
func roleOrder(cardID string) int {
	switch cardID {
	case dispatcher.PMCardID:
		return 0
	case dispatcher.IntegratorCardID:
		return 1
	}
	return 2
}

// inspectProcs は画面のプロセスのタブの読み方 (pro-con ps と同じ出どころ・同じ集め方。読むだけ)。読めなかったものは err にまとめ、読めた分も返す。
func inspectProcs(dir string, now func() time.Time, list procLister) ([]Proc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	procs, err := list(ctx)
	if err != nil {
		return nil, err
	}
	rows, warns := collectProcs(dir, now(), procs)
	if len(warns) > 0 {
		return rows, errors.New(strings.Join(warns, " / "))
	}
	return rows, nil
}
