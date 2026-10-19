// Package lanview は glogx の全画面 mac-lan-manager (L) と、L を出したまま閉じたときの画面の記憶。
// 画面の部品は src/mac-lan-manager の lanmgr、本物へのつなぎは live。開閉・横断・前景実行は main の browseModel が仲介する。
package lanview

// mac-lan-manager (L) の全画面。画面の部品は src/mac-lan-manager の lanmgr パッケージで、ここは glogx への配線だけを持つ
// (単体の bin/mac-lan-manager と同じ画面)。形は treefiler の F (filer_view.go) にそろえる。
//
// lanmgr はタイマーを張らない。glogx の tick で Advance を呼び、Busy (走査・網の読み込み・相手の確認・確認待ち・裏の後始末・名前の引き直し)
// の間だけ spinnerActive が tick を回す。
// 走査が落ち着いた後に見つかった Mac は Changed の合図で取り込む (Busy の間しか tick を張らないため)。

import (
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"

	"glogx/internal/app"
	"glogx/internal/crosskey"
	"glogx/internal/extcmd"
	"glogx/internal/term"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/live"
)

type lanAction int

const (
	LanSwallow lanAction = iota // lanmgr がキーを使った (裏の一覧へは流さない)
	LanClosed                   // L / q / Esc で閉じた (git log 一覧へ戻る。ガイド §1: q は 1 段戻る)
	LanCross                    // 別の全画面へ横断 (閉じ済み。行き先は takeWantCross)
	LanExec                     // s / o などプロセスを起こす (中身は TakeExec)
)

// lanOptions は本物へのつなぎ (テストは走査とネットワークの読み取りを偽物に差し替える。tui_helpers_test.go の TestMain)。
var lanOptions = live.Options

// newLanModel は板の中身を作る。クリップボードと時計は glogx の口を使う (テストの testenv.StubClipboard と時計の差し替えに従う)。
func newLanModel() (*lanmgr.Model, error) {
	o, err := lanOptions()
	if err != nil {
		return nil, err
	}
	o.Copy = func(s string) error { return extcmd.CopyToClipboard(s) } // glogx のクリップボードの口 (テストは testenv.StubClipboard で差し替える)
	o.Now = func() time.Time { return app.TimeNow() }                  // 呼ぶたびに読む (値をつかむとテストが差し替えた時計に追従しない)
	return lanmgr.New(o), nil
}

// lanChangedMsg は走査の結果が変わった合図。ch はどのチャネルからか (ok=false はそのチャネルが閉じた)。
type LanChangedMsg struct {
	ch <-chan struct{}
	ok bool
}

// Open は合図のチャネルがまだ開いているか (閉じた = 画面を閉じた・走査を止めた。取り込まずに捨てる)。
func (m LanChangedMsg) Open() bool { return m.ok }

func waitLanChange(ch <-chan struct{}) tea.Cmd {
	if ch == nil {
		return nil // 走査が無い (待っても来ない)
	}
	return func() tea.Msg {
		_, ok := <-ch
		return LanChangedMsg{ch: ch, ok: ok}
	}
}

type LanView struct {
	watching  <-chan struct{} // 今待っている走査の合図のチャネル (走査が替わったら待ち直す)
	shown     bool
	m         *lanmgr.Model
	wantCross string
	openErr   string
}

func (v *LanView) Visible() bool { return v.shown }

// DrainNotices は見えている間に溜まった知らせを deliver へ渡す (閉じていれば何もしない)。押したキーが効かなかった理由は
// NoticeRefused にする (lastWarning を汚さない)。
// 🚨 呼ぶ側はキーの後だけでなく Advance の後と開いたときにも呼ぶ: 走査の結果や読み込みの失敗 (台帳が読めない等) で積まれる知らせがある。
func (v *LanView) DrainNotices(deliver func(text string, kind app.NoticeKind) bool) {
	if v.openErr != "" { // 開けなかった (見えていないので live は nil)
		deliver(v.openErr, app.NoticeError)
		v.openErr = ""
	}
	lm := v.live()
	if lm == nil {
		return
	}
	for _, n := range lm.TakeNotices() {
		kind := app.NoticeRefused
		if n.OK {
			kind = app.NoticeOK
		}
		deliver(n.Text, kind)
	}
}

// RestorePlace は起動時の復元で、開いた画面を記憶の場所へ戻す (閉じていれば何もしない)。
func (v *LanView) RestorePlace(p lanmgr.Place) {
	if lm := v.live(); lm != nil {
		lm.RestorePlace(p)
	}
}

// live は見えている間の画面 (閉じていれば nil)。
func (v *LanView) live() *lanmgr.Model {
	if !v.shown {
		return nil
	}
	return v.m
}

// toggle は開く / 閉じる。2 回目からは前の状態 (タブ・カーソル) を使い、ネットワーク・台帳・設定を読み直して走査を起こし直す
// (閉じている間に Mac が増えた・消えた・台帳を単体の bin/mac-lan-manager で書き換えた)。
func (v *LanView) Toggle() {
	if v.shown {
		v.hide()
		return
	}
	if v.m == nil {
		m, err := newLanModel()
		if err != nil {
			v.openErr = "mac-lan-manager を開けません: " + app.FirstLine(err.Error())
			return
		}
		v.m = m
	} else {
		v.m.Reload()
	}
	v.shown = true
}

// hide は閉じる。走査 (dns-sd) も止める (閉じた後も子プロセスを走らせ続けない)。
func (v *LanView) hide() {
	v.shown = false
	v.watching = nil
	v.Stop()
}

// stop は走査を止める (cancelAll からも呼ぶ。終了・再起動で dns-sd を残さない)。
func (v *LanView) Stop() {
	if v.m != nil {
		v.m.Close()
	}
}

