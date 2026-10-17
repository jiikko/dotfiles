package main

import (
	"fmt"
	"slices"
	"strings"

	"glogx/internal/commitview"
	"glogx/internal/gitdata"
	"glogx/internal/term"

	"github.com/jiikko/dotfiles/src/tuikit/spinner"
)

// 状態記号は絵文字ではなく 1 カラム記号を使う (端末幅とフォント差異の影響を抑える: issue の設計)。

// StatusGlyph は状態 1 つ分の記号。どの状態も表示幅 2 に揃える: 幅 1 の文字と幅 2 の絵文字を
// 同じ列に混ぜると、桁数が合っていても縦に揃って見えない。絵文字は色を自前で持つので ANSI 色を
// 付けない。実行中と取得中は回るスピナー (幅 1 の点字なので空白で埋める)。色で見分ける:
// 実行中は黄、取得中はシアン。glyph (今のコマ) を渡さない経路 (静的出力) では先頭のフレームで止まって出る。
func StatusGlyph(state gitdata.CIState, colored bool, glyph string) string {
	switch state {
	case gitdata.StateSuccess:
		return "✅"
	case gitdata.StateFailure:
		return "❌"
	case gitdata.StatePending:
		return term.Paint(term.FillRight(spinnerOr(glyph), 2), term.AnsiYellow, colored)
	case gitdata.StateNeutral:
		return "🚫"
	case gitdata.StateNone:
		return "⬜"
	case gitdata.StateUnpushed:
		return "🔼"
	case gitdata.StateUnknown:
		return "❓"
	default:
		return term.Paint(term.FillRight(glyph, 2), term.AnsiCyan, colored)
	}
}

func spinnerOr(glyph string) string {
	if glyph == "" {
		return spinner.Frame(0)
	}
	return glyph
}

func stateFor(statuses map[string]gitdata.CIState, sha string) gitdata.CIState {
	if state, ok := statuses[sha]; ok {
		return state
	}
	return gitdata.StateLoading
}

// Line は描画 1 行分。TUI のビューポートとカーソル位置決めが CommitIdx/Header を使う。
type Line struct {
	Text      string
	CommitIdx int  // どのコミットに属する行か (-1 = どれでもない)
	Header    bool // コミットヘッダー行 (カーソルが乗る行) か
}

// RenderOpts は描画パラメータ。静的出力 (非 TTY / 最終出力) と TUI ビューの共通入力。
type RenderOpts struct {
	Oneline bool
	Colored bool
	Spinner string
	Width   int                       // >0 ならコミットメッセージを端末幅で折り返す (TUI 用)。
	Decor   *gitdata.DecorColors      // decoration の色 (nil = git 既定色)
	PRs     map[string]*gitdata.PRRef // コミット行末尾の PR バッジ (#123)。nil 値 = PR なし
	// Verbatim は git log 実出力の取り込み行 (VerbatimLines の結果)。非 nil なら
	// medium 形式の自前再構築 (mediumLines) の代わりにこれを装飾して使う = 見た目が
	// git log と機械的に一致する (ユーザー要望 2026-07-19「人力で寄せるのはめんどい」)。
	// nil は fallback (git log の実行/照合に失敗した場合と --oneline)
	Verbatim []Line
	// PushAnim は push 成功演出 (startPushAnim) 中。境界罫線は普段 dim の位置マーカー
	// だが、演出の主役 (1 段ずつ上へスライドする) になるため緑太字の pushing ラベルで
	// 目立たせる (dim のままでは動きが視認できない: ユーザーフィードバック 2026-07-23)
	PushAnim bool
	// HasRepo は GitHub repo が解決できているか。「全部 push 済み ✓」の先頭マークの
	// ゲート (remote 無し repo では push 状態が不明なだけなので出さない)
	HasRepo bool
}

func (o RenderOpts) decorColors() gitdata.DecorColors {
	if o.Decor != nil {
		return *o.Decor
	}
	return gitdata.DefaultDecorColors()
}

// subjectWidthCap は --oneline で subject 列を揃える幅の上限。極端に長い subject 1 件で
// 全行が右へ流れるのを防ぐ。
const subjectWidthCap = 60

