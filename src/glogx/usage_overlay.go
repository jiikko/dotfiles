package main

import (
	"context"
	"strings"
	"time"

	"glogx/usage"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// usageMsg は /usage の非同期取得結果 (右上オーバーレイ用)。
type usageMsg struct {
	snap *usage.Snapshot
	err  error
}

// usageOverlay は Claude Code の /usage 残量を右上に重ねるオーバーレイの状態と描画。
// browseModel から usage の関心事 (状態 + fetch/toggle/render) を 1 つの型へ切り出した
// サブコンポーネント。取得ロジック自体は bubbletea 非依存の usage パッケージにあり、こちらは
// overlay の UI 状態機械 (bubbletea 結合のため glogx 側に置く)。browseModel は 1 フィールド
// (usageOv) だけを持ち、キー/メッセージ/描画をこの型へ委譲する。
type usageOverlay struct {
	visible bool            // 表示中か (起動時 true = 起動時グランス表示)
	snap    *usage.Snapshot // 取得済みの /usage スナップショット (nil = 取得中)
	err     error           // 取得失敗 (表示は "取得失敗" に落とす)
	// cancel は fetch 専用の cancel。quit で走行中の subprocess を中断する。browseModel の
	// CI fetch 用 cancel とは別立て: 共有すると CI fetch 完了時の defer cancel() が走行中の
	// usage fetch を巻き添えキャンセルして "取得失敗" に落ちる (レビュー指摘 2026-07-21)。
	cancel context.CancelFunc
}

// fetchCmd は Claude Code の /usage を非同期取得する tea.Cmd。LLM を呼ばない軽いローカル
// コマンド (~440ms・ゼロコスト) だが初期描画のクリティカルパスには乗せない。cancel を保持し、
// quit 時に走行中の subprocess を中断できるようにする (fast-quit での claude 子プロセスの
// オーファン化を防ぐ)。起動時に 1 回 + 以降 usageRefreshInterval ごとにバックグラウンド再取得
// で呼ばれる (U トグルは再 fetch しない)。定期リフレッシュ中も表示は last-good を保つ (handle 参照)。
func (o *usageOverlay) fetchCmd() tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	o.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		snap, err := usage.Fetch(ctx)
		return usageMsg{snap: snap, err: err}
	}
}

// handle は取得結果 (usageMsg) を格納する。
//
// 不変条件: 一度取れた usage 表示は、定期リフレッシュの一時的な失敗では失わない。既に
// スナップショットがある状態で失敗結果が来たら last-good を保持し "取得失敗" へ落とさない
// (1 分ごとの再取得が回線瞬断等でたまに転けても、右上の残量表示がチラつかない)。初回取得の
// 失敗 (snap 未取得) はそのままエラー表示する。リフレッシュ成功は last-good を新値へ置き換え、
// 初回失敗からの回復 (err クリア) も担う。
func (o *usageOverlay) handle(msg usageMsg) {
	if msg.err != nil && o.snap != nil {
		return // 定期リフレッシュの一時失敗: last-good を保持し表示を崩さない
	}
	o.snap = msg.snap
	o.err = msg.err
}

// toggle は U キーで表示/非表示を反転する。
func (o *usageOverlay) toggle() { o.visible = !o.visible }

// dismiss は任意のナビゲーションキーで起動時グランス表示を引っ込める。
func (o *usageOverlay) dismiss() { o.visible = false }

// loading は取得待ち (spinner を回す) かどうか。表示中かつ結果未着 (snap も err も無い) の
// ときだけ true。これが true の間だけ tick を回してスピナーを animate する。
func (o *usageOverlay) loading() bool {
	return o.visible && o.snap == nil && o.err == nil
}

// stop は quit 時に走行中の usage fetch subprocess を cancel する (オーファン化防止)。
func (o *usageOverlay) stop() {
	if o.cancel != nil {
		o.cancel()
	}
}

// usageBoxChrome は影付き枠が内容幅に加える固定分 ("│ " + " │" + 影 1 桁 = 5)。
const usageBoxChrome = 5

// boxLines は右上オーバーレイの複数行モーダル (影付き枠) を組み立てる。非表示なら nil。
// 取得中は枠内でスピナー (呼び出し側が現在フレームを渡す) を回し、失敗時は理由、成功時は
// 枠ごとに 1 行整列表示する。spinner / colored / width は browseModel 側の状態を受け取る
// (この型は bubbletea の tick や端末幅を直接知らず、描画に必要な値だけを引数で受ける)。
func (o *usageOverlay) boxLines(width int, colored bool, spinner string) []string {
	if !o.visible {
		return nil
	}
	title := " Claude Code · usage "
	var rows []string
	switch {
	case o.err != nil:
		title = " usage "
		rows = []string{paint("取得失敗", ansiDim, colored)}
	case o.snap == nil:
		title = " usage "
		rows = []string{paint(spinner+" 取得中...", ansiDim, colored)}
	default:
		// CLI バージョンが取れていればタイトルに添える (取得失敗時は空で従来どおり)。
		if v := o.snap.Version; v != "" {
			title = " Claude Code v" + v + " · usage "
		}
		header, data := usage.RenderTable(o.snap, time.Now(), colored)
		// 区切り罫線は列内容の最大幅に合わせて引く (箱の inner 幅と一致させる)。ヘッダーは
		// 列見出し、罫線ともに dim。データ行はバーの色を活かすため素のまま。
		w := runewidth.StringWidth(stripANSI(header))
		for _, r := range data {
			w = max(w, runewidth.StringWidth(stripANSI(r)))
		}
		rows = append([]string{
			paint(header, ansiDim, colored),
			paint(strings.Repeat("─", w), ansiDim, colored),
		}, data...)
	}
	// 枠幅 = 内容の最大表示幅 + 罫線・影の余白。端末幅を超えない範囲で内容にフィットさせる。
	inner := 0
	for _, r := range rows {
		inner = max(inner, runewidth.StringWidth(stripANSI(r)))
	}
	boxWidth := min(inner+usageBoxChrome, width)
	return buildShadowPanelBox(title, rows, boxWidth, colored)
}

// overlayBoxTopRight は複数行の box をウィンドウ上部の右端へ矩形で重ねる (右揃え)。
// box の各行は buildPanelBox で幅が揃っているため、右端に清潔な長方形として載る。
// 覆われる各行の左側 (見えている部分) は truncateKeepANSI で色を保ったまま切り、境界で
// reset を挟んで開いた色/bg を閉じる (取得中に上部行の色が抜ける不具合の修正)。box 行自身の
// 色はそのまま活きる。
func overlayBoxTopRight(window, box []string, width int, colored bool) []string {
	if len(window) == 0 || width <= 0 || len(box) == 0 {
		return window
	}
	reset := ""
	if colored {
		reset = ansiReset // 左側の開いた色/bg を box の直前で閉じる
	}
	for i, row := range box {
		if i >= len(window) {
			break
		}
		bw := runewidth.StringWidth(stripANSI(row))
		if bw >= width {
			window[i] = clipToWidth(row, width)
			continue
		}
		leftWidth := width - bw
		left := truncateKeepANSI(window[i], leftWidth)
		pad := strings.Repeat(" ", max(leftWidth-runewidth.StringWidth(stripANSI(left)), 0))
		window[i] = left + reset + pad + row
	}
	return window
}
