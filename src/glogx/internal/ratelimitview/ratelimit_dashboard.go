// Package ratelimitview は glogx の全画面 ratelimit ダッシュボード (R) と、閉じたときの画面の記憶。
// 取得・整形は ratelimit/usage が持つ。
package ratelimitview

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

	"github.com/jiikko/dotfiles/src/tuikit/layout"

	"glogx/internal/app"
	"glogx/internal/crosskey"
	"glogx/internal/term"
	"ratelimit/usage"
)

// ratelimitDash は全画面ダッシュボードの表示状態。
type RatelimitDash struct {
	shown bool
	// wantCross は横断先 (rlDashCross を返したときだけ意味を持つ。takeWantCross で取り出す)
	wantCross string
	// 盤の描画結果のメモ。viewLines は毎 View で lines を呼び、
	// usage.RenderDashboard はセル数 (幅 x 高) に比例して braille のスライスを確保し直すので、
	// 12.5fps で回ると 1 フレーム 1065 allocs / 239KB (120x40 実測) がそのまま乗る。
	// 12.5fps で回るのは rlDashLoading() ではなく **spinnerActive() の他の項**
	// (len(awaitCI) > 0 = push 直後 / panelHasPendingJob() = CI 実行中) が真のとき。
	cache    []string
	cacheKey ratelimitCacheKey
	cacheOK  bool
}

// ratelimitCacheKey は盤の出力を決める入力の全部。
//
// 🚨 now を**秒に丸めて**鍵にする (ユーザー選定 2026-09-06)。針とゲージは now の連続関数で
// 描かれる設計 (usage/dial.go:cardPace の「1% 刻みに丸めると針がガタつく」) なので、丸めると
// 針の更新粒度が 80ms → 1s になる。枠は時間単位で動くので目には分からない一方、
// **これは見た目の挙動変更**であることを忘れないこと。
//
// spinner と err は鍵に入れない: 盤を描く分岐 (snap != nil) では両方とも使われず、
// spinner は毎フレーム変わるので入れると必ずキャッシュを外す。
type ratelimitCacheKey struct {
	width, page int
	colored     bool
	snap        *usage.Snapshot // 取得のたびに丸ごと差し替わる (usage_overlay.go) のでポインタで足りる
	sec         int64
	stale       string // last-good を保持した失敗の理由。snap が差し替わらずに注記だけ変わる
	waiting     string // 「取得中」の行 (スピナーのコマを含む。待っている間だけ毎フレーム外れる)
}

func (d *RatelimitDash) Visible() bool { return d.shown }
func (d *RatelimitDash) Toggle()       { d.shown = !d.shown }
func (d *RatelimitDash) close()        { d.shown = false; d.dropCache() }

// dropCache は盤のメモを捨てる (Snapshot を握り続けない)。
func (d *RatelimitDash) dropCache() { d.cache, d.cacheKey, d.cacheOK = nil, ratelimitCacheKey{}, false }

// ratelimitRenderOpts はダッシュボードの描画情報 (窓の寸法 + 色 + 描くデータ)。
type RatelimitRenderOpts struct {
	Width    int
	Page     int
	Colored  bool
	Spinner  string
	Snap     *usage.Snapshot // nil = 未取得 (取得中の表示に落ちる)
	Err      error           // 取得失敗 (snap があれば last-good を優先する)
	StaleErr error           // last-good を保持したまま失敗した理由 (usageview.UsageOverlay.StaleErr)
	Note     string          // 古い値が混じっているときの注記 (usageview.FetchNote。空 = 無い)。画面どうしを import しないよう main が組んで渡す
	Waiting  string          // まだ届いていない出所の「取得中」(usageview.UsageOverlay.WaitingNote。空 = 待っていない)
	Now      time.Time
}

// lines はダッシュボードをちょうど page 行で返す (全画面 viewer 共通の契約)。
func (d *RatelimitDash) Lines(o RatelimitRenderOpts) []string {
	head := []string{d.headerLine(o), ""}
	if note := o.Note; note != "" {
		head[1] = term.Paint(centerLine(note, o.Width), term.AnsiYellow, o.Colored)
	} else if o.Waiting != "" {
		head[1] = term.Paint(centerLine(o.Waiting, o.Width), term.AnsiDim, o.Colored)
	}
	body := max(o.Page-len(head), 1)
	switch {
	case o.Snap != nil:
		key := ratelimitCacheKey{
			width: o.Width, page: o.Page, colored: o.Colored, snap: o.Snap, sec: o.Now.Unix(),
			stale: errString(o.StaleErr), waiting: o.Waiting,
		}
		if d.cacheOK && d.cacheKey == key {
			// 🚨 コピーを返す。呼び出し側 (finishWithGlobalChrome) が append する経路があると、
			// キャッシュの裏地を書き換えて次のフレームが壊れる。40 行のコピーは
			// 盤を組み直すコストに比べれば無視できる。
			return append([]string(nil), d.cache...)
		}
		// 🚨 snap があっても描く枠が 0 件になりうる (RenderDashboard は nil を返す)。
		// Claude 側は既定の枠ラベル ("5h" / "7d") でしか拾わないので、/usage の文言が
		// 変わった環境では「パースは成功しているが描く枠が無い」が起こる。無言の白画面に
		// せず理由を出す (全画面なので、ユーザーには壊れたようにしか見えない)。
		var out []string
		if rows := usage.RenderDashboard(o.Snap, o.Now, o.Width, body, o.Colored); rows != nil {
			out = layout.PadTo(append(head, rows...), o.Page)
		} else {
			out = layout.PadTo(append(head, centerLine("表示できる利用枠がありません", o.Width)), o.Page)
		}
		d.cache, d.cacheKey, d.cacheOK = append([]string(nil), out...), key, true
		return out
	case o.Err != nil:
		// 🚨 理由も出す。前回の値が無いまま全部の取得元が失敗したとき、理由はここ以外のどこにも出ない
		// (U の箱は狭いので「理由は R」とだけ言う)。claude が PATH に無い・429 で止めている (再試行の時刻) などが読めないと次の手が無い
		head = append(head, centerLine("取得失敗", o.Width))
		if why := term.SanitizePlainLine(app.FirstLine(o.Err.Error())); why != "" {
			head = append(head, centerLine(why, o.Width))
		}
		return layout.PadTo(head, o.Page)
	default:
		return layout.PadTo(append(head, centerLine(o.Spinner+" 取得中...", o.Width)), o.Page)
	}
}

