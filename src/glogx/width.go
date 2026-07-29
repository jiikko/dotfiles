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
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return ansi.StringWidth(s)
		}
	}
	return len(s)
}

// truncateDisp は表示幅 width まで切り詰め末尾に tail を付す。SGR は保持する。
func truncateDisp(s string, width int, tail string) string { return ansi.Truncate(s, width, tail) }

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

// clusterWidth は grapheme クラスタ 1 個分の表示幅を返す (dispWidth と同一の幅モデル)。
// ⚠️ (U+26A0+U+FE0F) のような複数 rune のクラスタを rune 単位で数えて分断/誤幅にしない
// ため、クラスタ単位で幅を計算する必要のある dropToColumn 等が使う。
func clusterWidth(cluster string) int { return uniseg.StringWidth(cluster) }

// dropEmojiVS16 は絵文字異体字セレクタ VS16 (U+FE0F) を除去して、⚠️❤️✔️ 等の
// 「記号 + VS16」を bare な text presentation (⚠❤✔) へ倒す。
//
// なぜ幅統一 (dispWidth) と併用が必要か: VS16 付きの字は幅の解釈が層ごとに割れる。
// bare 記号は全層で幅 1 に一致するので、割れる文字を出さないための正規化を表示入口で行う。
//
// 実測値 (2026-07-25、CPR と tmux cursor_x で計測):
//
//	                 x/ansi  uniseg  displaywidth  runewidth  tmux 3.7b
//	bare ⚠ (U+26A0)       1       1             1          1          1   ← 全層一致
//	⚠+VS16                2       2             2          1          2
//	⚠+VS15                1       1             1          1          1
//	国旗 🇯🇵                2       2             2          2          -   (tmux 未計測)
//	● (ambiguous)         1       1             1        1/2          -   (runewidth は locale 依存)
//
// (displaywidth 列は x/ansi v0.11 以降が内部で使う幅ライブラリ。x/ansi 経由なので独立した
// 「第 3 の実装」ではない。runewidth は v0.0.27 でも VS16 を 1 と数え、ambiguous は
// LANG=ja_JP.* で 2 / LANG=C で 1 と実行環境で変わる = 唯一の外れ値のまま。
// この表はすべて GraphemeWidth モデルでの値。エンジン側が使う WcWidth との差は上のブロック参照。
// 2026-07-25 の依存更新 (ultraviolet / runewidth v0.0.27) 後に再計測済み)
//
// ⚠️ 過去の記述の訂正: 4c8ee8d は「ユーザーの端末は VS16 に 1 マスしか割り当てない
// (エンジン 2 と食い違う)」と書き、3c74ddf は逆に「端末が幅 2 で数える」と書いていた。
// tmux 3.7b の実測は 2 で、少なくとも tmux 層はエンジンと一致する。つまり
// 「エンジン 2 vs 端末 1」という当時の前提は少なくとも今の環境では成立しない。
// それでも正規化は続ける価値がある: runewidth が 1 と数えるため、glogx 自身が使わなくても
// 依存ライブラリや外部ツールが混在すると割れる余地が残る (bare なら全層 1 で一致する)。
//
// ⚠️ 意図的なトレードオフ: この正規化で絵文字は「脱色」される (カラー絵文字 → 端末フォントの
// 単色グリフ)。VS16 は幅だけでなく「カラー絵文字として描け」の指示でもあるため、外すと副作用として
// 色が落ちる。幅の安定と引き換えに受け入れている (ユーザー確認 2026-07-25: 脱色を確認した上で
// 揺れが止まったことを優先)。色を取り戻したいなら VS16 を戻すのではなく、下記いずれかで
// 「幅を割らせずに色を出す」方向で検討すること:
//   - Unicode Core Mode (DEC 2027, ansi.SetUnicodeCoreMode) を端末が支持するなら、grapheme
//     cluster 単位の幅計算をエンドツーエンドで揃えられるので VS16 を残せる。対応端末は限られる
//     (Contour/foot 等) ので ansi.RequestUnicodeCoreMode で問い合わせてから使う
//   - VS15 (U+FE0E) を明示的に付ける。幅は全層 1 で安定するが text presentation なので色は出ない
//     (脱色の解決にはならない。bare との違いは「端末の既定解釈に委ねない」点だけ)
//
// ⚠️ 未計測の層が 1 つ残っている: tmux の外側の端末エミュレータ本体。ここは TTY が要るので
// エージェント環境からは測れない。`go run ./tools/width-probe` を実端末で (tmux の内と外の
// 両方で) 走らせると各層の割り当てを端末自身に問い合わせて表になる。ズレが再発したら
// 推測で対策を足す前にまずこれを走らせること (この問題は前提を測らずに対策を重ねて
// revert 済みの試行が 1 件ある: 3c74ddf → 3e5787d)。
//
// 適用箇所は「表示に出る外部由来テキスト」の入口すべて: git 由来 (gitlog.go の 2 入口)、
// CI ログ / annotations 由来 (sanitizeDetailLine)、job 名 (detailsOf)。自前の静的テキストは
// ソースに bare 記号を直接書く (Usage() は正規化経路を通らないため TestUsageHasNoVS16 で守る)。
// VS15 (U+FE0E) は元々全層で幅 1 なので触らない。
func dropEmojiVS16(s string) string {
	const vs16 = '\ufe0f'               // VS16 (emoji presentation selector)
	if !strings.ContainsRune(s, vs16) { // 多数派 (絵文字なし) は無 alloc で素通り
		return s
	}
	return strings.ReplaceAll(s, string(vs16), "")
}