// defaultBoundaryWidth は静的出力 (Width=0) 時の push 境界の罫線幅。subjectWidthCap と
// 値が同じなのは偶然で意味は無関係 (端末幅の代替 vs subject 列上限)。片方だけ変えてよい。
const defaultBoundaryWidth = 60

// RenderLines はコミット列を描画行へ展開する。既定は git log 標準 (medium) 形式、
// --oneline はコンパクト 1 行形式。CI 記号はコミットヘッダー行にだけ付く (issue の設計)。
func RenderLines(commits []gitdata.Commit, statuses map[string]gitdata.CIState, o RenderOpts) []Line {
	var lines []Line
	switch {
	case o.Oneline:
		subjectWidth := 0
		authorWidth := 0
		for _, c := range commits {
			subjectWidth = max(subjectWidth, term.DispWidth(c.Subject))
			authorWidth = max(authorWidth, term.DispWidth(c.Author))
		}
		subjectWidth = min(subjectWidth, subjectWidthCap)
		for i, c := range commits {
			lines = append(lines, Line{Text: renderOnelineRow(c, stateFor(statuses, c.SHA), subjectWidth, authorWidth, o), CommitIdx: i, Header: true})
		}
	case o.Verbatim != nil:
		lines = decorateVerbatim(commits, statuses, o)
	default:
		for i, c := range commits {
			if i > 0 {
				lines = append(lines, Line{Text: "", CommitIdx: i - 1})
			}
			lines = append(lines, mediumLines(c, i, stateFor(statuses, c.SHA), o)...)
		}
	}
	return insertPushBoundary(lines, commits, statuses, o)
}

// insertPushBoundary は「どこまで push したか」の視覚化 (ユーザー選定 2026-07-19:
// 背景塗りつぶし案は却下、カーソルの行全体 bg 塗りと干渉するため):
//   - 未 push と push 済みが混在 → 先頭に件数サマリ (── origin (↑N unpushed) ──) を挿し、
//     さらに実際の push 境界の位置には dim の罫線 (── origin ──) を挿す。件数を先頭に置くのは
//     複数行表示だと境界がスクロール先に隠れるため (ユーザー要望 2026-07-22)
//   - 表示範囲が全部 push 済み → 先頭に ── origin (all pushed ✓) ── を挿す
//     (境界が先頭 = 見えている範囲は全部 origin 済み、という同じ語彙。ユーザー要望)
//
// 全部未 push / push 状態不明 (HasRepo=false) のときは何も挿さない。push/pull で
// statuses が変わると lines は再構築されるため、マークは常に現状を反映する。
func insertPushBoundary(lines []Line, commits []gitdata.Commit, statuses map[string]gitdata.CIState, o RenderOpts) []Line {
	if len(commits) == 0 {
		return lines
	}
	boundary := -1 // push 済み先頭のコミット index
	unpushed := 0
	for _, c := range commits {
		if stateFor(statuses, c.SHA) == gitdata.StateUnpushed {
			unpushed++
		}
	}
	if unpushed > 0 && unpushed < len(commits) {
		boundary = unpushed // commits は新しい順で未 push が先頭に並ぶ
	}
	allPushed := unpushed == 0 && o.HasRepo
	if boundary < 0 && !allPushed {
		return lines
	}
	width := o.Width
	if width <= 0 {
		width = defaultBoundaryWidth
	}
	// ラベル付き origin 行を組む共通ヘルパー。head/罫線は dim、ラベルだけ色+太字で強調。
	labeledRule := func(label, labelColor string) string {
		head := "── origin"
		gap := " "
		dashes := strings.Repeat("─", max(width-term.DispWidth(head+label+gap), 0))
		return term.Paint(head, term.AnsiDim, o.Colored) +
			term.Paint(label, labelColor, o.Colored) +
			term.Paint(gap+dashes, term.AnsiDim, o.Colored)
	}
	if allPushed {
		// 全 push 済みは「達成」状態なのでラベルだけ緑+太字で強調する (dim な罫線と差を
		// つけて視覚的に目立たせる。ユーザー要望)。罫線部分は従来どおり dim
		text := labeledRule(" (all pushed ✓)", term.AnsiGreen+term.AnsiBold)
		return append([]Line{{Text: text, CommitIdx: 0}}, lines...)
	}
	// 混在時: 未 push 件数 (= origin より何コミット先行しているか) を all pushed と同じ
	// 「先頭」に赤+太字で出す。複数行表示だと push 境界はずっと下 (未 push 件数ぶんの行の
	// 先) にあり、そこだけに件数を置くとスクロールしないと見えないため (ユーザー要望
	// 2026-07-22)。加えて push 境界の正確な位置には従来どおり dim の罫線を挿す。
	summary := labeledRule(fmt.Sprintf(" (↑%d unpushed)", unpushed), term.AnsiRed+term.AnsiBold)
	boundaryRule := labeledRule("", term.AnsiDim) // 位置マーカー: ラベル無しの dim 罫線
	if o.PushAnim {
		boundaryRule = labeledRule(" ↑ pushing…", term.AnsiGreen+term.AnsiBold)
	}
	for i, l := range lines {
		if l.Header && l.CommitIdx == boundary {
			out := make([]Line, 0, len(lines)+2)
			out = append(out, Line{Text: summary, CommitIdx: 0})
			out = append(out, lines[:i]...)
			out = append(out, Line{Text: boundaryRule, CommitIdx: boundary - 1})
			out = append(out, lines[i:]...)
			return out
		}
	}
	return lines
}

