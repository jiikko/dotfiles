// Package termsafe は「外部由来の文字列を端末へ出す前に無害化する」単一の関門。
//
// glogx が表示に出すテキストは全て自分以外が書いたもの (git のコミットメッセージ・CI の
// ログ・issue の markdown・作業ツリーのファイル名) で、そこに端末制御シーケンスが入ると
// 画面破壊・タイトル書き換え・OSC52 によるクリップボード書き込みが「表示しただけ」で起きる。
// 出所ごとに書き分けると必ずどこかが漏れる (実際 issue markdown と git status のパスが
// 漏れていた) ため、無害化はこのパッケージに集約し、各パッケージは入口で通すだけにする。
//
// main / issues の両方から使うため独立パッケージにしてある (main の非公開関数だと issues 側が
// 呼べず、コピーすると二重管理になる)。bubbletea にも幅計算にも依存しない純粋な文字列処理。
package termsafe

import (
	"strings"
	"unicode/utf8"
)

// DetailLine は CI 由来の表示文字列 (ログ・annotations・job 名) を無害化する。
//
//   - タブ → スペース 4: 幅計算 (dispWidth) は \t を幅 0 と数えるが端末は 8 桁タブストップへ
//     展開するため、右枠の桁計算がずれて行が折り返し、インライン再描画が崩壊する (実測バグ)
//   - ANSI は SGR (ESC[…m = 色/装飾) だけを通す allowlist。それ以外の CSI (画面消去・
//     カーソル移動) や OSC/DCS 等 (OSC52 のクリップボード書き込み・タイトル変更) は、
//     第三者 (任意の status インテグレーション等) が混入させられる端末制御シーケンス注入の
//     経路になるため、シーケンスごと落とす
//   - BOM (GitHub のログ先頭に付く U+FEFF) と \r 等の残る制御文字は落とす
//   - VS16 付き絵文字 (⚠️ 等) は描画エンジンと端末で幅が食い違いガタつくため bare 記号へ
//     正規化する (DropEmojiVS16 参照)
func DetailLine(s string) string { return sanitize(s, policy{keepTabs: false, keepSGR: true}) }

// LineKeepTabs は DetailLine のタブ非展開版。git 由来テキスト (commit の
// subject/message/body・verbatim 行) は、静的出力 (--no-pager / パイプ) で git log との
// パリティ (タブ保持) が契約されている (render_test の …TabsInTUI が pin) ため、入口では
// 制御シーケンスの除去だけ行い、タブ展開は TUI 描画側 (Width > 0 の mediumLines /
// decorateVerbatim) が担う。
func LineKeepTabs(s string) string { return sanitize(s, policy{keepTabs: true, keepSGR: true}) }

// PlainLine は SGR も含めて ANSI を全て落とす版。「色を持つ正当な理由がない外部テキスト」用:
// issue markdown の本文・見出し、作業ツリーのファイル名 (git status のパス) など。
//
// なぜ SGR まで落とすか: これらの出所が色を送ってくるのは細工されたときだけで、通す利益が無い。
// 逆に通すと、描画側が自分で塗った色の途中に外部由来の SGR が挟まって色が行をまたいで滲む
// (issue viewer は span ごとに自前で塗るため実害が出る)。git / CI ログは `--color` の出力を
// 表示する契約があるので DetailLine 側 (SGR 許可) を使う。
func PlainLine(s string) string { return sanitize(s, policy{}) }

// PlainLineKeepTabs は PlainLine のタブ非展開版。タブを「自前のタブストップ揃え」で展開する
// 整形層 (issues の expandTabs) の入口で使う。
//
// ⚠️ ここでタブを潰すと、桁揃えが「タブストップ」から「一律 4 スペース」に変わって崩れる
// (`ab<TAB>c` が `ab  c` でなく `ab    c` になる)。無害化はタブの有無に関係なく成立するので、
// タブの解釈は後段の整形層に任せる。無害化だけを掛けたいがタブ展開はしたくない、が使い分けの軸。
func PlainLineKeepTabs(s string) string { return sanitize(s, policy{keepTabs: true}) }

