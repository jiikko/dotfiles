package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyAutobuild(t *testing.T) {
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	later := base.Add(time.Minute)
	cases := []struct {
		name                                     string
		startBin, curBin, startFailed, curFailed time.Time
		want                                     autobuildResult
	}{
		{"変化なし = 未決着", base, base, time.Time{}, time.Time{}, autobuildRunning},
		{"バイナリが新しくなった = 導入済み", base, later, time.Time{}, time.Time{}, autobuildInstalled},
		{"失敗記録が現れた = 失敗", base, base, time.Time{}, later, autobuildFailed},
		{"失敗記録が更新された = 失敗", base, base, base, later, autobuildFailed},
		// 成功時 go_autobuild.zsh は失敗記録を消す (= zero へ後退) ので失敗と誤判定しない。
		{"失敗記録が消えてバイナリ更新 = 導入済み", base, later, base, time.Time{}, autobuildInstalled},
		// 古い失敗記録が残っているだけでは失敗と言わない (前回起動より前の失敗)。
		{"失敗記録が据え置き = 未決着", base, base, base, base, autobuildRunning},
	}
	for _, c := range cases {
		if got := classifyAutobuild(c.startBin, c.curBin, c.startFailed, c.curFailed); got != c.want {
			t.Errorf("%s: classifyAutobuild = %v, want %v", c.name, got, c.want)
		}
	}
}

// 監視しない条件 (env なし / exe パス不明) では tick を 1 本も増やさない。
func TestNewAutobuildWatchInactive(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		name    string
		exe     string
		pending bool
	}{
		{"env なし", "/some/bin/glogx", false},
		{"exe 不明", "", true},
		{"両方なし", "", false},
	} {
		w := newAutobuildWatch(c.exe, c.pending, now)
		if w.active {
			t.Errorf("%s: active=true (監視すべきでない)", c.name)
		}
		if w.tickCmd() != nil {
			t.Errorf("%s: tickCmd が非 nil (tick を増やしている)", c.name)
		}
	}
}

// newAutobuildWatch は監視開始時の mtime を捉え、tickCmd の観測でバイナリ差し替えを検出する。
// go_autobuild.zsh は一時ファイルへビルドして mv で置き換えるので、パスの mtime が進む。
func TestAutobuildWatchDetectsInstall(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "glogx")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(bin, old, old); err != nil {
		t.Fatal(err)
	}
	w := newAutobuildWatch(bin, true, time.Now())
	if !w.active {
		t.Fatal("active=false (監視すべき)")
	}
	// 差し替え前は未決着。
	if got := classifyAutobuild(w.binMtime, fileMtime(bin), w.failedMtime, fileMtime(w.failedPath)); got != autobuildRunning {
		t.Errorf("差し替え前 = %v, want autobuildRunning", got)
	}
	// mv 相当 (新しい mtime で置き換え) → 導入済みになる。
	if err := os.WriteFile(bin, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := classifyAutobuild(w.binMtime, fileMtime(bin), w.failedMtime, fileMtime(w.failedPath)); got != autobuildInstalled {
		t.Errorf("差し替え後 = %v, want autobuildInstalled", got)
	}
}

// 失敗記録は監視開始時のバイナリと同じディレクトリから探す (go_autobuild.zsh の置き場)。
func TestAutobuildWatchDetectsFailure(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "glogx")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := newAutobuildWatch(bin, true, time.Now())
	if w.failedPath != filepath.Join(dir, autobuildFailedStamp) {
		t.Fatalf("failedPath = %q, want %q", w.failedPath, filepath.Join(dir, autobuildFailedStamp))
	}
	if err := os.WriteFile(w.failedPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := classifyAutobuild(w.binMtime, fileMtime(bin), w.failedMtime, fileMtime(w.failedPath)); got != autobuildFailed {
		t.Errorf("失敗記録あり = %v, want autobuildFailed", got)
	}
}

