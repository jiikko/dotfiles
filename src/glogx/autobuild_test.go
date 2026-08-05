package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
		_, notify, keep := w.handle(autobuildRunning, now)
		if notify || !keep {
			t.Errorf("notify=%v keep=%v, want false/true", notify, keep)
		}
	})

	// 完成は伝えて監視終了 (ユーザー要望 2026-08-01)。⚠️ 以前は無言だった — 開始時に
	// 「次回起動で反映」と伝えているため二度言うことになるから。その場で再起動できるように
	// なったので意味が変わった: 完成は「行動できる」合図で、呼び出し側が再起動ダイアログを出す。
	t.Run("完成は通知して監視終了", func(t *testing.T) {
		w := newWatch()
		res, notify, keep := w.handle(autobuildInstalled, now)
		if res != autobuildInstalled || !notify || keep {
			t.Errorf("res=%v notify=%v keep=%v, want installed/true/false", res, notify, keep)
		}
		if w.active {
			t.Error("完成後も active=true (tick が残る)")
		}
	})

	t.Run("失敗は通知して監視終了", func(t *testing.T) {
		w := newWatch()
		res, notify, keep := w.handle(autobuildFailed, now)
		if res != autobuildFailed || !notify || keep {
			t.Errorf("res=%v notify=%v keep=%v, want failed/true/false", res, notify, keep)
		}
	})

	t.Run("開始は通知した後も監視を続ける (失敗を拾うため)", func(t *testing.T) {
		w := newWatch()
		w.pending = autobuildStarted
		res, notify, keep := w.handle(autobuildRunning, now)
		if res != autobuildStarted || !notify || !keep {
			t.Errorf("res=%v notify=%v keep=%v, want started/true/true", res, notify, keep)
		}
		if !w.active {
			t.Error("開始を伝えただけで監視を止めた (失敗を拾えない)")
		}
		// 続けて失敗したら、そちらも出す
		res, notify, keep = w.handle(autobuildFailed, now)
		if res != autobuildFailed || !notify || keep {
			t.Errorf("開始通知後の失敗: res=%v notify=%v keep=%v", res, notify, keep)
		}
	})

	t.Run("開始を出す前に失敗したら失敗を優先する", func(t *testing.T) {
		// 「ビルド中」を出せていないうちに落ちたら、出すべきは失敗の方
		w := newWatch()
		w.pending = autobuildStarted
		res, notify, _ := w.handle(autobuildFailed, now)
		if res != autobuildFailed || !notify {
			t.Errorf("res=%v notify=%v, want failed/true", res, notify)
		}
	})

	t.Run("期限切れは無言で監視終了", func(t *testing.T) {
		w := newWatch()
		res, notify, keep := w.handle(autobuildRunning, now.Add(autobuildWatchTimeout))
		if notify || keep || res != autobuildRunning {
			t.Errorf("res=%v notify=%v keep=%v, want running/false/false", res, notify, keep)
		}
		if w.active {
			t.Error("期限切れ後も active=true (tick が永久に残る)")
		}
	})

	t.Run("決着しないまま期限が来たら打ち切る", func(t *testing.T) {
		// ビルダーがシグナルで死ぬと新バイナリも失敗記録も現れない = 決着しない。
		// tick が永久に残らないよう期限で諦める。
		w := newWatch()
		w.pending = autobuildRunning
		if _, notify, keep := w.handle(autobuildRunning, now.Add(autobuildWatchTimeout)); notify || keep {
			t.Errorf("notify=%v keep=%v, want false/false", notify, keep)
		}
		if w.active {
			t.Error("期限後も active=true")
		}
	})

	t.Run("監視していない zero value は何もしない", func(t *testing.T) {
		var w autobuildWatch
		if _, notify, keep := w.handle(autobuildInstalled, now); notify || keep {
			t.Errorf("notify=%v keep=%v, want false/false", notify, keep)
		}
	})
}

