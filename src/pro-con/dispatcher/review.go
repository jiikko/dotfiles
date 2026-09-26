package dispatcher

// 敵対的レビューの担い手 (issue 514)。設定 review が codex なら、PG は commit 前の最終ゲートの敵対的レビューを codex exec で回す。
// 担い手を PG への指示と取り込みの係への知らせに書くのはここだけ (設定の値を差し込む)。レビューの作法の正本は
// ~/.claude/skills/codex-review/SKILL.md の「敵対的モード」で、ここには書き写さない。
// 🚨 claude (既定) のときは何も足さない: 514 の前の動き (PG が自分のサブエージェントで回す) のまま

import (
	"context"
	"fmt"
	"strings"

	"pro-con/eventlog"
	"pro-con/store"
)

// Review は今の敵対的レビューの担い手と、PG に渡す codex の実体。
type Review struct {
	Mode string // store.ReviewClaude / store.ReviewCodex
	From string // Mode の出どころ (ReviewFrom*)
	// Codex は codex の実体の絶対パス (ResolveCodex。解決できなければ空で、CodexErr が理由)
	Codex    string
	CodexErr string
}

// Mode の出どころ (store.DispatcherState.ReviewFrom)。
const (
	ReviewFromSetting = "設定"
	ReviewFromConfig  = "config.toml"
	ReviewFromDefault = "既定"
)

// review は今の担い手。設定 (pro-con config set review / 設定画面) > config.toml の review > 既定 (claude)。
func (d *Dispatcher) review() Review {
	r := Review{Mode: store.ReviewClaude, From: ReviewFromDefault, Codex: d.Codex.Path, CodexErr: d.CodexErr}
	switch {
	case d.settings.Review != "":
		r.Mode, r.From = d.settings.Review, ReviewFromSetting
	case d.ReviewDefault != "":
		r.Mode, r.From = d.ReviewDefault, ReviewFromConfig
	}
	return r
}

// resolveCodex は担い手が codex になった最初の Tick に codex の実体を 1 回だけ解き、結果を出来事に残す。
// claude のままなら解かない (既定の動き・起動の速さを変えない)。解けなくても止めない (PG が Claude で代わりに回す)
func (d *Dispatcher) resolveCodex(ctx context.Context) []eventlog.Event {
	if d.codexTried || d.ResolveCodex == nil || d.review().Mode != store.ReviewCodex {
		return nil
	}
	d.codexTried = true
	cx, err := d.ResolveCodex(ctx)
	if err != nil {
		d.CodexErr = err.Error()
		return []eventlog.Event{ev(eventlog.KindLaunch, "", "", "codex の実体を解けない (敵対的レビューは、PG が Claude で代わりに回す): "+d.CodexErr)}
	}
	d.Codex = cx
	return []eventlog.Event{ev(eventlog.KindLaunch, "", "", fmt.Sprintf("codex は %s (%s) を使う (敵対的レビューの担い手 = codex)", cx.Path, cx.Version))}
}

// codexFallbackNote は codex が使えず Claude で代わりに回したときに、PG がカードの履歴に残す一言の形。
const codexFallbackNote = "codex が使えない (<理由>) ので、敵対的レビューを Claude のサブエージェントで代わりに回した"

// pgRule は PG の規律に足す行 (claude なら空)。id はカード ID。
func (r Review) pgRule(id string) string {
	if r.Mode != store.ReviewCodex {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- 敵対的レビュー (判断ロジック・境界・状態遷移・外部 I/O が動いた変更の commit 前の最終ゲート) は、設定 review = codex (%s) により codex で回す。"+
		"作法は `~/.claude/skills/codex-review/SKILL.md` の「敵対的モード」(読み取りのみ。パターン G / H)。指摘は今と同じく実コードで裏を取ってから採る\n", r.From)
	fallback := fmt.Sprintf("黙って飛ばさず Claude のサブエージェントで代わりに回し、理由 (コマンドの出力) を書いたファイルを "+
		"`pro-con card attach %s <ファイル> --note \"%s\"` で添付する (履歴に残る)", id, codexFallbackNote)
	if r.Codex == "" {
		why := firstNonEmpty(r.CodexErr, "dispatcher が codex の実体を解決していない")
		fmt.Fprintf(&b, "  - 🚨 dispatcher は codex の実体を見つけられなかった (%s)。%s\n", why, fallback)
		return b.String()
	}
	fmt.Fprintf(&b, "  - codex は実体の絶対パス `%s` で呼ぶ (PATH の素の名前・シェルの関数を使わない)。時間がかかっても `pro-con card run` には頼まず自分で走らせる\n", r.Codex)
	fmt.Fprintf(&b, "  - 回す前に `ratelimit -source codex -check` を見る。rc≠0 (1 = 枠が尽きかけ / 3 = 判定できない) のときと、codex が rc≠0 で終わった (認証切れ・枠切れ等) ときは、%s\n", fallback)
	fmt.Fprintf(&b, "  - codex で回したら、その出力 (`-o` のファイル) を `pro-con card attach %s <ファイル> --note \"codex の敵対的レビュー\"` で添付する (証拠)\n", id)
	return b.String()
}

// codexReviewLine は取り込みの係への知らせに足す行。ids は、敵対的レビューを codex で回すよう指示して起動した PG のカード (無ければ空)。
func codexReviewLine(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return "敵対的レビューを codex で回すよう指示して PG を起動したカード: " + strings.Join(ids, ", ") + "。判断ロジック・境界・状態遷移が動いていれば、" +
		"履歴に「codex の敵対的レビュー」の添付か、codex が使えず Claude で代わりに回した添付があるかを `pro-con card show <カード>` で確かめる。どちらも無ければ差し戻す。\n"
}
