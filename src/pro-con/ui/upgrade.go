package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"tuikit/anim"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/upgrade"
)

// ライブアップグレード (upgrade package)。3 秒ごとに shim へ「ビルドが要るか」を尋ね (要れば shim が裏でビルドする)、
// バイナリが差し替わったら通知して ctrl+r で切り替える (勝手に切り替えない: 入力中や確認中に入れ替わると、
// 打っている途中の文字が迷子になる)。ビルドの失敗は shim が記録し (.autobuild.failed)、旧版のまま続ける。

const upgradeInterval = 3 * time.Second

type upgradeState int

const (
	upNone     upgradeState = iota // 新版なし
	upBuilding                     // shim が裏でビルド中
	upReady                        // 新版あり (ctrl+r で切り替え)
	upFailed                       // ビルドに失敗 (旧版のまま)
)

type upgrader struct {
	src       upgrade.Source
	start     os.FileInfo // 起動したときのバイナリ (これと違うファイルになったら新版)
	lastSpawn time.Time   // 最後に shim が裏ビルドを起動した時刻。失敗の記録はこれより後のものだけを見る
	run       upgrade.Runner
	state     upgradeState
	requested bool
	checkErr  string // shim に尋ねられなかった / バイナリを見られなかった理由 (同じ理由は繰り返し通知しない。回復したら忘れる)
}

type upgradeTickMsg struct{}

type upgradeCheckMsg struct {
	replaced, failed, spawned bool
	at                        time.Time // 裏ビルドを起動した時刻 (spawned のとき)
	err                       error     // バイナリを見られない (差し替えの途中等。次の周期で見直す)
	spawnErr                  error     // shim に尋ねられない (zsh が無い・timeout)
}

// EnableUpgrade はライブアップグレードを有効にする。exe がソースのディレクトリの中に無ければ (upgrade.Detect) 無効のまま。
func (m *Model) EnableUpgrade(exe string, run upgrade.Runner) error {
	src, err := upgrade.Detect(exe)
	if err != nil {
		return err
	}
	info, err := os.Stat(src.Exe)
	if err != nil {
		return err
	}
	// 起動したバイナリより新しい失敗の記録 = 動いている版より新しいソースがビルドに落ちている (起動前の失敗も出す)
	m.up = &upgrader{src: src, start: info, lastSpawn: info.ModTime(), run: run}
	return nil
}

// UpgradeRequested は ctrl+r で切り替えを頼まれて終了したか (main がそれを見て exec する)。
func (m *Model) UpgradeRequested() bool { return m.up != nil && m.up.requested }

// UpgradeExe は切り替え先のバイナリ。
func (m *Model) UpgradeExe() string { return m.up.src.Exe }

// UpgradeFailed は切り替え (exec) が失敗して戻ってきたときに呼ぶ。旧版のまま続ける。
func (m *Model) UpgradeFailed(err error) {
	m.up.requested = false
	if err == nil {
		err = errors.New("exec が戻ってきた")
	}
	m.fail("新版への切り替えに失敗した (旧版のまま続ける): " + err.Error())
}

func upgradeTick() tea.Cmd {
	return tea.Tick(upgradeInterval, func(time.Time) tea.Msg { return upgradeTickMsg{} })
}