func TestAutobuildToast(t *testing.T) {
	if text, ok := autobuildToast(autobuildStarted); text == "" || !ok {
		t.Errorf("started = %q/%v (成功色で文面が要る)", text, ok)
	}
	if text, _ := autobuildToast(autobuildInstalled); text != "" {
		t.Errorf("installed = %q, want 空 (成功は無言)", text)
	}
	if text, ok := autobuildToast(autobuildFailed); text == "" || ok {
		t.Errorf("failed = %q/%v (失敗色で文面が要る)", text, ok)
	}
	if text, _ := autobuildToast(autobuildRunning); text != "" {
		t.Errorf("running = %q, want 空 (未決着は出さない)", text)
	}
}

// Update 経路の結合: ビルド完了は消えるトーストでなく再起動ダイアログで出す
// (ユーザー要望 2026-08-01)。⚠️ トーストだと目を離している間に行動の機会だけが消える。
func TestAutobuildMsgInstalledOpensRestartPrompt(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.toast.phase = toastHidden
	m.autobuild = autobuildWatch{active: true, until: timeNow().Add(autobuildWatchTimeout)}

	m.Update(autobuildMsg{result: autobuildInstalled})
	if !m.restartPending {
		t.Error("完成しても再起動ダイアログが出ない")
	}
	if m.toast.visible() {
		t.Errorf("ダイアログとトーストが二重に出た: %q", m.toast.text)
	}
	if m.autobuild.active {
		t.Error("完了後も監視が続いている (tick が残る)")
	}
	if out := stripANSI(m.View().Content); !strings.Contains(out, "新しいバージョンが利用可能です") {
		t.Fatalf("ダイアログが画面に出ていない:\n%s", out)
	}
}

// 中断できない処理 (claude update / push / pull) の最中は再起動ダイアログを出さない。
//
// ⚠️ 実際に起きた不具合の回帰防止: 以前は「キーは actModal が先に飲む」「描画は再起動ダイアログを
// 後に重ねる」と順序を 2 箇所へ別々に書いていたため、update 中に裏ビルドが完成すると
// 「最前面のダイアログにどのキーも届かない」= 操作不能になった (実測: r/j/q/esc/enter/ctrl+g の
// すべてが無反応)。しかもダイアログが更新中モーダルを覆うので、効かない理由も画面から消えていた。
//
// 出さないのは届かないからだけではない: このダイアログの r は cancelAll で走行中の
// claude update / git を殺す = Ctrl-C をブロックしてまで防いでいる当のものなので、
// 押させてはいけない選択肢を提示しないのが正しい。完成の事実は restartPending が保持する。
func TestRestartPromptDefersWhileBlockingOperation(t *testing.T) {
	// running() の各状態で同じ規律にする (どれも走行中の subprocess を殺す点で同じ)
	for _, tc := range []struct {
		name  string
		apply func(*browseModel)
	}{
		{"claude update 中", func(m *browseModel) { m.actModal.updating = true }},
		{"push 中", func(m *browseModel) { m.actModal.pushing = true }},
		{"pull 中", func(m *browseModel) { m.actModal.pulling = true }},
		// ⚠️ 確認待ち (y/N) も同じ。むしろこちらが危険だった: キーは確認モーダルが持つのに
		// 最前面が再起動ダイアログになり、「その他のキー: 後で」に従って押した y が push を
		// 実行した (実測)。無反応より悪い = 画面の指示どおりに押すと破壊的操作が走る。
		{"push 確認中", func(m *browseModel) { m.actModal.pushConfirm = true }},
		{"pull 確認中", func(m *browseModel) { m.actModal.pullConfirm = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			tc.apply(m)
			m.restartPending = true

			if out := stripANSI(m.View().Content); strings.Contains(out, "新しいバージョンが利用可能です") {
				t.Errorf("actModal が出ているのに再起動ダイアログを重ねた:\n%s", out)
			}
			// ⚠️ 1 キーだけ押して見る: 確認待ちは 1 キーで解けるので、続けて押すと「解けた後の
			// ダイアログに答えた」ことになり、この分岐の主張と混ざる。
			// r はこのダイアログの実行キー = 一番危険なキーなので、これで代表させる。
			m.handleKey("r")
			if m.restartRequested {
				t.Errorf("actModal が出ている間の r で再起動してしまった (走行中の処理を殺す)")
			}
			if !m.restartPending {
				t.Error("actModal へ行ったキーで保留が消えた (完成を伝える機会が失われる)")
			}
		})
	}
}

