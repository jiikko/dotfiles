package main

// pro-con attach --leave — 戻れなくなった attach の接続だけを外から終わらせる口 (issue 527)。
// 2026-09-26 にユーザーが attach から戻れず、外から attach の接続 (pro-con の画面の子の claude attach) に SIGTERM を送って戻した。
// その手順を道具にする。終わらせるのは attach の接続だけで、PG の session (claude --bg の本体) には触らない。
//
//   - tmux の中 (popup): popup の中の入れ子の tmux サーバ (一時ディレクトリ pro-con-attach-*/sock) を kill-server する (戻るキーと同じ)
//   - tmux の外 (端末を渡した attach): pro-con の画面の子の claude attach (e2e モードは fake-attach) に SIGTERM を送る

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pro-con/ui"
)

const leaveUsage = "usage: pro-con attach --leave   (戻れなくなった attach の接続だけを終わらせる。PG の session は動き続ける)"

// psRow は ps の 1 行 (pid・親の pid・コマンド行)。
type psRow struct {
	pid, ppid int
	command   string
}

// attachLeaver は attach の接続を終わらせる手段 (テストが差し替える)。
type attachLeaver struct {
	list       func(ctx context.Context) ([]psRow, error)
	killServer func(sock string) error
	term       func(pid int) error
}

func realLeaver() attachLeaver {
	return attachLeaver{
		list: psRows,
		killServer: func(sock string) error {
			return exec.Command("tmux", "-S", sock, "kill-server").Run()
		},
		term: func(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) },
	}
}

func psRows(ctx context.Context) ([]psRow, error) {
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	var rows []psRow
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			rows = append(rows, psRow{pid: pid, ppid: ppid, command: strings.Join(f[2:], " ")})
		}
	}
	return rows, sc.Err()
}

// findAttaches は ps の行から、popup の入れ子の tmux の socket と、画面の子の attach の pid を拾う。
// 入れ子の tmux は client と server の 2 行が同じ socket を持つので、socket で束ねる。
func findAttaches(rows []psRow) (socks []string, pids []int) {
	cmdOf := map[int]string{}
	for _, r := range rows {
		cmdOf[r.pid] = r.command
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if sock := popupSocket(r.command); sock != "" {
			if !seen[sock] {
				seen[sock] = true
				socks = append(socks, sock)
			}
			continue
		}
		if isAttachCommand(r.command) && isProCon(cmdOf[r.ppid]) {
			pids = append(pids, r.pid)
		}
	}
	return socks, pids
}

// popupSocket は入れ子の tmux のコマンド行なら socket のパスを返す (tmux ... -S <dir>/pro-con-attach-*/sock ...)。
func popupSocket(command string) string {
	f := strings.Fields(command)
	if len(f) == 0 || filepath.Base(f[0]) != "tmux" {
		return ""
	}
	for i := 1; i+1 < len(f); i++ {
		if f[i] == "-S" && strings.HasPrefix(filepath.Base(filepath.Dir(f[i+1])), ui.PopupDirPrefix) {
			return f[i+1]
		}
	}
	return ""
}

func isAttachCommand(command string) bool {
	f := strings.Fields(command)
	switch {
	case len(f) >= 3 && filepath.Base(f[0]) == "claude" && f[1] == "attach":
		return true
	case len(f) >= 3 && isProCon(f[0]) && f[1] == "fake-attach": // e2e モードの偽の attach
		return true
	}
	return false
}

func isProCon(command string) bool {
	f := strings.Fields(command)
	return len(f) > 0 && filepath.Base(f[0]) == "pro-con"
}

func runAttach(args []string, l attachLeaver, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "--leave" {
		_, _ = fmt.Fprintln(stderr, leaveUsage)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := l.list(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con attach:", err)
		return 1
	}
	socks, pids := findAttaches(rows)
	if len(socks)+len(pids) == 0 {
		_, _ = fmt.Fprintln(stderr, "pro-con attach: 終わらせる attach の接続が無い (pro-con の画面から開いた attach だけを探す)")
		return 1
	}
	rc := 0
	for _, s := range socks {
		if err := l.killServer(s); err != nil {
			_, _ = fmt.Fprintf(stderr, "pro-con attach: popup の attach (%s) を終わらせられない: %v\n", s, err)
			rc = 1
			continue
		}
		_, _ = fmt.Fprintf(stdout, "popup の attach を終わらせた (%s)。PG の session は動き続ける\n", s)
	}
	for _, p := range pids {
		if err := l.term(p); err != nil {
			_, _ = fmt.Fprintf(stderr, "pro-con attach: attach の接続 (pid %d) を終わらせられない: %v\n", p, err)
			rc = 1
			continue
		}
		_, _ = fmt.Fprintf(stdout, "attach の接続 (pid %d) を終わらせた。PG の session は動き続ける\n", p)
	}
	return rc
}