// watchCmd は走査の合図の待ちを、今の走査のチャネルに張る (まだ張っていなければ)。開いた直後と、キーの後に呼ぶ:
// r (読み直し) は板の中で走査を起こし直すので、開いたときに張った待ちは古いチャネルを見ている。
// 古いチャネルは走査の Close で閉じるので、古い待ちは ok=false で抜ける。
func (v *LanView) WatchCmd() tea.Cmd {
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
func (v *LanView) Rearm(msg LanChangedMsg) tea.Cmd {
	if !msg.ok || v.live() == nil || msg.ch != v.watching {
		return nil
	}
	return waitLanChange(msg.ch)
}

// ownsKeys は lanmgr が全部のキーを持つ層 (入力欄・確認・板・設定) を開いているか (overlayOwnershipTable)。
func (v *LanView) OwnsKeys() bool { m := v.live(); return m != nil && m.OwnsKeys() }

// binds は key が lanmgr の意味を持つか (持つキーは glogx の横断キー・C の update・U に取らない。lanmgr.Model.Binds が正本)。
func (v *LanView) Binds(key string) bool { m := v.live(); return m != nil && m.Binds(key) }

func (v *LanView) Paste(text string) {
	if m := v.live(); m != nil {
		m.Paste(text)
	}
}

func (v *LanView) CaretPos() (x, y int, ok bool) {
	if m := v.live(); m != nil {
		return m.CaretPos()
	}
	return 0, 0, false
}

// busy は Bonjour の走査が続いているか (その間は spinnerActive が tick を回し、途中の結果を取り込む)。
func (v *LanView) Busy() bool { m := v.live(); return m != nil && m.Busy() }

func (v *LanView) Advance(now time.Time) {
	if m := v.live(); m != nil {
		m.Advance(now)
	}
}

// handleKey は lanmgr が飲むキー。L で閉じ、横断キー (判定は crosskey.Is、行き先は main の CrossTarget) で他の全画面へ移る。lanmgr が意味を持つキー (Binds) は
// 横断しない: s は ssh、入力中・確認・設定は全部のキー (横断で閉じると打っている文字が消える)。
func (v *LanView) HandleKey(key string) lanAction {
	if !v.m.Binds(key) {
		if key == "L" {
			v.hide()
			return LanClosed
		}
		if crosskey.Is(key) && key != "L" {
			v.hide()
			v.wantCross = key
			return LanCross
		}
	}
	switch v.m.HandleKey(key) {
	case lanmgr.Quit:
		v.hide()
		return LanClosed
	case lanmgr.Exec:
		return LanExec
	case lanmgr.None:
	}
	return LanSwallow
}

func (v *LanView) TakeWantCross() string {
	want := v.wantCross
	v.wantCross = ""
	return want
}

// lines は page 行の画面 (最下行の手前に lanmgr 自身のキーの案内がある)。
func (v *LanView) Lines(width, page int) []string {
	v.m.Resize(width, page)
	return v.m.View()
}

// hint は最下行の案内。抜ける手段 (L / q) を先に置き、幅で切る (glogx-ui-guide §5: 抜ける手段は必ず残す)。
func (v *LanView) Hint(width int) string {
	// 層 (初期設定・設定・確認の板・入力欄) が開いている間は L も ? も lanmgr が取り、q / esc は 1 段戻るだけ (Binds)。そのときに
	// 「L/q: 閉じる」を出すと嘘になった (整合の監査)
	// ただし足りない画面では q / esc が L ごと閉じ、L も glogx に渡る (Binds("L") が偽) ので「L/q: 閉じる」が正しい
	if lm := v.live(); lm != nil && lm.OwnsKeys() && lm.Binds("L") {
		return term.FitHintItems(width, []term.HintItem{{Text: "esc: 戻る", Prio: 1}})
	}
	return term.FitHintItems(width, []term.HintItem{
		{Text: "L/q: 閉じる", Prio: 1},
		{Text: "?: キーの一覧", Prio: 2},
	})
}

// lanExecCommand は mac-lan-manager の s (ssh) / o (画面共有) が頼んだプロセス。返した Cmd は extcmd.RunEditorCmd (tea.Exec + foreground.Exec) が
// 前景で起動する。免除の理由は extcmd.EditorCommand と同じ (ssh はパスワードとホスト鍵の確認に端末が要る。ctx が無く、端末を継承するのでパイプも
// copy goroutine も作らない)。
func lanExecCommand(r lanmgr.ExecRequest) *exec.Cmd {
	argv := r.RunArgv()                       // 初期設定の手順は、端末を渡す前に板を出す
	return exec.Command(argv[0], argv[1:]...) // subproc: no-waitdelay — 前景・ctx 無し・パイプ無し
}

// TakeExecCmd は s / o / 初期設定の手順が頼んだプロセスを前景で起こす Cmd (頼みが無ければ nil)。戻ると extcmd.EditorClosedMsg が
// 届くので、受け手は ExecReturned を呼ぶ。
func (v *LanView) TakeExecCmd() tea.Cmd {
	if v.m == nil {
		return nil
	}
	r, ok := v.m.TakeExec()
	if !ok || len(r.Argv) == 0 {
		return nil
	}
	return extcmd.RunEditorCmd(extcmd.OriginLan, lanExecCommand(r)) // 戻ると extcmd.EditorClosedMsg で ExecDone を呼ぶ
}

// ExecReturned はこの画面が起こした前景のプロセス (extcmd.OriginLan) から戻ったときに呼ぶ。返すのは起こせなかったときの
// 知らせ (ssh の終了コードは相手のシェルのものなので知らせない)。
func (v *LanView) ExecReturned(err error) string {
	if lm := v.live(); lm != nil {
		return lm.ExecDone(err)
	}
	return ""
}