// ⚠️ 実際に起きた事故そのものの回帰防止。push 確認 (y/N) 中に裏ビルドが完成すると、以前は
// 再起動ダイアログが最前面へ重なった。キーの持ち主は確認モーダルのままなので、画面の
// 「その他のキー: 後で」に従って押した y が push を実行した (無反応より悪い: 画面の指示に
// 従うと破壊的操作が走る)。ダイアログを出さないことで「見えている選択肢 = 効く選択肢」を保つ。
func TestRestartPromptDoesNotShadowPushConfirm(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.actModal.pushConfirm = true
	m.restartPending = true

	out := stripANSI(m.View().Content)
	if strings.Contains(out, "r: 今すぐ再起動") {
		t.Fatalf("push 確認中に再起動ダイアログを重ねた (押したキーの行き先と表示が食い違う):\n%s", out)
	}
	if !strings.Contains(out, "push") {
		t.Fatalf("push 確認が見えていない (前提が崩れた):\n%s", out)
	}
}

// 走行中の処理が終われば、保留していたダイアログが出て答えられる。
func TestRestartPromptAppearsAfterOperationFinishes(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.actModal.updating = true
	m.restartPending = true

	// 更新中は更新中モーダルが見えている (ダイアログに覆われない = 効かない理由が画面にある)
	if out := stripANSI(m.View().Content); !strings.Contains(out, "完了まで終了できません") {
		t.Fatalf("更新中モーダルが見えない (ダイアログが覆っている):\n%s", out)
	}

	m.Update(updateMsg{}) // 更新が決着
	if out := stripANSI(m.View().Content); !strings.Contains(out, "新しいバージョンが利用可能です") {
		t.Fatalf("走行中の処理が終わっても保留していたダイアログが出ない:\n%s", out)
	}
	m.handleKey("r")
	if !m.restartRequested || !m.done {
		t.Errorf("出た後の r が効かない: restartRequested=%v done=%v", m.restartRequested, m.done)
	}
}

// r で確認なしに再起動 (ユーザー要望)。exec は main.go が行うので、ここでは印と終了を見る。
func TestRestartPromptKeys(t *testing.T) {
	t.Run("r で再起動を予約して終了", func(t *testing.T) {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.restartPending = true
		m.handleKey("r")
		if !m.restartRequested {
			t.Error("r で再起動が予約されない")
		}
		if !m.done {
			t.Error("r で終了していない (exec は終了後に main.go が行う)")
		}
		if m.restartPending {
			t.Error("ダイアログが残っている")
		}
	})
	// ⚠️ r は issues viewer の再読込・job パネルの再実行にも割り当たっている。ダイアログが
	// 出ている間は 1 キーで必ず閉じ、r 以外では再起動しない ("再読込のつもりが再起動" を防ぐ)。
	for _, key := range []string{"j", "q", "esc", "i", "R"} {
		t.Run(key+" は再起動しない", func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.restartPending = true
			m.handleKey(key)
			if m.restartRequested {
				t.Errorf("%q で再起動してしまった", key)
			}
			if m.restartPending {
				t.Errorf("%q でダイアログが閉じない", key)
			}
		})
	}
	// viewer は全画面で全キーを飲むが、ダイアログには答えられる (答えられないと再起動できない)
	t.Run("issues viewer を開いていても r が届く", func(t *testing.T) {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.handleKey("i")
		m.restartPending = true
		m.handleKey("r")
		if !m.restartRequested {
			t.Error("viewer 表示中に r が viewer の再読込へ吸われた")
		}
	})
}

