// Package filerview は glogx の全画面 treefiler (F) と、F を出したまま閉じたときの場所の記憶。
// 画面の部品は src/treefiler の filer パッケージ。開閉・横断・前景実行は main の browseModel が仲介する。
package filerview

// treefiler (F) の全画面。画面の部品は src/treefiler の filer パッケージで、ここは glogx への配線だけを持つ
// (単体の bin/treefiler と同じ画面。仕様は docs/treefiler-spec.md §0.3)。
//
// filer はタイマーを張らない。glogx の tick で Advance を呼び、Animating の間だけ tickInterval が周期を上げる。

import (
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"

	"glogx/internal/app"
	"glogx/internal/crosskey"
	"glogx/internal/extcmd"
	"glogx/internal/term"
	"treefiler/filer"
)

type filerAction int

const (
	FilerSwallow filerAction = iota // filer がキーを使った (裏の一覧へは流さない)
	FilerClosed                     // F で閉じた (git log 一覧へ戻る)
	FilerCross                      // 別の全画面へ横断 (閉じ済み。行き先は takeWantCross)
	FilerQuit                       // q / Esc = glogx ごと終える (issues viewer と同じ。spec §0.1)
	FilerExec                       // ! / s / e = プロセスを起こす (中身は f.TakeExec)
)

// filerChangedMsg は treefiler のライブ更新の合図。ch はどのチャネルからか (ok=false はそのチャネルが閉じた)。
type FilerChangedMsg struct {
	ch <-chan struct{}
	ok bool
}

// Open は合図のチャネルがまだ開いているか (閉じた = 画面を閉じた・走査を止めた。取り込まずに捨てる)。
func (m FilerChangedMsg) Open() bool { return m.ok }

func waitFilerChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-ch
		return FilerChangedMsg{ch: ch, ok: ok}
	}
}

// rearm は合図を受けた後の待ち直し。今のチャネルからの合図のときだけ待ち直す
// (閉じる直前の合図が古い待ちを ok=true で返すと、新しいチャネルに待ちが 2 本張られて戻らない)。
func (v *FilerView) Rearm(msg FilerChangedMsg) tea.Cmd {
	if f := v.live(); !msg.ok || f == nil || f.WatchingChan() != msg.ch {
		return nil
	}
	return waitFilerChange(msg.ch)
}

type FilerView struct {
	shown     bool
	f         *filer.Model
	dir       string
	wantCross string
	openErr   string
}

func (v *FilerView) Visible() bool { return v.shown }

// place は開いている F の場所 (開いていなければ ok=false)。終了時の記憶 (filer_resume.go) が使う。
func (v *FilerView) Place() (filer.Place, bool) {
	if f := v.live(); f != nil {
		return f.Place(), true
	}
	return filer.Place{}, false
}

// toggle は開く / 閉じる。開くときは pwd を root にする (spec §0.1)。同じ dir なら前の状態 (カーソル・開いたフォルダ) を使う。
func (v *FilerView) Toggle(dir string) {
	if v.shown {
		v.Hide()
		return
	}
	if v.f == nil || v.dir != dir {
		// app.TimeNow を呼ぶたびに読む (値をつかむと、テストが差し替えた時計に追従しない)
		f, err := filer.New(dir, filer.Options{Now: func() time.Time { return app.TimeNow() }})
		if err != nil {
			v.openErr = "ファイラーを開けません: " + app.FirstLine(err.Error())
			return
		}
		v.f, v.dir = f, dir
	} else {
		v.f.Refresh() // 閉じている間に増えた・消えたファイルを出す (開閉とカーソルは保つ)
	}
	v.shown = true
}

// hide は閉じる。ライブ更新のポーリングも止める (閉じた後も 1 秒ごとにディスクを読み続けない)。
func (v *FilerView) Hide() {
	v.shown = false
	if v.f != nil {
		v.f.Close()
	}
}

