package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// 幅計算の単一情報源。glogx の表示幅は必ずこのファイルの関数を通す。
//
// なぜ ansi (charmbracelet/x/ansi) に一本化するか: 描画エンジン (Bubble Tea の
// standardRenderer.flush) は各行の切り詰め・パディングを ansi.StringWidth で行う。
// glogx 側が別ライブラリ (mattn/go-runewidth) で幅を測ると、両者が食い違う文字
// (⚠️ 等の VS16 付き絵文字・国旗 🇯🇵 は runewidth=1 だが ansi/端末=2) で glogx が
// 整えた行をエンジンが別位置で測り直し、毎秒の再描画のたびに桁がずれてガタつく
// (Terminal.app + tmux, ユーザー報告 2026-07-24)。同一ライブラリに揃えれば glogx と
// エンジンが構造的に一致し、絵文字を削らずに揺れが止まる。
//
// East Asian Ambiguous (罫線・✓・● 等) は ansi では幅 1 で、locale に依存しない
// (旧 runewidth は LANG=ja_JP.* 等で幅 2 に切り替わりパネル枠計算が実行環境依存でずれた)。

// dispWidth は文字列の端末表示幅を返す。ANSI エスケープは幅 0 として無視するので
// stripANSI 前処理は不要。
func dispWidth(s string) int { return ansi.StringWidth(s) }

// truncateDisp は表示幅 width まで切り詰め末尾に tail を付す。SGR は保持する。
func truncateDisp(s string, width int, tail string) string { return ansi.Truncate(s, width, tail) }

// fillRight は表示幅 width まで右を空白で詰める (runewidth.FillRight の置換)。
func fillRight(s string, width int) string {
	if pad := width - dispWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// clusterWidth は grapheme クラスタ 1 個分の表示幅を返す (dispWidth と同一の幅モデル)。
// ⚠️ (U+26A0+U+FE0F) のような複数 rune のクラスタを rune 単位で数えて分断/誤幅にしない
// ため、クラスタ単位で幅を計算する必要のある dropToColumn 等が使う。
func clusterWidth(cluster string) int { return uniseg.StringWidth(cluster) }

// dropEmojiVS16 は絵文字異体字セレクタ VS16 (U+FE0F) を除去して、⚠️❤️✔️ 等の
// 「記号 + VS16」を bare な text presentation (⚠❤✔) へ倒す。
//
// なぜ幅統一 (dispWidth) と併用が必要か: VS16 付きの字は描画エンジン (x/ansi) が幅 2 と
// 数える一方、ユーザーの端末 (Terminal.app + tmux, 実測 2026-07-24: printf で数字が
// めり込む) はカーソルを 1 マスしか進めない。エンジン↔端末が食い違う文字はどちらの幅を
// glogx が採用してもズレるため、「食い違わない文字 (bare 記号 = 双方幅 1)」へ正規化して
// 出すしかない。git 由来テキスト (commit message 等) の表示入口で適用する。
// 端末の絵文字幅の扱いが将来変わったら (Terminal.app が VS16 に 2 マス割り当てるように
// なったら) この正規化は不要になる。VS15 (U+FE0E) は元々双方幅 1 なので触らない。
func dropEmojiVS16(s string) string {
	const vs16 = '\ufe0f'               // VS16 (emoji presentation selector)
	if !strings.ContainsRune(s, vs16) { // 多数派 (絵文字なし) は無 alloc で素通り
		return s
	}
	return strings.ReplaceAll(s, string(vs16), "")
}
