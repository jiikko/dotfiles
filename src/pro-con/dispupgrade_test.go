package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/upgrade"
)

// upgradeRig は dispUpgrade を偽の shim・確かめ・切り替えで組む。
type upgradeRig struct {
	t        *testing.T
	exe      string
	now      time.Time
	asks     int
	checks   []string // preflight を走らせたバイナリの中身
	checkErr error
	busyWhy  string
	switches int
	switchTo error
	said     []string
	u        *dispUpgrade
}

func newUpgradeRig(t *testing.T) *upgradeRig {
	t.Helper()
	r := &upgradeRig{t: t, exe: filepath.Join(t.TempDir(), "pro-con"), now: time.Date(2026, 9, 26, 9, 0, 0, 0, time.Local)}
	r.put("v1")
	start, err := os.Stat(r.exe)
	if err != nil {
		t.Fatal(err)
	}
	notNeeded := exec.Command("false").Run() // shim の rc=1 (ビルドは要らない)
	r.u = &dispUpgrade{src: upgrade.Source{Dir: filepath.Dir(r.exe), Exe: r.exe}, start: start, from: "v1",
		run: func(context.Context, string, ...string) error { r.asks++; return notNeeded },
		preflight: func(_ context.Context, exe string) (string, error) {
			b, _ := os.ReadFile(exe)
			r.checks = append(r.checks, string(b))
			return string(b), r.checkErr
		},
		self:      func() error { return nil },
		busy:      func() (string, error) { return r.busyWhy, nil },
		switchTo:  func(ctx context.Context) error { r.switches++; return cmpErr(ctx.Err(), r.switchTo) },
		say:       func(kind, text string) { r.said = append(r.said, kind+": "+text) },
		now:       func() time.Time { return r.now },
		every:     30 * time.Second,
		waitWarn:  30 * time.Minute,
		lastSpawn: start.ModTime()}
	return r
}

// put は shim の差し替えを真似る (別のファイルに書いて rename = inode が変わる)。
func (r *upgradeRig) put(content string) {
	r.t.Helper()
	tmp := r.exe + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Rename(tmp, r.exe); err != nil {
		r.t.Fatal(err)
	}
}

// step は時計を every だけ進めて 1 回回す。
func (r *upgradeRig) step() {
	r.now = r.now.Add(r.u.every)
	r.u.step(context.Background())
}

