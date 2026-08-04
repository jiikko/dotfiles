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

import "strings"

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
func DetailLine(s string) string { return sanitize(s, false, true) }

// LineKeepTabs は DetailLine のタブ非展開版。git 由来テキスト (commit の
// subject/message/body・verbatim 行) は、静的出力 (--no-pager / パイプ) で git log との
// パリティ (タブ保持) が契約されている (render_test の …TabsInTUI が pin) ため、入口では
// 制御シーケンスの除去だけ行い、タブ展開は TUI 描画側 (Width > 0 の mediumLines /
// decorateVerbatim) が担う。
func LineKeepTabs(s string) string { return sanitize(s, true, true) }

// PlainLine は SGR も含めて ANSI を全て落とす版。「色を持つ正当な理由がない外部テキスト」用:
// issue markdown の本文・見出し、作業ツリーのファイル名 (git status のパス) など。
//
// なぜ SGR まで落とすか: これらの出所が色を送ってくるのは細工されたときだけで、通す利益が無い。
// 逆に通すと、描画側が自分で塗った色の途中に外部由来の SGR が挟まって色が行をまたいで滲む
// (issue viewer は span ごとに自前で塗るため実害が出る)。git / CI ログは `--color` の出力を
// 表示する契約があるので DetailLine 側 (SGR 許可) を使う。
func PlainLine(s string) string { return sanitize(s, false, false) }

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
func sanitize(s string, keepTabs, keepSGR bool) string {
	s = DropEmojiVS16(s)
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\ufeff' }) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\t' && keepTabs:
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("    ")
		case r == '\x1b':
			i = keepOnlySGR(&b, rs, i, keepSGR)
		case r < 0x20 || r == 0x7f || r == '\ufeff':
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// keepOnlySGR は rs[i] の ESC から始まるシーケンスを解釈し、SGR (色/装飾) だけを b へ
// 書き出してそれ以外は捨てる。keepSGR=false なら SGR も捨てる。
// 戻り値は消費したシーケンスの最終 index。
func keepOnlySGR(b *strings.Builder, rs []rune, i int, keepSGR bool) int {
	if i+1 >= len(rs) {
		return i // 末尾の裸 ESC は捨てる
	}
	switch rs[i+1] {
	case '[': // CSI: ESC [ <param/intermediate 0x20-0x3f>* <final 0x40-0x7e>
		j := i + 2
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x3f {
			j++
		}
		if j >= len(rs) {
			return len(rs) - 1 // 途切れた CSI は捨てる
		}
		if keepSGR && rs[j] == 'm' && runesOnly(rs[i+2:j], "0123456789;:") {
			b.WriteString(string(rs[i : j+1])) // SGR のみ通す
		}
		return j
	case ']', 'P', '_', '^', 'X': // OSC / DCS / APC / PM / SOS: ST (ESC \) か BEL まで捨てる
		for j := i + 2; j < len(rs); j++ {
			if rs[j] == '\a' {
				return j
			}
			if rs[j] == '\x1b' && j+1 < len(rs) && rs[j+1] == '\\' {
				return j + 1
			}
		}
		return len(rs) - 1
	default:
		return i + 1 // その他の 2 文字エスケープ (ESC 7 等) は捨てる
	}
}

func runesOnly(rs []rune, allowed string) bool {
	for _, r := range rs {
		if !strings.ContainsRune(allowed, r) {
			return false
		}
	}
	return true
}
