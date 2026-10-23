package main

import (
	"fmt"
	"strings"
	"time"
)

// 表示の整形 (待ちの行・status・hook の案内・history.log)。札の中身は書き換えず、見せるときにここを通す

// printable は制御文字 (C0・DEL・C1・双方向の上書き) を ? に落とす。他の session の cwd とコマンド行は、ESC シーケンスを含んでいても
// そのまま別の session の LLM の additionalContext と人の端末に出るので、表示の入口で落とす (札の JSON は同一性に使わないので書き換えない)
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return '?'
		}
		return r
	}, s)
}

// displayCmd は表示用のコマンド行 (空白を 1 つに畳み、制御文字を落とし、120 字で切る)。札・history.log・hook.log に書く前に通す。
// 改行もタブも含まないので、history.log と hook.log の 1 行 1 本の形を崩さない
func displayCmd(argv []string) string {
	s := printable(oneLine(strings.Join(argv, " ")))
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分 %02d 秒", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d 時間 %02d 分", int(d.Hours()), int(d.Minutes())%60)
}

// age は表示する時間と「長く持っている」かを返す。走り出していれば子を起こしてから数える (待って先頭に上がった札を、走り出した直後から 🚨 にしない)。
// まだ走っていない札は札を出してから数え、🚨 は先頭のときだけ付ける。先頭は順番が来ればすぐ子を起こすので、走らないまま長いのは
// 持ち主が止まった (Ctrl-Z・SIGSTOP) か、run_start を書かない古い版が走らせている。待っている後ろの札には付けない
func (i ticketInfo) age(head bool) (label string, long bool) {
	if !i.RunStart.IsZero() {
		d := time.Since(i.RunStart)
		return "走り出してから " + fmtDur(d), d >= longHold
	}
	d := time.Since(i.Start)
	return "札を出してから " + fmtDur(d), head && d >= longHold
}

// holderSummary は札の持ち主を「<cwd> の <コマンド> (<経過>)」で書く (待ちの行・status・hook の案内が同じ形を使う)
func holderSummary(i ticketInfo, head bool) string {
	age, _ := i.age(head)
	return fmt.Sprintf("%s の %s (%s)", printable(i.Cwd), printable(i.Cmd), age)
}

// longHoldNote は長く持っている札への注意と、待たされている側が取れる手を返す。長くなければ空
func longHoldNote(i ticketInfo, head bool) string {
	if _, long := i.age(head); !long {
		return ""
	}
	return fmt.Sprintf("🚨 長く持っている (pid %d %s)。持ち主の session が止まっているならユーザーに知らせる。"+
		"待つ側は --max-wait <秒> で待ちを切り上げられる", i.Pid, statNote(i.Pid))
}

func statNote(pid int) string {
	st, known := procStat(pid)
	if !known || st == "" {
		return "ps で見えない"
	}
	return "stat=" + st
}
