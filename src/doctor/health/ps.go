package health

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"proctree"
)

// proc は ps の 1 行。
//
// Exe は comm 列で、中身は argv[0] (起動した側が書いた文字列)。普段は実行ファイルのパスだが、exec -a や process.title で
// 何でも書けるので、トークンが載りうる。🚨 Exe はレポートに出さない: Chrome の版 (…/Versions/<版>/…) や .app の所属のような
// 内部の判定にだけ使い、表示する名前は UName (ucomm 列 = カーネルが実行ファイルから付けた名前。最長 16 文字) から取る。
// 引数 (args) は同じ理由で最初から取らない。
type proc struct {
	PID, PPID int
	Stat      string
	CPU       float64
	RSSKB     int64
	Elapsed   time.Duration
	TTY       string
	Exe       string
	UName     string
}

// Name は表示に使う名前 (ucomm)。ucomm が取れなかった行は "?" (argv[0] へ戻さない)。
func (p proc) Name() string {
	if p.UName == "" {
		return "?"
	}
	return p.UName
}

// ucommMax は ucomm の最長 (MAXCOMLEN)。名前で照合するときは相手の名前もこの長さで切る。
const ucommMax = 16

// sameName は ucomm と実行ファイル名が同じかを、ucomm の切り詰めを考えて比べる。
func sameName(uname, exeName string) bool {
	if len(exeName) > ucommMax {
		exeName = exeName[:ucommMax]
	}
	return uname == exeName
}

func (p proc) zombie() bool { return strings.HasPrefix(p.Stat, "Z") }

// psArgs は ps の列。comm は空白を含みうるので最後に置く (前の 7 列は空白を含まない)。
var psArgs = []string{"-axww", "-o", "pid=,ppid=,stat=,pcpu=,rss=,etime=,tty=,comm="}

// ucommArgs は表示用の名前。ucomm も空白を含みうる ("Google Chrome He") ので、comm と同じ ps には並べられず、別に読んで pid で付ける。
var ucommArgs = []string{"-axww", "-o", "pid=,ucomm="}

// parseUcomm は "pid 名前" を読む。
func parseUcomm(out string) map[int]string {
	m := map[int]string{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		m[pid] = restAfterFields(line, 1)
	}
	return m
}

// parsePS は ps の出力を読む。読めない行は数を返す (黙って捨てない)。
func parsePS(out string) (ps []proc, bad int) {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 8 {
			bad++
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		cpu, e3 := strconv.ParseFloat(f[3], 64)
		rss, e4 := strconv.ParseInt(f[4], 10, 64)
		el, ok := proctree.ParseEtime(f[5])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || !ok {
			bad++
			continue
		}
		// comm は 8 列目以降。空白を 1 つに潰さないよう、元の行から 7 列を読み飛ばした残りを使う
		ps = append(ps, proc{PID: pid, PPID: ppid, Stat: f[2], CPU: cpu, RSSKB: rss, Elapsed: el, TTY: f[6], Exe: restAfterFields(line, 7)})
	}
	return ps, bad
}

// restAfterFields は先頭の n 個の欄を読み飛ばした残り (前後の空白を除く)。
func restAfterFields(line string, n int) string {
	s := strings.TrimLeft(line, " \t")
	for range n {
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return ""
		}
		s = strings.TrimLeft(s[i:], " \t")
	}
	return strings.TrimRight(s, " \t\r")
}

// psSnapshot は間隔をあけて 2 回読んだ ps。
type psSnapshot struct {
	First, Second []proc
	Interval      time.Duration
}

// secondByPID は 2 回目を pid で引く表。
func (s *psSnapshot) secondByPID() map[int]proc {
	m := make(map[int]proc, len(s.Second))
	for _, p := range s.Second {
		m[p.PID] = p
	}
	return m
}

// snapshot は 1 回の Scan の中で 1 度だけ ps を 2 回読む (proc / tmux / nvim / chrome / apps が同じ材料を使う)。
func (s *scanner) snapshot(ctx context.Context) (*psSnapshot, error) {
	if s.snap != nil || s.snapErr != nil {
		return s.snap, s.snapErr
	}
	read := func() ([]proc, error) {
		out, stderr, rc, err := s.cmd(ctx, 20*time.Second, "/bin/ps", psArgs...)
		if err != nil || rc != 0 {
			return nil, errors.New(cmdNote("ps", rc, err, stderr))
		}
		ps, bad := parsePS(out)
		if len(ps) == 0 {
			return nil, fmt.Errorf("ps の出力が読めない (読めない行 %d)", bad)
		}
		return ps, nil
	}
	first, err := read()
	if err == nil {
		err = s.o.Sleep(ctx, s.o.Interval)
	}
	var second []proc
	if err == nil {
		second, err = read()
	}
	if err == nil {
		// 名前は 2 回目の直後に 1 度だけ読み、両方に付ける (数秒のうちに pid が使い回されることは実際上ない)
		out, stderr, rc, cerr := s.cmd(ctx, 20*time.Second, "/bin/ps", ucommArgs...)
		if cerr != nil || rc != 0 {
			err = errors.New(cmdNote("ps", rc, cerr, stderr))
		} else {
			names := parseUcomm(out)
			for _, ps := range [][]proc{first, second} {
				for i := range ps {
					ps[i].UName = names[ps[i].PID]
				}
			}
		}
	}
	if err != nil {
		s.snapErr = err
		return nil, err
	}
	s.snap = &psSnapshot{First: first, Second: second, Interval: s.o.Interval}
	return s.snap, nil
}

func fmtMB(kb int64) string {
	switch {
	case kb >= 1024*1024:
		return fmt.Sprintf("%.1fGB", float64(kb)/1024/1024)
	default:
		return fmt.Sprintf("%dMB", kb/1024)
	}
}

func fmtDur(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%d日", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%d時間", int(d.Hours()))
	default:
		return fmt.Sprintf("%d分", int(d.Minutes()))
	}
}