// checkUpgrade は裏で「差し替わったか」を見て、まだなら毎回 shim に尋ねる (再挑戦するかは shim が決める。
// 失敗の記録を見て尋ねるのをやめると、ソースを直しても二度とビルドされない: 敵対レビュー 2 周目 P1)。
// 失敗と表示するのは、最後に頼んだビルドより後に失敗が記録されたときだけ。
func (m *Model) checkUpgrade() tea.Cmd {
	u := *m.up
	return m.child(func() tea.Msg {
		replaced, err := u.src.Replaced(u.start)
		if err != nil || replaced {
			return upgradeCheckMsg{replaced: replaced, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		at := time.Now()
		spawned, spawnErr := u.src.Spawn(ctx, u.run)
		if spawned {
			return upgradeCheckMsg{spawned: true, at: at}
		}
		return upgradeCheckMsg{failed: u.src.FailedSince(u.lastSpawn), spawnErr: spawnErr}
	})
}

func (m *Model) onUpgradeCheck(msg upgradeCheckMsg) tea.Cmd {
	u := m.up
	if msg.err == nil && msg.spawnErr == nil {
		u.checkErr = "" // 回復した: 次に失敗したらまた知らせる
	}
	switch {
	case msg.err != nil: // バイナリが見えない (差し替えの途中なら次の周期で戻る。消えたまま = clean 等は知らせる)
		if e := "バイナリを見られない (bin/pro-con で起動し直すとビルドされる): " + msg.err.Error(); u.checkErr != e {
			m.fail("新版の確認ができない: " + e)
			u.checkErr = e
		}
	case msg.replaced:
		if u.state != upReady {
			m.done("新版ができた: ctrl+r で切り替える (カードも UI の状態も引き継ぐ)")
		}
		u.state = upReady
	case msg.spawned:
		u.state, u.lastSpawn = upBuilding, msg.at
	case msg.failed:
		if u.state != upFailed {
			m.fail("新版のビルドに失敗した (旧版のまま続ける): " + u.src.LogPath())
		}
		u.state = upFailed
	case msg.spawnErr != nil:
		if e := msg.spawnErr.Error(); u.checkErr != e {
			m.fail("新版の確認ができない: " + e)
			u.checkErr = e
		}
	}
	return upgradeTick()
}

// requestUpgrade は ctrl+r。新版があるときだけ終了して main に切り替えを任せる。
func (m *Model) requestUpgrade() tea.Cmd {
	switch {
	case m.up == nil:
		m.refuse("ライブアップグレードは無効 (bin/pro-con = ソースのディレクトリから起動していない)")
	case m.up.state != upReady:
		m.info("新版はまだ無い")
	default:
		m.up.requested = true
		return tea.Quit
	}
	return nil
}

// upgradeSummary はゲージに出す 1 項目 ("" なら出さない)。
func (m *Model) upgradeSummary() string {
	if m.up == nil {
		return ""
	}
	switch m.up.state {
	case upBuilding:
		return "新版をビルド中"
	case upReady:
		return sgrYellow + "新版あり ctrl+r" + sgrFgReset
	case upFailed:
		return sgrRed + "新版のビルド失敗" + sgrFgReset
	case upNone:
	}
	return ""
}

// child は裏で外部コマンドを起こす処理 (tea.Cmd) を包み、走っている数を数える。数え始めは Cmd の関数が
// 実際に走り始めたとき (作ったときに数えると、終了の直前に作られて bubbletea に捨てられた Cmd が永久に数に残り、
// 以後の切り替えが毎回待ち切れなくなる: 敵対レビュー 2 周目 P2)。exec の前に WaitChildren で 0 を待つ
// (syscall.Exec はプロセス像を置き換えるので、待たないと走行中の子と ctx の見張りが消えて取り残される。glogx issue 211)。
func (m *Model) child(f func() tea.Msg) tea.Cmd {
	n := m.children
	return func() tea.Msg {
		n.Add(1)
		defer n.Add(-1)
		return f()
	}
}

// WaitChildren は走っている裏の処理が 0 になるのを待つ。timeout を過ぎたら false (待ち切れなかった)。
func (m *Model) WaitChildren(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for m.children.Load() > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond) // 条件のポーリングの刻み (待ちの実体は数)
	}
	return true
}

// SwitchWait は切り替えの前に裏の処理 (claude agents / shim への問い合わせ) の終わりを待つ上限。
const SwitchWait = 5 * time.Second

// PrepareSwitch は切り替え (exec) の前の後始末と、引き継ぐ UI の状態の書き出し。裏の処理が終わらなければ
// 書き出さずにエラー (exec すると走行中の子と ctx の見張りが消えて取り残される。glogx issue 211)。
func (m *Model) PrepareSwitch(wait time.Duration) ([]byte, error) {
	if !m.WaitChildren(wait) {
		return nil, fmt.Errorf("裏の処理が %v 以内に終わらない (もう一度 ctrl+r)", wait)
	}
	return m.ExportState()
}

