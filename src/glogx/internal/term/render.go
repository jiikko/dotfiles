// Package term は glogx の描画の土台: ANSI 色と Paint・切り詰め / 折り返しの純関数・幅計算の別名・枠と影・
// 端末サニタイズの入口・glide のキー語彙・最下行の案内の詰めを持つ。画面のパッケージと main の両方が使う
// (main は下位から import できないので、画面が使う部品は main ではなくここに置く)。
package term

import (
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/sgr"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// 基本色は glogx/sgr の別名 (issues / usage と同じ値を 1 箇所から引く)。
const (
	AnsiReset   = sgr.Reset
	AnsiRed     = sgr.Red
	AnsiGreen   = sgr.Green
	AnsiYellow  = sgr.Yellow
	AnsiMagenta = sgr.Magenta
	AnsiCyan    = sgr.Cyan
	AnsiDim     = sgr.Dim
	// カーソル行の bg (256色の暗灰 237)。テーマの意味マップ (docs/theme-colors.md) の
	// カーソル行の bg。無彩色の 237 では地色 234/235 と近く見失いやすかったため、
	// 色相のある暗青 24 に変更 (ユーザー要望 2026-07-21。fg 色はそのまま透ける)
	AnsiCursorBg = "\x1b[48;5;24m"
	// AnsiScreenBg は全画面 ratelimit ダッシュボードだけが敷く地の色 (256 色の黒 16)。
	//
	// 🚨 「端末の既定の背景」に戻す (\x1b[49m) では解決しない場面のための固定色。scratch popup は
	// display-popup 自身が `-s bg=colour17` (濃紺) を敷いており、**その popup では既定の背景が
	// 濃紺**なので既定へ戻しても青いままになる (scripts/tmux_scratch_popup.sh)。
	// ユーザー要望 2026-09-01: 全画面の残量表示だけは地色を固定したい。
	AnsiScreenBg = "\x1b[48;5;16m"
	AnsiBold     = sgr.Bold
	// 落ち影の前景色 (256色の近黒 232)。bg ベタ塗りではなく █/▓ の前景ブロックで影を描き、
	// グリフの隙間から端末の地色が透けて自然な penumbra になる (端末 bg に依存せず、色の
	// 濃淡 █>▓ で縁をフェザーできる)。BuildShadowPanelBox の drop shadow。
	// 🚨 tuikit の既定値を指す: 確認ダイアログ (confirm.Box) は tuikit の既定の影で描くので、
	// ここだけ別の値にすると確認ダイアログと他の板で影の色が割れる。
	AnsiShadowFg = layout.ShadowNearBlack
	// 最外周フレームの罫線色 (256色のマゼンタ 201)。dotfiles のテーマ意味マップ
	// (docs/theme-colors.md) の「点滅/scratch アイデンティティ」= tmux の scratch popup 枠と
	// 同じ色で、glogx も「ふだんの pane とは別の一時的な板」であることを色で示す
	// (ユーザー要望 2026-07-25「scratch tmux terminal みたいに色をつけたい」)。
	//
	// 🚨 色番号は theme/colors.yml の blink_magenta が出典。変えるなら yml 側と揃えること
	// (yml は機械可読な単一ソースで、tests/theme/test_theme_colors.sh が tmux/nvim 側の
	// 定数との一致を検証している。glogx は Go で定数を共有できないため手書きコピーになる)。
	AnsiFrameBorder = "\x1b[38;5;201m"
)

// 幅計算は width.go の DispWidth (= ansi.StringWidth) に一本化している。ambiguous
// 文字は locale 非依存で幅 1、絵文字は端末/描画エンジンと一致する幅で数える。

// WrapToWidth は表示幅 width で折り返す (ANSI を含まない行の前提)。width <= 0 なら
// 折り返さない。単語境界は考慮せず端末の折り返しと同じく文字単位で折る
// (日本語混じりの commit message では単語境界折りの利得が薄いため)。
func WrapToWidth(s string, width int) []string {
	if width <= 0 || DispWidth(s) <= width {
		return []string{s}
	}
	// 書記素クラスタ単位で折り、各行の幅は termwidth.Of で width 以下を保証する (rune 単位だと ⚠️ を VS16 の手前で
	// 分断し、ansi.Hardwrap はキーキャップで幅を超えた)。単語境界は見ず、折り返しの頭の空白は落とす。
	return termwidth.Wrap(s, width, true)
}

func Paint(s, color string, colored bool) string {
	if !colored {
		return s
	}
	return color + s + AnsiReset
}

// ClipToWidth は端末幅を超える行を切り詰める (インライン TUI で折り返しが再描画範囲を
// ずらすのを防ぐ: issue の懸念点)。ANSI を除いた実効幅で判定し、超過時は末尾に `…` を付けて
// 切り詰める。**SGR は保持する**。静的出力では使わない。
//
// TruncateKeepANSI との違いは `…` を付けるかどうかだけ (どちらも色は残る)。切り詰めた行の
// 表示幅も要る hot path では termwidth.ClipMeasure を使う (幅の二度測りを避けるため)。
func ClipToWidth(line string, width int) string { return termwidth.Clip(line, width) }

// ReapplyAfterReset は行内の SGR リセットの直後に bg を張り直す (tuikit sgr.ReapplyAfterReset の別名)。
func ReapplyAfterReset(text, bg string) string { return sgr.ReapplyAfterReset(text, bg) }

// TruncateKeepANSI は SGR を保ったまま表示幅 width へ切る (末尾に … を付けない。tuikit termwidth.Cut の別名)。
func TruncateKeepANSI(s string, width int) string { return termwidth.Cut(s, width) }

// DropToColumn は表示列 n 以降を返す (手前の SGR は replay する。tuikit termwidth.DropColumns の別名)。
func DropToColumn(s string, n int) string { return termwidth.DropColumns(s, n) }

// StripANSI は SGR を剥がす (tuikit termwidth.StripSGR の別名)。
func StripANSI(s string) string { return termwidth.StripSGR(s) }
