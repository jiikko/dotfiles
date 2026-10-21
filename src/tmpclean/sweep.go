package tmpclean

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"safefs"
)

// sweep は $TMPDIR の直下に Swift のツールチェーン (swift-driver の ArgsResolver) が残す空の TemporaryDirectory.* を、
// 承認なしで消す。apply と違ってゴミ箱を経由しない。消すのは「空」か「空の .keep-directory だけ」のフォルダだけで、
// 最後の rmdir は kernel が空でなければ断る (safefs.RemoveEmptyDir)。失うのは空のフォルダと長さ 0 のファイルだけ。
//
// 守り (どれか 1 つでも満たさなければ残す):
//   - 名前が TemporaryDirectory.XXXXXX (英数字 6 字)・実体のフォルダ (symlink でない)
//   - 開かれていない (lsof の全体の写し)
//   - 自身と中の全部が N 日より前 (staleReason。socket / FIFO も残す)
//   - 動いている Swift のツール (sourcekit-lsp・Xcode・xcodebuild・swift-build など) のうち一番古いものの起動より前に作られた。
//     ArgsResolver は自分が作ったフォルダへ後から応答ファイルを書くことがあるので、作った可能性のあるプロセスが全部
//     居なくなってから消す。プロセスは作る前に起動しているので、一番古いものの起動より前に生まれたフォルダはどれのものでもない。
//     作成時刻 (Btim) は、mtime を作成時刻より前に書き換えると macOS がそこまで引き戻す (2026-10-21 に touch -t で実測)。
//     ArgsResolver のフォルダの mtime を過去へ書き換えるものは無いので、この判定には効かない
const keepFile = ".keep-directory"

var (
	swiftTempDirName = regexp.MustCompile(`^TemporaryDirectory\.[A-Za-z0-9]{6}$`)
	// 拾いすぎは守りすぎ (消すのが遅れる) になるだけなので広めに取る。ArgsResolver を自分の中で使うのは
	// swift-driver・SwiftPM・sourcekit-lsp・Xcode (IDE と swift-build のサービス)
	swiftToolCommand = regexp.MustCompile(`/Xcode[^/]*\.app/|/Toolchains/|/CommandLineTools/|(^|/)(swift[A-Za-z0-9_-]*|sourcekit-lsp|xcodebuild|SourceKitService|SWBBuildService|XCBBuildService)( |$)`)
)

type swiftProc struct {
	pid     int
	started time.Time
	command string
}

// collectSwiftProcs は動いている Swift のツールと、その起動時刻 (今 - etime) を返す。ps が読めなければ止める
// (読めないのを「居ない」と読むと、生きているものの作業場所を消す)。
func collectSwiftProcs(ctx context.Context, d deps, now time.Time) ([]swiftProc, error) {
	out, _, err := runOK(ctx, d, []int{0}, "ps", "-Ao", "pid=,etime=,command=")
	if err != nil {
		return nil, err
	}
	var ps []swiftProc
	lines := 0
	for _, line := range strings.Split(out, "\n") {
		pid, rest, ok := pidRest(line)
		if !ok || rest == "" {
			continue
		}
		lines++
		et, cmd, _ := strings.Cut(rest, " ")
		el, ok := parseEtime(et)
		if !ok {
			return nil, stopf("ps の etime を読めない: %q", line)
		}
		cmd = strings.TrimLeft(cmd, " ")
		if swiftToolCommand.MatchString(cmd) {
			ps = append(ps, swiftProc{pid: pid, started: now.Add(-el), command: cmd})
		}
	}
	if lines == 0 {
		return nil, stopf("ps からプロセスを 1 つも読めない")
	}
	return ps, nil
}

// parseEtime は ps の etime ([[dd-]hh:]mm:ss) を時間にする。
func parseEtime(s string) (time.Duration, bool) {
	days := 0
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil || n < 0 {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var secs int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" || strings.ContainsAny(p, "+-") {
			return 0, false
		}
		secs = secs*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(secs)*time.Second, true
}

type sweepResult struct {
	removed []string
	kept    []kept
	failed  []kept
	limit   *swiftProc // 消してよい範囲を決めた一番古い Swift のツール (nil = 居ない)
}

