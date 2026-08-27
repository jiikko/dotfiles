// Package termwidth は端末表示幅の単一情報源。glogx の表示幅 (main / issues / usage) は必ず
// このパッケージを通す。
//
// main と issues / usage の両方から使うため独立パッケージにしてある (main の非公開関数だと
// 下位パッケージが呼べず、以前は素の ansi.StringWidth を各パッケージへ写していた。issue 106)。
// main 側は呼び出し箇所が多いので dispWidth 等の別名で呼ぶ (width.go)。
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
// East Asian Ambiguous (罫線・✓・● 等) は ansi では既定で幅 1 で、**locale には依存しない**
// (旧 runewidth は LANG=ja_JP.* 等で幅 2 に切り替わりパネル枠計算が実行環境依存でずれた)。
// ⚠️ ただし env `RUNEWIDTH_EASTASIAN` が真だと ansi は幅 2 に切り替わる。この層 (dispWidth /
// symWidthTable) は幅をライブラリから引くので追従するが、**glogx の描画はこの env を支持しない**
// (枠・影・区切り線を strings.Repeat でグリフ数ぶん埋めている箇所があり、幅 2 になると要求の
// 2 倍近くまで膨らむ。実測 issue 054)。検出と扱いは widthenv パッケージが一次情報。
package termwidth

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Of は文字列の端末表示幅を返す。ANSI エスケープは幅 0 として無視するので
// 呼び出し側での ANSI 除去は不要。
//
// grapheme 走査 (ansi.StringWidth) を省ける文字列は自前で数える。判定できない文字が
// 1 つでもあれば全体を ansi へ委ねるので、幅モデルは変わらない。
//
// ⚠️ 「印字可能 ASCII だけ」という条件では効かない (2026-08-14 実測)。ESC (0x1b) が
// その条件を外すため、**色を付けた行は 1 行も fast-path を通れなかった** — 一覧で
// 到達率 35.8% / slow-path 64.2%、slow に落ちた要因の 96.2% が ESC。さらに「ASCII + SGR
// だけ」で構成される行も 0.0〜0.7% しかない (glogx の行にはたいてい幅 1 の記号 —
// CI 状態の ✓✗●⊘↑・枠線 ─│・切り詰めの …・スピナー ⠋ — が混ざる)。
// そのため fast-path は下記 3 種を扱う: 印字可能 ASCII / SGR / 幅を表に持っている記号。
// この形で slow-path の 97.6〜98.8% を拾い、ansi.StringWidth との幅の不一致は 0 件だった
// (詳細は issues/done/046-perf-glogx-dispwidth-fastpath-dead.md)。
func Of(s string) int {
	if w, ok := fastDispWidth(s); ok {
		return w
	}
	return ansi.StringWidth(s)
}

// fastDispWidth は s が「印字可能 ASCII + SGR + 幅を表に持っている記号」だけで
// できているとき、その表示幅と true を返す。そうでなければ (0, false)。
//
// なぜ 1 文字ずつ足すだけで ansi.StringWidth と一致するか (grapheme クラスタを走査
// しなくてよい理由): 受理する文字はどれも必ず単独で 1 クラスタを成す。クラスタが
// 伸びる規則は 2 方向あり、どちらも受理集合では成立しない:
//   - **後続**が Extend / ZWJ / SpacingMark / 異体字セレクタ / regional indicator (GB9/9a/12/13)
//   - **先行**が Prepend、または Indic conjunct の連結 (GB9b / GB9c)
//
// 後者を見落とすと「受理文字が直前の文字に飲まれる」形で崩れる (実測: U+0600 + "1" は
// 1 クラスタ・幅 0 になる)。受理 5 ブロックに Prepend / Linker は 1 つも無いことを
// TestAcceptedSymbolsNeverCombineWithEachOther が総当たりで確かめている。
// ⚠️+VS16 のような組み合わせは VS16 の時点で受理に失敗し、全体が ansi へ落ちる。
func fastDispWidth(s string) (int, bool) {
	w := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c >= 0x20 && c <= 0x7e: // 印字可能 ASCII
			w++
			i++
		case c == 0x1b: // SGR (ESC [ 数字と ; の並び m) だけを幅 0 として飛ばす
			if i+1 >= len(s) || s[i+1] != '[' {
				return 0, false
			}
			j := i + 2
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j >= len(s) || s[j] != 'm' {
				return 0, false // SGR 以外の CSI / 途中で切れた列 → ansi に委ねる
			}
			i = j + 1
		case c < 0x80:
			// C0 制御・DEL の早期棄却。⚠️ 正しさには寄与しない (この分岐を消しても
			// default が rune を復号して isWidth1Symbol で落とすので結果は同じ。
			// 変異検証でも green のままだった)。diff 本文のタブのように C0 は実際に
			// 来るので、復号を省く最適化として残している
			return 0, false
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			sw := symbolWidth(r) // 不正な UTF-8 (RuneError) もここで 0 になる
			if sw == 0 {
				return 0, false
			}
			w += sw
			i += n
		}
	}
	return w, true
}