// inputTargetChanged は書きかけの入力の宛先が入れ替えの前と変わったか。変わっていたら理由 (戻すと別の宛先へ送られる)。
func (m *Model) inputTargetChanged(st uiState) string {
	switch st.InputKind {
	case inputNew: // 宛先はタブの repo
		if m.tab != st.Tab {
			return "宛先のタブ (" + orDash(st.Tab) + ") が無くなった"
		}
	case inputIssue: // 宛先は選んだ issue (状態に入っている)
	case inputQuit: // 引き継がない (ExportState。入れ替えの後に終了しない)
	case inputAnswer, inputOrder, inputBtw: // 宛先は選択中のカード
		if m.selected != st.Selected {
			return "宛先のカード (" + st.Selected + ") が無くなった"
		}
		if c, _ := m.selectedCard(); st.InputKind == inputAnswer && !c.Answerable() {
			return "宛先のカード (" + st.Selected + ") がもう質問待ちでない"
		}
	}
	return ""
}

// uiState は入れ替えの前後で引き継ぐ UI の状態。確認中 (y/N) は引き継がない (入れ替えの後に破壊的な操作を
// 勝手に確定させない)。issue の一覧は閉じて引き継ぐ (開き直せば読み直す)。
type uiState struct {
	Tab          string               `json:"tab"`
	Col          int                  `json:"col"`
	Selected     string               `json:"selected"`
	ShowDetail   bool                 `json:"showDetail"`
	ShowSessions bool                 `json:"showSessions"`
	Input        bool                 `json:"input"`
	InputKind    inputKind            `json:"inputKind"`
	OrderKind    card.OrderKind       `json:"orderKind"`
	Line         string               `json:"line"`
	Cursor       int                  `json:"cursor"`
	Target       *backend.IssueTarget `json:"target,omitempty"`
	TargetRepo   backend.Repo         `json:"targetRepo"`
}

// ExportState は引き継ぐ UI の状態。
func (m *Model) ExportState() ([]byte, error) {
	return json.Marshal(uiState{Tab: m.tab, Col: m.col, Selected: m.selected, ShowDetail: m.showDetail, ShowSessions: m.showSessions,
		Input: m.mode == modeInput && m.inputKind != inputQuit, InputKind: m.inputKind, OrderKind: m.orderKind, Line: m.line.String(), Cursor: m.line.Cursor(),
		Target: m.picker.target, TargetRepo: m.picker.repo})
}

// ImportState は ExportState した UI の状態を戻す。読めなければ何も変えない (UI の状態を失うだけで、カードは backend にある)。
func (m *Model) ImportState(data []byte) error {
	var st uiState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	m.tab, m.col, m.selected = st.Tab, st.Col, st.Selected
	m.showDetail, m.showSessions = st.ShowDetail, st.ShowSessions
	if m.showDetail { // 引き出しは開いた状態から (切り替えの前後で演出を挟まない)
		m.drawer, m.drawerCard = anim.NewOpen(), m.selected
	}
	m.ensureTab()
	m.ensureSelection()
	m.done("新版に切り替えた (UI の状態とカードを引き継いだ)")
	// 書きかけの入力は宛先のカードが同じときだけ戻す (カードが無くなっていたら、別のカードへ送られないよう戻さない。
	// 書いた文は失わないよう通知に出す)。新しい依頼 (n) と issue からの依頼はカードに依らない
	if st.Input {
		if why := m.inputTargetChanged(st); why != "" {
			m.sticky = "書きかけの入力は" + why + "ので戻さなかった。書いた文: " + st.Line + "  (esc で消す)"
			return nil
		}
	}
	if st.Input {
		m.startInput(st.InputKind)
		m.orderKind = st.OrderKind
		m.picker.target, m.picker.repo = st.Target, st.TargetRepo
		m.line.Insert(st.Line)
		for back := m.line.Cursor() - max(0, st.Cursor); back > 0; back-- { // 範囲外のカーソルは端に寄せる
			m.line.Key("left", "")
		}
	}
	return nil
}
