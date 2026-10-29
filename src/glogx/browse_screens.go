package main

import (
	"errors"
	"os/exec"
	"time"

	"doctor/disk"
	"glogx/issues"

	"glogx/internal/extcmd"
	"glogx/internal/filerview"
	"glogx/internal/issuesview"
	"glogx/internal/lanview"
	"glogx/internal/ratelimitview"
	"glogx/internal/statusview"
	"glogx/internal/usageview"

	"glogx/internal/app"
	"glogx/internal/doctorview"

	tea "charm.land/bubbletea/v2"
)

// issuesRestoreMsg は前回終了時の issues viewer の画面を復元する合図 (起動時。issues_state.go)。
// repo の照合まで済んでいるので、受け取り側は開くだけ。
type issuesRestoreMsg struct{ screen issuesview.IssuesScreen }

// restoreScreensCmd は全画面を出したまま終了していたら、その画面を開き直す (各画面の *_resume.go)。
// 全画面は同時に 1 枚 (fullscreen.go) なので、同期で開ける画面を優先順に試し、開いた (fullScreenActive) ところで止める。
// 「Cmd が nil でない」を開いた印に使わない (開いても nil を返す画面があると、2 枚目を開く)。
// issues は最後: repo の照合 (git fork) を非同期で行い、着いたときに別の全画面が出ていれば issuesRestoreMsg のガードが捨てる。
// 照合の前に記憶があるだけで他の画面が譲ると、照合が外れたときに何も復元されない。
// 記憶は閉じていた画面のぶんを終了時に消すので、2 つ以上残るのは異常終了のときだけ。
func (m *browseModel) restoreScreensCmd(now time.Time) tea.Cmd {
	cmds := make([]tea.Cmd, 0, 5)
	for _, open := range []func() tea.Cmd{
		func() tea.Cmd {
			if ratelimitview.LoadRatelimitScreen(now) {
				return m.toggleRatelimitDash() // 取得の起こし方 (キャッシュ優先 / stale 判定) は R と同じ経路に委ねる
			}
			return nil
		},
		func() tea.Cmd { cmd, _ := m.doctorOv.RestoreScreen(now); return cmd },
		m.restoreFilerScreen, // 同じ dir で開いたときだけ (filer_resume.go)
		m.restoreLanScreen,
	} {
		cmds = append(cmds, open())
		if m.fullScreenActive() {
			return tea.Batch(cmds...)
		}
	}
	// ファイル読みだけ同期で済ませ、repo の照合は記憶があるときだけ非同期で行う (記憶が無い通常の起動では fork が増えない)
	if s, ok := issuesview.LoadIssuesScreen(now); ok {
		cmds = append(cmds, issuesRestoreCmd(s))
	}
	return tea.Batch(cmds...)
}

// issuesRestoreCmd は記憶した画面が今の repo のものか確かめる (別 repo で開いた glogx に
// 前の repo の issue を出さない)。一致しなければ nil Msg = 無音で通常起動。
func issuesRestoreCmd(s issuesview.IssuesScreen) tea.Cmd {
	return func() tea.Msg {
		if issues.RepoRoot(app.CurrentDir()) != s.Root {
			return nil
		}
		return issuesRestoreMsg{screen: s}
	}
}

func (m *browseModel) onFilerChanged(msg filerview.FilerChangedMsg) (tea.Model, tea.Cmd) {
	// treefiler のライブ更新の合図。取り込んで、今のチャネルからの合図なら次の合図を待ち直す
	if !msg.Open() {
		return m, nil
	}
	m.filerV.Advance(app.TimeNow())
	m.filerV.DrainNotices(m.deliverNotice)
	return m, tea.Batch(m.filerV.Rearm(msg), m.maybeTick())
}

func (m *browseModel) onLanChanged(msg lanview.LanChangedMsg) (tea.Model, tea.Cmd) {
	// mac-lan-manager の走査の合図。取り込んで、今のチャネルからの合図なら次の合図を待ち直す
	if !msg.Open() {
		return m, nil
	}
	m.lanV.Advance(app.TimeNow())
	m.lanV.DrainNotices(m.deliverNotice)
	return m, tea.Batch(m.lanV.Rearm(msg), m.maybeTick())
}