// notify と keep が同時に立ったとき、監視の再アームを落とさない (issue 032)。
//
// handle は「開始を伝えた後も失敗を拾うため監視を続ける」をこの組み合わせで表す。通知した側で
// tickCmd を捨てると監視チェーンが切れ、その後のビルド失敗が二度と通知されない (監視は失敗を
// 検出する唯一の経路)。Batch の要素数で「通知の tick と監視の tick が両方束ねられている」を見る。
func TestAutobuildMsgKeepsWatchingWhileNotifying(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.toast.phase = toastHidden
	m.ticking = false // single-flight で maybeTick が nil を返すと Batch の要素が減る
	m.autobuild = autobuildWatch{
		binPath: "/x/glogx", failedPath: "/x/" + autobuildFailedStamp,
		active: true, until: timeNow().Add(autobuildWatchTimeout), pending: autobuildStarted,
	}
	_, cmd := m.Update(autobuildMsg{result: autobuildRunning})
	if !strings.Contains(m.toast.text, "ビルド中") {
		t.Fatalf("開始の通知が出ていない: %q", m.toast.text)
	}
	if !m.autobuild.active {
		t.Fatal("開始を伝えただけで監視が止まった (失敗を拾えない)")
	}
	// maybeTick は 1 本なので、2 本以上あれば監視の再アームも入っている (Batch は nil を捨てる)
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("通知の tick と監視の再アームが両方束ねられていない: ok=%v len=%d", ok, len(batch))
	}
}

// 起動直後 (Init) に「ビルド中」を出す。完成を待たない (ユーザー要望 2026-07-31)。
func TestAutobuildNotifiesAtStartup(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.toast.phase = toastHidden
	m.autobuild = newAutobuildWatch("/some/dir/glogx", true, timeNow())
	m.Init()
	if !m.toast.visible() {
		t.Fatal("起動直後に「ビルド中」トーストが出ていない")
	}
	if !strings.Contains(m.toast.text, "ビルド中") || !strings.Contains(m.toast.text, "次回起動") {
		t.Errorf("文面が想定と違う: %q", m.toast.text)
	}
	if !m.autobuild.active {
		t.Error("開始を伝えただけで監視を止めた (失敗を拾えない)")
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

// 前回のビルドが失敗したまま再挑戦も止まっている状態は、起動時に警告として出す。
//
// ⚠️ この経路が無いと完全に無言になる: backoff が効いているので GO_AUTOBUILD_PENDING は
// 立たず、ビルドも走らないので監視も何も検出しない。「新しいコードを書いたのに旧版が
// 動き続ける」ことに誰も気づけない (実例 2026-07-31: 13 分間 stale なバイナリで操作していた)。
func TestAutobuildStaleWarnsAtStartup(t *testing.T) {
	stubSelfExe(t, staleWorkdir(t, true))
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.toast.phase = toastHidden
	m.Init()
	if !m.toast.visible() {
		t.Fatal("失敗が残っているのに起動時の警告が出ない")
	}
	if m.toast.ok {
		t.Error("失敗の警告が成功色になっている")
	}
	// 文面は原因 (失敗記録 / 誰もビルドしていない) ではなく次の行動を出す。どちらの根拠でも
	// 復旧手順は同じで、理由はログにある (issue 033)。
	for _, want := range []string{"古い版", "GO_AUTOBUILD_SYNC=1", ".autobuild.log"} {
		if !strings.Contains(m.toast.text, want) {
			t.Errorf("文面に %q が無い: %q", want, m.toast.text)
		}
	}
	// w でコピーできるよう lastWarning にも残す (調べ方をユーザーが持ち出せる)
	if !strings.Contains(m.lastWarning, ".autobuild.log") {
		t.Errorf("lastWarning に残っていない: %q", m.lastWarning)
	}
}

// 失敗記録が無い / 記録より新しいバイナリが動いている場合は何も出さない (通常起動にノイズを
// 足さない)。後者は「失敗の後に手動 build で反映済み」の状態で、警告すると嘘になる。
func TestAutobuildStaleSilentWhenNotStale(t *testing.T) {
	for _, c := range []struct {
		name  string
		stale bool
	}{
		{"失敗記録が無い", false},
		{"失敗記録より新しいバイナリが動いている", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "glogx")
			if c.stale {
				writeStamp(t, filepath.Join(dir, autobuildFailedStamp), time.Now().Add(-time.Hour))
			}
			writeStamp(t, exe, time.Now())
			stubSelfExe(t, exe)

			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.toast.phase = toastHidden
			m.autobuild = autobuildWatch{} // 監視もしていない通常起動
			m.Init()
			if m.toast.visible() {
				t.Errorf("通常起動で警告が出た: %q", m.toast.text)
			}
		})
	}
}

