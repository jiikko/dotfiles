package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jiikko/dotfiles/src/tuikit/tickchain"

	"treefiler/filer"

	"testwait"
)

// q で終わるとき Close を呼ぶ (Remember place が保存される)。
func TestQuitSavesPlace(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("TREEFILER_CONFIG_DIR", cfg)
	if err := os.WriteFile(filepath.Join(cfg, "treefiler.toml"), []byte("remember = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := filer.New(t.TempDir(), filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if _, err := os.Stat(filepath.Join(cfg, "treefiler-places")); err != nil {
		t.Fatalf("q で終えても前回の場所が保存されない: %v", err)
	}
}

type exitStatusError struct{}

func (exitStatusError) Error() string { return "exit status 1" }
func (exitStatusError) ExitCode() int { return 1 }

// シェルを起動できなかったときは知らせ、終了コードでは知らせない。
func TestExecStartFailureIsShown(t *testing.T) {
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	f, err := filer.New(t.TempDir(), filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(execDoneMsg{err: exitStatusError{}})
	if a.toast.Visible() {
		t.Fatalf("終了コードを失敗と出した: %q", a.toast.Text())
	}
	a.Update(execDoneMsg{err: errors.New("chdir: no such file or directory")})
	if !a.toast.Visible() || !strings.Contains(a.toast.Text(), "起動できません") {
		t.Fatalf("起動の失敗が出ない: visible=%v %q", a.toast.Visible(), a.toast.Text())
	}
}

// 裏で終わった処理の知らせ (explode の完了) は、次のキーを待たずに tick で toast に出る。
func TestBackgroundNoticeShowsOnTick(t *testing.T) {
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := filer.New(root, filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(tea.KeyPressMsg{Code: '>', Text: ">"}) // カーソルは sub (フォルダ) → explode
	testwait.Until(t, "explode の完了の知らせが tick で出ない", func() bool { return strings.Contains(a.toast.Text(), "開いた") },
		testwait.Within(5*time.Second), testwait.Step(func() { a.Update(tickMsg{}) }), testwait.Tick(time.Millisecond),
		testwait.Diag(func() string { return fmt.Sprintf("toast=%q", a.toast.Text()) }))
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func newToggleApp(t *testing.T, toggleKey, prefix string) *app {
	t.Helper()
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	f, err := filer.New(t.TempDir(), filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f, toggleKey: toggleKey}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(prefixMsg{prefix})
	return a
}

var (
	keyPrefix = tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	keyF      = tea.KeyPressMsg{Code: 'f', Text: "f"}
	keyQ      = tea.KeyPressMsg{Code: '?', Text: "?"}
)

// tmux の popup で開いたとき、開いた prefix+f で閉じる (popup の中のキーは tmux の bind を通らない)。
func TestPrefixToggleKeyQuits(t *testing.T) {
	a := newToggleApp(t, "f", "ctrl+t")
	if _, cmd := a.Update(keyPrefix); isQuit(cmd) {
		t.Fatal("prefix だけで閉じた")
	}
	if _, cmd := a.Update(keyF); !isQuit(cmd) {
		t.Fatal("prefix → f で閉じない")
	}
	// prefix を続けて押してから f でも閉じる (押し直した prefix で待ちが解けない)
	b := newToggleApp(t, "f", "ctrl+t")
	b.Update(keyPrefix)
	b.Update(keyPrefix)
	if _, cmd := b.Update(keyF); !isQuit(cmd) {
		t.Fatal("prefix → prefix → f で閉じない")
	}
}

// prefix の後の別のキーは普通の操作として効き、その後の f は閉じない。
func TestPrefixThenOtherKeyPassesThrough(t *testing.T) {
	a := newToggleApp(t, "f", "ctrl+t")
	a.Update(keyPrefix)
	a.Update(keyQ) // ? はキーの一覧を開く
	if !a.f.OwnsKeys() {
		t.Fatal("prefix の後の ? が効かない (キーの一覧が開かない)")
	}
	a.Update(keyQ) // 一覧を閉じる
	if _, cmd := a.Update(keyF); isQuit(cmd) {
		t.Fatal("prefix の待ちが解けず、後の f で閉じた")
	}
}

// 閉じるキーを渡されていない (ペインで直接起動・tmux の外) なら prefix → f で閉じない。
func TestNoToggleKeyDoesNotQuit(t *testing.T) {
	for _, c := range []struct{ name, toggle, prefix string }{
		{"閉じるキーなし", "", "ctrl+t"},
		{"prefix が取れない", "f", ""},
	} {
		a := newToggleApp(t, c.toggle, c.prefix)
		a.Update(keyPrefix)
		if _, cmd := a.Update(keyF); isQuit(cmd) {
			t.Fatalf("%s: prefix → f で閉じた", c.name)
		}
	}
}

// 裏の処理を待つ遅い tick が張ってある間に打鍵で動きが始まったら、動きの次のフレームを遅い tick まで待たせない
// (打鍵の直後に最大 busyInterval 止まって見える)。届いた tick を受けて次を張り、張った Cmd は張ったときの seq を運ぶ。
// 時計は差し替える (実時間で測ると、Update が遅い日に張り直しの判定が偶然通る)。
func TestKeyDuringSlowTickArmsFrameTick(t *testing.T) {
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(root, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := filer.New(root, filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	a := &app{f: f, clock: func() time.Time { return clock }}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 0; f.Animating(); i++ { // 開いたときの動きを終わらせる
		if i > 1000 {
			t.Fatal("開いたときの動きが 16 秒分進めても終わらない")
		}
		clock = clock.Add(frameInterval)
		a.Update(tickMsg{})
	}

	a.ticks = tickchain.Chain{}
	slowSeq, _ := a.ticks.Want(clock, busyInterval) // 裏の処理を待つ遅い tick が張ってある
	clock = clock.Add(10 * time.Millisecond)
	_, cmd := a.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if !f.Animating() {
		t.Fatal("j で動きが始まらない (前提が崩れた)")
	}
	due, seq, ok := a.ticks.Pending()
	if want := clock.Add(frameInterval); !ok || !due.Equal(want) {
		t.Fatalf("動きが始まったのに、次の tick は %v 後 (frameInterval %v 後に張り直していない)", due.Sub(clock), frameInterval)
	}
	if got, ok := tickIn(t, cmd); !ok || got.seq != seq {
		t.Fatalf("返した Cmd が張った tick を運ばない: %+v ok=%v (張った seq %d)", got, ok, seq)
	}

	clock = due
	a.Update(tickMsg{seq}) // 張り直した tick が届く
	next, _, ok := a.ticks.Pending()
	if !ok || !next.After(due) {
		t.Fatalf("届いた tick を受けて次の tick を張らない (動きが止まる): pending=%v next=%v", ok, next)
	}
	_, live, _ := a.ticks.Pending()
	clock = clock.Add(busyInterval)
	a.Update(tickMsg{slowSeq}) // 追い越された遅い tick が後から届いても、張ってある tick を外さない
	// seq で見る: 古い tick で外すと、同じ Update の末尾で張り直されて「張ってある」ことは変わらない
	if _, now, ok := a.ticks.Pending(); !ok || now != live {
		t.Fatalf("追い越された古い tick で、張ってある tick (seq %d) を外した (今は seq %d ok=%v)", live, now, ok)
	}
}

// tickIn は Update が返した Cmd (tea.Batch) の中の tick を取り出す。tick の Cmd は張った周期だけ待ってから返す。
func tickIn(t *testing.T, cmd tea.Cmd) (tickMsg, bool) {
	t.Helper()
	if cmd == nil {
		return tickMsg{}, false
	}
	msgs := []tea.Msg{cmd()}
	for len(msgs) > 0 {
		m := msgs[0]
		msgs = msgs[1:]
		switch m := m.(type) {
		case tickMsg:
			return m, true
		case tea.BatchMsg:
			for _, c := range m {
				if c != nil {
					msgs = append(msgs, c())
				}
			}
		}
	}
	return tickMsg{}, false
}