func (m *browseModel) onIssuesScan(msg issuesview.IssuesScanMsg) (tea.Model, tea.Cmd) {
	// 戻り値は畳まれていた取り直しの予約 (issuesview.IssuesView.Receive の doc)。捨てると
	// 「自分がファイルを動かしたのに一覧が古いまま」が残る
	cmd := m.issuesOv.Receive(msg)
	// receive → rebindOpen が置く notice (開いていた issue の消失) をこのフレームで配達する。
	// 打鍵経路の takeNotice に任せると、次のキーまで「本文が無言で畳まれた」に見え、
	// その次のキーが q だと理由が 1 度も描かれずに終了する。
	// maybeTick はトーストを出したときだけ束ねる (issuesWatchMsg の 1s チェーンの意図を崩さない)
	if m.deliverNotice(m.issuesOv.TakeNotice()) {
		return m, tea.Batch(cmd, m.maybeTick())
	}
	return m, cmd
}

func (m *browseModel) onEditorClosed(msg extcmd.EditorClosedMsg) (tea.Model, tea.Cmd) {
	if msg.ReclaimErr != nil {
		m.showWarning("端末の前面を取り戻せませんでした (" + app.FirstLine(msg.ReclaimErr.Error()) + ")。止まったら fg で戻してください")
	}
	// 後処理は起動元で決める (extcmd.ExecOrigin)。treefiler の ! / s / e、mac-lan-manager の s / o / 初期設定の手順から
	// 戻ったときの知らせる文は各画面の ExecReturned が決める (シェルの 0 以外の終了で「エディタが異常終了しました」を出さない)
	var w string
	switch msg.Origin {
	case extcmd.OriginFiler:
		w = m.filerV.ExecReturned(msg.Err)
	case extcmd.OriginLan:
		w = m.lanV.ExecReturned(msg.Err)
	case extcmd.OriginEditor:
	}
	if msg.Origin != extcmd.OriginEditor {
		if w != "" {
			m.showWarning(app.FirstLine(w))
		}
		return m, m.maybeTick()
	}
	// エディタを閉じて復帰。job ログは stdin 渡しなのでファイルは残らず、バッファも破棄済み。
	// 🚨 issues viewer の e (別名 v) だけは実ファイルを編集可能で開く (メモを足せるように
	// readonly にしていない) ので、復帰の境界で取り直す。取り直さないと編集結果 (H1・front matter の
	// status・チェックボックス) が一覧にも本文にも出ず、viewer が古い内容を最新として表示する。
	// 🚨 viewer 表示中でも IssuesView の通知 (TakeNotice で受け取る) へ回さない: 通知はどのヘッダーも描かず、
	// 次の打鍵で TakeNotice されるまで画面に出ない (キーを押すまで失敗が黙殺される)。
	// viewLines が viewer の窓にもトーストを合成するので、ここは常にトーストでよい。
	// 🚨 起動対象は $VISUAL/$EDITOR で変わる (extcmd.EditorCommand) ので、文言でツール名を名指し
	// しない。job ログ・repo root だけは nvim 固定だが、失敗の主因は可変側 (typo した
	// $EDITOR・PATH に無いエディタ) なので総称で出す。
	//
	// 🚨 「起動できなかった」と「起動できたが 0 以外で終了した」を分ける。後者 (nvim の :cq 等)
	// はエディタが実際に開いてファイルを保存できているので、reload を飛ばすと上の不変条件
	// (復帰の境界で取り直す) が破れ、保存済みの編集が出ないまま古い内容を最新として表示する。
	if extcmd.IsQuitRequest(msg.Err) {
		// エディタの <C-g> (glogx ごと閉じて)。保存は nvim 側が済ませている (保存できなければ抜けずに止まる)。
		// 終了が飲まれたとき (push 中など) は viewer に戻るので、取り直しも積む
		_, quitCmd := m.handleQuitKey("ctrl+g")
		return m, tea.Batch(m.issuesOv.ReloadAfterEdit(), quitCmd, m.maybeTick())
	}
	var exitErr *exec.ExitError
	switch {
	case msg.Err == nil:
	case errors.As(msg.Err, &exitErr):
		m.showWarning("エディタが異常終了しました: " + app.FirstLine(msg.Err.Error()))
	default:
		// 起動失敗はファイルが変わっていないので取り直さない
		m.showWarning("エディタを開けませんでした: " + app.FirstLine(msg.Err.Error()))
		return m, m.maybeTick()
	}
	return m, tea.Batch(m.issuesOv.ReloadAfterEdit(), m.maybeTick())
}

