package lint

import "strings"

// G10 指示の残り (instruction_residue。本家 yomiyasu_lint.py の 2940-2965 と 5806-5815)。正規表現は g10_tables.go。

const instructionResidueMessage = "AIへの指示が本文に残る形（書き方や除外の予告）が検出されました。除外や書き方の指示を述べる文を削ってください。ただし、読者のための範囲・対象読者・前提・参照先・用語の定義を示す文は残してください。"

// g10InstructionResidue は引用を伏せた文 (s.rule9[r.lineNo]) の各文と次の文について、そのままの文か、文末の飾りを除いた文で当たれば 1 行 1 件。
func (s *lintState) g10InstructionResidue(r *rowCtx) {
	closers := s.doc.endClosersFor(r.lineNo)
	for _, c := range s.rule9[r.lineNo] {
		if hasInstructionResidue(c.Sentence, c.Following) || hasInstructionResidueCore(c.Sentence, c.Following, closers) {
			s.add(newFinding("instruction_residue", r.lineNo, "info", instructionResidueMessage, r.stripped))
			return
		}
	}
}

// hasInstructionResidue は本家の _has_instruction_residue。書く行為の予告と書き方の条件が同居する文か、範囲の除外の文 (読者のための範囲や参照を伴うものは除く)。
func hasInstructionResidue(sentence, following string) bool {
	writing := searchText(writingAction, sentence)
	if writing != nil {
		matched, _ := writing.Group(0)
		if (strings.Contains(matched, "ます") || mustSearch(residueOpener, sentence)) && mustSearch(writingStyle, sentence) {
			return true
		}
	}
	if !mustSearch(scopeExclusion, sentence) && (writing == nil || !mustSearch(writingLimit, sentence)) {
		return false
	}
	return !mustSearch(readerScope, sentence) && !mustSearch(scopeReference, sentence) && !mustSearch(scopeReference, following)
}

// hasInstructionResidueCore は本家の _has_instruction_residue_core。文末の後ろの飾りを除いた文で確かめ直す (飾りがなければ確かめない)。
// 句点の代わりに絵文字を置いて続けた文は、分けた文ごとに次の文と合わせて確かめる。
func hasInstructionResidueCore(sentence, following, closers string) bool {
	if core, changed := stripEndDecoration(sentence, closers); changed && hasInstructionResidue(core, following) {
		return true
	}
	pieces := decorationBreaks(sentence)
	for i, piece := range pieces {
		next := following
		if i+1 < len(pieces) {
			next = pieces[i+1]
		}
		core, _ := stripEndDecoration(piece, closers)
		if hasInstructionResidue(core, next) {
			return true
		}
	}
	return false
}