// ビルド中は「ビルド中」だけを出す (失敗記録が残っていても重ねない)。再挑戦の決着はこの
// セッションの監視が伝えるので、1 つの出来事に 2 枚積まない。
func TestAutobuildRunningWinsOverStaleStamp(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "glogx")
	writeStamp(t, exe, time.Now().Add(-time.Hour))
	writeStamp(t, filepath.Join(dir, autobuildFailedStamp), time.Now()) // 前回失敗が残っている
	stubSelfExe(t, exe)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.toast.phase = toastHidden
	m.autobuild = newAutobuildWatch(exe, true, timeNow()) // 再挑戦が走っている
	m.Init()
	if !strings.Contains(m.toast.text, "ビルド中") {
		t.Fatalf("ビルド中が出ていない: %q", m.toast.text)
	}
	if len(m.toast.older) != 0 {
		t.Errorf("失敗の警告が重ねて積まれた: %+v", m.toast.older)
	}
}

// staleWorkdir は「失敗記録が自バイナリより新しい」(stale=true) / その逆 のディレクトリを作り、
// 自バイナリのパスを返す。
func staleWorkdir(t *testing.T, stale bool) string {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "glogx")
	stamp := filepath.Join(dir, autobuildFailedStamp)
	now := time.Now()
	if stale {
		writeStamp(t, exe, now.Add(-time.Hour))
		writeStamp(t, stamp, now)
	} else {
		writeStamp(t, stamp, now.Add(-time.Hour))
		writeStamp(t, exe, now)
	}
	return exe
}