// routeKeyToDoctor は doctor 表示中のキー処理。全画面なのでキーはここで飲み切る
// (裏の一覧をスクロールさせない)。呼ぶのは handleKey の activeFullScreen dispatch 1 箇所で、
// **dispatch の位置とその理由はそちらに書く** (ここへ再掲しない)。
func (m *browseModel) routeKeyToDoctor(key string) (tea.Model, tea.Cmd) {
	// 描画中に選択行が消えて寄せていたら、**キーを飲まずに**知らせる。
	// カーソルの寄せは View から起きるので、通知経路はここしか無い
	if t := m.doctorOv.TakeCursorFellBack(); t != "" {
		m.toast.Show(t, false)
	}
	// U (usage) は doctor の上でも効く (issues / status と同じ契約)。usage の箱は
	// finishWithGlobalChrome がどの画面にも重ねるので「取得だけ走って画面に出ない」は起きない。
	// 🚨 doctor が自分でキーを解釈し切る状態 (削除の y/N 確認・実行中) では横取りごと飛ばして
	// 委譲へ落とす: 飛ばさないと確認中の U が箱を出し、確認は裏に残ったままになる
	// (issues の URL ピッカーで実際に起きた形)。
	if !m.doctorOv.OwnsKeys() {
		if key == "U" {
			return m, m.toggleUsage()
		}
		m.usageOv.Dismiss() // 他のキーで引っ込むのは一覧と同じ語彙 (U で出し、次のキーで消える)
	}
	switch m.doctorOv.HandleKey(key, m.pageSize()) {
	case doctorview.DoctorClosed:
		return m, m.maybeTick()
	case doctorview.DoctorRescan:
		// 再スキャンの理由がある場合は伝える (「前回の結果だったので取り直します」等)。
		// 🚨 黙って走らせない: ユーザーは Space / d を押したのであって r を押していない
		m.deliverNotice(m.doctorOv.TakeNotice())
		// close() を経由しない: 数件だけの partial を書いて完全な結果を潰さない (doctorview.DoctorView.saveCache の注記)
		return m, tea.Batch(m.doctorOv.Rescan(), m.maybeTick())
	case doctorview.DoctorCopyPath:
		m.copyWithToast(m.doctorOv.CopyPayload(), m.doctorOv.CopyNoun()+"をコピーしました")
	case doctorview.DoctorCopyText:
		m.copyWithToast(m.doctorOv.CopyPayload(), "解説をコピーしました (LLM にそのまま貼れます)")
	case doctorview.DoctorCopyLog:
		m.copyWithToast(m.doctorOv.CopyPayload(), "実行の記録をコピーしました (LLM にそのまま貼れます)")
	case doctorview.DoctorNothing:
		m.toast.Show("この行にはコピーするものがありません", false)
	case doctorview.DoctorToast:
		m.deliverNotice(m.doctorOv.TakeNotice())
	case doctorview.DoctorRunDelete:
		// 削除 (と、その下見) は doctorview.DoctorView が組んだ Cmd をそのまま走らせる
		return m, tea.Batch(m.doctorOv.TakeDeleteCmd(), m.maybeTick())
	case doctorview.DoctorCross:
		// 横断 (i / s / R)。doctor は handleKey が閉じ済み (演出なし) なので、そのまま開いてよい
		return m, m.openFullScreen(crossScreen(m.doctorOv.TakeWantCross()))
	case doctorview.DoctorSwallow:
	}
	return m, m.maybeTick()
}

