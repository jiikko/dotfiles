package main

// dispatcher のライブアップグレード (issue 505)。dispatcher は長く走り続けるので、master に入った変更は手で起動し直すまで効かなかった
// (見張りの係 475・起動時の復旧 483 の実例)。画面の ctrl+r (package upgrade) と同じ口で、新版のバイナリができたら安全な区切りで
// 自分を syscall.Exec で入れ替える。PID はそのまま、dispatcher の lock も外さずに引き継ぐ (dispatcher.HeldLock.Exec)。
//
// 🚨 ビルドするかの判定とビルドそのものは shim (bin/lib/go_autobuild.zsh) に任せる (upgrade.Source.Spawn。Go 側に写経しない)。
// ここがするのは「shim に尋ねる」「新版が今の記録を読めるか確かめる」「区切りを待つ」「切り替える」だけ。
//
// 失敗モード:
//   - 新版が起動しない・壊れている / 記録の形を変えて今の記録を読めない → exec の前に新版に --preflight を走らせて確かめ、
//     通らなければ旧版のまま続けて出来事にする (その新版はもう試さない。次のビルドで差し替わったら試す)
//   - exec が戻ってきた (失敗) → lock と見張りを元へ戻して旧版のまま続ける
//   - 入れ替えの隙に画面の keeper が 2 つ目を起こす → lock の fd を exec に持ち越すので、2 つ目は ErrRunning で抜ける
//   - 区切りが来ない (テストの係が長い等) → 待ち続ける。upgradeWaitWarn を過ぎたら出来事にする
//   - 確かめた後にバイナリがまた差し替わった → 切り替える直前に見直し、違えば確かめ直す
//     (見直しから exec までの間に差し替わるのは防げない。そのときは確かめていない新版が走る)

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"

	"pro-con/dispatcher"
	"pro-con/eventlog"
	"pro-con/upgrade"
)

const (
	upgradeCheckEvery = 30 * time.Second // shim に尋ねる間隔 (zsh を起こすので Tick ごとにはしない)
	upgradeWaitWarn   = 30 * time.Minute // 区切りをこれより長く待ったら出来事にする
	upgradeNoteFor    = 10 * time.Minute // 切り替えたことをゲージに出す長さ
	preflightTimeout  = 30 * time.Second
	// upgradedFromEnv は入れ替えの前の版の名前を新しいプロセス像へ渡す (出来事とゲージの「旧版 → 新版」。入れ替えで起きた印も兼ねる)
	upgradedFromEnv = "PRO_CON_DISPATCHER_UPGRADED_FROM"
	preflightFlag   = "preflight"
	preflightOK     = "ok"
)

// dispUpgrade は dispatcher の入れ替えの様子。serve の goroutine だけが触る (step と note は Tick と同じ goroutine から呼ぶ)。
type dispUpgrade struct {
	src   upgrade.Source
	start os.FileInfo // 動いている版のバイナリ (これと違うファイルになったら新版)
	from  string      // 動いている版の名前 (binaryLabel)
	run   upgrade.Runner
	// preflight は新版のバイナリに今の記録を読ませ、新版の名前を返す。self は旧版が同じ確かめを自分で行う (旧版でも読めないなら新版のせいにしない)
	preflight func(ctx context.Context, exe string) (string, error)
	self      func() error
	busy      func() (string, error) // 今入れ替えてはいけない理由 (dispatcher.Busy)
	switchTo  func() error           // 入れ替える。成功すると戻らない
	say       func(kind, text string)
	now       func() time.Time
	every     time.Duration
	waitWarn  time.Duration

	checkedAt time.Time
	lastSpawn time.Time // 最後に shim が裏ビルドを起動した時刻。失敗の記録はこれより後のものだけを見る
	buildFail bool      // ビルドの失敗を出来事にした (次のビルドを頼むまで重ねない)
	askErr    string    // shim に尋ねられなかった理由 (同じ理由は重ねない)
	ready     os.FileInfo
	to        string // ready の版の名前
	readyAt   time.Time
	waitWhy   string
	warned    bool
	rejected  os.FileInfo // 切り替えられなかった新版 (これが差し替わるまで試さない)
	// switched はこのプロセス像が入れ替えで起きたときの「旧版 → 新版」と時刻 (ゲージに upgradeNoteFor だけ出す)
	switched   string
	switchedAt time.Time
}