// PlainBlock は PlainLine の「改行だけ残す」版。**1 件が複数行の塊として描かれる**自由文
// (brew doctor の警告本文のように、段落ごと畳んで出すもの) 用。
//
// 🚨 改行を残す = **行数を相手に決めさせる**ということ。1 行として描く場所でこれを使うと、
// 偽の行を差し込まれて固定高パネルの行数が狂う (幅を数えるテストは改行を検出しないので
// 素通りする)。「1 件 = 1 行」の場所では必ず PlainLine を使うこと。行数・長さの上限は
// 用途ごとの関心なので、この関数ではなく呼び出し側が持つ。
func PlainBlock(s string) string { return sanitize(s, policy{keepNewlines: true}) }

// DropEmojiVS16 は絵文字異体字セレクタ VS16 (U+FE0F) を除去して、⚠️❤️✔️ 等の
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
// CI ログ / annotations 由来 (DetailLine)、job 名 (detailsOf)、issue markdown (issues/)。
// 自前の静的テキストはソースに bare 記号を直接書く (Usage() は正規化経路を通らないため
// TestUsageHasNoVS16 で守る)。VS15 (U+FE0E) は元々全層で幅 1 なので触らない。
func DropEmojiVS16(s string) string {
	const vs16 = '\ufe0f'               // VS16。⚠️ エスケープで書く (直接書くと不可視文字になる)
	if !strings.ContainsRune(s, vs16) { // 多数派 (絵文字なし) は無 alloc で素通り
		return s
	}
	return strings.ReplaceAll(s, string(vs16), "")
}

// sanitize は 1 行分の無害化本体。keepTabs=false はタブをスペース 4 へ展開し、keepSGR=false は
// SGR も含めて ANSI を全て落とす。
//
// ⚠️ 改行を保存しない (\n は制御文字として落ちる)。複数行を渡す呼び出し側は行へ分割してから
// 1 行ずつ通すこと。
// policy は sanitize の分岐 (どれを残すか)。bool の位置引数を増やすと呼び出し側が
// `sanitize(s, false, true, false)` になって読めないので、名前で渡す。
type policy struct {
	keepTabs     bool // タブをスペース 4 へ展開しない
	keepSGR      bool // 色 / 装飾 (ESC[…m) を残す
	keepNewlines bool // 改行を残す (複数行が正常な塊のみ)
}

