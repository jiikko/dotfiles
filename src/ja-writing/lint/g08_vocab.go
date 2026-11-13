package lint

import "slices"

// G8 語彙 (slop_vocabulary・metaphor_verb・translated_term。本家 yomiyasu_lint.py の 921-996・1188-1348・1397-1559・5340・5586-5651・5754-5771)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を持つ。g08Window 以外は中身が空 (候補を出さない)。入れる人は、このファイルだけを書く
// (implementedRules の表の true への変更を除く)。
//
// 依存: T2 (endDecorationRun を _MOMENT_PATTERN が使う)。G14 (calque) が、同じ行の metaphor_verb・translated_term の候補と _KIKU_RE・_PUBLICATION_WORDS を読む。
// 本家の「速さのための絞り込み」(地図 3.7 節。_narrowed_document_rows) は、元の文字に手掛かりが無い行の見える文字を調べない形で、答えが変わる所がある。そのまま写す。

// kikuWindowLines は比喩の「効く」で、前の行の文字どおりの語 (薬・エアコンなど) を見る窓の行数 (_KIKU_WINDOW_LINES)。
const kikuWindowLines = 3

// g08State は G8 が文書の中で持つ状態。本家の narrowed の表 (slop・比喩動詞・直訳・フィラー・info の型を、文書の語で絞ったもの) などをここに持つ。
// G9 (フィラー) と G11 (cleft_list_ending) が絞った表を読むので、フィラーの表は G9 の g09State に持たせる (G8 と G9 が同じ narrowed から作る)。
type g08State struct{}

// g08Prepare は行のループの前に 1 回。本家の _narrowed_document_rows(text) (5530) に当たる。
func (s *lintState) g08Prepare() {}

// g08Window は行のループの 1 行ごとに呼ばれ、比喩の「効く」の窓 (この行より前の、直近 kikuWindowLines 行) を返して、この行を覚える。
// 本家の `kiku_before = kiku_lines[-3:]; kiku_lines = kiku_before + [plain_text]`。
func (s *lintState) g08Window(plain string) []string {
	before := slices.Clone(s.kikuLines[max(0, len(s.kikuLines)-kikuWindowLines):])
	s.kikuLines = append(slices.Clone(before), plain)
	return before
}

// g08Vocabulary は見出しの行と、地の文の行で呼ばれる (本家の _vocabulary_findings の slop_vocabulary → metaphor_verb の順。5586-5651)。
// 見出しの行では文頭と文末を前提にした型を当てない。snippet は見出しなら r.stripped、地の文なら元の行の前後の空白を除いたもの。
func (s *lintState) g08Vocabulary(_ *rowCtx, _ string) {}

// g08Translated は地の文の行で、g08Vocabulary の後に呼ばれる (translated_term。同じ desc の型は 1 件にまとめる。5754-5771)。
func (s *lintState) g08Translated(_ *rowCtx) {}
