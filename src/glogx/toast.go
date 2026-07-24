package main

import (
	"math"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// toastHold は「にゅっと出た」あと引っ込むまでの静止時間。push/pull 完了の結果を見落とさない
// 程度。実時間 (3s) をテストで待たずに退場遷移 (holdCmd → toastMsg) を検証できるよう var に
// してある (本番値は不変、テストだけ短い値へ差し替える)。
var toastHold = 3 * time.Second

// toastSlideFrames は入場/退場の横スライドを何フレームで渡り切るか。frame を 0→N で進め、
// 表示カラム shown = easeOutCubic(frame/N) × 箱幅 とする (箱幅に依らずほぼ一定時間
// ~12frame × scrollInterval ≈ 400ms)。行 (縦) でなくカラム (横) を動かすため、箱が数行でも
// 解像度の高い滑らかなスライドになる。
const toastSlideFrames = 12

// easedShown は frame (0..toastSlideFrames) に対する表示カラム数を easeOutCubic で返す。
// 線形だと入場/退場の始点・終点で速度が急に切り替わり「カクッ」と見える。easeOutCubic は
// 終点付近で減速するので、入場 (frame 0→N) は「すっと収まり」、退場 (frame N→0 と逆走) は
// 曲線を逆に辿るため始めゆっくり→終わり加速で「すっと消える」自然な動きになる。
func easedShown(frame, w int) int {
	if frame <= 0 {
		return 0
	}
	if frame >= toastSlideFrames {
		return w
	}
	p := float64(frame) / float64(toastSlideFrames)
	eased := 1 - math.Pow(1-p, 3) // easeOutCubic
	return int(math.Round(eased * float64(w)))
}

// toastMsg は静止 (holding) が終わって退場アニメを始める合図。seq で世代管理し、新しいトーストが
// 上書きした後に届く古いタイマーは無視する (連続 push/pull で前の退場が後のを消さないように)。
type toastMsg struct{ seq int }

type toastPhase int

const (
	toastHidden   toastPhase = iota // 非表示
	toastEntering                   // 右画面外から左へ 滑り込み中 (shown 0→boxWidth)
	toastHolding                    // 全幅表示で静止 (toastHold 後に leaving へ)
	toastLeaving                    // 右画面外へ 滑り出し中 (shown boxWidth→0)
)

// toast は右下に出す結果フィードバック (push/pull 完了)。右の画面外から左へ「にゅっと」滑り込んで
// 現れ、数秒静止し、また右へ「にゅっと」滑り出て消える横スライド (shown = 箱の左から見せている
// カラム数を tick で増減させ、右端揃えで overlay すると箱が水平移動して見える)。行単位でなく
// カラム単位で動かすため、箱が数行でも滑らかなアニメになる。glogx は tmux の display-popup 内で
// 動くため tmux-toast (floating pane) は popup に隠れて出せず、glogx 自身の TUI 内に描く。
type toast struct {
	text  string
	ok    bool // true=成功 (✓緑) / false=失敗 (✗赤)。info=true のときは無視される
	info  bool // true=進行中/中立 (…シアン)。ok より優先し、完了/失敗どちらでもない状態を表す
	seq   int  // 世代: 退場タイマーの有効性判定 + 再表示リセット
	phase toastPhase
	shown int // 現在見せている箱の左カラム数 (0=画面右外に収納 / boxWidth=全幅表示)
	frame int // スライドの進捗フレーム (入場 0→N / 退場 N→0)。shown = easedShown(frame)
}

// show は新しいトーストを右画面外からの滑り込みで出し始める。呼び出し側で maybeTick を Batch して
// tick を回すこと (アニメは tickMsg で進む)。既存トーストは上書きし世代を進める。
func (t *toast) show(text string, ok bool) {
	t.seq++
	t.text, t.ok, t.info = text, ok, false
	t.phase = toastEntering
	t.shown, t.frame = 0, 0
}

// showInfo は進行中/中立のトースト (…シアン) を出す。成功でも失敗でもない一時状態 (例:「検索中」)
// 用で、直後に show(…) の結果トーストで上書きされることを前提とする。
func (t *toast) showInfo(text string) {
	t.seq++
	t.text, t.info = text, true
	t.phase = toastEntering
	t.shown, t.frame = 0, 0
}

// animating は入場/退場アニメ中か (tick を回す必要がある + spinnerActive に含める)。holding は
// 全幅のまま静止 (tea.Tick の toastMsg 待ち) なので tick 不要。
func (t *toast) animating() bool { return t.phase == toastEntering || t.phase == toastLeaving }

// visible は表示中か (holding 含む)。
func (t *toast) visible() bool { return t.phase != toastHidden }

// boxWidth は箱の総カラム幅 (スライドの終点)。実描画幅と一致させるため fullBox の 1 行目の
// 表示幅を使う (buildShadowPanelBox の最小幅クランプ込み)。色に依らず一定。
func (t *toast) boxWidth(colored bool) int {
	full := t.fullBox(colored)
	if len(full) == 0 {
		return 0
	}
	return dispWidth(full[0])
}

// advance はアニメを 1 フレーム進める。frame を入場で 0→N、退場で N→0 に動かし、表示カラムは
// easedShown(frame) で求める (easeOutCubic)。入場完了で holding へ移り toastHold 後の退場
// タイマーを予約して返す。退場完了で hidden。
func (t *toast) advance(colored bool) (holdCmd tea.Cmd) {
	w := t.boxWidth(colored)
	switch t.phase {
	case toastEntering:
		t.frame++
		t.shown = easedShown(t.frame, w)
		if t.frame >= toastSlideFrames {
			t.shown = w
			t.phase = toastHolding
			seq := t.seq
			return tea.Tick(toastHold, func(time.Time) tea.Msg { return toastMsg{seq: seq} })
		}
	case toastLeaving:
		t.frame--
		t.shown = easedShown(t.frame, w)
		if t.frame <= 0 {
			t.shown = 0
			t.phase = toastHidden
			t.text = ""
		}
	}
	return nil
}

// startLeaving は holding の静止時間が明けたら (toastMsg) 退場アニメへ移す。世代一致時のみ。
func (t *toast) startLeaving(msg toastMsg) {
	if msg.seq == t.seq && t.phase == toastHolding {
		t.phase = toastLeaving
	}
}

// fullBox は内容幅にフィットした影付き小箱 (全行)。スライドの基準になる全幅・全行の算出にも使う。
func (t *toast) fullBox(colored bool) []string {
	mark, color := "✓", ansiGreen
	switch {
	case t.info:
		mark, color = "…", ansiCyan
	case !t.ok:
		mark, color = "✗", ansiRed
	}
	row := paint(mark+" "+t.text, color, colored)
	boxW := dispWidth(row) + usageBoxChrome
	// 枠線も種別色 (成功=緑 / 失敗=赤 / 進行=シアン) で染めて一体感を出す。影は中立の dim のまま。
	return buildShadowPanelBox("", []string{row}, boxW, colored, color)
}

// boxLines は現フレームで見せる箱行 (全行) を返す。各行を箱の左 shown カラムに切り、右端揃えで
// overlay されると「右画面外から左へ滑り込む/右へ滑り出る」横スライドになる。左カラム切りで開いた
// SGR は行末で閉じる (右端揃え合成の背景に色がにじまないように)。非表示なら nil。
func (t *toast) boxLines(colored bool) []string {
	if t.phase == toastHidden {
		return nil
	}
	full := t.fullBox(colored)
	if len(full) == 0 {
		return nil
	}
	// 箱幅は full から直に導く (boxWidth を呼ぶと fullBox をもう一度組んでしまう。表示中は毎フレーム
	// 走るので二重構築を避ける)。
	v := min(max(t.shown, 0), dispWidth(full[0]))
	if v <= 0 {
		return nil
	}
	out := make([]string, len(full))
	for i, row := range full {
		clipped := truncateKeepANSI(row, v) // 箱の左 v カラム (右側は画面右端の外へ)
		if colored {
			clipped += ansiReset
		}
		out[i] = clipped
	}
	return out
}
