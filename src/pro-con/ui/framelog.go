package ui

// 演出のカクつきを実際の起動で観測する口 (issue 494)。置き場に FrameLogOn の印があるあいだだけ、画面に届いたメッセージの時刻・種類・
// Update の所要と、描画 (View) の所要を FrameLogFile へ追記する。印は tick (1 秒) ごとに見るので、起動し直さずに入れたり切ったりできる。
// 演出のコマ (frameMsg) は 33ms ごとに届くはずなので、frameMsg どうしの間が大きく空いていればループが詰まった証拠になる
// (bubbletea は端末への書き込みが詰まると次の View の受け渡しで待つので、端末・tmux の側の詰まりもここに出る)。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	FrameLogOn   = "framelog.on"  // この印があるあいだ記録する (中身は見ない)
	FrameLogFile = "framelog.tsv" // 時刻 (RFC3339Nano) \t 種類 \t 所要 (µs)
	// frameLogMax を超えたら書くのをやめる (印を消し忘れても置き場を埋めない)
	frameLogMax = 20 << 20
)

type frameLog struct {
	dir     string
	f       *os.File
	written int64
}

// SetFrameLog は観測の置き場を決める ("" なら観測しない)。
func (m *Model) SetFrameLog(dir string) { m.flog = &frameLog{dir: dir} }

// check は印を見て記録を始める・やめる (tick ごと)。
func (l *frameLog) check() {
	if l == nil || l.dir == "" {
		return
	}
	_, err := os.Stat(filepath.Join(l.dir, FrameLogOn))
	switch on := err == nil; {
	case on && l.f == nil:
		f, err := os.OpenFile(filepath.Join(l.dir, FrameLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			l.f = f
		}
	case !on && l.f != nil:
		_ = l.f.Close()
		l.f, l.written = nil, 0
	}
}

func (l *frameLog) record(at time.Time, kind string, d time.Duration) {
	if l == nil || l.f == nil || l.written > frameLogMax {
		return
	}
	n, _ := fmt.Fprintf(l.f, "%s\t%s\t%d\n", at.Format(time.RFC3339Nano), kind, d.Microseconds())
	l.written += int64(n)
}

// msgKind はメッセージの種類の短い名前 (記録の 2 列目)。
func msgKind(msg any) string {
	s := fmt.Sprintf("%T", msg)
	return s[strings.LastIndex(s, ".")+1:]
}