// sweep は候補を 1 件ずつ判定してすぐ消す (全部を判定してから消すと、判定の後の書き込みを消す窓が件数ぶん広がる)。
func sweep(ctx context.Context, d deps, days int) (sweepResult, error) {
	var res sweepResult
	if days < 1 {
		return res, stopf("日数は 1 以上 (%d)", days)
	}
	r, err := resolveRoots(d, true, true)
	if err != nil {
		return res, err
	}
	open, err := collectOpen(ctx, d)
	if err != nil {
		return res, err
	}
	now := d.now()
	procs, err := collectSwiftProcs(ctx, d, now)
	if err != nil {
		return res, err
	}
	// etime は秒の切り捨てなので、起動時刻は 1 秒ぶん早めに見積もる (遅く見積もると、起動の直後に作ったものを消しうる)
	var born time.Time
	for i := range procs {
		if res.limit == nil || procs[i].started.Before(res.limit.started) {
			res.limit = &procs[i]
		}
	}
	if res.limit != nil {
		born = res.limit.started.Add(-time.Second)
	}
	cut := now.Add(-time.Duration(days) * 24 * time.Hour)
	guard := sweepGuard(r.tmpdir, d.inner)
	keep := func(p, why string) { res.kept = append(res.kept, kept{path: p, reason: why}) }
	// readDirSorted は読めないと空を返す。sweep では「読めない」を「消すものが無い」(rc=0) に畳まない
	names, err := os.ReadDir(r.tmpdir)
	if err != nil {
		return res, stopf("TMPDIR の一覧を読めない: %v", err)
	}
	for _, de := range names {
		name := de.Name()
		if err := ctx.Err(); err != nil {
			return res, stopf("止められた: %v", err)
		}
		if !swiftTempDirName.MatchString(name) {
			continue
		}
		p := filepath.Join(r.tmpdir, name)
		var st unix.Stat_t
		if err := unix.Lstat(p, &st); err != nil {
			keep(p, "読めない")
			continue
		}
		switch {
		case st.Mode&unix.S_IFMT != unix.S_IFDIR:
			keep(p, "フォルダでない")
		case open.has(p):
			keep(p, "開かれている")
		case st.Flags&unmovableMask != 0:
			keep(p, "消せない属性 (immutable / SIP)") // rmdir が EPERM で断るのを、毎回「消せなかった」(rc=1) と数えない
		case st.Btim.Sec <= 0:
			keep(p, "作成時刻を読めない") // 0 を「1970 年 = どの起動よりも前」と読まない
		case res.limit != nil && !time.Unix(st.Btim.Unix()).Before(born):
			keep(p, "動いている Swift のツールの起動より後に作られた")
		default:
			if why := staleReason(p, cut, true); why != "" {
				keep(p, why)
				continue
			}
			id := safefs.ID{Dev: uint64(st.Dev), Ino: st.Ino}
			err := safefs.RemoveEmptyDir(r.tmpdir, name, id, keepFile, guard)
			switch {
			case err == nil:
				res.removed = append(res.removed, p)
			case errors.Is(err, safefs.ErrNotEmpty):
				keep(p, "中身がある")
			case errors.Is(err, safefs.ErrSwapped):
				keep(p, "判定の後に差し替わった")
			case errors.Is(err, os.ErrNotExist), errors.Is(err, unix.ENOENT):
				keep(p, "判定の後に消えた")
			default:
				res.failed = append(res.failed, kept{path: p, reason: err.Error()})
			}
		}
	}
	return res, nil
}

// sweepGuard は sweep の検査点。消してよいのは tmpdir の直下の TemporaryDirectory.* (rmdir) と、その直下の .keep-directory (unlink)
// だけ。候補の選定が壊れても、他の名前・深さを消せないようにする 2 段目。その上で inner を呼ぶ。
func sweepGuard(tmpdir string, inner safefs.Guard) safefs.Guard {
	return func(op, path string) error {
		path = filepath.Clean(path)
		ok := false
		switch op {
		case "rmdir":
			ok = underAtDepth(tmpdir, path, 1) && swiftTempDirName.MatchString(filepath.Base(path))
		case "unlink":
			ok = underAtDepth(tmpdir, path, 2) && filepath.Base(path) == keepFile &&
				swiftTempDirName.MatchString(filepath.Base(filepath.Dir(path)))
		}
		if !ok {
			return fmt.Errorf("tmpclean sweep: 消してよい形でない (%s): %s", op, path)
		}
		if inner == nil {
			return fmt.Errorf("tmpclean sweep: 検査点が無い: %s %s", op, path)
		}
		return inner(op, path)
	}
}
