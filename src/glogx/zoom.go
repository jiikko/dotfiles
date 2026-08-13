package main

// アプリ全体の開閉演出 (ユーザー要望 2026-08-01)。起動で枠が中央から開き、終了で中央へ
// 縮んで消える。枠の中身は常に実画面の**左上**から映す (中央アンカーに戻さないこと。
// 理由は zoomWindow の doc: 左上寄せのテキスト画面では中央切り出しが最初白く見える)。
//
// 端末では文字を縮小できないので、「枠を中央から広げ、その中に実画面の中央部分を切り出して
// 入れる」形にする。⚠️ 枠だけを開いて最後に中身を出す形にしない: 最後のフレームだけ中身が
// パッと現れる段差が出る (実装前に両方の中間フレームを描いて選定した 2026-08-01)。
//
// ⚠️ 枠は buildPanelBoxImpl で組む (最外周フレーム wrapWindowFrame と同じ関数)。自前で罫線を
// 描くと、開き切る瞬間に本物の枠と字形・影・内側余白がずれて「最後にガタつく」。同じ関数を
// 通せば、開き切った姿がそのまま本物になる。
//
// ⚠️ 終了は「演出してから抜ける」ので、キーを押してから popup が消えるまで appZoomDuration
// かかる。押されたキーで即着地させる逃げ道を必ず持たせる (q が効かない時間を作らない。issues の
// 引き出しと同じ規律)。Ctrl-C は演出なしで即終了する (緊急脱出はいつでも最短)。

import (
	"math"
	"time"
)

const (
	// appZoomDuration は片道の所要。
	//
	// 端末の演出は文字セル単位でしか動けないので、所要がそのまま滑らかさになる: 40 行の画面が
	// 開き切るまでに 36 行ぶん育つため、フレームが少ないと 1 フレームで何行も跳ぶ。60fps での実測
	// (1 フレームあたりの平均) は 220ms = 2.7 行 / 320ms = 1.8 行 / 420ms = 1.3 行。
	// ⚠️ 伸ばすほど滑らかになるが、起動と終了のたびに待たされる時間でもある。ここは体感の綱引きで
	// 決める値なので、滑らかさが足りないと感じたらまずここを疑う (fps は既に上限近い)。
	appZoomDuration = 320 * time.Millisecond
	// appZoomMinRows は枠が枠として見える最小の高さ (上辺 + 中身 1 行 + 下辺 + 影)。
	appZoomMinRows = 4
	// appZoomSnap は「ほぼ開き切った」とみなす割合。ここを超えたら実画面をそのまま出す:
	// 演出の枠が本物の枠とほぼ重なる領域では、切り出しの誤差が二重罫線に見える。
	// ⚠️ scale の終点でもある (scale の doc)。閾値としてだけ読むと、曲線側の * appZoomSnap を
	// 「なぜ掛けているのか分からない」と外され、所要の 31% が死んだ元の状態に戻る。
	appZoomSnap = 0.97
)

// appZoomPhase は演出の状態。zero value = 演出なし (テスト・非対話経路はここから動かない)。
type appZoomPhase uint8

const (
	appZoomShown   appZoomPhase = iota // 実画面 (演出していない)
	appZoomOpening                     // 中央から開く途中
	appZoomClosing                     // 中央へ吸い込まれる途中
	appZoomClosed                      // 閉じ切った (呼び出し側が終了する)
)

// appZoom は画面全体の開閉演出の状態。
type appZoom struct {
	phase   appZoomPhase
	started time.Time
	off     bool // 演出しない (テスト・端末が小さすぎる場合)
}

// start は開く演出を始める (Init から)。
func (z *appZoom) start(now time.Time) {
	if z.off {
		return
	}
	z.phase, z.started = appZoomOpening, now
}

// startClose は閉じる演出を始める。演出しない設定なら false (呼び出し側は即終了する)。
func (z *appZoom) startClose(now time.Time) bool {
	if z.off || z.phase == appZoomClosing {
		return z.phase == appZoomClosing
	}
	z.phase, z.started = appZoomClosing, now
	return true
}

// closing は閉じる演出の途中か (キーで即着地させる判定に使う)。
func (z *appZoom) closing() bool { return z.phase == appZoomClosing }

// rawProgress は現在の演出の素の進捗 0..1 (演出中でなければ 1)。
func (z *appZoom) rawProgress(now time.Time) float64 {
	if z.phase != appZoomOpening && z.phase != appZoomClosing {
		return 1
	}
	return max(min(float64(now.Sub(z.started))/float64(appZoomDuration), 1), 0)
}