// DrainNotices は見えている間に溜まった知らせを deliver へ渡す (閉じていれば何もしない)。押したキーが効かなかった理由は
// NoticeRefused にする (lastWarning を汚さない)。
// 🚨 呼ぶ側はキーの後だけでなく Advance の後にも呼ぶ: explode の完了や `o` の失敗は Advance の中で積まれるので、キーの後だけだと次のキーまで出ない。
func (v *FilerView) DrainNotices(deliver func(text string, kind app.NoticeKind) bool) {
	if v.openErr != "" { // 開けなかった (見えていないので live は nil)
		deliver(v.openErr, app.NoticeError)
		v.openErr = ""
	}
	f := v.live()
	if f == nil {
		return
	}
	for _, n := range f.TakeNotices() {
		kind := app.NoticeRefused
		if n.OK {
			kind = app.NoticeOK
		}
		deliver(n.Text, kind)
	}
}

// RestorePlace は起動時の復元で、開いた画面を記憶の場所へ戻す (閉じていれば何もしない)。
func (v *FilerView) RestorePlace(p filer.Place) {
	if f := v.live(); f != nil {
		f.RestorePlace(p)
	}
}

// live は見えている間の filer (閉じていれば nil)。「見えているなら f がある」をメソッドごとに確かめ直さず、ここ 1 本で読む。
func (v *FilerView) live() *filer.Model {
	if !v.shown {
		return nil
	}
	return v.f
}

// watchCmd は開いている間のライブ更新の待ち (開いた直後と、合図を受けるたびに張り直す)。
func (v *FilerView) WatchCmd() tea.Cmd {
	if f := v.live(); f != nil {
		return waitFilerChange(f.Changed())
	}
	return nil
}

// ownsKeys は filer が入力モード中か (overlayOwnershipTable)。
func (v *FilerView) OwnsKeys() bool { f := v.live(); return f != nil && f.OwnsKeys() }

// binds は key が filer の意味を持つか (持つキーは glogx の横断キー・C の update・U に取らない。filer.Model.Binds が正本)。
func (v *FilerView) Binds(key string) bool { f := v.live(); return f != nil && f.Binds(key) }

// paste は貼り付けを filer へ渡す (入力中だけ入る。そうでなければ filer が捨てる)。
func (v *FilerView) Paste(text string) {
	if f := v.live(); f != nil {
		f.Paste(text)
	}
}

// caretPos は filer の入力欄のキャレット (filer の画面の座標)。
func (v *FilerView) CaretPos() (x, y int, ok bool) {
	if f := v.live(); f != nil {
		return f.CaretPos()
	}
	return 0, 0, false
}

func (v *FilerView) Animating() bool { f := v.live(); return f != nil && f.Animating() }

// busy は filer の裏の走査・git の取得が走っているか (その間は spinnerActive が tick を回し、結果を取り込む)。
func (v *FilerView) Busy() bool { f := v.live(); return f != nil && f.Busy() }

// resize は画面の大きさを伝える。描くときではなく大きさが変わったときに伝えないと、カメラの目標が変わっても
// tick が回らず、次のキーまで古い位置のまま残る。
func (v *FilerView) Resize(width, page int) {
	if f := v.live(); f != nil {
		f.Resize(width, page)
	}
}

func (v *FilerView) Advance(now time.Time) {
	if f := v.live(); f != nil {
		f.Advance(now)
	}
}

// handleKey は filer が飲むキー。F で閉じ、横断キー (i / R / D。判定は crosskey.Is、行き先は main の CrossTarget) で他の全画面へ移る。
// filer が意味を持つキー (Binds) は横断しない: s は treebeard の「シェルを開く」(spec §0.1。ユーザー回答 2026-10-07)、
// 入力中 (検索・! のコマンド行・設定の板・キー一覧) は全部のキー (F で閉じると打っている文字が消える)
func (v *FilerView) HandleKey(key string) filerAction {
	if !v.f.Binds(key) {
		if key == "F" {
			v.Hide()
			return FilerClosed
		}
		if crosskey.Is(key) && key != "F" {
			v.Hide()
			v.wantCross = key
			return FilerCross
		}
	}
	switch v.f.HandleKey(key) {
	case filer.Quit:
		return FilerQuit
	case filer.Exec:
		return FilerExec
	case filer.None:
	}
	return FilerSwallow
}