// newDispUpgrade は動いている dispatcher d の入れ替えを組む。args は dispatcher の引数 (新版も同じ引数で起こす)、start は動いている版の
// バイナリ、lock は持っている dispatcher の lock (入れ替えで引き継ぐ)。pause は入れ替えを包む (子の見張りを止め、失敗したら起こし直す)。
// ソースのディレクトリから起動していなければ (upgrade.Detect) 無効 = エラー。
func newDispUpgrade(d *dispatcher.Dispatcher, dir string, args []string, start os.FileInfo, startErr error, lock *dispatcher.HeldLock, pause func(func() error) error) (*dispUpgrade, error) {
	if startErr != nil {
		return nil, startErr
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	src, err := upgrade.Detect(exe)
	if err != nil {
		return nil, err
	}
	from := binaryLabel(start)
	u := &dispUpgrade{src: src, start: start, from: from, run: upgrade.ExecRunner,
		preflight: func(ctx context.Context, exe string) (string, error) { return runPreflight(ctx, exe, args) },
		self:      func() error { return dispatcher.Preflight(dir) },
		busy:      d.Busy,
		say:       func(kind, text string) { say(d, kind, text) },
		now:       time.Now, every: upgradeCheckEvery, waitWarn: upgradeWaitWarn,
		lastSpawn: start.ModTime(), // 起動したバイナリより新しい失敗の記録 = 動いている版より新しいソースがビルドに落ちている
	}
	u.switchTo = func() error {
		return pause(func() error {
			argv := append([]string{src.Exe, "dispatcher"}, args...)
			return lock.Exec(src.Exe, argv, upgrade.Env(os.Environ(), upgradedFromEnv+"="+from), execFn)
		})
	}
	return u, nil
}

// selfBinary は動いているバイナリのファイルの情報。
func selfBinary() (os.FileInfo, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return os.Stat(exe)
}

// step は Tick の後に呼ぶ。新版を探し、あれば区切りで切り替える (成功すると戻らない)。
func (u *dispUpgrade) step(ctx context.Context) {
	now := u.now()
	if u.ready == nil {
		u.look(ctx, now)
	}
	if u.ready != nil {
		u.trySwitch(now)
	}
}

// look はバイナリが差し替わったかを見て (毎回。stat だけ)、差し替わっていなければ every ごとに shim に尋ねる (要れば shim が裏でビルドする)。
func (u *dispUpgrade) look(ctx context.Context, now time.Time) {
	cur, err := os.Stat(u.src.Exe)
	if err != nil {
		u.sayAskErr("バイナリを見られない: " + err.Error())
		return
	}
	if sameBinary(u.start, cur) || (u.rejected != nil && sameBinary(u.rejected, cur)) {
		if !u.checkedAt.IsZero() && now.Sub(u.checkedAt) < u.every {
			return
		}
		u.checkedAt = now
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		spawned, err := u.src.Spawn(sctx, u.run)
		switch {
		case err != nil:
			u.sayAskErr(err.Error())
		case spawned:
			u.askErr, u.lastSpawn, u.buildFail = "", now, false
		default:
			u.askErr = ""
			if !u.buildFail && u.src.FailedSince(u.lastSpawn) {
				u.buildFail = true
				u.say(eventlog.KindError, "dispatcher の新版のビルドに失敗した (旧版のまま続ける): "+u.src.LogPath())
			}
		}
		return
	}
	u.buildFail = false
	pctx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()
	to, err := u.preflight(pctx, u.src.Exe)
	if err != nil {
		u.rejected = cur
		why := "新版は今の記録を読めない・起動しない"
		if serr := u.self(); serr != nil {
			why = "旧版でも記録を読めない (" + serr.Error() + ")"
		}
		u.say(eventlog.KindError, fmt.Sprintf("dispatcher の新版へ切り替えない: %s: %v。旧版 (%s) のまま続ける (次のビルドでまた試す)", why, err, u.from))
		return
	}
	u.ready, u.to, u.readyAt, u.waitWhy, u.warned = cur, to, now, "", false
	u.say(eventlog.KindUpgrade, fmt.Sprintf("dispatcher の新版ができた (%s → %s)。区切りで切り替える", u.from, to))
}

// trySwitch は区切りなら切り替える。区切りでなければ待つ (待ちすぎたら出来事にする)。
func (u *dispUpgrade) trySwitch(now time.Time) {
	if cur, err := os.Stat(u.src.Exe); err != nil || !sameBinary(u.ready, cur) { // 確かめた後にまた差し替わった: 次の step で確かめ直す
		u.ready = nil
		return
	}
	why, err := u.busy()
	if err != nil {
		why = "記録を読めない: " + err.Error()
	}
	if why != "" {
		u.waitWhy = why
		if !u.warned && now.Sub(u.readyAt) >= u.waitWarn {
			u.warned = true
			u.say(eventlog.KindHold, fmt.Sprintf("dispatcher の新版 (%s) への切り替えを %s 待っている (%s)。区切りが来るまで旧版のまま続ける", u.to, u.waitWarn, why))
		}
		return
	}
	u.say(eventlog.KindUpgrade, fmt.Sprintf("dispatcher を新版へ切り替える (%s → %s)。PG・PM・取り込みの係は止めない", u.from, u.to))
	err = u.switchTo() // 戻ってきたら失敗
	u.rejected, u.ready = u.ready, nil
	u.say(eventlog.KindError, fmt.Sprintf("dispatcher を新版 (%s) へ切り替えられない (旧版のまま続ける。次のビルドでまた試す): %v", u.to, err))
}

func (u *dispUpgrade) sayAskErr(e string) {
	if u.askErr != e {
		u.askErr = e
		u.say(eventlog.KindError, "dispatcher の新版を確かめられない: "+e)
	}
}

// note はゲージに出す様子 (dispatcher.Dispatcher.UpgradeNote)。
func (u *dispUpgrade) note(now time.Time) (string, bool) {
	switch {
	case u.ready != nil && u.waitWhy != "":
		return "dispatcher 新版待ち (" + u.waitWhy + ")", u.warned
	case u.rejected != nil:
		return "dispatcher 新版に切り替えられない (pro-con log)", true
	case u.buildFail:
		return "dispatcher 新版のビルド失敗", true
	case u.switched != "" && now.Sub(u.switchedAt) <= upgradeNoteFor:
		return "dispatcher 新版 " + u.switched, false
	}
	return "", false
}

// sameBinary は 2 つが同じバイナリか (同一性 = inode と、更新時刻・大きさ。upgrade.Source.Replaced と同じ見方)。
func sameBinary(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size()
}

// binaryLabel は動いているバイナリの名前 (commit と、ビルドした時刻 = バイナリの更新時刻)。出来事とゲージの「旧版 → 新版」に使う。
// 🚨 起動した直後に取る (shim が差し替えた後にパスを見ると、新版の時刻を読む)。
func binaryLabel(start os.FileInfo) string {
	rev, dirty := "", false
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value[:min(7, len(s.Value))]
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
	}
	if dirty {
		rev += "+"
	}
	built := "?"
	if start != nil {
		built = start.ModTime().Format("01-02 15:04")
	}
	return strings.TrimSpace(rev + " " + built)
}

// runPreflight は新版のバイナリ exe を `dispatcher <args> --preflight` で起こし、記録を読めたら新版の名前を返す。
func runPreflight(ctx context.Context, exe string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, append(append([]string{"dispatcher"}, args...), "--"+preflightFlag)...)
	cmd.WaitDelay = time.Second
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	line := strings.TrimSpace(out.String())
	if err != nil || !strings.HasPrefix(line, preflightOK+" ") {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = line
		}
		return "", fmt.Errorf("%s --%s: %v (%s)", exe, preflightFlag, err, msg)
	}
	return strings.TrimPrefix(line, preflightOK+" "), nil
}

// takeUpgradedFrom は入れ替えの前の版の名前を取り、環境変数からは消す (子へ漏らさない)。入れ替えで起きたのでなければ ""。
func takeUpgradedFrom() string {
	v := os.Getenv(upgradedFromEnv)
	_ = os.Unsetenv(upgradedFromEnv)
	return v
}