// writeStamp は指定 mtime のファイルを作る (mtime 比較の基準を作るため)。
func writeStamp(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// stubSelfExe は selfExePath を差し替える (自バイナリの位置を偽装して失敗記録を読ませる)。
func stubSelfExe(t *testing.T, exe string) {
	t.Helper()
	orig := selfExePath
	selfExePath = func() string { return exe }
	t.Cleanup(func() { selfExePath = orig })
}

// autobuildStaleBinary は失敗記録と自バイナリの前後関係だけで判定する (env に依存しない)。
func TestAutobuildStaleBinary(t *testing.T) {
	if !autobuildStaleBinary(staleWorkdir(t, true)) {
		t.Error("記録が新しいのに stale と判定されない")
	}
	if autobuildStaleBinary(staleWorkdir(t, false)) {
		t.Error("バイナリが新しいのに stale と判定された")
	}
	if autobuildStaleBinary(filepath.Join(t.TempDir(), "glogx")) {
		t.Error("失敗記録が無いのに stale と判定された")
	}
	if autobuildStaleBinary("") {
		t.Error("パス不明 (os.Executable 失敗) で stale と判定された")
	}
}

// ソースが自バイナリより新しい = 誰もビルドしていない、も stale の根拠にする。
//
// ⚠️ これが無いと無言で旧版に固定される経路が残る (issue 033): lock 残留で shim が
// 「他がビルド中」と誤認する / 同期ツールでソースの mtime が巻き戻り shim の -nt が偽になる /
// shim を経ずバイナリを直接起動する。失敗記録はどれでも作られない。
func TestAutobuildStaleFromNewerSources(t *testing.T) {
	old, now := time.Now().Add(-time.Hour), time.Now()
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{"直下の .go が新しい", func(t *testing.T, dir string) {
			writeStamp(t, filepath.Join(dir, "main.go"), now)
		}, true},
		{"サブパッケージの .go が新しい", func(t *testing.T, dir string) {
			mkdir(t, filepath.Join(dir, "issues"))
			writeStamp(t, filepath.Join(dir, "issues", "parse.go"), now)
		}, true},
		{"go.sum が新しい", func(t *testing.T, dir string) {
			writeStamp(t, filepath.Join(dir, "go.sum"), now)
		}, true},
		{"ソースが古い (通常の起動)", func(t *testing.T, dir string) {
			writeStamp(t, filepath.Join(dir, "main.go"), old)
		}, false},
		{"新しいのはテストだけ (go build の入力ではない)", func(t *testing.T, dir string) {
			writeStamp(t, filepath.Join(dir, "main_test.go"), now)
		}, false},
		{"新しいのは .go 以外", func(t *testing.T, dir string) {
			writeStamp(t, filepath.Join(dir, "README.md"), now)
		}, false},
		{"サブモジュールの go.mod (shim も直下しか見ない)", func(t *testing.T, dir string) {
			mkdir(t, filepath.Join(dir, "tools", "probe"))
			writeStamp(t, filepath.Join(dir, "tools", "probe", "go.mod"), now)
		}, false},
		{"dot ディレクトリの中 (shim の ** も辿らない)", func(t *testing.T, dir string) {
			mkdir(t, filepath.Join(dir, ".git"))
			writeStamp(t, filepath.Join(dir, ".git", "hook.go"), now)
		}, false},
		{"ビルド中 (lock がある) は黙る", func(t *testing.T, dir string) {
			writeStamp(t, filepath.Join(dir, "main.go"), now)
			mkdir(t, filepath.Join(dir, autobuildLockDir))
		}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "glogx")
			writeStamp(t, filepath.Join(dir, "go.mod"), old) // ソース木の印
			writeStamp(t, exe, old.Add(time.Minute))         // バイナリは go.mod より後・now より前
			c.setup(t, dir)
			if got := autobuildStaleBinary(exe); got != c.want {
				t.Errorf("stale = %v, want %v", got, c.want)
			}
		})
	}
}

// ソース木の外 (go.mod が隣に無い) では判定しない。配布・コピーされたバイナリや、テストバイナリの
// 一時ディレクトリで「古い」と言い出さないため。
func TestAutobuildStaleSilentOutsideSourceTree(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "glogx")
	writeStamp(t, exe, time.Now().Add(-time.Hour))
	writeStamp(t, filepath.Join(dir, "main.go"), time.Now()) // go.mod は作らない
	if autobuildStaleBinary(exe) {
		t.Error("ソース木の外で stale と判定された")
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// 「古い版で動いています」に、動いている版の手がかりを添える。
// ⚠️ 判定には使わない (tree hash はコミット済みしか見ないので「今より古いか」は言えない)。
// ここで言えるのは「どの版か」だけで、追う人が git show で辿れれば足りる。
func TestAutobuildRunningRev(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "glogx")

	if got := autobuildRunningRev(exe); got != "" {
		t.Fatalf("記録が無いのに何か言った: %q", got)
	}
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, autobuildRevStamp), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("88a4e09ede06805e5cb6403eb2c17cee621b9edb\n")
	if got := autobuildRunningRev(exe); !strings.Contains(got, "88a4e09ede06") {
		t.Fatalf("tree hash が出ない: %q", got)
	}
	if got := autobuildRunningRev(exe); strings.Contains(got, "805e5cb6403") {
		t.Fatalf("短縮していない (トーストが長くなる): %q", got)
	}
	// 未コミットから作った版はそう言う (同じ tree hash でも中身が違いうるため)
	write("88a4e09ede06805e5cb6403eb2c17cee621b9edb +dirty\n")
	if got := autobuildRunningRev(exe); !strings.Contains(got, "+dirty") {
		t.Fatalf("+dirty が落ちている: %q", got)
	}
	write("   \n")
	if got := autobuildRunningRev(exe); got != "" {
		t.Fatalf("空の記録で何か言った: %q", got)
	}
}
