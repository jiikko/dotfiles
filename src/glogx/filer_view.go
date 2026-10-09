package main

// treefiler (F) の全画面。画面の部品は src/treefiler の filer パッケージで、ここは glogx への配線だけを持つ
// (単体の bin/treefiler と同じ画面。仕様は docs/treefiler-spec.md §0.3)。
//
// filer はタイマーを張らない。glogx の tick で Advance を呼び、Animating の間だけ tickInterval が周期を上げる。

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"treefiler/filer"
)

type filerAction int

const (
	filerSwallow filerAction = iota // filer がキーを使った (裏の一覧へは流さない)
	filerClosed                     // F で閉じた (git log 一覧へ戻る)
	filerCross                      // 別の全画面へ横断 (閉じ済み。行き先は takeWantCross)
	filerQuit                       // q / Esc = glogx ごと終える (issues viewer と同じ。spec §0.1)
	filerExec                       // ! / s / e = プロセスを起こす (中身は f.TakeExec)
)

// execPending は ! / s / e で起こしたプロセスから戻るのを待っているか (戻ったときにエディタ向けの文言を出さないため)。

// filerChangedMsg は treefiler のライブ更新の合図。ch はどのチャネルからか (ok=false はそのチャネルが閉じた)。
type filerChangedMsg struct {
	ch <-chan struct{}
	ok bool
}

func waitFilerChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-ch
		return filerChangedMsg{ch: ch, ok: ok}
	}
}

// rearm は合図を受けた後の待ち直し。今のチャネルからの合図のときだけ待ち直す
// (閉じる直前の合図が古い待ちを ok=true で返すと、新しいチャネルに待ちが 2 本張られて戻らない。レビューの指摘 2026-10-08)。
func (v *filerView) rearm(msg filerChangedMsg) tea.Cmd {
	if !msg.ok || !v.shown || v.f == nil || v.f.WatchingChan() != msg.ch {
		return nil
	}
	return waitFilerChange(msg.ch)
}

type filerView struct {
	execPending bool
	shown       bool
	f           *filer.Model
	dir         string
	wantCross   fullScreenID
	openErr     string
}

func (v *filerView) visible() bool { return v.shown }

// toggle は開く / 閉じる。開くときは pwd を root にする (spec §0.1)。同じ dir なら前の状態 (カーソル・開いたフォルダ) を使う。
func (v *filerView) toggle(dir string) {
	if v.shown {
		v.hide()
		return
	}
	if v.f == nil || v.dir != dir {
		// timeNow を呼ぶたびに読む (値をつかむと、テストが差し替えた時計に追従しない)
		f, err := filer.New(dir, filer.Options{Now: func() time.Time { return timeNow() }})
		if err != nil {
			v.openErr = "ファイラーを開けません: " + firstLine(err.Error())
			return
		}
		v.f, v.dir = f, dir
	} else {
		v.f.Refresh() // 閉じている間に増えた・消えたファイルを出す (開閉とカーソルは保つ)
	}
	v.shown = true
}

// hide は閉じる。ライブ更新のポーリングも止める (閉じた後も 1 秒ごとにディスクを読み続けない)。
func (v *filerView) hide() {
	v.shown = false
	if v.f != nil {
		v.f.Close()
	}
}

// watchCmd は開いている間のライブ更新の待ち (開いた直後と、合図を受けるたびに張り直す)。
func (v *filerView) watchCmd() tea.Cmd {
	if !v.shown || v.f == nil {
		return nil
	}
	return waitFilerChange(v.f.Changed())
}

func (v *filerView) takeOpenErr() string {
	e := v.openErr
	v.openErr = ""
	return e
}

// ownsKeys は filer が入力モード中か (overlayOwnershipTable)。
func (v *filerView) ownsKeys() bool { return v.shown && v.f != nil && v.f.OwnsKeys() }

// binds は key が filer の意味を持つか (持つキーは glogx の横断キー・C の update・U に取らない。filer.Model.Binds が正本)。
func (v *filerView) binds(key string) bool { return v.shown && v.f != nil && v.f.Binds(key) }