// headerLine は CLI 名とバージョンの見出し。バージョンは外部バイナリの出力なので無害化して載せる。
func (d *RatelimitDash) headerLine(o RatelimitRenderOpts) string {
	title := "Claude Code"
	if o.Snap != nil {
		if v := term.SanitizePlainLine(o.Snap.Version); v != "" {
			title += " v" + v
		}
		if o.Snap.HasCodex() {
			title += " + codex"
			if v := term.SanitizePlainLine(o.Snap.CodexVersion); v != "" {
				title += " v" + v
			}
		}
	}
	return term.Paint(centerLine(title+" · ratelimit", o.Width), term.AnsiDim, o.Colored)
}

// hint は最下行のキー案内。
// hint は最下行の案内。
//
// 🚨 width を受けて term.FitHintItems を通すこと。固定文字列だと 70 桁あり、
// 最小サポート幅 (frameMinWidth=60 のとき予算 58) で切り詰められ、さらに狭いと
// 出口 `R/q/esc/h: 閉じる` が完全に消える。全画面かつ rlDashSwallow で裏へキーを
// 通さないので、出口が消えると抜け方が分からなくなる。
func (d *RatelimitDash) Hint(width int) string {
	return term.FitHintItems(width, []term.HintItem{
		{Text: "r: 今すぐ更新", Prio: 3},
		{Text: "i: issues", Prio: 4},
		{Text: "s: status", Prio: 4},
		{Text: "R/q/esc/h: 閉じる", Prio: 1},
		{Text: "(毎分読み直し)", Prio: 5},
	})
}

// rlDashAction は handleKey の結果。閉じ→開きの連携 (横断) は viewer 単体では完結しないので、
// issuesView.wantStatus / statusView.wantIssues と同じ語彙で browseModel へ返す。
type rlDashAction int

const (
	// rlDashSwallow = 握り潰す。全画面なので裏の一覧へ素通りさせない (見えないままスクロールし、
	// 閉じたときにカーソルが移動している状態になる)。
	RlDashSwallow rlDashAction = iota
	RlDashClosed               // 閉じた (裏の画面へ戻る)
	RlDashRefresh              // 今すぐ取り直す (取得は browseModel が起こす)
	RlDashCross                // 別の全画面へ横断 (このダッシュボードは閉じ済み。行き先は takeWantCross)
)

// handleKey はダッシュボードが飲むキー。
//
// i / s / D は他の全画面への横断 (ユーザー要望 2026-09-01。判定は crosskey.Is、行き先は main の CrossTarget)。viewer 側の R と対で、
// 全画面どうしを往復できる。🚨 横断でも自分は必ず閉じる: 全画面は同時に 1 枚の前提で、
// 重ねると「見えている画面」と「キーを受ける画面」が食い違う (issues ↔ status と同じ作法)。
func (d *RatelimitDash) HandleKey(key string) rlDashAction {
	switch key {
	case "R", "q", "esc", "h", "left":
		d.close()
		return RlDashClosed
	case "r":
		return RlDashRefresh
	}
	// 横断キー (i / s / D。判定は crosskey.Is、行き先は main の CrossTarget)。R は自分自身なので上の「閉じる」に任せる
	if crosskey.Is(key) && key != "R" {
		d.close()
		d.wantCross = key
		return RlDashCross
	}
	return RlDashSwallow
}

// takeWantCross は押された横断キー (無ければ "") を一度だけ取り出す (viewer と同じ語彙)。
func (d *RatelimitDash) TakeWantCross() string {
	want := d.wantCross
	d.wantCross = ""
	return want
}

// centerLine は幅 w の中で s を中央寄せする (左余白のみ)。
//
// 🚨 入らないときは切り詰める。そのまま返すと見出し (CLI 名 + バージョン 2 つで 49 桁固定) が
// 狭い端末で width を超え、フレームが自動 OFF になる帯 (frameMinWidth 未満) ではクリップも
// 効かないので折り返して画面が崩れる。
func centerLine(s string, w int) string {
	pad := w - term.DispWidth(s)
	if pad <= 0 {
		return term.ClipToWidth(s, w)
	}
	return term.PadSpaces(pad/2) + s
}

// errString は err の文字列 (nil は空)。比較できる鍵に error を載せるときに使う。
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
