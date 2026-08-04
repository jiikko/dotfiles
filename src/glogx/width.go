package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// 幅計算の単一情報源。glogx の表示幅は必ずこのファイルの関数を通す。
//
// なぜ ansi (charmbracelet/x/ansi) に一本化するか: 描画エンジンの幅モデルと一致させるため。
// glogx 側が別ライブラリ (mattn/go-runewidth) で幅を測ると、両者が食い違う文字
// (⚠️ 等の VS16 付き絵文字・国旗 🇯🇵 は runewidth=1 だが ansi/端末=2) で glogx が
// 整えた行をエンジンが別位置で測り直し、毎秒の再描画のたびに桁がずれてガタつく
// (Terminal.app + tmux, ユーザー報告 2026-07-24)。同一ライブラリに揃えれば glogx と
// エンジンが構造的に一致し、絵文字を削らずに揺れが止まる。
//
// ⚠️ ライブラリを揃えても「幅モデル (Method)」までは揃わない。x/ansi は 2 つの数え方を持つ:
//   - GraphemeWidth: grapheme クラスタ単位 (パッケージ関数 ansi.StringWidth = これ。glogx はこちら)
//   - WcWidth: rune 単位の wcwidth (ansi.StringWidthWc)
//
// エンジンがどちらを使うかは bubbletea の実装詳細で、v1 と v2 で変わっている:
//   - v1 (standardRenderer): パッケージ関数 ansi.StringWidth = GraphemeWidth (glogx と一致)
//   - v2 (cursed renderer / ultraviolet): ansi.Method 経由で、既定は **WcWidth**。
//     端末が Unicode Core (mode 2027) 対応を報告したときだけ GraphemeWidth へ切り替わる
//     (bubbletea v2 tea.go の ModeReportMsg → setWidthMethod(ansi.GraphemeWidth))。
//     Terminal.app + tmux は 2027 を報告しない見込みなので、実運用では WcWidth 側になる。
//
// 2 モデルが食い違うのは「1 クラスタ = 複数 rune」の字だけで、実測 (2026-07-25) では:
//
//	                    Grapheme  WcWidth
//	⚠+VS16                     2        1   ← dropEmojiVS16 で bare 化するので出てこない
//	国旗 🇯🇵                     2        1   ← 残る (正規化していない)
//	keycap 1️⃣                  2        1   ← VS16 を外しても 1⃣ (数字+U+20E3) で残る
//	ZWJ 👨‍💻 / 家族 / 肌色       2        2   (一致)
//	漢 / 🚀 / ● / 罫線           =        =   (一致)
//
// つまり国旗・keycap を含む行は glogx が 1 セル多く数える。実測では v2 の描画のほうが
// ASCII 行と揃っており (国旗入り行の右枠が v1: 98 セル / v2: 97 セル、ASCII 行: 97)、
// v2 化で悪化はしていない。ただし前提が「エンジンが WcWidth である」ことに乗っているので、
// bubbletea を上げたら Method の既定と切替条件を読み直すこと (ここが桁ズレ再発の経路)。
//
// East Asian Ambiguous (罫線・✓・● 等) は ansi では幅 1 で、locale に依存しない
// (旧 runewidth は LANG=ja_JP.* 等で幅 2 に切り替わりパネル枠計算が実行環境依存でずれた)。

// dispWidth は文字列の端末表示幅を返す。ANSI エスケープは幅 0 として無視するので
// stripANSI 前処理は不要。
//
// 印字可能 ASCII だけの文字列は len がそのまま表示幅なので、grapheme 走査 (ansi.StringWidth)
// を省く。View 1 フレームの CPU の ~56% が stringWidth だった実測 (2026-07-29 pprof) への
// 対処で、medium 形式の Author/Date/メッセージ行など大半の行がこの fast-path を通る。
// 制御文字 (ESC 含む)・8bit 以上は従来どおり ansi に委ねるため幅モデルは変わらない。
func dispWidth(s string) int {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return ansi.StringWidth(s)
		}
	}
	return len(s)
}

// truncateDisp は表示幅 width まで切り詰め末尾に tail を付す。SGR は保持する。
func truncateDisp(s string, width int, tail string) string { return ansi.Truncate(s, width, tail) }

// truncateDispLeft は表示幅 width になるよう**先頭**を削り、頭に head (… 等) を付す。
// 末尾を残したいもの (ファイルパスの basename) に使う: 末尾から切ると「どのファイルか」が
// 分からなくなるため。幅計算は dispWidth と同じモデルを通す (この層に一本化する規律)。
func truncateDispLeft(s string, width int, head string) string {
	drop := dispWidth(s) - width + dispWidth(head)
	if drop <= 0 {
		return s
	}
	return ansi.TruncateLeft(s, drop, head)
}

// padSpaces は n 個の空白を返す。毎フレーム全行で呼ばれるため、事前確保した定数文字列の
// スライス (バッキング共有 = 無 alloc) で返し、超過分だけ strings.Repeat に落ちる。
const padSpacesBuf = "                                                                " +
	"                                                                " +
	"                                                                " +
	"                                                                " // 256 桁 (通常の端末幅を包含)

func padSpaces(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= len(padSpacesBuf) {
		return padSpacesBuf[:n]
	}
	return strings.Repeat(" ", n)
}

// fillRight は表示幅 width まで右を空白で詰める (runewidth.FillRight の置換)。
func fillRight(s string, width int) string {
	if pad := width - dispWidth(s); pad > 0 {
		return s + padSpaces(pad)
	}
	return s
}

// fillLeft は表示幅 width まで左を空白で詰める (行番号のような右揃えの数値に使う)。
func fillLeft(s string, width int) string {
	if pad := width - dispWidth(s); pad > 0 {
		return padSpaces(pad) + s
	}
	return s
}

// clusterWidth は grapheme クラスタ 1 個分の表示幅を返す (dispWidth と同一の幅モデル)。
// ⚠️ (U+26A0+U+FE0F) のような複数 rune のクラスタを rune 単位で数えて分断/誤幅にしない
// ため、クラスタ単位で幅を計算する必要のある dropToColumn 等が使う。
func clusterWidth(cluster string) int { return uniseg.StringWidth(cluster) }

// 幅の層ごとの実測表と VS16 正規化のトレードオフは termsafe.DropEmojiVS16 のドキュメント
// コメントにある (関数ごと termsafe へ移した。main からは dropEmojiVS16 の別名で呼べる)。
