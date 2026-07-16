package main

import (
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
)

// spinnerFrames は取得中表示のフレーム。
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

// StateLoading は表示専用の擬似状態。statuses map に SHA が無いとき render 側で使う。
const StateLoading CIState = "loading"

func stateFor(statuses map[string]CIState, sha string) CIState {
	if state, ok := statuses[sha]; ok {
		return state
	}
	return StateLoading
}

// subjectWidthCap は subject 列を揃える幅の上限。極端に長い subject 1 件で
// 全行が右へ流れるのを防ぐ。
const subjectWidthCap = 60

// RenderCommits はコミット列を描画する。plain モードは 1 コミット 1 行で列を揃える。
// --stat / -p はヘッダー行 + git 本文 (CI 記号はヘッダー行にだけ付く: issue の設計)。
func RenderCommits(commits []Commit, statuses map[string]CIState, width int, colored bool, spinner string) string {
	hasBody := false
	for _, c := range commits {
		if c.Body != "" {
			hasBody = true
			break
		}
	}
	var b strings.Builder
	if !hasBody {
		subjectWidth := 0
		authorWidth := 0
		for _, c := range commits {
			subjectWidth = max(subjectWidth, runewidth.StringWidth(c.Subject))
			authorWidth = max(authorWidth, runewidth.StringWidth(c.Author))
		}
		subjectWidth = min(subjectWidth, subjectWidthCap)
		for i, c := range commits {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(renderLine(c, stateFor(statuses, c.SHA), subjectWidth, authorWidth, width, colored, spinner))
		}
		return b.String()
	}
	for i, c := range commits {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(renderHeader(c, stateFor(statuses, c.SHA), colored, spinner))
		if c.Body != "" {
			b.WriteString("\n")
			b.WriteString(c.Body)
		}
	}
	return b.String()
}

// renderLine は plain モードの 1 行。列: 記号 / short SHA / (decorations) / subject / author / 相対日時。
func renderLine(c Commit, state CIState, subjectWidth, authorWidth, termWidth int, colored bool, spinner string) string {
	var b strings.Builder
	b.WriteString(StatusGlyph(state, colored, spinner))
	b.WriteString(" ")
	b.WriteString(paint(c.ShortSHA, ansiYellow, colored))
	if c.Decoration != "" {
		b.WriteString(" ")
		b.WriteString(paint("("+c.Decoration+")", ansiCyan, colored))
	}
	b.WriteString(" ")
	subject := runewidth.Truncate(c.Subject, subjectWidthCap, "…")
	b.WriteString(runewidth.FillRight(subject, subjectWidth))
	b.WriteString("  ")
	b.WriteString(paint(runewidth.FillRight(c.Author, authorWidth), ansiDim, colored))
	b.WriteString("  ")
	b.WriteString(paint(c.RelDate, ansiDim, colored))
	return truncateToWidth(b.String(), termWidth, colored)
}

// renderHeader は --stat / -p モードのヘッダー行 (列揃えなし)。
func renderHeader(c Commit, state CIState, colored bool, spinner string) string {
	var b strings.Builder
	b.WriteString(StatusGlyph(state, colored, spinner))
	b.WriteString(" ")
	b.WriteString(paint(c.ShortSHA, ansiYellow, colored))
	if c.Decoration != "" {
		b.WriteString(" ")
		b.WriteString(paint("("+c.Decoration+")", ansiCyan, colored))
	}
	b.WriteString(" ")
	b.WriteString(c.Subject)
	b.WriteString("  ")
	b.WriteString(paint(c.Author+", "+c.RelDate, ansiDim, colored))
	return b.String()
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

func paint(s, color string, colored bool) string {
	if !colored {
		return s
	}
	return color + s + ansiReset
}

// truncateToWidth は端末幅を超える行を切り詰める (インライン再描画で折り返し行が
// 描画範囲をずらすのを防ぐ: issue の懸念点)。色付き時は ANSI を考慮できないため、
// 折り返し許容でそのまま返す... とはせず、幅計算前に色を使わない列構成にしている。
// 色コードを含む行は幅超過の検出が保守的になる (見かけより長く数える) ので、
// 超過検出時のみ非色で再構築せず末尾を落とす。
func truncateToWidth(line string, width int, colored bool) string {
	if width <= 0 {
		return line
	}
	if colored {
		// ANSI を除いた実効幅で判定する
		if runewidth.StringWidth(stripANSI(line)) <= width {
			return line
		}
		// 超過時は色を諦めて素の行を切り詰める (稀なケース: 端末が極端に狭い)
		return runewidth.Truncate(stripANSI(line), width, "…")
	}
	if runewidth.StringWidth(line) <= width {
		return line
	}
	return runewidth.Truncate(line, width, "…")
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
