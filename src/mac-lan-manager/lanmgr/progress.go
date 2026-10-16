package lanmgr

// 初期設定の進捗の見せ方 (ユーザー報告 2026-10-16):
// - 手順のプロセスから戻った後、その結果を確かめている間 (相手とこの Mac を読み直す) は進捗のモーダルを出す (何待ちか分からなかった)
// - 読み込み中に押した x は断らず、読み終えたら次の手順の板を出す (断る toast がうるさかった)
// - 端末を渡す前に、最初の UI と同じ枠の板を出す (渡すと TUI の枠が消えて素のプロンプトだけになった。ExecRequest.RunArgv)

import (
	"fmt"
	"strings"
	"time"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spin は今の時刻の回る印 (tick ごとに進む。Busy の間は呼び出し側が tick を刻む)。
func (m *Model) spin() string { return spinFrames[(m.now.UnixMilli()/100)%int64(len(spinFrames))] }

// setupBusy は初期設定の画面が読み込みを待っているか (相手の観測・この Mac の網)。
func (m *Model) setupBusy() bool { return m.su != nil && (m.su.ch != nil || m.netCh != nil) }

// progressShown は進捗のモーダルを出すか (板が開いていない・確かめている手順か予約した x があり、まだ読み込み中)。
func (m *Model) progressShown() bool {
	return m.sc == scSetup && m.ov == ovNone && m.su != nil && (m.su.after != "" || m.su.queuedX) && m.setupBusy()
}

// settleProgress は読み込みが終わっていたら進捗を閉じ、予約した x を走らせる (Advance とキーの入口で呼ぶ)。
func (m *Model) settleProgress() {
	if m.su == nil || m.setupBusy() {
		return
	}
	m.su.after = ""
	if m.su.queuedX {
		m.su.queuedX = false
		if m.ov == ovNone {
			m.setupX()
		}
	}
}

// stepTitle は手順の名前 (計画の手順から引く。名前を写さない)。
func (m *Model) stepTitle(id string) string {
	for _, s := range m.setupSteps() {
		if string(s.ID) == id {
			return s.Title
		}
	}
	if id == string(stepConfirm) {
		return "新しい IP で入り直して確認する"
	}
	return "手順"
}

// progressBox は進捗のモーダル。読んでいるものごとに回る印か済みの印を出す。
func (m *Model) progressBox() string {
	su := m.su
	title := "相手を読んでいる"
	if su.after != "" {
		title = su.after + " — 結果を確かめている"
	}
	line := func(busy bool, what string) string {
		if busy {
			return sAccent.Render(m.spin()) + " " + sText.Render(what)
		}
		return sOK.Render("✓") + " " + sDim.Render(what)
	}
	body := []string{
		line(su.ch != nil, "相手 "+su.label+" を ssh で読む (鍵で入れるか・ネットに繋がっていない有線の設定)"),
	}
	if m.netCh != nil || su.after != "" {
		body = append(body, line(m.netCh != nil, "この Mac の網を読む"))
	}
	el := int(m.now.Sub(su.waitSince) / time.Second)
	body = append(body, "", sDim.Render(fmt.Sprintf("%d 秒", max(el, 0))))
	if su.queuedX {
		body = append(body, sDim.Render("読み終えたら次の手順の確認を出す"))
	}
	return modal(title, body, "esc 閉じる (読み込みは裏で続く)", m.w)
}

// execBanner は端末を渡す前に出す板 (確認の板と同じ枠。題・何を聞かれるか・戻り方)。
func (m *Model) execBanner(a *ask) string {
	var body []string
	for _, l := range a.body {
		if !strings.HasPrefix(l, "$ ") {
			body = append(body, l)
		}
	}
	body = append(body, "", "ここから下はコマンドの画面。終わると mac-lan-manager に戻る")
	return modal(a.title, body, "パスワードや確認はこの下で聞かれる", min(max(m.w, 40), 110)) // 描いた枠に幅を掛け直すと折り返して崩れた
}
