package main

// mac-lan-manager (L) の全画面。画面の部品は src/mac-lan-manager の lanmgr パッケージで、ここは glogx への配線だけを持つ
// (単体の bin/mac-lan-manager と同じ画面。要件と決定は issue 747)。形は treefiler の F (filer_view.go) にそろえる。
//
// lanmgr はタイマーを張らない。glogx の tick で Advance を呼び、Busy (走査・網の読み込み・相手の確認・確認待ち・裏の後始末・名前の引き直し)
// の間だけ spinnerActive が tick を回す。
// 走査が落ち着いた後に見つかった Mac は Changed の合図で取り込む (Busy の間しか tick を張らないため)。

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"glogx/internal/term"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/live"
)

type lanAction int

const (
	lanSwallow lanAction = iota // lanmgr がキーを使った (裏の一覧へは流さない)
	lanClosed                   // L / q / Esc で閉じた (git log 一覧へ戻る。ガイド §1: q は 1 段戻る)
	lanCross                    // 別の全画面へ横断 (閉じ済み。行き先は takeWantCross)
	lanExec                     // s / o などプロセスを起こす (中身は TakeExec)
)

// lanOptions は本物へのつなぎ (テストは走査とネットワークの読み取りを偽物に差し替える。tui_helpers_test.go の TestMain)。
var lanOptions = live.Options

// newLanModel は板の中身を作る。クリップボードと時計は glogx の口を使う (テストの stubClipboard と時計の差し替えに従う)。
func newLanModel() (*lanmgr.Model, error) {
	o, err := lanOptions()
	if err != nil {
		return nil, err
	}
	o.Copy = func(s string) error { return copyToClipboard(s) } // glogx のクリップボードの口 (テストは stubClipboard で差し替える)
	o.Now = func() time.Time { return timeNow() }               // 呼ぶたびに読む (値をつかむとテストが差し替えた時計に追従しない)
	return lanmgr.New(o), nil
}

// lanChangedMsg は走査の結果が変わった合図。ch はどのチャネルからか (ok=false はそのチャネルが閉じた)。
type lanChangedMsg struct {
	ch <-chan struct{}
	ok bool
}

func waitLanChange(ch <-chan struct{}) tea.Cmd {
	if ch == nil {
		return nil // 走査が無い (待っても来ない)
	}
	return func() tea.Msg {
		_, ok := <-ch
		return lanChangedMsg{ch: ch, ok: ok}
	}
}

type lanView struct {
	watching    <-chan struct{} // 今待っている走査の合図のチャネル (走査が替わったら待ち直す)
	execPending bool
	shown       bool
	m           *lanmgr.Model
	wantCross   fullScreenID
	openErr     string
}

func (v *lanView) visible() bool { return v.shown }

// live は見えている間の画面 (閉じていれば nil)。
func (v *lanView) live() *lanmgr.Model {
	if !v.shown {
		return nil
	}
	return v.m
}

// toggle は開く / 閉じる。2 回目からは前の状態 (タブ・カーソル) を使い、ネットワーク・台帳・設定を読み直して走査を起こし直す
// (閉じている間に Mac が増えた・消えた・台帳を単体の bin/mac-lan-manager で書き換えた)。
func (v *lanView) toggle() {
	if v.shown {
		v.hide()
		return
	}
	if v.m == nil {
		m, err := newLanModel()
		if err != nil {
			v.openErr = "mac-lan-manager を開けません: " + firstLine(err.Error())
			return
		}
		v.m = m
	} else {
		v.m.Reload()
	}
	v.shown = true
}

// hide は閉じる。走査 (dns-sd) も止める (閉じた後も子プロセスを走らせ続けない)。
func (v *lanView) hide() {
	v.shown = false
	v.watching = nil
	v.stop()
}

// stop は走査を止める (cancelAll からも呼ぶ。終了・再起動で dns-sd を残さない)。
func (v *lanView) stop() {
	if v.m != nil {
		v.m.Close()
	}
}

// watchCmd は走査の合図の待ちを、今の走査のチャネルに張る (まだ張っていなければ)。開いた直後と、キーの後に呼ぶ:
// r (読み直し) は板の中で走査を起こし直すので、開いたときに張った待ちは古いチャネルを見ている (red team の指摘 2026-10-15)。
// 古いチャネルは走査の Close で閉じるので、古い待ちは ok=false で抜ける。
func (v *lanView) watchCmd() tea.Cmd {
	m := v.live()
	if m == nil {
		return nil
	}
	ch := m.Changed()
	if ch == nil || ch == v.watching {
		return nil
	}
	v.watching = ch
	return waitLanChange(ch)
}

// rearm は合図を受けた後の待ち直し。今待っているチャネルからの合図のときだけ待ち直す (走査が替わった後に古い待ちが
// ok=true で戻ると、新しいチャネルに待ちが 2 本張られる。filerView.rearm と同じ理由)。
// 🚨 この待ち直しは mac-lan-manager の app.watch・glogx の lanView / filerView.rearm・treefiler の waitChange にもあるが、共通の部品にしない:
// 比べるのは「今のチャネルか」の 1 行で、状態の持ち主 (自分で持つ / model が持つ / 比べない)・ok=false のときに取り込むか・tick との関係が
// 4 か所で違い、部品にすると各画面の前提を引数で表すだけになる (tuikit は bubbletea を import できないので tea.Cmd も返せない)。
// 4 か所のどれかで穴を直したら、残りの 3 か所も同じ穴を grep で探す。
func (v *lanView) rearm(msg lanChangedMsg) tea.Cmd {
	if !msg.ok || v.live() == nil || msg.ch != v.watching {
		return nil
	}
	return waitLanChange(msg.ch)
}

