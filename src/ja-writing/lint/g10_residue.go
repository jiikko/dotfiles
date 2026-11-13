package lint

// G10 指示の残り (instruction_residue。本家 yomiyasu_lint.py の 2940-2965 と 5806-5815)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T2・T3

// g10State は G10 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g10State struct{}

// g10InstructionResidue はs.rule9[r.lineNo] の各文 (と次の文) について _has_instruction_residue か _has_instruction_residue_core。
func (s *lintState) g10InstructionResidue(_ *rowCtx) {}
