package main

// 全画面 ratelimit ダッシュボード (R キー)。右上の usage オーバーレイ (U) と同じ Snapshot を、
// 枠ごとのアナログ盤にして画面いっぱいに描く。
//
// ⚠️ データの取得経路はこの型が持たない。取得・キャッシュ・1 分ごとのリフレッシュ・quit 時の
// subprocess 中断はすべて usageOverlay の配管をそのまま使う (tui.go の usageRefreshMsg が
// 「U が見えている」に加えて「R が見えている」でも fetch を回す)。同じ値に取得経路を 2 本
// 持たせると、周期・タイムアウト・single-flight ガードが二重管理になる。
//
// 盤の読み方と描画そのものは usage.RenderDashboard (bubbletea 非依存の純関数)。ここは
// 「開いているか」と「窓をどう埋めるか」だけを持つ。

import (
	"time"

	"glogx/usage"
)

// ratelimitDash は全画面ダッシュボードの表示状態。
type ratelimitDash struct {
	shown bool
}

func (d *ratelimitDash) visible() bool { return d.shown }
func (d *ratelimitDash) toggle()       { d.shown = !d.shown }
func (d *ratelimitDash) close()        { d.shown = false }

// ratelimitRenderOpts はダッシュボードの描画情報 (窓の寸法 + 色 + 描くデータ)。
type ratelimitRenderOpts struct {
	width   int
	page    int
	colored bool
	spinner string
	snap    *usage.Snapshot // nil = 未取得 (取得中の表示に落ちる)
	err     error           // 取得失敗 (snap があれば last-good を優先する)
	now     time.Time
}

// lines はダッシュボードをちょうど page 行で返す (全画面 viewer 共通の契約)。
func (d *ratelimitDash) lines(o ratelimitRenderOpts) []string {
	head := []string{d.headerLine(o), ""}
	body := max(o.page-len(head), 1)
	switch {
	case o.snap != nil:
		return padTo(append(head, usage.RenderDashboard(o.snap, o.now, o.width, body, o.colored)...), o.page)
	case o.err != nil:
		return padTo(append(head, centerLine("取得失敗", o.width)), o.page)
	default:
		return padTo(append(head, centerLine(o.spinner+" 取得中...", o.width)), o.page)
	}
}

// headerLine は CLI 名とバージョンの見出し。バージョンは外部バイナリの出力なので無害化して載せる。
func (d *ratelimitDash) headerLine(o ratelimitRenderOpts) string {
	title := "Claude Code"
	if o.snap != nil {
		if v := sanitizePlainLine(o.snap.Version); v != "" {
			title += " v" + v
		}
		if o.snap.HasCodex() {
			title += " + codex"
			if v := sanitizePlainLine(o.snap.CodexVersion); v != "" {
				title += " v" + v
			}
		}
	}
	return paint(centerLine(title+" · ratelimit", o.width), ansiDim, o.colored)
}

// hint は最下行のキー案内。
func (d *ratelimitDash) hint() string {
	return "r: 今すぐ更新  R/q/esc/h: 閉じる  (毎分自動更新)"
}

// handleKey はダッシュボードが飲むキー。戻り値 true = このキーをここで処理した。
// ⚠️ 「閉じる / 更新以外は握り潰す」を明示する: 全画面なので、素通りさせると裏の一覧が
// 見えないままスクロールし、閉じたときにカーソルが移動している。
func (d *ratelimitDash) handleKey(key string) (closed, refresh bool) {
	switch key {
	case "R", "q", "esc", "h", "left":
		d.close()
		return true, false
	case "r":
		return false, true
	}
	return false, false
}

// centerLine は幅 w の中で s を中央寄せする (左余白のみ)。
func centerLine(s string, w int) string {
	pad := w - dispWidth(s)
	if pad <= 0 {
		return s
	}
	return padSpaces(pad/2) + s
}
