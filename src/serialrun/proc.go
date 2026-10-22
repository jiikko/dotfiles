package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// psTimeout は ps 1 回の上限。詰まった ps で待ちの select に戻れなくなるのを防ぐ (超えたら判定不能)
const psTimeout = 2 * time.Second

// psField は pid の ps の 1 欄を返す。known=false は判定不能 (ps を起こせない・時間切れ・想定外の失敗)。
// known=true で値が空なら、その pid は居ない。
// 🚨 LC_ALL と TZ を固定する。lstart の書式はロケールとタイムゾーンで変わり、札を書いた serial-run と読む serial-run の環境が
// 違うと、同じ子の開始時刻が一致しなくなる (孤児の子を「死んだ」と読んで札を消し、2 本同時に走る)
func psField(pid int, field string) (val string, known bool) { return psFieldEnv(pid, field, true) }

// psFieldEnv は fixed=false なら環境を固定せずに ps を呼ぶ (古い版と同じ書式の child_start を書くためだけに使う)
func psFieldEnv(pid int, field string, fixed bool) (val string, known bool) {
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, psBin(), "-o", field+"=", "-p", strconv.Itoa(pid))
	if fixed {
		cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	}
	out, err := cmd.Output()
	s := strings.TrimSpace(string(out))
	if err == nil {
		return s, s != ""
	}
	// ps は指定の pid が居ないと rc=1 で何も出さない。それ以外の失敗は「居ない」と区別する
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 && s == "" && ctx.Err() == nil {
		return "", true
	}
	return "", false
}

// procStart は pid の開始時刻 (pid の使い回しと見分けるため) を返す
func procStart(pid int) (string, bool) { return psField(pid, "lstart") }

// procStat は pid の状態 (ps の stat。停止なら T で始まる) を返す
func procStat(pid int) (string, bool) { return psField(pid, "stat") }

// startSlack は「札を出した後に起きたか」の判定で許す壁時計の戻り
const startSlack = time.Minute

// lstartLayout は LC_ALL=C TZ=UTC の ps の lstart の書式
const lstartLayout = "Mon Jan _2 15:04:05 2006"

// startedAfter は、pid の開始時刻 (lstart) が t より後か (pid の使い回しで、札より前から居た別人を自分の子と読まないため)。
// 読めなければ false
func startedAfter(lstart string, t time.Time) bool {
	st, ok := parseLstart(lstart)
	if !ok {
		return false
	}
	// lstart は秒の精度。加えて、札を出してから子が起きるまでに壁時計が戻る (NTP の補正) と、本物の子が札より前に見える。
	// 余裕を持たせる: 外れたとき (使い回された pid を子と読む) に起きるのは余計に待つことだけで、回収はグループの一致も条件にしている
	return !st.Before(t.Add(-startSlack).Truncate(time.Second))
}

// parseLstart は LC_ALL=C TZ=UTC の lstart を時刻として読む (1 桁の日の空白 2 つも読む)
func parseLstart(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	st, err := time.ParseInLocation(lstartLayout, strings.Join(strings.Fields(s), " "), time.UTC)
	return st, err == nil
}

// ancestors は自分の祖先の pid を返す (親から順に。init の手前まで)。ps を使えなければ取れた所まで
func ancestors() map[int]bool {
	out := map[int]bool{}
	pid := os.Getppid()
	for range 64 {
		if pid <= 1 || out[pid] {
			return out
		}
		out[pid] = true
		// 負荷の下では ps が時間切れになりうる。取りこぼすと入れ子が自分の祖先の後ろに並ぶので、1 段ごとに数回試す
		s, known := "", false
		for range 3 {
			if s, known = psField(pid, "ppid"); known {
				break
			}
		}
		if !known || s == "" {
			return out
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return out
		}
		pid = n
	}
	return out
}