func sanitize(s string, p policy) string {
	s = DropEmojiVS16(s)
	if !needsSanitize(s) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\t' && p.keepTabs:
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("    ")
		case r == '\n' && p.keepNewlines:
			b.WriteRune(r)
		case r == '\x1b':
			i = skipEscape(&b, rs, i, p.keepSGR)
		case isC1(r):
			i = skipC1(rs, i)
		case mustStrip(r):
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// needsSanitize は「そのまま返してよいか」の判定 (fast path のゲート)。
//
// ⚠️ mustStrip と同じ述語を使うこと。ここと本体で判定を別々に書くと、片方だけ更新して
// 静かに素通しする穴が空く (実際 C1 制御文字がこの乖離で丸ごと素通ししていた)。
// 不正な UTF-8 も対象にする: []rune 変換で U+FFFD に化けるため、fast path で素通しすると
// 「同じ入力なのに他の文字の有無で結果が変わる」不一致になる。
func needsSanitize(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsFunc(s, mustStrip)
}

// mustStrip はそのままでは端末へ出せない rune。C0 制御文字 / DEL / C1 制御文字 / BOM。
//
// C1 (U+0080-U+009F) を含めるのは、8bit 版の CSI (U+009B) / OSC (U+009D) が 7bit の
// ESC[ / ESC] と同じ制御機能を持つため。UTF-8 の端末が解釈するかは実装依存だが、
// 「解釈しない端末が多い」は安全の根拠にならないので出さない。git のブランチ名は ASCII 制御
// 文字しか禁じておらず、C1 入りの ref は正当に作れる (= 外部から実際に入ってくる)。
func mustStrip(r rune) bool {
	return r < 0x20 || r == 0x7f || isC1(r) || r == '\ufeff'
}

func isC1(r rune) bool { return r >= 0x80 && r <= 0x9f }

// skipEscape は rs[i] の ESC から始まるシーケンスを解釈し、SGR (色/装飾) だけを b へ
// 書き出してそれ以外は捨てる。keepSGR=false なら SGR も捨てる。
// 戻り値は消費したシーケンスの最終 index。
//
// ⚠️ 終端が見つからないときは導入子 (ESC) だけを消費して残りを本文として扱う。行末まで
// 捨てると、`BUILD <ESC>]FAILED: 12 tests failed` のような入力で「失敗の記録が黙って
// 消える」= 制御シーケンスを使わずに文字を隠せてしまう。ESC さえ落とせば残りはただの文字列
// なので端末は何も解釈しない (安全性を落とさずに情報を保てる)。
func skipEscape(b *strings.Builder, rs []rune, i int, keepSGR bool) int {
	if i+1 >= len(rs) {
		return i // 末尾の裸 ESC は捨てる
	}
	switch rs[i+1] {
	case '[': // CSI: ESC [ <param/intermediate 0x20-0x3f>* <final 0x40-0x7e>
		j := i + 2
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x3f {
			j++
		}
		if j >= len(rs) || rs[j] < 0x40 || rs[j] > 0x7e {
			return i // 終端の無い CSI: 導入子だけ落とす
		}
		if keepSGR && rs[j] == 'm' && runesOnly(rs[i+2:j], "0123456789;:") {
			b.WriteString(string(rs[i : j+1])) // SGR のみ通す
		}
		return j
	case ']', 'P', '_', '^', 'X': // OSC / DCS / APC / PM / SOS: ST か BEL まで捨てる
		if end := stringTerminator(rs, i+2); end >= 0 {
			return end
		}
		return i // 終端の無い OSC/DCS 等: 導入子だけ落とす
	default:
		return i + 1 // その他の 2 文字エスケープ (ESC 7 等) は捨てる
	}
}

// skipC1 は 8bit 版の制御機能 (U+0080-U+009F) を、対応する 7bit 版と同じ範囲だけ消費する。
// 戻り値は消費した最終 index。
//
// ⚠️ 8bit CSI の SGR は通さない (7bit と違って allowlist を作らない)。色を 8bit CSI で送って
// くる正当な出所が無い一方、通すと「解釈する端末では色以外も効く」余地を残すため。
func skipC1(rs []rune, i int) int {
	switch rs[i] {
	case 0x9b: // CSI
		j := i + 1
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x3f {
			j++
		}
		if j >= len(rs) || rs[j] < 0x40 || rs[j] > 0x7e {
			return i
		}
		return j
	case 0x90, 0x98, 0x9d, 0x9e, 0x9f: // DCS / SOS / OSC / PM / APC
		if end := stringTerminator(rs, i+1); end >= 0 {
			return end
		}
		return i
	case 0x8e, 0x8f: // SS2 / SS3 は次の 1 文字が対象
		if i+1 < len(rs) {
			return i + 1
		}
		return i
	default:
		return i // 制御機能を導入しない C1 は単体で落とす
	}
}

// stringTerminator は from から探した ST (7bit の ESC \ / 8bit の U+009C) か BEL の index を
// 返す (-1 = 見つからない)。OSC / DCS / PM / APC / SOS が共有する終端規則。
func stringTerminator(rs []rune, from int) int {
	for j := from; j < len(rs); j++ {
		if rs[j] == '\a' || rs[j] == 0x9c {
			return j
		}
		if rs[j] == '\x1b' && j+1 < len(rs) && rs[j+1] == '\\' {
			return j + 1
		}
	}
	return -1
}

func runesOnly(rs []rune, allowed string) bool {
	for _, r := range rs {
		if !strings.ContainsRune(allowed, r) {
			return false
		}
	}
	return true
}