func (v *lanView) takeOpenErr() string {
	e := v.openErr
	v.openErr = ""
	return e
}

// ownsKeys は lanmgr が全部のキーを持つ層 (入力欄・確認・板・設定) を開いているか (overlayOwnershipTable)。
func (v *lanView) ownsKeys() bool { m := v.live(); return m != nil && m.OwnsKeys() }

// binds は key が lanmgr の意味を持つか (持つキーは glogx の横断キー・C の update・U に取らない。lanmgr.Model.Binds が正本)。
func (v *lanView) binds(key string) bool { m := v.live(); return m != nil && m.Binds(key) }

func (v *lanView) paste(text string) {
	if m := v.live(); m != nil {
		m.Paste(text)
	}
}

func (v *lanView) caretPos() (x, y int, ok bool) {
	if m := v.live(); m != nil {
		return m.CaretPos()
	}
	return 0, 0, false
}

// busy は Bonjour の走査が続いているか (その間は spinnerActive が tick を回し、途中の結果を取り込む)。
func (v *lanView) busy() bool { m := v.live(); return m != nil && m.Busy() }

func (v *lanView) advance(now time.Time) {
	if m := v.live(); m != nil {
		m.Advance(now)
	}
}

// handleKey は lanmgr が飲むキー。L で閉じ、横断キー (表は crossTarget) で他の全画面へ移る。lanmgr が意味を持つキー (Binds) は
// 横断しない: s は ssh、入力中・確認・設定は全部のキー (横断で閉じると打っている文字が消える)。
func (v *lanView) handleKey(key string) lanAction {
	if !v.m.Binds(key) {
		if key == "L" {
			v.hide()
			return lanClosed
		}
		if target, ok := crossTarget(key); ok && target != fullScreenLan {
			v.hide()
			v.wantCross = target
			return lanCross
		}
	}
	switch v.m.HandleKey(key) {
	case lanmgr.Quit:
		v.hide()
		return lanClosed
	case lanmgr.Exec:
		return lanExec
	case lanmgr.None:
	}
	return lanSwallow
}

func (v *lanView) takeWantCross() fullScreenID {
	want := v.wantCross
	v.wantCross = fullScreenNone
	return want
}

// lines は page 行の画面 (最下行の手前に lanmgr 自身のキーの案内がある)。
func (v *lanView) lines(width, page int) []string {
	v.m.Resize(width, page)
	return v.m.View()
}

// hint は最下行の案内。抜ける手段 (L / q) を先に置き、幅で切る (glogx-ui-guide §5: 抜ける手段は必ず残す)。
func (v *lanView) hint(width int) string {
	// 層 (初期設定・設定・確認の板・入力欄) が開いている間は L も ? も lanmgr が取り、q / esc は 1 段戻るだけ (Binds)。そのときに
	// 「L/q: 閉じる」を出すと嘘になった (整合の監査)
	// ただし足りない画面では q / esc が L ごと閉じ、L も glogx に渡る (Binds("L") が偽) ので「L/q: 閉じる」が正しい (敵対レビュー)
	if lm := v.live(); lm != nil && lm.OwnsKeys() && lm.Binds("L") {
		return term.FitHintItems(width, []term.HintItem{{Text: "esc: 戻る", Prio: 1}})
	}
	return term.FitHintItems(width, []term.HintItem{
		{Text: "L/q: 閉じる", Prio: 1},
		{Text: "?: キーの一覧", Prio: 2},
	})
}

// drainLanNotices は lanmgr に溜まった知らせを toast へ出す (キーの後と Advance の後。走査の結果で積まれる知らせもある)。
func (m *browseModel) drainLanNotices() {
	lm := m.lanV.live()
	if lm == nil {
		return
	}
	for _, n := range lm.TakeNotices() {
		kind := noticeRefused // 押したキーが効かなかった理由 (lastWarning を汚さない)
		if n.OK {
			kind = noticeOK
		}
		m.deliverNotice(n.Text, kind)
	}
}

// routeKeyToLan は mac-lan-manager 表示中のキー処理 (全画面なのでキーは全部 lanmgr が飲む)。
func (m *browseModel) routeKeyToLan(key string) (tea.Model, tea.Cmd) {
	if key == "U" && !m.lanV.binds(key) {
		return m, m.toggleUsage() // 他の全画面と同じく利用枠を重ねる
	}
	act := m.lanV.handleKey(key)
	m.drainLanNotices()
	switch act {
	case lanCross:
		return m, m.openFullScreen(m.lanV.takeWantCross())
	case lanExec:
		if r, ok := m.lanV.m.TakeExec(); ok && len(r.Argv) > 0 {
			m.lanV.execPending = true
			return m, runEditorCmd(lanExecCommand(r)) // 戻ると editorClosedMsg で ExecDone を呼ぶ
		}
	case lanClosed, lanSwallow:
	}
	return m, tea.Batch(m.lanV.watchCmd(), m.maybeTick())
}
