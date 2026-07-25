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
// なぜ幅統一 (dispWidth) と併用が必要か: VS16 付きの字は幅の解釈が層ごとに割れる。
// bare 記号は全層で幅 1 に一致するので、割れる文字を出さないための正規化を表示入口で行う。
//
// 実測値 (2026-07-25、CPR と tmux cursor_x で計測):
//
//	                 x/ansi  uniseg  runewidth  tmux 3.7b
//	bare ⚠ (U+26A0)       1       1          1          1   ← 全層一致
//	⚠+VS16                2       2          1          2
//	⚠+VS15                1       1          1          1
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