// routeKeyToRatelimitDash は残量ダッシュボード表示中のキー処理 (doctor と同じく全画面)。
//
// 🚨 U (usage) は**意図的に受けない**。ダッシュボードは同じ Snapshot を全画面で描いたもので、
// 右上に小さい方を重ねると同じ値が 2 か所に出る (toggleRatelimitDash が開くときに
// usageOv.dismiss() しているのと同じ判断)。issues / status が U を受けるのとはここが違う。
func (m *browseModel) routeKeyToRatelimitDash(key string) (tea.Model, tea.Cmd) {
	switch m.rlDash.HandleKey(key) {
	case ratelimitview.RlDashClosed:
		return m, m.maybeTick()
	case ratelimitview.RlDashRefresh:
		return m, tea.Batch(m.usageOv.FetchNowCmd(), m.maybeTick())
	// i / s / D = 他の全画面へ横断 (viewer 側の R と対。ユーザー要望 2026-09-01)。handleKey が
	// 既に閉じているので、issues ↔ status の横断と同じく閉じ演出は待たずに即着地する。
	// 🚨 toggle を呼ぶので、ここへ来る時点で相手の viewer が開いていないこと (全画面は
	// 同時に 1 枚) が前提。開いていると toggle が「開く」でなく「閉じる」に化ける。起動時
	// 復元との競合はその前提を守るために issuesRestoreMsg 側で弾いている。
	//
	// 🚨 一覧の i/s が持つ `m.panelSHA == ""` ガードは、ここでは**意図的に付けない**。あのガードの理由は「job パネルを開いている間はそちらの
	// キー語彙を優先する」= キーの取り合いの解消で、ダッシュボードは全画面で全キーを飲む
	// ため取り合いが起きない。パネル (panelSHA / panelCursor / details) は viewer とは独立
	// した状態で、閉じれば元のパネルがそのまま描き直される。🚨 逆にガードを付けると、
	// パネルを開いたまま R → i が無音の no-op になる (禁じた形)。
	// パネル側の状態を viewer が読むようになったら、この判断を再評価すること。
	case ratelimitview.RlDashCross:
		return m, m.openFullScreen(crossScreen(m.rlDash.TakeWantCross()))
	case ratelimitview.RlDashSwallow:
		// 閉じる / 更新 / 横断以外は握り潰す (下の return へ落ちる)
	}
	return m, nil
}

// routeKeyToIssues は issues viewer 表示中のキー処理 (全画面なのでキーは全部 viewer が飲む)。
//
// U (usage) はここで受ける: viewLines が viewer の窓へ usage を合成するので開けてよい
// (「viewer が全画面で描かれないのに取得だけ走る」形にしない)。
func (m *browseModel) routeKeyToIssues(key string) (tea.Model, tea.Cmd) {
	// U は viewer の上でも効く (ユーザー要望 2026-08-01)。viewLines が usage を viewer の窓へ
	// 合成するので「取得だけ走って画面に出ない」問題は起きない (トーストと同じ経路)。
	//
	// 🚨 viewer が自分でキーを解釈し切る状態 (URL ピッカー / 番号入力 / y/N 確認) では、この
	// 横取りを飛ばして下の委譲へ落とす。飛ばさないと URL ピッカーが宣言している
	// 「印字文字はすべて検索語に流す」が大文字 U だけ破れる。
	// 🚨 ガードは横取りだけに掛けること (この if 自体に付けると委譲も飛ぶ)。
	if !m.issuesOv.OwnsKeys() {
		if key == "U" {
			return m, m.toggleUsage()
		}
		m.usageOv.Dismiss() // 他のキーで引っ込むのは一覧と同じ語彙 (U で出し、次のキーで消える)
	}
	cmd := m.issuesOv.HandleKey(key, m.issuesOpts().Viewport())
	// viewer の操作結果 (コピー・URL 起動・読み込み失敗) は glogx 共通の右下トーストで出す
	// (ユーザー要望 2026-07-31)。viewer が全画面でトーストが隠れていた時代はヘッダー行に
	// 出していたが、下の viewLines でトーストを viewer の上にも合成するようにした。
	m.deliverNotice(m.issuesOv.TakeNotice())
	// q/esc = glogx ごと終了 (ユーザー要望 2026-08-06: git log 一覧へは戻らない)。viewer を
	// 出したまま終了するので、次回起動は再開記憶で同じ画面から始まる (C-g と同じ経路)
	if m.issuesOv.TakeWantQuit() {
		return m.quit()
	}
	// 横断 (s / R / D。表は CrossTarget。ユーザー要望 2026-08-06 / 2026-09-01)。閉じる演出は待たず即着地させる:
	// 全画面 viewer は同時に 1 枚の前提で、閉じ演出と次の開き演出を重ねない
	if target := crossScreen(m.issuesOv.TakeWantCross()); target != FullScreenNone {
		m.issuesOv.FinishClose()
		return m, tea.Batch(cmd, m.openFullScreen(target))
	}
	return m, tea.Batch(cmd, m.maybeTick())
}