// animating は演出の途中か (tick を回し続ける判定に使う)。
func (z *appZoom) animating(now time.Time) bool {
	return (z.phase == appZoomOpening || z.phase == appZoomClosing) && z.rawProgress(now) < 1
}

// settle は演出が終わっていれば静止状態へ進め、閉じ切ったなら true を返す。
func (z *appZoom) settle(now time.Time) (closed bool) {
	if z.rawProgress(now) < 1 {
		return false
	}
	return z.finish()
}

// finish は演出を即座に着地させる (キー操作は待たせない)。閉じ切ったなら true。
func (z *appZoom) finish() (closed bool) {
	switch z.phase {
	case appZoomOpening:
		z.phase = appZoomShown
	case appZoomClosing:
		z.phase = appZoomClosed
		return true
	case appZoomShown, appZoomClosed:
	}
	return z.phase == appZoomClosed
}

// scale は今フレームで画面が占める割合 (1 = 実画面)。
//
// 開くときは easeOutCubic で終点に向けて減速し、閉じるときはその逆再生 (進捗を反転するだけ)。
// 別のカーブを使うと「開いた動きと閉じる動きが違う」ちぐはぐさが出る (引き出しと同じ規律)。
//
// ⚠️ 曲線の値をそのまま返さず appZoomSnap 倍して返す。素の easeOutCubic は進捗 69% で
// appZoomSnap (0.97) に達してしまい、残り 31% (68ms) は絵が変わらない = 開くときは「早々に
// 開き切って静止」、閉じるときは「68ms 何も起きてから縮み始める」ことになる (実測 2026-08-01)。
// 終点を snap 閾値に合わせておけば、動きが所要いっぱいに広がってフレームあたりの跳びが小さくなる
// (実測: 動くフレーム 10 → 14 枚、1 フレームの平均 3.8 行 → 2.7 行)。
func (z *appZoom) scale(now time.Time) float64 {
	switch z.phase {
	case appZoomOpening:
		return easeOutCubicFloat(z.rawProgress(now)) * appZoomSnap
	case appZoomClosing:
		return easeOutCubicFloat(1-z.rawProgress(now)) * appZoomSnap
	case appZoomShown, appZoomClosed:
	}
	return 1
}

// zoomWindow は画面 lines を割合 scale の姿へ変換する (1 = そのまま)。
//
// 中身は実画面の**左上**から切り出して枠へ入れる (枠自体は中央から開く)。切り出しは表示幅で
// 行う (ANSI を壊さないよう truncateKeepANSI を通す)。
//
// 左上アンカーにしている理由 (ユーザー要望 2026-08-13): 当初は縦横とも中央アンカーだったが、
// コミット一覧のテキストは左上寄せなので、小さい枠のうちは「画面中央の空白 (行の右半分)」を
// 映して最初のフレームが白く見え、文字が後から枠に入ってくる見え方になっていた。左上なら
// 最初のフレームから 1 行目のコミットが見え、起動時に最初に映るもの = 最終的に読む位置になる。
// 開き切る瞬間に実画面と一致する性質は変わらない (scale→1 で切り出しが全画面になる)。
//
// ⚠️ framed は「実画面が最外周フレームを持つか」。持たない画面 (--no-frame / 小さい端末) で
// 演出だけ枠を描くと、開き切った瞬間に枠が消える段差が出る。実画面に合わせて枠の有無を決める。
func zoomWindow(lines []string, scale float64, width int, colored, framed bool) []string {
	h := len(lines)
	if h == 0 || width <= 0 || scale >= appZoomSnap {
		return lines
	}
	boxW := max(min(int(math.Round(float64(width)*scale)), width), minPanelWidth)
	boxH := max(min(int(math.Round(float64(h)*scale)), h), appZoomMinRows)
	// buildPanelBoxImpl は右 1 桁を影に使い、返す行数は 中身 + 3 (上辺 + 下辺 + 下影)
	inner, innerH := boxW, boxH // 枠なしは切り出した中身がそのまま演出フレームになる
	if framed {
		inner, innerH = panelInnerWidth(boxW-1), max(boxH-3, 1)
	}
	rows := make([]string, 0, innerH)
	for i := range innerH {
		src := ""
		if i < h {
			src = truncateKeepANSI(lines[i], inner)
		}
		rows = append(rows, src)
	}
	box := rows
	if framed {
		box = buildPanelBoxImpl("", rows, boxW, colored,
			panelBoxStyle{glyphs: borderDouble, color: ansiFrameBorder})
	}
	out := make([]string, h)
	pad := padSpaces(max((width-boxW)/2, 0))
	top := max((h-len(box))/2, 0)
	for i, l := range box {
		if y := top + i; y < h {
			out[y] = pad + l
		}
	}
	return out
}
