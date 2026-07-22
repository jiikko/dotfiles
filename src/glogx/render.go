package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
)

// 状態記号は絵文字ではなく 1 カラム記号を使う (端末幅とフォント差異の影響を抑える: issue の設計)。
const (
	ansiReset   = "\x1b[0m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiDim     = "\x1b[2m"
	// カーソル行の bg (256色の暗灰 237)。テーマの意味マップ (docs/theme-colors.md) の
	// カーソル行の bg。無彩色の 237 では地色 234/235 と近く見失いやすかったため、
	// 色相のある暗青 24 に変更 (ユーザー要望 2026-07-21。fg 色はそのまま透ける)
	ansiCursorBg = "\x1b[48;5;24m"
	ansiBold     = "\x1b[1m"
	// confirm モーダルの落ち影 bg (256色の暗灰 233)。地色 234/235 より一段暗くして、
	// 枠の右下に落とすと板が浮いて見える (buildShadowPanelBox の drop shadow)
	ansiShadowBg = "\x1b[48;5;233m"
)

// spinnerFrames は取得中表示のフレーム。
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func init() {
	// East Asian Ambiguous (罫線・✓・● 等) を幅 1 として扱う。runewidth の既定は
	// locale (LANG=ja_JP.* 等) で幅 2 に切り替わり、パネル枠や列揃えの計算が実行環境
	// 依存でずれるため固定する (Terminal.app 等の既定も ambiguous = narrow)。
	runewidth.DefaultCondition.EastAsianWidth = false
}

// StateLoading は表示専用の擬似状態。statuses map に SHA が無いとき render 側で使う。
const StateLoading CIState = "loading"

// StatusGlyph は状態 1 つ分の記号 (+色)。loading は spinner フレームを渡す。
func StatusGlyph(state CIState, colored bool, spinner string) string {
	var glyph, color string
	switch state {
	case StateSuccess:
		glyph, color = "✓", ansiGreen
	case StateFailure:
		glyph, color = "✗", ansiRed
	case StatePending:
		glyph, color = "●", ansiYellow
	case StateNeutral:
		glyph, color = "⊘", ansiDim
	case StateNone:
		glyph, color = "–", ansiDim
	case StateUnpushed:
		glyph, color = "↑", ansiDim
	case StateUnknown:
		glyph, color = "?", ansiDim
	default:
		glyph, color = spinner, ansiCyan
	}
	if !colored {
		return glyph
	}
	return color + glyph + ansiReset
}

func stateFor(statuses map[string]CIState, sha string) CIState {
	if state, ok := statuses[sha]; ok {
		return state
	}
	return StateLoading
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
	Width   int               // >0 ならコミットメッセージを端末幅で折り返す (TUI 用)。
	Decor   *DecorColors      // decoration の色 (nil = git 既定色)
	PRs     map[string]*PRRef // コミット行末尾の PR バッジ (#123)。nil 値 = PR なし
	// Verbatim は git log 実出力の取り込み行 (VerbatimLines の結果)。非 nil なら
	// medium 形式の自前再構築 (mediumLines) の代わりにこれを装飾して使う = 見た目が
	// git log と機械的に一致する (ユーザー要望 2026-07-19「人力で寄せるのはめんどい」)。
	// nil は fallback (git log の実行/照合に失敗した場合と --oneline)
	Verbatim []Line
	// HasRepo は GitHub repo が解決できているか。「全部 push 済み ✓」の先頭マークの
	// ゲート (remote 無し repo では push 状態が不明なだけなので出さない)
	HasRepo bool
}

func (o RenderOpts) decorColors() DecorColors {
	if o.Decor != nil {
		return *o.Decor
	}
	return DefaultDecorColors()
}

// subjectWidthCap は --oneline で subject 列を揃える幅の上限。極端に長い subject 1 件で
// 全行が右へ流れるのを防ぐ。
const subjectWidthCap = 60

// defaultBoundaryWidth は静的出力 (Width=0) 時の push 境界の罫線幅。subjectWidthCap と
// 値が同じなのは偶然で意味は無関係 (端末幅の代替 vs subject 列上限)。片方だけ変えてよい。
const defaultBoundaryWidth = 60

// RenderLines はコミット列を描画行へ展開する。既定は git log 標準 (medium) 形式、
// --oneline はコンパクト 1 行形式。CI 記号はコミットヘッダー行にだけ付く (issue の設計)。
func RenderLines(commits []Commit, statuses map[string]CIState, o RenderOpts) []Line {
	var lines []Line
	switch {
	case o.Oneline:
		subjectWidth := 0
		authorWidth := 0
		for _, c := range commits {
			subjectWidth = max(subjectWidth, runewidth.StringWidth(c.Subject))
			authorWidth = max(authorWidth, runewidth.StringWidth(c.Author))
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
func insertPushBoundary(lines []Line, commits []Commit, statuses map[string]CIState, o RenderOpts) []Line {
	if len(commits) == 0 {
		return lines
	}
	boundary := -1 // push 済み先頭のコミット index
	unpushed := 0
	for _, c := range commits {
		if stateFor(statuses, c.SHA) == StateUnpushed {
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
		dashes := strings.Repeat("─", max(width-runewidth.StringWidth(head+label+gap), 0))
		return paint(head, ansiDim, o.Colored) +
			paint(label, labelColor, o.Colored) +
			paint(gap+dashes, ansiDim, o.Colored)
	}
	if allPushed {
		// 全 push 済みは「達成」状態なのでラベルだけ緑+太字で強調する (dim な罫線と差を
		// つけて視覚的に目立たせる。ユーザー要望)。罫線部分は従来どおり dim
		text := labeledRule(" (all pushed ✓)", ansiGreen+ansiBold)
		return append([]Line{{Text: text, CommitIdx: 0}}, lines...)
	}
	// 混在時: 未 push 件数 (= origin より何コミット先行しているか) を all pushed と同じ
	// 「先頭」に赤+太字で出す。複数行表示だと push 境界はずっと下 (未 push 件数ぶんの行の
	// 先) にあり、そこだけに件数を置くとスクロールしないと見えないため (ユーザー要望
	// 2026-07-22)。加えて push 境界の正確な位置には従来どおり dim の罫線を挿す。
	summary := labeledRule(fmt.Sprintf(" (↑%d unpushed)", unpushed), ansiRed+ansiBold)
	boundaryRule := labeledRule("", ansiDim) // 位置マーカー: ラベル無しの dim 罫線
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
// 「stripANSI 後に "commit <次に期待する SHA>" で始まる行」なので、コミットメッセージ内の
// "commit ..." 行 (インデント 4 だが --color 無しでは理論上先頭に来ない) を誤認しない。
// ヘッダー数が commits と一致しない場合は nil (呼び出し側が自前レンダリングへ fallback)。
func VerbatimLines(raw []string, commits []Commit) []Line {
	lines := make([]Line, 0, len(raw))
	next := 0
	for _, line := range raw {
		if next < len(commits) && strings.HasPrefix(stripANSI(line), "commit "+commits[next].SHA) {
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
func decorateVerbatim(commits []Commit, statuses map[string]CIState, o RenderOpts) []Line {
	out := make([]Line, 0, len(o.Verbatim))
	for _, l := range o.Verbatim {
		if l.Header {
			c := commits[l.CommitIdx]
			text := StatusGlyph(stateFor(statuses, c.SHA), o.Colored, o.Spinner) + " " + l.Text + prBadge(c.SHA, o)
			out = append(out, Line{Text: text, CommitIdx: l.CommitIdx, Header: true})
			continue
		}
		text := l.Text
		if o.Width > 0 {
			// mediumLines と同じ理由 (タブは clip の幅計算をすり抜けて再描画を崩す)
			text = strings.ReplaceAll(text, "\t", "    ")
			if !strings.Contains(text, "\x1b") {
				for _, seg := range wrapToWidth(text, o.Width) {
					out = append(out, Line{Text: seg, CommitIdx: l.CommitIdx})
				}
				continue
			}
			// 色付き行 (diff 本文等) は wrapToWidth が ANSI 非対応のため折り返さない
			// (View 側の clip に任せる。自前レンダリング時代の body 行と同じ扱い)
		}
		out = append(out, Line{Text: text, CommitIdx: l.CommitIdx})
	}
	return out
}

// RenderStatic は静的出力 (非 TTY / TUI 終了後の最終表示) 用に行を結合する。
func RenderStatic(commits []Commit, statuses map[string]CIState, o RenderOpts) string {
	lines := RenderLines(commits, statuses, o)
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	return strings.Join(texts, "\n")
}

// commitHeaderPrefix は "記号 SHA (decorations)" を組み立てる。shaText は表記の違い
// (short SHA / "commit <full>") を呼び出し側が渡す。PR バッジ・subject は含めない
// (行末・列揃えの都合で呼び出し側が付ける)。3 つのレンダラ間で共通化し、SHA 表記や
// decoration の表示条件を 1 箇所で変えられるようにする。
func commitHeaderPrefix(shaText, decoration string, state CIState, o RenderOpts) string {
	var b strings.Builder
	b.WriteString(StatusGlyph(state, o.Colored, o.Spinner))
	b.WriteString(" ")
	b.WriteString(paint(shaText, ansiYellow, o.Colored))
	if decoration != "" {
		b.WriteString(" ")
		b.WriteString(renderDecoration(decoration, o))
	}
	return b.String()
}

// renderOnelineRow は --oneline の 1 行。列: 記号 / short SHA / (decorations) / subject / author / 相対日時。
func renderOnelineRow(c Commit, state CIState, subjectWidth, authorWidth int, o RenderOpts) string {
	var b strings.Builder
	b.WriteString(commitHeaderPrefix(c.ShortSHA, c.Decoration, state, o))
	b.WriteString(" ")
	subject := runewidth.Truncate(c.Subject, subjectWidthCap, "…")
	b.WriteString(runewidth.FillRight(subject, subjectWidth))
	b.WriteString("  ")
	b.WriteString(paint(runewidth.FillRight(c.Author, authorWidth), ansiDim, o.Colored))
	b.WriteString("  ")
	b.WriteString(paint(c.RelDate, ansiDim, o.Colored))
	b.WriteString(prBadge(c.SHA, o))
	return b.String()
}

// prBadge はコミットに紐づく PR のバッジ (" #123")。行末に置く (列揃えを崩さない)。
// 色は GitHub の慣例: OPEN=緑 / MERGED=マゼンタ / CLOSED=赤。
func prBadge(sha string, o RenderOpts) string {
	pr := o.PRs[sha]
	if pr == nil {
		return ""
	}
	color := ansiDim
	switch pr.State {
	case "OPEN":
		color = ansiGreen
	case "MERGED":
		color = ansiMagenta
	case "CLOSED":
		color = ansiRed
	}
	return " " + paint(fmt.Sprintf("#%d", pr.Number), color, o.Colored)
}

// mediumLines は git log 標準形式の 1 コミット分。
//
//	✓ commit <sha> (decorations)
//	Author: name <email>
//	Date:   Thu Jul 16 19:12:47 2026 +0900
//
//	    message...
//
//	[--stat / -p の本文]
func mediumLines(c Commit, idx int, state CIState, o RenderOpts) []Line {
	var lines []Line
	var h strings.Builder
	h.WriteString(commitHeaderPrefix("commit "+c.SHA, c.Decoration, state, o))
	h.WriteString(prBadge(c.SHA, o))
	lines = append(lines, Line{Text: h.String(), CommitIdx: idx, Header: true})
	lines = append(lines, Line{Text: "Author: " + c.Author + " <" + c.AuthorEmail + ">", CommitIdx: idx})
	lines = append(lines, Line{Text: "Date:   " + c.Date, CommitIdx: idx})
	lines = append(lines, Line{Text: "", CommitIdx: idx})
	// メッセージは git log と同じく切り詰めず折り返す (Width=0 の静的出力では折り返さず
	// 端末に任せる = git log と同じ挙動)。インデント 4 の分を差し引いて折る
	for msgLine := range strings.SplitSeq(c.Message, "\n") {
		for _, seg := range wrapToWidth(msgLine, o.Width-4) {
			lines = append(lines, Line{Text: "    " + seg, CommitIdx: idx})
		}
	}
	if c.Body != "" {
		lines = append(lines, Line{Text: "", CommitIdx: idx})
		for bodyLine := range strings.SplitSeq(c.Body, "\n") {
			// TUI (Width > 0) ではタブを展開する。clipToWidth は \t を幅 0 と数えるが端末は
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
func RenderCached(head *Commit, state CIState, diff string, colored bool, spinner string) string {
	var b strings.Builder
	b.WriteString("HEAD CI: ")
	// staged 変更に decoration は無い。RenderOpts は表記共通化のための最小構築。
	b.WriteString(commitHeaderPrefix(head.ShortSHA, "", state, RenderOpts{Colored: colored, Spinner: spinner}))
	b.WriteString(" ")
	b.WriteString(head.Subject)
	b.WriteString("\nStaged changes:\n")
	if diff == "" {
		b.WriteString(paint("(staged な変更はありません)", ansiDim, colored))
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
	b.WriteString(ansiYellow + "(" + ansiReset)
	first := true
	for item := range strings.SplitSeq(deco, ", ") {
		if !first {
			b.WriteString(ansiYellow + ", " + ansiReset)
		}
		first = false
		b.WriteString(decorItem(item, dc))
	}
	b.WriteString(ansiYellow + ")" + ansiReset)
	return b.String()
}

func decorItem(item string, dc DecorColors) string {
	if name, ok := strings.CutPrefix(item, "HEAD -> "); ok {
		return dc.HEAD + "HEAD" + ansiReset + " -> " + refColor(name, dc) + name + ansiReset
	}
	if item == "HEAD" {
		return dc.HEAD + item + ansiReset
	}
	if strings.HasPrefix(item, "tag: ") {
		return dc.Tag + item + ansiReset
	}
	return refColor(item, dc) + item + ansiReset
}

// refColor はブランチ名がリポジトリの remote 配下 (<remote>/...) なら remote branch 色、
// それ以外はローカルブランチ色。名前に / を含むローカルブランチ (feature/x) を
// 誤判定しないよう、先頭セグメントが実在する remote 名のときだけ remote 扱いにする。
func refColor(name string, dc DecorColors) string {
	if remote, _, ok := strings.Cut(name, "/"); ok && slices.Contains(dc.Remotes, remote) {
		return dc.RemoteBranch
	}
	return dc.Branch
}

// decorateDetailLine は job 詳細の GitHub Actions マーカー行 (##[error] 等) と
// annotations のレベル見出し ([failure] 等) を Web UI 風に着色する。raw ログに
// これらの色情報は無い (Web の配色は UI 側の後付け) ため glog 側で再現する。
// キャッシュには焼き込まず描画時に付ける (colored 判定と将来の色変更を尊重)。
func decorateDetailLine(line string, colored bool) string {
	if !colored {
		return line
	}
	switch {
	case strings.HasPrefix(line, "##[error]"), strings.HasPrefix(line, "[failure]"):
		return ansiRed + line + ansiReset
	case strings.HasPrefix(line, "##[warning]"), strings.HasPrefix(line, "[warning]"):
		return ansiYellow + line + ansiReset
	case strings.HasPrefix(line, "##[notice]"), strings.HasPrefix(line, "[notice]"):
		return ansiCyan + line + ansiReset
	case strings.HasPrefix(line, "##[group]"), strings.HasPrefix(line, "##[endgroup]"),
		strings.HasPrefix(line, "##[command]"), strings.HasPrefix(line, "##[debug]"):
		return paint(line, ansiDim, true)
	}
	return line
}

// wrapToWidth は表示幅 width で折り返す (ANSI を含まない行の前提)。width <= 0 なら
// 折り返さない。単語境界は考慮せず端末の折り返しと同じく文字単位で折る
// (日本語混じりの commit message では単語境界折りの利得が薄いため)。
func wrapToWidth(s string, width int) []string {
	if width <= 0 || runewidth.StringWidth(s) <= width {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > width && w > 0 {
			out = append(out, cur.String())
			cur.Reset()
			w = 0
		}
		cur.WriteRune(r)
		w += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func paint(s, color string, colored bool) string {
	if !colored {
		return s
	}
	return color + s + ansiReset
}

// clipToWidth は端末幅を超える行を切り詰める (インライン TUI で折り返しが再描画範囲を
// ずらすのを防ぐ: issue の懸念点)。ANSI を除いた実効幅で判定し、超過時は色を落として
// 切り詰める (色コードを保ったままの部分切りは複雑さに見合わない)。静的出力では使わない。
func clipToWidth(line string, width int) string {
	if width <= 0 {
		return line
	}
	// fast-path: ANSI 無しなら整形式 UTF-8 で表示幅 ≤ byte 長が成り立つので、byte 長が
	// width 以内なら確実に幅内 = stripANSI の alloc も StringWidth 走査も省ける
	// (View の可視行の多数派 = Author/Date/message 平文・NO_COLOR 全行がここを通る)。
	if len(line) <= width && strings.IndexByte(line, '\x1b') < 0 {
		return line
	}
	plain := stripANSI(line)
	if runewidth.StringWidth(plain) <= width {
		return line
	}
	return runewidth.Truncate(plain, width, "…")
}

// isANSITerminator は ESC シーケンス中の rune r がシーケンスを終端する最終バイトか
// を返す (CSI の最終バイトは英字)。truncateKeepANSI / dropToColumn / stripANSI が
// 同じ終端判定を共有し、OSC 等への対応拡張時に 1 箇所だけ直せばよいようにする。
func isANSITerminator(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// truncateKeepANSI は s を表示幅 width まで切り詰めるが SGR エスケープは保持する
// (可視文字だけを幅に数える)。clipToWidth が切り詰め時に色を捨てるのと対照的に、
// overlay で覆う行の「見えている左側」を色付きのまま残すために使う。末尾に reset は
// 付けない (開いたままの色は呼び出し側が境界で閉じる)。
func truncateKeepANSI(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if strings.IndexByte(s, '\x1b') < 0 {
		return runewidth.Truncate(s, width, "") // ESC 無し: 素の幅切りで十分
	}
	var b strings.Builder
	w := 0
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			b.WriteRune(r)
			if isANSITerminator(r) {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
			b.WriteRune(r)
		default:
			rw := runewidth.RuneWidth(r)
			if w+rw > width {
				return b.String()
			}
			b.WriteRune(r)
			w += rw
		}
	}
	return b.String()
}

// dropToColumn は s のうち表示列 n (0-based) 以降の suffix を返す。ANSI 対応。cut より前に
// 現れた SGR エスケープは結果の先頭で replay するので、残った suffix は元の色を保つ
// (truncateKeepANSI が「左の prefix を保持」する鏡像で、こちらは「左 n 桁を捨てて右を残す」)。
// 浮動ボックスの右側に背景テキストを合成するのに使う。全角グリフが列 n をまたぐ場合はその
// グリフを落とし、列 n に揃うよう空白で左詰めする。列 n が内容末尾以降なら "" (右に何も無い)。
func dropToColumn(s string, n int) string {
	if n <= 0 {
		return s
	}
	var sgr, esc strings.Builder
	inEscape := false
	w := 0
	runes := []rune(s)
	i := 0
	for ; i < len(runes) && w < n; i++ {
		r := runes[i]
		switch {
		case inEscape:
			esc.WriteRune(r)
			if isANSITerminator(r) {
				inEscape = false
				sgr.WriteString(esc.String()) // cut 前の SGR を蓄積 (後で replay)
				esc.Reset()
			}
		case r == '\x1b':
			inEscape = true
			esc.Reset()
			esc.WriteRune(r)
		default:
			w += runewidth.RuneWidth(r)
		}
	}
	if i >= len(runes) {
		return "" // 列 n は内容の末尾以降: 右側に残すものが無い
	}
	pad := ""
	if w > n { // 全角グリフが cut をまたいだ: 列 n に揃えるため空白で埋める
		pad = strings.Repeat(" ", w-n)
	}
	return sgr.String() + pad + string(runes[i:])
}

func stripANSI(s string) string {
	if strings.IndexByte(s, '\x1b') < 0 {
		return s // ESC 無しは Builder 確保・rune 走査とも不要
	}
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			if isANSITerminator(r) {
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