// VerbatimLines は git log 実出力をコミット境界で分類する。ヘッダー検出は
// 「term.StripANSI 後に "commit <次に期待する SHA>" で始まる行」なので、コミットメッセージ内の
// "commit ..." 行 (インデント 4 だが --color 無しでは理論上先頭に来ない) を誤認しない。
// ヘッダー数が commits と一致しない場合は nil (呼び出し側が自前レンダリングへ fallback)。
func VerbatimLines(raw []string, commits []gitdata.Commit) []Line {
	lines := make([]Line, 0, len(raw))
	next := 0
	for _, line := range raw {
		if next < len(commits) && strings.HasPrefix(term.StripANSI(line), "commit "+commits[next].SHA) {
			lines = append(lines, Line{Text: line, CommitIdx: next, Header: true})
			next++
			continue
		}
		lines = append(lines, Line{Text: line, CommitIdx: max(next-1, 0)})
	}
	if next != len(commits) {
		return nil
	}
	return lines
}

// decorateVerbatim は verbatim 行へ glog の付加情報だけを足す: ヘッダー行の CI 記号前置と
// PR バッジ後置。それ以外の行は git log の出力そのまま (TUI では色なし行のみ幅で折り返し、
// タブは幅計算のため展開する)。
func decorateVerbatim(commits []gitdata.Commit, statuses map[string]gitdata.CIState, o RenderOpts) []Line {
	out := make([]Line, 0, len(o.Verbatim))
	for _, l := range o.Verbatim {
		if l.Header {
			c := commits[l.CommitIdx]
			text := StatusGlyph(stateFor(statuses, c.SHA), o.Colored, o.Spinner) + " " + prBadgeLead(c.SHA, o) + l.Text
			out = append(out, Line{Text: text, CommitIdx: l.CommitIdx, Header: true})
			continue
		}
		text := l.Text
		if o.Width > 0 {
			// mediumLines と同じ理由 (タブは clip の幅計算をすり抜けて再描画を崩す)。
			// 入口 (term.SanitizeLineKeepTabs) は静的出力のパリティのためタブを残す設計なので
			// TUI 側で展開する
			text = strings.ReplaceAll(text, "\t", "    ")
			if !strings.Contains(text, "\x1b") {
				for _, seg := range term.WrapToWidth(text, o.Width) {
					out = append(out, Line{Text: seg, CommitIdx: l.CommitIdx})
				}
				continue
			}
			// 色付き行 (diff 本文等) は term.WrapToWidth が ANSI 非対応のため折り返さない
			// (View 側の clip に任せる。自前レンダリング時代の body 行と同じ扱い)
		}
		out = append(out, Line{Text: text, CommitIdx: l.CommitIdx})
	}
	return out
}

// RenderStatic は静的出力 (非 TTY / TUI 終了後の最終表示) 用に行を結合する。
func RenderStatic(commits []gitdata.Commit, statuses map[string]gitdata.CIState, o RenderOpts) string {
	lines := RenderLines(commits, statuses, o)
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	return strings.Join(texts, "\n")
}