func (r *upgradeRig) saidWith(sub string) int {
	n := 0
	for _, s := range r.said {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

// 新版ができたら確かめ、区切りでなければ待ち (待ちすぎたら 1 度だけ出来事にする)、区切りが来たら 1 度だけ切り替える。
func TestDispUpgradeWaitsForSafePointThenSwitches(t *testing.T) {
	r := newUpgradeRig(t)
	r.step()
	if r.asks != 1 || r.switches != 0 || len(r.checks) != 0 {
		t.Fatalf("新版が無いのに asks=%d switches=%d checks=%v", r.asks, r.switches, r.checks)
	}
	r.put("v2")
	r.busyWhy = "テストの係が C-001 を実行中"
	r.step()
	if len(r.checks) != 1 || r.checks[0] != "v2" || r.switches != 0 {
		t.Fatalf("区切りでないのに切り替えた / 確かめていない: checks=%v switches=%d", r.checks, r.switches)
	}
	if n, _ := r.u.note(r.now); !strings.Contains(n, "新版待ち (テストの係が C-001 を実行中)") {
		t.Fatalf("ゲージに待っている理由が出ない: %q", n)
	}
	for range 70 { // 35 分 (区切りを待つ間は Tick ごとに見る)
		r.step()
	}
	if r.switches != 0 || len(r.checks) != 1 {
		t.Fatalf("待っている間に切り替えた / 確かめ直した: switches=%d checks=%v", r.switches, r.checks)
	}
	if got := r.saidWith("待っている"); got != 1 {
		t.Fatalf("待ちすぎの出来事が %d 回 (1 回だけ): %v", got, r.said)
	}
	if _, alert := r.u.note(r.now); !alert {
		t.Fatal("待ちすぎをゲージで知らせない")
	}
	r.busyWhy = ""
	r.step()
	if r.switches != 1 || r.saidWith("v1 → v2") < 2 {
		t.Fatalf("区切りで切り替えない: switches=%d said=%v", r.switches, r.said)
	}
}

// 切り替え (exec) が戻ってきたら旧版のまま続けて知らせ、同じ新版は試し直さない。次のビルドで差し替わったら試す。
func TestDispUpgradeSwitchFailureKeepsOldAndWaitsForNextBuild(t *testing.T) {
	r := newUpgradeRig(t)
	r.switchTo = errors.New("exec できない")
	r.put("v2")
	r.step()
	if r.switches != 1 || r.saidWith("切り替えられない") != 1 {
		t.Fatalf("switches=%d said=%v", r.switches, r.said)
	}
	for range 5 {
		r.step()
	}
	if r.switches != 1 || len(r.checks) != 1 {
		t.Fatalf("切り替えられなかった新版を試し直した: switches=%d checks=%v", r.switches, r.checks)
	}
	if r.asks != 5 { // 同じ新版の間も shim には尋ねる (ソースを直せば shim が次をビルドする)
		t.Fatalf("shim に尋ねた回数 %d (5)", r.asks)
	}
	if _, alert := r.u.note(r.now); !alert {
		t.Fatal("切り替えられないことをゲージで知らせない")
	}
	r.switchTo = nil
	r.put("v3")
	r.step()
	if r.switches != 2 || r.checks[len(r.checks)-1] != "v3" {
		t.Fatalf("次のビルドを試さない: switches=%d checks=%v", r.switches, r.checks)
	}
}

// 新版が今の記録を読めない・起動しない (--preflight が通らない) なら切り替えない。同じ新版は確かめ直さない。
func TestDispUpgradePreflightFailureKeepsOld(t *testing.T) {
	r := newUpgradeRig(t)
	r.checkErr = errors.New("カードの記録を読めない")
	r.put("v2")
	for range 3 {
		r.step()
	}
	if r.switches != 0 || len(r.checks) != 1 {
		t.Fatalf("確かめの通らない新版へ切り替えた / 確かめ直した: switches=%d checks=%v", r.switches, r.checks)
	}
	if r.saidWith("新版は今の記録を読めない・起動しない") != 1 {
		t.Fatalf("出来事にしない: %v", r.said)
	}
	r.u.self = func() error { return errors.New("壊れた記録") } // 旧版でも読めないなら、新版のせいにしない書き方
	r.checkErr = errors.New("読めない")
	r.put("v3")
	r.step()
	if r.switches != 0 || r.saidWith("旧版でも記録を読めない") != 1 {
		t.Fatalf("switches=%d said=%v", r.switches, r.said)
	}
}

// 確かめた後にバイナリがまた差し替わったら、確かめていない版へは切り替えず、確かめ直す。
func TestDispUpgradeRechecksWhenReplacedAgain(t *testing.T) {
	r := newUpgradeRig(t)
	r.busyWhy = "btw の答えを C-001 に作っている"
	r.put("v2")
	r.step()
	r.put("v3")
	r.busyWhy = ""
	r.step()
	if r.switches != 0 {
		t.Fatal("確かめていない版へ切り替えた")
	}
	r.step()
	if len(r.checks) != 2 || r.checks[1] != "v3" || r.switches != 1 {
		t.Fatalf("確かめ直してから切り替えない: checks=%v switches=%d", r.checks, r.switches)
	}
}

// shim に尋ねるのは every ごと (zsh を Tick ごとに起こさない)。ビルドの失敗は 1 度だけ知らせる。
func TestDispUpgradeAsksShimEveryInterval(t *testing.T) {
	r := newUpgradeRig(t)
	r.step()
	for range 5 {
		r.now = r.now.Add(3 * time.Second)
		r.u.step(context.Background())
	}
	if r.asks != 1 {
		t.Fatalf("間隔より短く shim に尋ねた: %d", r.asks)
	}
	failed := filepath.Join(r.u.src.Dir, ".autobuild.failed")
	if err := os.WriteFile(failed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(failed, future, future); err != nil {
		t.Fatal(err)
	}
	r.step()
	r.step()
	if r.saidWith("ビルドに失敗") != 1 {
		t.Fatalf("ビルドの失敗の出来事: %v", r.said)
	}
}

// 入れ替えで起きた dispatcher は、切り替えたこと (旧版 → 新版) をゲージに一定の時間だけ出す。
func TestDispUpgradeNoteAfterSwitch(t *testing.T) {
	r := newUpgradeRig(t)
	r.u.switched, r.u.switchedAt = "v0 → v1", r.now
	if n, alert := r.u.note(r.now.Add(time.Minute)); n != "dispatcher 新版 v0 → v1" || alert {
		t.Fatalf("切り替えた直後のゲージ: %q %v", n, alert)
	}
	if n, _ := r.u.note(r.now.Add(upgradeNoteFor + time.Second)); n != "" {
		t.Fatalf("時間が過ぎても出し続ける: %q", n)
	}
}

func cmpErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return errors.New("exec が戻ってきた")
}

// 止める信号 (ctx の取り消し) の後は、入れ替えずに止める側へ返す。確かめが切られても、入れ替えを取り消しても、失敗として出来事にしない。
func TestDispUpgradeCanceledIsNotAFailure(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	r := newUpgradeRig(t)
	r.checkErr = errors.New("signal: killed") // 確かめの子が ctx で殺された形
	r.put("v2")
	r.u.step(canceled)
	if r.u.rejected != nil || len(r.said) != 0 {
		t.Fatalf("止める途中に切られた確かめを新版の失敗にした: %v", r.said)
	}
	r.checkErr = nil
	r.busyWhy = "進捗を集めている"
	r.u.step(context.Background()) // 確かめ直して新版あり (区切り待ち)
	r.busyWhy = ""
	r.said = nil
	r.u.step(canceled) // 入れ替えの直前に止める信号
	if r.switches != 1 || r.u.rejected != nil || r.u.ready == nil || r.saidWith("切り替えられない") != 0 {
		t.Fatalf("取り消しで入れ替えなかったのを失敗にした: switches=%d rejected=%v said=%v", r.switches, r.u.rejected != nil, r.said)
	}
}
