package main

// 全画面 ratelimit ダッシュボード (R キー)。右上の usage オーバーレイ (U) と同じ Snapshot を、
// 枠ごとのアナログ盤にして画面いっぱいに描く。
//
// 🚨 データの取得経路はこの型が持たない。取得・キャッシュ・1 分ごとのリフレッシュ・quit 時の
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
		// 🚨 snap があっても描く枠が 0 件になりうる (RenderDashboard は nil を返す)。
		// Claude 側は既定の枠ラベル ("5h" / "7d") でしか拾わないので、/usage の文言が
		// 変わった環境では「パースは成功しているが描く枠が無い」が起こる。無言の白画面に
		// せず理由を出す (全画面なので、ユーザーには壊れたようにしか見えない)。
		if rows := usage.RenderDashboard(o.snap, o.now, o.width, body, o.colored); rows != nil {
			return padTo(append(head, rows...), o.page)
		}
		return padTo(append(head, centerLine("表示できる利用枠がありません", o.width)), o.page)
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
	return "r: 今すぐ更新  i: issues  s: status  R/q/esc/h: 閉じる  (毎分自動更新)"
}

// rlDashAction は handleKey の結果。閉じ→開きの連携 (横断) は viewer 単体では完結しないので、
// issuesView.wantStatus / statusView.wantIssues と同じ語彙で browseModel へ返す。
type rlDashAction int

const (
	// rlDashSwallow = 握り潰す。全画面なので裏の一覧へ素通りさせない (見えないままスクロールし、
	// 閉じたときにカーソルが移動している状態になる)。
	rlDashSwallow rlDashAction = iota
	rlDashClosed               // 閉じた (裏の画面へ戻る)
	rlDashRefresh              // 今すぐ取り直す (取得は browseModel が起こす)
	rlDashIssues               // issues viewer へ横断 (このダッシュボードは閉じ済み)
	rlDashStatus               // status viewer へ横断 (同上)
)

// handleKey はダッシュボードが飲むキー。
//
// i / s は issues / status viewer への横断 (ユーザー要望 2026-09-01)。viewer 側の R と対で、
// 全画面どうしを往復できる。🚨 横断でも自分は必ず閉じる: 全画面は同時に 1 枚の前提で、
// 重ねると「見えている画面」と「キーを受ける画面」が食い違う (issues ↔ status と同じ作法)。
func (d *ratelimitDash) handleKey(key string) rlDashAction {
	switch key {
	case "R", "q", "esc", "h", "left":
		d.close()
		return rlDashClosed
	case "r":
		return rlDashRefresh
	case "i":
		d.close()
		return rlDashIssues
	case "s":
		d.close()
		return rlDashStatus
	}
	return rlDashSwallow
}

// centerLine は幅 w の中で s を中央寄せする (左余白のみ)。
//
// 🚨 入らないときは切り詰める。そのまま返すと見出し (CLI 名 + バージョン 2 つで 49 桁固定) が
// 狭い端末で width を超え、フレームが自動 OFF になる帯 (frameMinWidth 未満) ではクリップも
// 効かないので折り返して画面が崩れる (セルフレビュー指摘 2026-09-01)。
func centerLine(s string, w int) string {
	pad := w - dispWidth(s)
	if pad <= 0 {
		return clipToWidth(s, w)
	}
	return padSpaces(pad/2) + s
}