// paste は貼り付けを filer へ渡す (入力中だけ入る。そうでなければ filer が捨てる)。
func (v *filerView) paste(text string) {
	if v.shown && v.f != nil {
		v.f.Paste(text)
	}
}

// caretPos は filer の入力欄のキャレット (filer の画面の座標)。
func (v *filerView) caretPos() (x, y int, ok bool) {
	if !v.shown || v.f == nil {
		return 0, 0, false
	}
	return v.f.CaretPos()
}

func (v *filerView) animating() bool { return v.shown && v.f != nil && v.f.Animating() }

// busy は filer の裏の走査・git の取得が走っているか (その間は spinnerActive が tick を回し、結果を取り込む)。
func (v *filerView) busy() bool { return v.shown && v.f != nil && v.f.Busy() }

// resize は画面の大きさを伝える。描くときではなく大きさが変わったときに伝えないと、カメラの目標が変わっても
// tick が回らず、次のキーまで古い位置のまま残る (レビューの指摘 2026-10-08)。
func (v *filerView) resize(width, page int) {
	if v.shown && v.f != nil {
		v.f.Resize(width, page)
	}
}

func (v *filerView) advance(now time.Time) {
	if v.shown && v.f != nil {
		v.f.Advance(now)
	}
}

// handleKey は filer が飲むキー。F で閉じ、横断キー (i / R / D。表は crossTarget) で他の全画面へ移る。
// filer が意味を持つキー (Binds) は横断しない: s は treebeard の「シェルを開く」(spec §0.1。ユーザー回答 2026-10-07)、
// 入力中 (検索・! のコマンド行・設定の板・キー一覧) は全部のキー (F で閉じると打っている文字が消える。レビューの指摘 2026-10-09)
func (v *filerView) handleKey(key string) filerAction {
	if !v.f.Binds(key) {
		if key == "F" {
			v.hide()
			return filerClosed
		}
		if target, ok := crossTarget(key); ok && target != fullScreenFiler {
			v.hide()
			v.wantCross = target
			return filerCross
		}
	}
	switch v.f.HandleKey(key) {
	case filer.Quit:
		return filerQuit
	case filer.Exec:
		return filerExec
	case filer.None:
	}
	return filerSwallow
}

func (v *filerView) takeWantCross() fullScreenID {
	want := v.wantCross
	v.wantCross = fullScreenNone
	return want
}

// lines は page 行の画面 (最下行は filer のステータスバー)。
func (v *filerView) lines(width, page int) []string {
	v.f.Resize(width, page)
	return v.f.View()
}

// hint は最下行の案内。抜ける手段 (F / q) を先に置き、幅で切る (glogx-ui-guide §5: 抜ける手段は必ず残す)。
func (v *filerView) hint(width int) string {
	return fitHintItems(width, []hintItem{
		{text: "F: 閉じる", prio: 1},
		{text: "q: glogx を終了", prio: 2},
		{text: "キーは src/treefiler/README.md", prio: 5},
	})
}

// routeKeyToFiler は filer 表示中のキー処理 (全画面なのでキーは全部 filer が飲む)。
func (m *browseModel) routeKeyToFiler(key string) (tea.Model, tea.Cmd) {
	// U は他の viewer と同じく利用枠を重ねる (spec §0.3: R D U X は filer の中でも効かせる)
	if key == "U" && !m.filerV.binds(key) {
		return m, m.toggleUsage()
	}
	act := m.filerV.handleKey(key)
	if m.filerV.f != nil {
		for _, n := range m.filerV.f.TakeNotices() {
			kind := noticeRefused // 押したキーが効かなかった理由 (lastWarning を汚さない)
			if n.OK {
				kind = noticeOK
			}
			m.deliverNotice(n.Text, kind)
		}
	}
	switch act {
	case filerQuit:
		m.filerV.hide()
		return m.quit()
	case filerCross:
		return m, m.openFullScreen(m.filerV.takeWantCross())
	case filerExec:
		if r, ok := m.filerV.f.TakeExec(); ok && len(r.Argv) > 0 {
			m.filerV.execPending = true
			return m, runEditorCmd(filerExecCommand(r)) // 戻ると editorClosedMsg で読み直す
		}
	case filerClosed, filerSwallow:
	}
	return m, m.maybeTick()
}