// routeKeyToStatus は status viewer 表示中のキー処理 (issues と同じ形。全画面)。
// p / b / u の扱いだけが issues と違う (docs/status-viewer-spec.md 3 節)。
func (m *browseModel) routeKeyToStatus(key string) (tea.Model, tea.Cmd) {
	// 🚨 viewer が自分でキーを解釈し切る状態 (全画面 pager / 破棄確認) では、この横取りを
	// まるごと飛ばして下の委譲へ落とす。飛ばさないと viewer のキー語彙を外側が奪い、
	// `b` (半ページ戻り) が push 確認に化けて続く Enter で実 push が走る (実測 2026-08-21)。
	// 🚨 ガードは横取りだけに掛けること: この if 自体に付けると委譲も飛んで pager が
	// キーを受け取れなくなる (実装中に踏んだ)。
	if !m.statusOv.OwnsKeys() {
		if key == "U" {
			return m, m.toggleUsage() // viewer の上でも usage は出せる (issues と同じ契約)
		}
		m.usageOv.Dismiss()
		if key == "p" {
			// pull は viewer の中からも打てる (ユーザー要望 2026-08-05)。🚨 一覧の u とキーを
			// 分けているのは spec 3 節の判断を残すため: staging 中に「隣のキー」で remote 操作へ
			// 滑るのを防ぎつつ、明示的に p を押したときだけ通す。確認 (y/N) は actModal が出す。
			m.actModal.askPull()
			return m, m.maybeTick()
		}
		if key == "b" {
			// push も viewer の中から打てる (ユーザー要望 2026-08-07)。p (pull) と同じく
			// 確認 (y/N) は actModal が viewer の上に重ねて出す。b は一覧と同じキーで、
			// staging の語彙 (j/k/Space/a/X/d…) と衝突しない
			return m, m.confirmPush()
		}
		if key == "u" {
			// u は一覧の pull キーだが、ここでは p を使う (誤爆しやすい隣接キーで remote を
			// 叩かせない。spec 3 節)。黙って無視すると「押したのに何も起きない」になるので理由を返す
			m.toast.Show("pull は p です (status viewer では u を使いません)", false)
			return m, m.maybeTick()
		}
	}
	cmd := m.statusOv.HandleKey(key, m.statusOpts().Viewport())
	m.deliverNotice(m.statusOv.TakeNotice())
	// q/esc = glogx ごと終了 (issues 側と同じ契約。ユーザー要望 2026-08-06)
	if m.statusOv.TakeWantQuit() {
		return m.quit()
	}
	// 横断 (i / R / D。issues 側と同じ経路で、即着地も同じ理由)
	if target := crossScreen(m.statusOv.TakeWantCross()); target != FullScreenNone {
		m.statusOv.FinishClose()
		return m, tea.Batch(cmd, m.openFullScreen(target))
	}
	return m, tea.Batch(cmd, m.maybeTick())
}

// issuesOpts は issues viewer へ渡す描画情報。カーソル行の強調はコミット一覧と同じ
// cursorLine (溝の矢印 + 暗青 bg) を貸すことで、見た目の語彙を 1 つに保つ。
// statusOpts は status viewer の描画情報。🚨 cursorPaint は渡さない (contentWidth まで塗る
// bgLine では 2 カラムのプレビュー側まで背景が伸びる。statusRenderOpts の doc 参照)。
func (m *browseModel) statusOpts() statusview.StatusRenderOpts {
	return statusview.StatusRenderOpts{
		Width:   m.contentWidth(),
		Page:    m.pageSize(),
		Colored: m.colored,
		Spinner: m.spinner(),
	}
}

// ratelimitOpts は全画面 ratelimit ダッシュボードの描画情報。データは usageOv の Snapshot を
// そのまま渡す (取得経路を 1 本に保つ。ratelimit_dashboard.go 冒頭)。
func (m *browseModel) doctorOpts() doctorview.DoctorRenderOpts {
	return doctorview.DoctorRenderOpts{Env: disk.RealEnv(), Width: m.contentWidth(), Page: m.pageSize(), Colored: m.colored, Spinner: m.spinner(), Now: app.TimeNow()}
}