// commitHeaderPrefix は "記号 [lead]SHA (decorations)" を組み立てる。shaText は表記の違い
// (short SHA / "commit <full>") を呼び出し側が渡す。lead は記号の直後に置く付加情報 (medium の PR バッジ。
// prBadgeLead)。subject は含めない (列揃えの都合で呼び出し側が付ける)。3 つのレンダラ間で共通化し、SHA 表記や
// decoration の表示条件を 1 箇所で変えられるようにする。
func commitHeaderPrefix(lead, shaText, decoration string, state gitdata.CIState, o RenderOpts) string {
	var b strings.Builder
	b.WriteString(StatusGlyph(state, o.Colored, o.Spinner))
	b.WriteString(" ")
	b.WriteString(lead)
	b.WriteString(term.Paint(shaText, term.AnsiYellow, o.Colored))
	if decoration != "" {
		b.WriteString(" ")
		b.WriteString(renderDecoration(decoration, o))
	}
	return b.String()
}

// renderOnelineRow は --oneline の 1 行。列: 記号 / short SHA / (decorations) / subject / author / 相対日時。
func renderOnelineRow(c gitdata.Commit, state gitdata.CIState, subjectWidth, authorWidth int, o RenderOpts) string {
	var b strings.Builder
	b.WriteString(commitHeaderPrefix("", c.ShortSHA, c.Decoration, state, o))
	b.WriteString(" ")
	subject := term.TruncateDisp(c.Subject, subjectWidthCap, "…")
	b.WriteString(term.FillRight(subject, subjectWidth))
	b.WriteString("  ")
	b.WriteString(term.Paint(term.FillRight(c.Author, authorWidth), term.AnsiDim, o.Colored))
	b.WriteString("  ")
	b.WriteString(term.Paint(c.RelDate, term.AnsiDim, o.Colored))
	b.WriteString(prBadge(c.SHA, o))
	return b.String()
}

// prBadge はコミットに紐づく PR のバッジ (" #123")。--oneline の行末に置く (列揃えを崩さない)。
// 色は GitHub の慣例: OPEN=緑 / MERGED=マゼンタ / CLOSED=赤。
func prBadge(sha string, o RenderOpts) string {
	if t := prBadgeText(sha, o); t != "" {
		return " " + t
	}
	return ""
}

// prBadgeLead は medium のヘッダーで記号の直後に置く PR バッジ ("#123 ")。行末に置くと、decoration で長くなった
// ヘッダーが端末幅で切られてバッジから消える。git の出力 (commit <sha> (decorations)) の途中には差し込まない
// (verbatim の行は git log の出力を改変しない契約。TestRenderLinesVerbatimDecoratesHeaderOnly)。
func prBadgeLead(sha string, o RenderOpts) string {
	if t := prBadgeText(sha, o); t != "" {
		return t + " "
	}
	return ""
}

func prBadgeText(sha string, o RenderOpts) string {
	pr := o.PRs[sha]
	if pr == nil {
		return ""
	}
	return term.Paint(fmt.Sprintf("#%d", pr.Number), commitview.PrStateColor(pr.State), o.Colored)
}

