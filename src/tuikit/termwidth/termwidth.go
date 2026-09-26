// Package termwidth は端末表示幅の単一情報源。tuikit とそれを使う画面の表示幅は必ずこの
// パッケージを通す (呼び出し側で ansi.StringWidth を直接呼ばない。glogx では素の
// ansi.StringWidth を各パッケージへ写して食い違っていた。issue 106)。
//
// なぜ ansi (charmbracelet/x/ansi) に一本化するか: 描画エンジンの幅モデルと一致させるため。
// 使う側が別ライブラリ (mattn/go-runewidth) で幅を測ると、両者が食い違う文字
// (⚠️ 等の VS16 付き絵文字・国旗 🇯🇵 は runewidth=1 だが ansi/端末=2) で、整えた行を
// エンジンが別位置で測り直し、毎秒の再描画のたびに桁がずれてガタつく
// (glogx, Terminal.app + tmux, ユーザー報告 2026-07-24)。同一ライブラリに揃えれば
// エンジンと構造的に一致し、絵文字を削らずに揺れが止まる。
//
// ⚠️ ライブラリを揃えても「幅モデル (Method)」までは揃わない。x/ansi は 2 つの数え方を持つ:
//   - GraphemeWidth: grapheme クラスタ単位 (パッケージ関数 ansi.StringWidth = これ。この層はこちら)
//   - WcWidth: rune 単位の wcwidth (ansi.StringWidthWc)
//
// エンジンがどちらを使うかは bubbletea の実装詳細で、v1 と v2 で変わっている:
//   - v1 (standardRenderer): パッケージ関数 ansi.StringWidth = GraphemeWidth (この層と一致)
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
// つまり国旗・keycap を含む行はこの層が 1 セル多く数える (glogx で実測)。実測では v2 の描画のほうが
// ASCII 行と揃っており (国旗入り行の右枠が v1: 98 セル / v2: 97 セル、ASCII 行: 97)、
// v2 化で悪化はしていない。ただし前提が「エンジンが WcWidth である」ことに乗っているので、
// bubbletea を上げたら Method の既定と切替条件を読み直すこと (ここが桁ズレ再発の経路)。
//
// East Asian Ambiguous (罫線・✓・● 等) は ansi では既定で幅 1 で、**locale には依存しない**
// (旧 runewidth は LANG=ja_JP.* 等で幅 2 に切り替わりパネル枠計算が実行環境依存でずれた)。
// ⚠️ ただし env `RUNEWIDTH_EASTASIAN` が真だと ansi は幅 2 に切り替わる。この層 (Of /
// symWidthTable) は幅をライブラリから引くので追従するが、**tuikit の描画 (layout) はこの env を支持しない**
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
// (詳細は issue 046)。
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
//
// 実体は arch ごとに選ぶ (fastwidth_arm64.go / fastwidth_other.go)。arm64 は同じ受理規則を
// アセンブリで持ち、印字可能 ASCII を 16 byte ずつ NEON で数える。ここにある Go 版が
// 受理規則の正本で、arm64 版はこれとの一致を差分 fuzz (FuzzFastDispWidthMatchesGeneric) で守る。
func fastDispWidthGeneric(s string) (int, bool) {
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
// ansi.Truncate / ansi.TruncateLeft の幅が食い違い、FillRight が要求幅を超え
// TruncateLeft が要求の 2 倍以上を返す (実測 2026-08-14: 罫線 10 個で
// FillRight(s,30) の実幅が 40、TruncateLeft(...,8) が 19)。
// そのため幅は**ライブラリから 1 度だけ引く**。こうすると env の設定が何であれ
// この層の幅モデルは ansi と構造的に一致する。
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

// FirstCluster は s の先頭の grapheme クラスタ 1 個と、その表示幅を返す。s が空なら ("", 0)。
//
// ⚠️ **分割は必ずこれを通す。uniseg で切って Of で測らないこと** (issue 124)。
// 幅を 1 本にしても (issue 112)、**分割器がもう 1 本ある**と列不変条件
// `Of(dropToColumn(s,n)) == Of(s)-n` は成立しない: 境界が食い違えば「クラスタごとの幅の
// 総和 = 全体の幅」が破れるので、クラスタの幅を何で測ろうが直らない。
// 実測 2026-08-27 (2-rune クラスタの全域走査): uniseg で切ると違反 757 件。原因は Unicode の
// 版差で、uniseg v0.4.7 は 15.0 (graphemerules.go の宣言)、x/ansi v0.11.7 は 16。
// 例: "axࢗz" (U+0897 は 16 で追加された結合マーク) は Of=3 だが uniseg は 4 クラスタに割る。
// 件数は依存を上げるたびに動く (uniseg が 16 に上がれば 0、次の版でまた出る) ので、
// **版を追うのではなく出典を 1 本にして構造で閉じる**。
//
// 幅は ansi が分割と同時に返す値をそのまま使う (Of で測り直さない。同じ Method なので
// 値は一致するが、測り直す形は「分割と幅が別経路」を復活させる入口になる)。
//
// 契約: 入力が非空なら**必ず非空のクラスタ**を返す。呼び出し側は `s = s[len(cluster):]` で
// 前進するので、空を返すと無限ループになる (TUI では固まる)。この性質は
// TestFirstClusterWidthsSumToOf が 2 rune クラスタの全域で確かめている。
func FirstCluster(s string) (cluster string, width int) {
	if s == "" {
		return "", 0
	}
	return ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
}

// Truncate は表示幅 width まで切り詰め末尾に tail を付す。SGR は保持する。
//
// 🚨 ansi.Truncate の結果が Of で width を超えることがある: ASCII + VS16 (キーキャップ `1️⃣` 等) を
// 切り詰めの走査では幅 1、StringWidth では幅 2 と数える (x/ansi v0.11.7 の内部の食い違い。issue 416)。
// はみ出したときは、Of で width に収まる最も長い切り方を探し直す (この層の幅は Of が正本)。
func Truncate(s string, width int, tail string) string {
	r, _ := TruncateMeasure(s, width, tail)
	return r
}

// TruncateMeasure は Truncate と同じ切り詰めを行い、結果の表示幅も返す。切った行を呼び出し側で
// もう一度測らない (「切ってから余りを空白で埋める」形が毎フレーム全行で走る)。
//
// width <= 0 の扱いも含めて ansi.Truncate と同じ (違うのは Truncate の doc にあるはみ出しの直しだけ)。
// Clip / Cut の「width <= 0 なら空」の契約は持たない: ansi.Truncate から置き換える呼び出しの振る舞いを
// 変えないため。
func TruncateMeasure(s string, width int, tail string) (string, int) {
	// 収まる行 (多数派) は速い道の 1 回で済ませる。ansi.Truncate は収まるかどうかを先頭で
	// ansi.StringWidth (grapheme の走査) で確かめるので、任せると収まる行でも遅い走査が 1 回走る。
	// 速い道に乗らない行は ansi.Truncate の中の 1 回に任せる (ここで Of を呼ぶと遅い走査が 2 回になる)
	if w, ok := fastDispWidth(s); ok && w <= width {
		return s, w
	}
	return truncateOver(s, width, tail)
}

// truncateOver は TruncateMeasure の本体 (速い道で収まると分からなかった行)。ansi.Truncate は収まる行を
// そのまま返すので、収まる行が来ても結果は正しい。
//
// ほとんどの行は最初の 1 回で収まる。はみ出したときだけ、ansi へ渡す幅 t を二分探索して
// 「Of(結果) <= width」を満たす最大の t を取る (結果の幅は t について単調)。🚨 比べる相手は常に
// 要求された width で、t ではない: t と比べると、はみ出すたびに目標を詰めて削りすぎる
// (`Cut("x1️⃣y", 3)` が "x" になった。敵対的レビューで実証)。1 字ずつ詰める形は、キーキャップの
// 数だけ切り直すので長い行で遅い (O(はみ出し × 長さ))。二分探索なら O(長さ × log width)。
func truncateOver(s string, width int, tail string) (string, int) {
	r := ansi.Truncate(s, width, tail)
	w := Of(r)
	if width <= 0 || w <= width {
		return r, w
	}
	// t = 0 も ansi に聞く (幅 0 の byte — SGR・タブ等 — を残すので "" と決め打ちしない)。t = 0 の結果は
	// 幅 0 なので必ず収まり、ほかがすべて溢れても best は埋まる
	best, bestW := "", 0
	lo, hi := 0, width-1
	for lo <= hi {
		t := lo + (hi-lo)/2
		c := ansi.Truncate(s, t, tail)
		if cw := Of(c); cw <= width {
			best, bestW, lo = c, cw, t+1
		} else {
			hi = t - 1
		}
	}
	return best, bestW
}

// Slice は s の表示桁 [left, right) を切り出す (ansi.Cut と同じ。SGR は保持し、right <= left なら "")。
// 右端は Cut と同じく全角を跨がない側で切り、左端は ansi.TruncateLeft と同じく跨いだ全角を残す。
// ansi.Cut との違いは右端の切り方が Truncate の doc にあるはみ出しの直しを通ることだけ。
func Slice(s string, left, right int) string {
	if right <= left {
		return ""
	}
	head, _ := TruncateMeasure(s, right, "")
	if left <= 0 {
		return head
	}
	return ansi.TruncateLeft(head, left, "")
}

// SliceFrom は s の表示桁 left から末尾までを返す (Slice(s, left, Of(s)) と同じ。left が末尾以降なら "")。
func SliceFrom(s string, left int) string {
	return sliceFromKnown(s, Of(s), left)
}

// sliceFromKnown は幅 sw が分かっている s の SliceFrom (同じ行を測り直さない)。
func sliceFromKnown(s string, sw, left int) string {
	switch {
	case left >= sw:
		return ""
	case left <= 0:
		return s
	}
	return ansi.TruncateLeft(s, left, "")
}

// SplitAround は s から表示桁 [x, x+w) を抜いた左右を返す。左は [0, x) で全角を跨がない側で切り (幅 leftW <= x)、
// 右は x+w から末尾まで (Slice / SliceFrom と同じ切り方)。total は s の幅。x が [0, total) の外なら
// 抜く所が無いので、左右は空で total だけを返す (呼び出し側は s をそのまま使う)。
//
// 行の途中に別の字を差し込む (移動中のカード・枠の辺) のに使う。Slice と SliceFrom を別々に呼ぶと、
// 1 つの差し込みで同じ行の幅を 4 回測る (Slice の中・切った左・SliceFrom の中・呼び出し側の total)。
func SplitAround(s string, x, w int) (left string, leftW int, right string, total int) {
	total = Of(s)
	if x < 0 || x >= total {
		return "", 0, "", total
	}
	if x > 0 { // x == 0 の左は空 (Slice の right <= left と同じ)
		left, leftW = truncateOver(s, x, "")
	}
	return left, leftW, sliceFromKnown(s, total, x+w), total
}

// Clip は表示幅 width を超える行を、末尾に `…` を付けて切り詰める。SGR は保持する。
// width <= 0 なら "" (幅 0 以下に収まる表示は空しかない。呼び出し側の「幅 - 固定列」が
// 極小幅で負になったとき、行が枠を突き破らないように)。
func Clip(line string, width int) string {
	if width <= 0 {
		return ""
	}
	// fast-path: ANSI 無しなら整形式 UTF-8 で表示幅 ≤ byte 長が成り立つので、byte 長が
	// width 以内なら確実に幅内 = 幅の走査を省ける (画面の可視行の多数派がここを通る)。
	if len(line) <= width && strings.IndexByte(line, '\x1b') < 0 {
		return line
	}
	if Of(line) <= width {
		return line
	}
	return Truncate(line, width, "…")
}

// TruncateLeft は表示幅 width になるよう**先頭**を削り、頭に head (… 等) を付す。
// 末尾を残したいもの (ファイルパスの basename) に使う: 末尾から切ると「どのファイルか」が
// 分からなくなるため。幅計算は Of と同じモデルを通す (この層に一本化する規律)。
func TruncateLeft(s string, width int, head string) string {
	if Of(head) > width {
		head = "" // 印すら入らない幅では印を諦める (幅 0 以下なら、全部削って空になる)
	}
	sw := Of(s)
	if sw <= width {
		return s // 収まっているものは削らない (head の幅を足して削る量を出すと、収まる行まで削っていた)
	}
	drop := sw - width + Of(head)
	// 🚨 ansi.TruncateLeft の結果は Of で見ると width からずれる: 切れ目が全角の途中に落ちるとその字を
	// 残して 1 桁はみ出し (issue 416: TruncateLeft("あ", 1) が幅 2)、キーキャップは幅 1 と数えて削りすぎる。
	// 見積もった drop で幅ちょうどになればそれで確定 (それより広くはできない。ほとんどの行はここで終わる)。
	// 狭いときは 1 桁少なく削って収まらないことを確かめて確定する (全角の跨ぎ)。🚨 確かめる呼び出しを
	// 常にすると、status viewer の 1 フレームの確保が 56 回増えた (glogx の TestFrameAllocBudget)。
	// それでも決まらなければ、収まる最小の drop を二分探索する (結果の幅は drop について単調)。
	// どれも収まらなければ空を返す
	fits := func(d int) (string, int, bool) {
		r := ansi.TruncateLeft(s, d, head)
		w := Of(r)
		return r, w, w <= width
	}
	if r, w, ok := fits(drop); ok {
		if w == width {
			return r
		}
		if _, _, fewer := fits(drop - 1); !fewer {
			return r
		}
	}
	best, found := "", false
	lo, hi := 1, sw
	for lo <= hi {
		d := lo + (hi-lo)/2
		if r, _, ok := fits(d); ok {
			best, found, hi = r, true, d-1
		} else {
			lo = d + 1
		}
	}
	if !found {
		return ""
	}
	return best
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

// ClipMeasure は Clip と同じ切り詰めを行い、結果の表示幅も返す。
//
// 枠やスクロールバーの hot path 用: 「Clip で 1 回 + 埋め草の計算でもう 1 回」同じ行を走査すると、
// 収まる行 (多数派) の 2 回目が丸ごと無駄になる (glogx で 1 フレームの CPU の ~35% が幅計算に
// 残った実測 2026-07-29)。切り詰めた行だけは幅を測り直す (全角の境界で width-1 に落ちる場合に
// 埋め草を誤ると枠がずれるため、推定でなく実測する)。
func ClipMeasure(line string, width int) (string, int) {
	if width <= 0 {
		return "", 0 // Clip と同じ契約 (幅 0 以下に収まる表示は空だけ)
	}
	w := Of(line)
	if w <= width {
		return line, w
	}
	return truncateOver(line, width, "…")
}

// CutMeasure は Cut と同じ切り詰めを行い、結果の表示幅も返す (切った行をもう一度測らない)。
func CutMeasure(s string, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	return TruncateMeasure(s, width, "")
}

// Cut は s を表示幅 width まで切る (SGR は保持)。Clip との違いは**末尾に `…` を付けないこと**だけ:
// 重ねる板で覆う行の「見えている左側」をそのまま残す用途なので、切れた印を足すと覆い先と
// つながって見える。
//
// 末尾に reset は付かない。ansi.Truncate は自分で reset を足さず、入力にあった SGR を持ち越す
// だけなので、色を開いたままの入力は開いたまま返る (実測 2026-08-26)。開いた色は呼び出し側が
// 境界で閉じる。
func Cut(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return Truncate(s, width, "")
}

// isSGRTerminator は ESC シーケンス中の r がシーケンスを終端する最終バイトか (CSI の最終バイトは英字)。
// DropColumns / StripSGR が同じ終端判定を共有し、OSC 等へ広げるときに 1 箇所だけ直せばよいようにする。
func isSGRTerminator(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// DropColumns は s のうち表示列 n (0-based) 以降の suffix を返す。n より前に現れた SGR は結果の
// 先頭で replay するので、残った suffix は元の色を保つ (Cut が「左の prefix を残す」の鏡像)。
// 浮かせた板の右側に背景の行を合成するのに使う。
//
// 全角グリフが列 n をまたぐ場合はそのグリフを落とし、列 n に揃うよう空白で左詰めする。列 n が
// 内容の末尾以降なら "" (右に何も無い)。不変条件: Of(DropColumns(s, n)) == Of(s) - n。
func DropColumns(s string, n int) string {
	if n <= 0 {
		return s
	}
	var sgr strings.Builder
	w := 0
	i := 0 // s へのバイト index
	for i < len(s) && w < n {
		if s[i] == '\x1b' { // SGR (幅 0): 後で replay するため蓄積
			j := i + 1
			for j < len(s) && !isSGRTerminator(rune(s[j])) {
				j++
			}
			if j < len(s) {
				j++ // 終端バイトも含める
			}
			sgr.WriteString(s[i:j])
			i = j
			continue
		}
		// 次の grapheme クラスタを 1 個。🚨 等の複数 rune クラスタを分断・誤幅にしない
		// (分割と幅は同じエンジンから同時に受け取る。FirstCluster の doc)
		cluster, cw := FirstCluster(s[i:])
		if w+cw > n { // 全角グリフが cut をまたいだ: そのグリフを落とし列 n に揃えて空白で埋める
			i += len(cluster)
			return sgr.String() + PadSpaces((w+cw)-n) + s[i:]
		}
		w += cw
		i += len(cluster)
	}
	if i >= len(s) {
		return "" // 列 n は内容の末尾以降: 右側に残すものが無い
	}
	return sgr.String() + s[i:]
}

// StripSGR は s から ESC シーケンスを取り除く。
func StripSGR(s string) string {
	if strings.IndexByte(s, '\x1b') < 0 {
		return s // ESC 無しは Builder の確保も rune の走査も不要
	}
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			if isSGRTerminator(r) {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