// 記号の幅表が覆う範囲。ここを外れた rune は受理しない (= ansi へ委ねる) ので、
// 上限より大きい rune が受理されることは構造的に起こらない。
const (
	symTableLo = 0x00B7 // 受理する最小の rune (·)
	symTableHi = 0x28FF // Braille の末尾
)

// symWidthTable は受理する記号の表示幅 +1 を引く表 (0 = 受理しない)。
//
// ⚠️ 幅を 1 と決め打ちしてはいけない。x/ansi は `RUNEWIDTH_EASTASIAN` が真のとき
// East Asian Ambiguous (罫線・矢印・…・· 等) を**幅 2** として数える (x/ansi の
// method.go の init が env を読む)。決め打ちすると同じ文字について「自前の幅」と
// ansi.Truncate / ansi.TruncateLeft の幅が食い違い、fillRight が要求幅を超え
// truncateDispLeft が要求の 2 倍以上を返す (実測 2026-08-14: 罫線 10 個で
// fillRight(s,30) の実幅が 40、truncateDispLeft(...,8) が 19)。
// そのため幅は**ライブラリから 1 度だけ引く**。こうすると env の設定が何であれ
// glogx の幅モデルは ansi と構造的に一致する。
//
// 受理集合を足すときは acceptSymbol を触る。幅の主張はこの表が機械的に持つので、
// コメントに幅を書き写さない。
var symWidthTable [symTableHi - symTableLo + 1]uint8

// init は受理集合の幅をライブラリから引いて表に焼く。x/ansi の init は import 順で
// 先に走るので、env 由来の設定はこの時点で確定している。
func init() {
	for r := rune(symTableLo); r <= symTableHi; r++ {
		if !acceptSymbol(r) {
			continue
		}
		w := ansi.StringWidth(string(r))
		if w <= 0 || w > 2 {
			continue // 想定外の幅は受理しない (0 のままにして ansi へ委ねる)
		}
		symWidthTable[r-symTableLo] = uint8(w) + 1
	}
}

// symbolWidth は r の表示幅を表から返す。受理しない rune は 0。
func symbolWidth(r rune) int {
	if r < symTableLo || r > symTableHi {
		return 0
	}
	if v := symWidthTable[r-symTableLo]; v != 0 {
		return int(v) - 1
	}
	return 0
}

// acceptSymbol は r を fast-path で扱う記号として受理するか (幅は問わない。幅は
// symWidthTable がライブラリから引く)。ブロック単位で受けているものは
// 「そのブロックに結合性を持つ文字が無い」ことをテストが総当たりで確かめている。
func acceptSymbol(r rune) bool {
	switch {
	case r >= 0x2190 && r <= 0x21FF: // Arrows
		return true
	case r >= 0x2200 && r <= 0x22FF: // Mathematical Operators
		return true
	case r >= 0x2500 && r <= 0x257F: // Box Drawing
		return true
	case r >= 0x2580 && r <= 0x259F: // Block Elements
		return true
	case r >= 0x2800 && r <= 0x28FF: // Braille (スピナー)
		return true
	}
	switch r {
	case '·', '–', '—', '‘', '’', '“', '”', '•', '‥', '…', '‹', '›', // General Punctuation ほか
		'⏸',                                         // Miscellaneous Technical
		'■', '▰', '▱', '▶', '▸', '○', '●', '◐', '◦', // Geometric Shapes
		'☐', '☑', '⚠', // Miscellaneous Symbols
		'✓', '✗', '❯': // Dingbats
		return true
	}
	return false
}

// Truncate は表示幅 width まで切り詰め末尾に tail を付す。SGR は保持する。
func Truncate(s string, width int, tail string) string { return ansi.Truncate(s, width, tail) }

// TruncateLeft は表示幅 width になるよう**先頭**を削り、頭に head (… 等) を付す。
// 末尾を残したいもの (ファイルパスの basename) に使う: 末尾から切ると「どのファイルか」が
// 分からなくなるため。幅計算は Of と同じモデルを通す (この層に一本化する規律)。
func TruncateLeft(s string, width int, head string) string {
	drop := Of(s) - width + Of(head)
	if drop <= 0 {
		return s
	}
	return ansi.TruncateLeft(s, drop, head)
}

// PadSpaces は n 個の空白を返す。毎フレーム全行で呼ばれるため、事前確保した定数文字列の
// スライス (バッキング共有 = 無 alloc) で返し、超過分だけ strings.Repeat に落ちる。
const padSpacesBuf = "                                                                " +
	"                                                                " +
	"                                                                " +
	"                                                                " // 256 桁 (通常の端末幅を包含)

func PadSpaces(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= len(padSpacesBuf) {
		return padSpacesBuf[:n]
	}
	return strings.Repeat(" ", n)
}

// FillRight は表示幅 width まで右を空白で詰める (runewidth.FillRight の置換)。
func FillRight(s string, width int) string {
	if pad := width - Of(s); pad > 0 {
		return s + PadSpaces(pad)
	}
	return s
}

// FillLeft は表示幅 width まで左を空白で詰める (行番号のような右揃えの数値に使う)。
func FillLeft(s string, width int) string {
	if pad := width - Of(s); pad > 0 {
		return PadSpaces(pad) + s
	}
	return s
}