// mediumLines は git log 標準形式の 1 コミット分。
//
//	✅ commit <sha> (decorations)
//	Author: name <email>
//	Date:   Thu Jul 16 19:12:47 2026 +0900
//
//	    message...
//
//	[--stat / -p の本文]
func mediumLines(c gitdata.Commit, idx int, state gitdata.CIState, o RenderOpts) []Line {
	var lines []Line
	var h strings.Builder
	h.WriteString(commitHeaderPrefix(prBadgeLead(c.SHA, o), "commit "+c.SHA, c.Decoration, state, o))
	lines = append(lines, Line{Text: h.String(), CommitIdx: idx, Header: true})
	lines = append(lines, Line{Text: "Author: " + c.Author + " <" + c.AuthorEmail + ">", CommitIdx: idx})
	lines = append(lines, Line{Text: "Date:   " + c.Date, CommitIdx: idx})
	lines = append(lines, Line{Text: "", CommitIdx: idx})
	// メッセージは git log と同じく切り詰めず折り返す (Width=0 の静的出力では折り返さず
	// 端末に任せる = git log と同じ挙動)。インデント 4 の分を差し引いて折る
	for msgLine := range strings.SplitSeq(c.Message, "\n") {
		// TUI (Width > 0) ではタブを展開する (下の Body ループ・decorateVerbatim と同じ理由:
		// term.ClipToWidth は \t を幅 0 と数えるが端末は展開するため、タブ入りメッセージ行が幅判定を
		// すり抜けてインライン再描画が崩れる)。静的出力 (Width=0) は git log と同じ素通し。
		if o.Width > 0 {
			msgLine = strings.ReplaceAll(msgLine, "\t", "    ")
		}
		for _, seg := range term.WrapToWidth(msgLine, o.Width-4) {
			lines = append(lines, Line{Text: "    " + seg, CommitIdx: idx})
		}
	}
	if c.Body != "" {
		lines = append(lines, Line{Text: "", CommitIdx: idx})
		for bodyLine := range strings.SplitSeq(c.Body, "\n") {
			// TUI (Width > 0) ではタブを展開する。term.ClipToWidth は \t を幅 0 と数えるが端末は
			// タブストップへ展開するため、タブ入り diff (Go ソース等) が幅判定をすり抜けて
			// 折り返し、インライン再描画が崩壊する (job ログで実測したのと同じ病気)。
			// 静的出力 (Width=0) は git log と同じ素通しにする
			if o.Width > 0 {
				bodyLine = strings.ReplaceAll(bodyLine, "\t", "    ")
			}
			lines = append(lines, Line{Text: bodyLine, CommitIdx: idx})
		}
	}
	return lines
}

// RenderCached は --cached モードの出力 (HEAD の CI 状態 + staged diff)。
// staged 変更自体に CI 結果は存在しないため「HEAD の状態」であることを明示する (issue の設計)。
func RenderCached(head *gitdata.Commit, state gitdata.CIState, diff string, colored bool, glyph string) string {
	var b strings.Builder
	b.WriteString("HEAD CI: ")
	// staged 変更に decoration は無い。RenderOpts は表記共通化のための最小構築。
	b.WriteString(commitHeaderPrefix("", head.ShortSHA, "", state, RenderOpts{Colored: colored, Spinner: glyph}))
	b.WriteString(" ")
	b.WriteString(head.Subject)
	b.WriteString("\nStaged changes:\n")
	if diff == "" {
		b.WriteString(term.Paint("(staged な変更はありません)", term.AnsiDim, colored))
	} else {
		b.WriteString(diff)
	}
	return b.String()
}

// renderDecoration は %D の decoration を git log と同じ配色で描画する。
// 括弧とカンマは commit 行の地色 (yellow)、HEAD は cyan、ローカルブランチは green、
// remote branch は red、tag は yellow (いずれも git config color.decorate.* を尊重)。
func renderDecoration(deco string, o RenderOpts) string {
	if !o.Colored {
		return "(" + deco + ")"
	}
	dc := o.decorColors()
	var b strings.Builder
	b.WriteString(term.AnsiYellow + "(" + term.AnsiReset)
	first := true
	for item := range strings.SplitSeq(deco, ", ") {
		if !first {
			b.WriteString(term.AnsiYellow + ", " + term.AnsiReset)
		}
		first = false
		b.WriteString(decorItem(item, dc))
	}
	b.WriteString(term.AnsiYellow + ")" + term.AnsiReset)
	return b.String()
}

func decorItem(item string, dc gitdata.DecorColors) string {
	if name, ok := strings.CutPrefix(item, "HEAD -> "); ok {
		return dc.HEAD + "HEAD" + term.AnsiReset + " -> " + refColor(name, dc) + name + term.AnsiReset
	}
	if item == "HEAD" {
		return dc.HEAD + item + term.AnsiReset
	}
	if strings.HasPrefix(item, "tag: ") {
		return dc.Tag + item + term.AnsiReset
	}
	return refColor(item, dc) + item + term.AnsiReset
}

// refColor はブランチ名がリポジトリの remote 配下 (<remote>/...) なら remote branch 色、
// それ以外はローカルブランチ色。名前に / を含むローカルブランチ (feature/x) を
// 誤判定しないよう、先頭セグメントが実在する remote 名のときだけ remote 扱いにする。
func refColor(name string, dc gitdata.DecorColors) string {
	if remote, _, ok := strings.Cut(name, "/"); ok && slices.Contains(dc.Remotes, remote) {
		return dc.RemoteBranch
	}
	return dc.Branch
}
