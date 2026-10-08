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
)

type filerView struct {
	shown     bool
	f         *filer.Model
	dir       string
	wantCross fullScreenID
	openErr   string
}

func (v *filerView) visible() bool { return v.shown }

// toggle は開く / 閉じる。開くときは pwd を root にする (spec §0.1)。同じ dir なら前の状態 (カーソル・開いたフォルダ) を使う。
func (v *filerView) toggle(dir string) {
	if v.shown {
		v.shown = false
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

func (v *filerView) takeOpenErr() string {
	e := v.openErr
	v.openErr = ""
	return e
}

// ownsKeys は filer が入力モード中か (C / X の update を譲る判定。overlayOwnershipTable)。
func (v *filerView) ownsKeys() bool { return v.shown && v.f != nil && v.f.OwnsKeys() }

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
// 🚨 s は横断しない: filer では treebeard の「シェルを開く」に上書きした (spec §0.1。ユーザー回答 2026-10-07)
func (v *filerView) handleKey(key string) filerAction {
	if key == "F" {
		v.shown = false
		return filerClosed
	}
	if !v.f.OwnsKeys() {
		if target, ok := crossTarget(key); ok && target != fullScreenFiler && key != "s" {
			v.shown = false
			v.wantCross = target
			return filerCross
		}
	}
	if v.f.HandleKey(key) == filer.Quit {
		return filerQuit
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
	if key == "U" {
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
		return m.quit()
	case filerCross:
		return m, m.openFullScreen(m.filerV.takeWantCross())
	case filerClosed, filerSwallow:
	}
	return m, m.maybeTick()
}