func (v *FilerView) TakeWantCross() string {
	want := v.wantCross
	v.wantCross = ""
	return want
}

// lines は page 行の画面 (最下行は filer のステータスバー)。
func (v *FilerView) Lines(width, page int) []string {
	v.f.Resize(width, page)
	return v.f.View()
}

// hint は最下行の案内。抜ける手段 (F / q) を先に置き、幅で切る (glogx-ui-guide §5: 抜ける手段は必ず残す)。
// 🚨 filer がキーを取っている間 (/ の検索・! と : の入力欄・パネル・? の一覧・確認) は F も q も filer に入るので、
// 「F: 閉じる」を出すと嘘になる。そのときはその層で効くキー (filer.Model.LayerHints) を出す (lan の Hint と同じ形)
func (v *FilerView) Hint(width int) string {
	if f := v.live(); f != nil && f.OwnsKeys() { // 層の間はその層で効くキーだけ (glogx-ui-guide §5)
		hs := f.LayerHints()
		items := make([]term.HintItem, 0, len(hs))
		for _, h := range hs {
			items = append(items, term.HintItem{Text: h.Key + ": " + h.Label, Prio: h.Prio})
		}
		if len(items) == 0 {
			items = append(items, term.HintItem{Text: "esc: 戻る", Prio: 1})
		}
		return term.FitHintItems(width, items)
	}
	return term.FitHintItems(width, []term.HintItem{
		{Text: "F: 閉じる", Prio: 1},
		{Text: "q: glogx を終了", Prio: 2},
		{Text: "?: キーの一覧", Prio: 5},
	})
}

// filerExecCommand は treefiler の ! / s / e が頼んだプロセス (選んだフォルダで、$f に選んだパスを入れて起こす)。
// 返した Cmd は extcmd.RunEditorCmd (tea.Exec + foreground.Exec) が前景で起動する。免除の理由は extcmd.EditorCommand と同じ
// (前景の対話プロセスで ctx が無く、端末を継承するのでパイプも copy goroutine も作らない)。
func filerExecCommand(r filer.ExecRequest) *exec.Cmd {
	cmd := exec.Command(r.Argv[0], r.Argv[1:]...) // subproc: no-waitdelay — 前景・ctx 無し・パイプ無し
	cmd.Dir = r.Dir
	cmd.Env = r.Env
	return cmd
}

// TakeExecCmd は ! / s / e が頼んだプロセスを前景で起こす Cmd (頼みが無ければ nil)。戻ると extcmd.EditorClosedMsg が
// 届くので、受け手は ExecReturned を呼ぶ。
func (v *FilerView) TakeExecCmd() tea.Cmd {
	if v.f == nil {
		return nil
	}
	r, ok := v.f.TakeExec()
	if !ok || len(r.Argv) == 0 {
		return nil
	}
	return extcmd.RunEditorCmd(extcmd.OriginFiler, filerExecCommand(r)) // 戻ると extcmd.EditorClosedMsg で読み直す
}

// ExecReturned はこの画面が起こした前景のプロセス (extcmd.OriginFiler) から戻ったときに呼ぶ。
// 戻ったら読み直す (シェルやエディタで作った・消したファイルを出す)。返すのは知らせる文 (空なら無し)。
// 「エディタが異常終了しました」は出さない: シェルは最後のコマンドの終了コードで抜けるのが普通 (e のエディタも filer.ExecDone の判断に揃える)。
func (v *FilerView) ExecReturned(err error) string {
	if f := v.live(); f != nil { // 前景で起こしている間は閉じられないので、ここで見えていないことは無い
		return f.ExecDone(err)
	}
	return ""
}