func TestAutobuildWatchHandle(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	newWatch := func() autobuildWatch {
		return autobuildWatch{active: true, until: now.Add(autobuildWatchTimeout)}
	}

	t.Run("未決着なら監視継続・通知なし", func(t *testing.T) {
		w := newWatch()
		_, notify, keep := w.handle(autobuildRunning, false, now)
		if notify || !keep {
			t.Errorf("notify=%v keep=%v, want false/true", notify, keep)
		}
	})

	t.Run("決着かつトースト空きなら通知して監視終了", func(t *testing.T) {
		w := newWatch()
		res, notify, keep := w.handle(autobuildInstalled, false, now)
		if res != autobuildInstalled || !notify || keep {
			t.Errorf("res=%v notify=%v keep=%v, want installed/true/false", res, notify, keep)
		}
		if w.active {
			t.Error("通知後も active=true (tick が残る)")
		}
	})

	t.Run("トースト表示中は保持して次 tick で出し直す", func(t *testing.T) {
		w := newWatch()
		if _, notify, keep := w.handle(autobuildFailed, true, now); notify || !keep {
			t.Fatalf("塞がり中: notify=%v keep=%v, want false/true", notify, keep)
		}
		// 次の tick は未決着 (mtime はもう動かない) だが、保持した結果を出す。
		res, notify, keep := w.handle(autobuildRunning, false, now.Add(autobuildPollInterval))
		if res != autobuildFailed || !notify || keep {
			t.Errorf("空いた後: res=%v notify=%v keep=%v, want failed/true/false", res, notify, keep)
		}
	})

	t.Run("期限切れは無言で監視終了", func(t *testing.T) {
		w := newWatch()
		res, notify, keep := w.handle(autobuildRunning, false, now.Add(autobuildWatchTimeout))
		if notify || keep || res != autobuildRunning {
			t.Errorf("res=%v notify=%v keep=%v, want running/false/false", res, notify, keep)
		}
		if w.active {
			t.Error("期限切れ後も active=true (tick が永久に残る)")
		}
	})

	t.Run("塞がり続けても期限で打ち切る", func(t *testing.T) {
		w := newWatch()
		w.handle(autobuildInstalled, true, now)
		if _, notify, keep := w.handle(autobuildRunning, true, now.Add(autobuildWatchTimeout)); notify || keep {
			t.Errorf("notify=%v keep=%v, want false/false", notify, keep)
		}
	})

	t.Run("監視していない zero value は何もしない", func(t *testing.T) {
		var w autobuildWatch
		if _, notify, keep := w.handle(autobuildInstalled, false, now); notify || keep {
			t.Errorf("notify=%v keep=%v, want false/false", notify, keep)
		}
	})
}

func TestAutobuildToast(t *testing.T) {
	if text, ok := autobuildToast(autobuildInstalled); text == "" || !ok {
		t.Errorf("installed = %q/%v (成功色で文面が要る)", text, ok)
	}
	if text, ok := autobuildToast(autobuildFailed); text == "" || ok {
		t.Errorf("failed = %q/%v (失敗色で文面が要る)", text, ok)
	}
	if text, _ := autobuildToast(autobuildRunning); text != "" {
		t.Errorf("running = %q, want 空 (未決着は出さない)", text)
	}
}

// Update 経路の結合: autobuildMsg を受けるとトーストが出て、監視 tick が止まる。
func TestAutobuildMsgShowsToast(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.toast.phase = toastHidden // 起動時の警告トーストが出ていても塞がり扱いにしない
	m.autobuild = autobuildWatch{active: true, until: timeNow().Add(autobuildWatchTimeout)}

	m.Update(autobuildMsg{result: autobuildInstalled})
	if !m.toast.visible() {
		t.Fatal("導入通知でトーストが出ていない")
	}
	if !strings.Contains(m.toast.text, "次回起動") {
		t.Errorf("文面に「次回起動で反映」が無い: %q", m.toast.text)
	}
	if !m.toast.ok {
		t.Error("導入通知が失敗色になっている")
	}
	if m.autobuild.active {
		t.Error("通知後も監視が続いている (tick が残る)")
	}
}

func TestAutobuildMsgShowsFailureToast(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.toast.phase = toastHidden
	m.autobuild = autobuildWatch{active: true, until: timeNow().Add(autobuildWatchTimeout)}

	m.Update(autobuildMsg{result: autobuildFailed})
	if !m.toast.visible() || m.toast.ok {
		t.Errorf("失敗通知が失敗色のトーストになっていない: visible=%v ok=%v", m.toast.visible(), m.toast.ok)
	}
	if !strings.Contains(m.toast.text, ".autobuild.log") {
		t.Errorf("失敗の調べ方 (ログの場所) が文面に無い: %q", m.toast.text)
	}
}

// env が無い通常起動では監視しない = tick を 1 本も増やさない (恒久 wakeup を足さない)。
func TestAutobuildNotWatchedWithoutEnv(t *testing.T) {
	t.Setenv(autobuildPendingEnv, "")
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	if m.autobuild.active {
		t.Error("env 無しで監視が有効になっている")
	}
	if m.autobuild.tickCmd() != nil {
		t.Error("env 無しで autobuild の tick が張られている")
	}
}
