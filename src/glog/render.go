package main

import (
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
)

// 状態記号は絵文字ではなく 1 カラム記号を使う (端末幅とフォント差異の影響を抑える: issue の設計)。
const (
	ansiReset  = "\x1b[0m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiDim    = "\x1b[2m"
	ansiBold   = "\x1b[1m"
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

// Line は描画 1 行分。TUI のビューポートとカーソル位置決めが CommitIdx/Header/JobNum を使う。
type Line struct {
	Text      string
	CommitIdx int  // どのコミットに属する行か (-1 = どれでもない)
	Header    bool // コミットヘッダー行 (カーソルが乗る行) か
	JobNum    int  // CI job 行なら 1 始まりの番号 (JobNum-1 = details の index)。0 = job 行ではない
}

// RenderOpts は描画パラメータ。静的出力 (非 TTY / 最終出力) と TUI ビューの共通入力。
type RenderOpts struct {
	Oneline        bool
	Colored        bool
	Spinner        string
	Width          int             // >0 ならコミットメッセージを端末幅で折り返す (TUI 用)。
	Decor          *DecorColors    // decoration の色 (nil = git 既定色)
	Expanded       map[string]bool // 展開中の SHA
	Details        map[string][]CheckDetail
	DetailsLoading map[string]bool // 展開したが詳細を取得中の SHA
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

// RenderLines はコミット列を描画行へ展開する。既定は git log 標準 (medium) 形式、
// --oneline はコンパクト 1 行形式。CI 記号はコミットヘッダー行にだけ付く (issue の設計)。
func RenderLines(commits []Commit, statuses map[string]CIState, o RenderOpts) []Line {
	var lines []Line
	if o.Oneline {
		subjectWidth := 0
		authorWidth := 0
		for _, c := range commits {
			subjectWidth = max(subjectWidth, runewidth.StringWidth(c.Subject))
			authorWidth = max(authorWidth, runewidth.StringWidth(c.Author))
		}
		subjectWidth = min(subjectWidth, subjectWidthCap)
		for i, c := range commits {
			lines = append(lines, Line{Text: renderOnelineRow(c, stateFor(statuses, c.SHA), subjectWidth, authorWidth, o), CommitIdx: i, Header: true})
			lines = append(lines, expandedLines(c, i, o)...)
		}
		return lines
	}
	for i, c := range commits {
		if i > 0 {
			lines = append(lines, Line{Text: "", CommitIdx: i - 1})
		}
		lines = append(lines, mediumLines(c, i, stateFor(statuses, c.SHA), o)...)
	}
	return lines
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

// renderOnelineRow は --oneline の 1 行。列: 記号 / short SHA / (decorations) / subject / author / 相対日時。
func renderOnelineRow(c Commit, state CIState, subjectWidth, authorWidth int, o RenderOpts) string {
	var b strings.Builder
	b.WriteString(StatusGlyph(state, o.Colored, o.Spinner))
	b.WriteString(" ")
	b.WriteString(paint(c.ShortSHA, ansiYellow, o.Colored))
	if c.Decoration != "" {
		b.WriteString(" ")
		b.WriteString(renderDecoration(c.Decoration, o))
	}
	b.WriteString(" ")
	subject := runewidth.Truncate(c.Subject, subjectWidthCap, "…")
	b.WriteString(runewidth.FillRight(subject, subjectWidth))
	b.WriteString("  ")
	b.WriteString(paint(runewidth.FillRight(c.Author, authorWidth), ansiDim, o.Colored))
	b.WriteString("  ")
	b.WriteString(paint(c.RelDate, ansiDim, o.Colored))
	return b.String()
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
	h.WriteString(StatusGlyph(state, o.Colored, o.Spinner))
	h.WriteString(" ")
	h.WriteString(paint("commit "+c.SHA, ansiYellow, o.Colored))
	if c.Decoration != "" {
		h.WriteString(" ")
		h.WriteString(renderDecoration(c.Decoration, o))
	}
	lines = append(lines, Line{Text: h.String(), CommitIdx: idx, Header: true})
	lines = append(lines, expandedLines(c, idx, o)...)
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
			lines = append(lines, Line{Text: bodyLine, CommitIdx: idx})
		}
	}
	return lines
}

// expandedLines は展開中コミットの CI job 一覧 (ヘッダー行直下に差し込む)。
func expandedLines(c Commit, idx int, o RenderOpts) []Line {
	if o.Expanded == nil || !o.Expanded[c.SHA] {
		return nil
	}
	if o.DetailsLoading != nil && o.DetailsLoading[c.SHA] {
		return []Line{{Text: "    " + paint(o.Spinner+" CI job を取得中...", ansiDim, o.Colored), CommitIdx: idx}}
	}
	details, ok := o.Details[c.SHA]
	if !ok {
		return []Line{{Text: "    " + paint("(CI job 情報なし)", ansiDim, o.Colored), CommitIdx: idx}}
	}
	if len(details) == 0 {
		return []Line{{Text: "    " + paint("(Check はありません)", ansiDim, o.Colored), CommitIdx: idx}}
	}
	lines := make([]Line, 0, len(details))
	for i, d := range details {
		lines = append(lines, Line{
			Text:      "    " + StatusGlyph(d.State, o.Colored, o.Spinner) + " " + d.Name,
			CommitIdx: idx,
			JobNum:    i + 1,
		})
	}
	return lines
}

// RenderCached は --cached モードの出力 (HEAD の CI 状態 + staged diff)。
// staged 変更自体に CI 結果は存在しないため「HEAD の状態」であることを明示する (issue の設計)。
func RenderCached(head *Commit, state CIState, diff string, colored bool, spinner string) string {
	var b strings.Builder
	b.WriteString("HEAD CI: ")
	b.WriteString(StatusGlyph(state, colored, spinner))
	b.WriteString(" ")
	b.WriteString(paint(head.ShortSHA, ansiYellow, colored))
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
	plain := stripANSI(line)
	if runewidth.StringWidth(plain) <= width {
		return line
	}
	return runewidth.Truncate(plain, width, "…")
}

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
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