func (m *browseModel) ratelimitOpts() ratelimitview.RatelimitRenderOpts {
	snap := m.usageOv.View(m.spinner())
	return ratelimitview.RatelimitRenderOpts{
		Width:    m.contentWidth(),
		Page:     m.pageSize(),
		Colored:  m.colored,
		Spinner:  m.spinner(),
		Snap:     snap,
		Err:      m.usageOv.Err(),
		StaleErr: m.usageOv.StaleErr(),
		Note:     usageview.FetchNote(snap, m.usageOv.StaleErr()),
		Waiting:  m.usageOv.WaitingNote(m.spinner()),
		Now:      app.TimeNow(),
	}
}

func (m *browseModel) issuesOpts() issuesview.IssuesRenderOpts {
	return issuesview.IssuesRenderOpts{
		Width:       m.contentWidth(),
		Page:        m.pageSize(),
		Colored:     m.colored,
		Spinner:     m.spinner(),
		CursorPaint: m.cursorEmphasis, // 溝は viewer が持つので強調だけ渡す (二重矢印の防止)
	}
}

// routeKeyToFiler は filer 表示中のキー処理 (全画面なのでキーは全部 filer が飲む)。
func (m *browseModel) routeKeyToFiler(key string) (tea.Model, tea.Cmd) {
	// U は他の viewer と同じく利用枠を重ねる (spec §0.3: R D U X は filer の中でも効かせる)
	if key == "U" && !m.filerV.Binds(key) {
		return m, m.toggleUsage()
	}
	act := m.filerV.HandleKey(key)
	m.filerV.DrainNotices(m.deliverNotice)
	switch act {
	case filerview.FilerQuit:
		// quit (記憶の保存) を hide より先に: hide の後だと「F を出していない」と判定されて記憶が消える
		mm, cmd := m.quit()
		m.filerV.Hide()
		return mm, cmd
	case filerview.FilerCross:
		return m, m.openFullScreen(crossScreen(m.filerV.TakeWantCross()))
	case filerview.FilerExec:
		if cmd := m.filerV.TakeExecCmd(); cmd != nil {
			return m, cmd
		}
	case filerview.FilerClosed, filerview.FilerSwallow:
	}
	return m, m.maybeTick()
}

// restoreFilerScreen は起動時の復元。他の全画面の復元が当たっていないときだけ呼ぶ (全画面は同時に 1 枚)。
func (m *browseModel) restoreFilerScreen() tea.Cmd {
	dir := app.CurrentDir()
	p, ok := filerview.LoadFilerScreen(app.TimeNow(), dir)
	if !ok {
		return nil
	}
	cmd := m.openFullScreen(FullScreenFiler)
	m.filerV.RestorePlace(p)
	return cmd
}

// routeKeyToLan は mac-lan-manager 表示中のキー処理 (全画面なのでキーは全部 lanmgr が飲む)。
// routeKeyToFiler と同じ形だが 1 本に寄せない: 返る合図の種類 (filerview.FilerAction / lanview.LanAction) と、抜けたときの後処理
// (filer は終了で記憶を保存、lan は監視を張り直す) が違い、寄せても違いを引数で渡すだけになる。
func (m *browseModel) routeKeyToLan(key string) (tea.Model, tea.Cmd) {
	if key == "U" && !m.lanV.Binds(key) {
		return m, m.toggleUsage() // 他の全画面と同じく利用枠を重ねる
	}
	act := m.lanV.HandleKey(key)
	m.lanV.DrainNotices(m.deliverNotice)
	switch act {
	case lanview.LanCross:
		return m, m.openFullScreen(crossScreen(m.lanV.TakeWantCross()))
	case lanview.LanExec:
		if cmd := m.lanV.TakeExecCmd(); cmd != nil {
			return m, cmd
		}
	case lanview.LanClosed, lanview.LanSwallow:
	}
	return m, tea.Batch(m.lanV.WatchCmd(), m.maybeTick())
}

// restoreLanScreen は起動時の復元。他の全画面の復元が当たっていないときだけ呼ぶ (全画面は同時に 1 枚)。
func (m *browseModel) restoreLanScreen() tea.Cmd {
	p, ok := lanview.LoadLanScreen(app.TimeNow())
	if !ok {
		return nil
	}
	cmd := m.openFullScreen(FullScreenLan)
	m.lanV.RestorePlace(p)
	return cmd
}
