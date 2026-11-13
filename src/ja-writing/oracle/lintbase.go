package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"strings"
)

// LintBaseFile は lint の土台 (絵文字・文末の飾り・文字の下ごしらえ。yomiyasu_lint.py の T1〜T3) の関数の記録 (testdata/golden/ の中)。
// 形は visibility.json と同じ (case ごとの呼び出しの digest の列)。
const LintBaseFile = "lint-base.json"

// LintEmojiTablesPath は GenerateLintEmojiTables の出力の置き場 (module の根から)。
const LintEmojiTablesPath = "lint/emoji_tables_gen.go"

// LintModule は記録の対象の本家の module。
const LintModule = "yomiyasu_lint"

// LintBaseGolden は lint-base.json の中身。
type LintBaseGolden = VisibilityGolden

// LintBaseFunctions は記録の対象の関数 (module は LintModule。analysis を作る analyze_markdown は markdown_visibility のもので、数えない)。
// 記録の件数が 0 の名前は、記録の検査が落とす (空振りを緑にしない)。
var LintBaseFunctions = []string{
	"_emoji_character_class", "_emoji_flag_pattern", "_emoji_matches",
	"_merged_ranges", "_bracket_opens_sentence", "_strip_end_decoration", "_decoration_breaks", "_with_undecorated", "_end_quote_closers", "_end_closers_for",
	"_mask_quoted_segments", "_mask_quoted_segments_sparse", "_question_content_end", "_last_context_sentence", "_ends_with_question",
	"_prose_visible_text", "_image_only_row", "_plain_sentence_records", "_rule9_context", "_v220_scan_text",
	"_v220_load_list",
	"_say_hedge_sentence", "_copy_comma_noun_sentence", "_bare_verb_chain_sentence",
	"_top_quote_contents", "_dialogue_lines", "_narrative_counts",
	"_flag_pattern", "_replacement_bounded", "_project_term_message", "_project_term_findings",
	"_calque_type1", "_calque_type2", "_calque_type3", "_calque_type4", "_calque_type5", "_calque_saki_spans", "_calque_has_org", "_calque_subject_end", "_v220_doc_tech",
	"_calque_message", "_label_is_phrase", "_label_shape", "_label_kanji_runs",
}

// LintBaseConstants は記録する定数 (Get で読む)。表のもの (_EMOJI_*) は GenerateLintEmojiTables が Go の表にする。
var LintBaseConstants = []string{
	"_EMOJI_DATA_VERSION", "_EMOJI_REGISTERED_TEXT_BASES", "_EMOJI_EXPLICIT_VARIATION_BASES", "_EMOJI_OTHER_BASES", "_EMOJI_FLAG_REGION_CODES",
	"_END_SYMBOL_RANGES", "_END_JOINER_RANGES", "_END_VISIBLE_RANGES", "_END_VISIBLE_CHARS", "_END_JOINER_CHARS", "_END_DECORATION_CHARS", "_END_KEYCAP", "_END_DECORATION_RUN",
	"_END_BRACKETS", "_END_CLOSERS", "_END_QUOTES", "_END_MARKS", "_END_BREAK_HEAD_CHARS", "_END_BREAK_PAIRS",
	"_QUESTION_CLOSERS", "_QUESTION_SUFFIXES",
	// 規則の表と正規表現 (G8〜G12。lint/gNN_tables.go が本家と同じかを TestLintTablesMatchPythonRecording が比べる)
	"SLOP_WORDS", "SLOP_WORD_PATTERNS", "METAPHOR_VERB_PATTERNS", "TRANSLATED_TERM_PATTERNS", "_KIKU_PATTERN", "_KIKU_CANDIDATE", "_KIKU_RE",
	"_METAPHOR_VERB_MESSAGE_TAILS", "_METAPHOR_VERB_MESSAGE_TAIL", "_PATTERN_LITERAL_HINTS",
	"FILLER_PATTERNS", "FILLER_NOTES", "INFO_SENTENCE_PATTERNS", "_OVERSTATEMENT_PATTERN",
	"_WRITING_ACTION", "_RESIDUE_OPENER_RE", "_WRITING_STYLE", "_WRITING_LIMIT", "_SCOPE_EXCLUSION", "_READER_SCOPE", "_SCOPE_REFERENCE",
	"_FRAGMENT_RUN", "_SHORT_MOCHIRON", "_MOCHIRON_ANSWER", "_QUOTED_SEGMENT", "_CLEFT_LIST_COMMA", "_CLEFT_LIST_MIXED", "_CLEFT_LIST_JOINER", "_CLEFT_LIST_COMMA_MARK",
	"_CLEFT_LIST_MASKED_RUN", "_CLEFT_LIST_SPACE", "_CLEFT_LIST_STEM",
	"_NEGATION_EXTRA", "_NEGATION_JANAI", "_NEGATION_ALSO", "_BOLD_LABEL_ITEM",
	"_V220_MESSAGE_TAIL", "_RENYO_BEFORE", "_RENYO_AFTER", "_RENYO_MESSAGE",
	"_V220_SENTENCE", "_AI_PHRASE_HEAD", "_AI_JUDGE", "_AI_INVITER", "_AI_PHRASE_FORMS", "_AI_PHRASE_MESSAGE",
	"_SLIDE_LINE_MAX", "_SLIDE_COMMA", "_SLIDE_ACTOR_AFTER", "_SLIDE_LIST_AFTER", "_SLIDE_SAME_NEXT", "_SLIDE_LEAD", "_SLIDE_BULLET", "_SLIDE_MARKER_ZONE", "_SLIDE_MESSAGE",
	// G18 (集合は昇順の配列で記録する)
	"_SHAPE_RULE_MESSAGES", "_SHAPE_GATE_WORDS", "_SHAPE_COMMA_GATE", "_SHAPE_HEADING_MARK", "_SHAPE_QUOTE_MARKS", "_SHAPE_CURLY_QUOTED", "_SHAPE_SENTENCE",
	"_SAY_HEDGE", "_SAY_HEDGE_NOUN_END", "_SAY_HEDGE_NA_ADJECTIVE", "_SAY_HEDGE_JUDGE", "_COPY_COMMA_NOUN", "_COPY_MODIFIER_PARTICLE", "_COPY_NOT_MODIFIER", "_COPY_I_NOUNS", "_COPY_NOUN_NOT",
	"_BARE_VERB_PART", "_BARE_VERB_LAST", "_BARE_VERB_POLITE", "_BARE_VERB_NOT_VERB", "_BARE_VERB_PARTICLE",
	// G19
	"DIALOGUE_MIN", "DIALOGUE_RATIO", "FIRST_PERSON_MAX", "DENSITY_MIN", "NARRATIVE_MIN_CHARS", "_DOCUMENT_SKIP_KINDS", "_QUOTE_PAIRS", "_QUOTE_CLOSERS",
	"_SPEAKER_LINE", "_NOT_SPEAKER", "_EDGE_STAGE_NOTE", "_FIRST_PERSON", "_NARRATIVE_NUMBER", "_ASCII_IDENTIFIER",
	// G20
	"_PROJECT_TERM_MESSAGE", "_PROJECT_TERM_PATTERN_MESSAGE", "_PROJECT_TERM_BOTH_MESSAGE", "_WORD_ASCII", "_WORD_BEFORE", "_WORD_AFTER", "_WORD_KANJI_BEFORE", "_WORD_KANJI_AFTER",
	"_REPLACEMENT_BEFORE", "_REPLACEMENT_AFTER",
	// G14 (集合は昇順の配列。_V220_BUILTIN_LISTS は内蔵の一覧)
	"_CALQUE_SUBJECT_WORD", "_CALQUE_PERSON_SUBJECT", "_CALQUE_PERSON_EXT", "_CALQUE_ESTABLISHED_TYPE1_RE", "_CALQUE_KANA_RUN", "_CALQUE_SUBJECT_BREAK", "_CALQUE_PLACE_ALWAYS", "_CALQUE_PLACE_CONTEXT", "_CALQUE_PLACE_REAL", "_CALQUE_PLACE_REAL_DOC", "_CALQUE_ENTRY_WAIT", "_CALQUE_PLACE_ORG", "_CALQUE_MOUTH_REAL", "_CALQUE_MOUTH_IDIOM", "_CALQUE_TECH", "_CALQUE_ENTRY_FLOW", "_CALQUE_ENTRY_SUBJECT", "_CALQUE_CODE", "_CALQUE_SAKI_TIME", "_CALQUE_MATTERS_FOCUS", "_CALQUE_TABLE_SUBJECT", "_CALQUE_TABLE_ATTRIBUTIVE", "_CALQUE_TURN_CONTEXT", "_CALQUE_TURN_GAME", "_CALQUE_KIKU_DECIDE", "_CALQUE_HIBIKU_LITERAL", "_CALQUE_HIBIKU_ALL", "_CALQUE_HIBIKU_ALL_LITERAL", "_CALQUE_HIBIKU_MACHINE", "_CALQUE_HIBIKU_ALL_NOUN", "_CALQUE_HIBIKU_LISTED", "_CALQUE_MACHINE_TALK", "_LABEL_NUMBERING", "_LABEL_ASCII", "_LABEL_NOT_JAPANESE", "_LABEL_SEPARATED", "_CALQUE_VERSION_PUB", "_CALQUE_MARK", "_CALQUE_MARK_CONTEXT", "_CALQUE_HEAD", "_CALQUE_BRANCH", "_CALQUE_BRANCH_CONTEXT", "_CALQUE_TITLE", "_CALQUE_RULE", "_CALQUE_STYLE_CONTEXT", "_CALQUE_PERSON_END", "_CALQUE_ORG_SUFFIXES", "_CALQUE_SAKI_TAIL", "_CALQUE_PHRASE_KIKU", "_CALQUE_PHRASE_MATTERS", "_LABEL_MARKS", "_LABEL_AFTER_NOUN", "_LABEL_ATTRIBUTIVE", "_LABEL_ANYWHERE", "_LABEL_BETWEEN", "_LABEL_VERB_END", "_THIN_ADVERB", "_THIN_TAIL", "_KIKU_LITERAL", "_PUBLICATION_WORDS", "_CALQUE_STYLE_CONTEXT_PATTERN", "_CALQUE_SUBJECT_EXT", "_CALQUE_SUBJECT_REACH", "_CALQUE_DOC_TECH_MIN", "_LABEL_MAX", "_LABEL_MIN_BODY", "_CALQUE_BEFORE_WINDOW", "_THIN_PREFIX_MAX", "_V220_TYPE1_END", "_V220_TYPE2_END", "_V220_TYPE3_END", "_V220_TYPE4_END", "_CALQUE_SUBJECTS", "_CALQUE_SUBJECT_PARTICLES", "_CALQUE_COMPOUND_OK", "_CALQUE_REQUEST", "_CALQUE_ATTRIBUTIVE_END", "_CALQUE_ESTABLISHED_SAKI", "_CALQUE_SAKI_KANJI", "_CALQUE_ORG_WORDS", "_CALQUE_DOC_TECH_GENERAL", "_CALQUE_HIBIKU_COST", "_LABEL_COMMON", "_LABEL_ENDINGS", "_LABEL_NOT_ENDINGS", "_LABEL_ABSTRACT_ENDINGS", "_V220_TRIGGER_LITERALS", "_CALQUE_PHRASE_VERB", "_CALQUE_TYPE_NAMES", "_CALQUE_PHRASES", "_CALQUE_PHRASES_V250", "_CALQUE_VERSION", "_CALQUE_VERB_EACH", "_CALQUE_TERMS_V300", "_CALQUE_PHRASE", "_V220_QUOTES", "_V220_IDENTIFIER", "_V220_BUILTIN_LISTS",
}

// RecordLintBase は本家の関数に cases を通して記録にする (RecordVisibility と同じ仕組み)。
func (r Runner) RecordLintBase(ctx context.Context, header Header, cases []VisCase) (LintBaseGolden, error) {
	return r.RecordVisibility(ctx, header, cases)
}

// MarshalLintBase は LintBaseGolden を決まったバイト列にする。
func MarshalLintBase(g LintBaseGolden) ([]byte, error) { return MarshalVisibility(g) }

// ParseLintBase は lint-base.json のバイト列を読む。
func ParseLintBase(b []byte) (LintBaseGolden, error) {
	var g LintBaseGolden
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&g); err != nil {
		return g, fmt.Errorf("lint-base.json を読めません: %w", err)
	}
	return g, nil
}

// LintBaseConstant は記録の constants の case から、定数 name の値 (JSON) を返す。
func LintBaseConstant(g LintBaseGolden, name string) (json.RawMessage, error) {
	for _, c := range g.Cases {
		if c.Name != lintConstantsCase {
			continue
		}
		for i, n := range LintBaseConstants {
			if n != name {
				continue
			}
			for _, k := range c.Kept {
				if k.Index == i {
					return k.Result, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("記録に定数 %s の値がありません", name)
}

type tableRow [2]int

// GenerateLintEmojiTables は lint-base.json の記録から lint の絵文字の表 (lint/emoji_tables_gen.go) の Go のソースを作る。
// 表は本家の定数を読んだ結果そのもので、手で編集しない。同じ記録からは同じバイト列ができる。
func GenerateLintEmojiTables(g LintBaseGolden) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by ja-writing/oracle from testdata/golden/lint-base.json (make golden); DO NOT EDIT.\n\n")
	b.WriteString("package lint\n\n")
	raw, err := LintBaseConstant(g, "_EMOJI_DATA_VERSION")
	if err != nil {
		return nil, err
	}
	var version string
	if err := json.Unmarshal(raw, &version); err != nil {
		return nil, fmt.Errorf("_EMOJI_DATA_VERSION が文字列でない: %w", err)
	}
	fmt.Fprintf(&b, "// emojiDataVersion は表のもとにした Unicode の emoji データのバージョン。\nconst emojiDataVersion = %q\n\n", version)
	for _, t := range []struct{ constant, name, doc string }{
		{"_EMOJI_REGISTERED_TEXT_BASES", "emojiRegisteredTextBases", "登録されたテキスト表現の基底 (U+FE0E が続くと絵文字にしない)"},
		{"_EMOJI_EXPLICIT_VARIATION_BASES", "emojiExplicitVariationBases", "U+FE0F が続くと絵文字になる基底"},
		{"_EMOJI_OTHER_BASES", "emojiOtherBases", "そのほかの基底 (1 字で絵文字)"},
	} {
		raw, err := LintBaseConstant(g, t.constant)
		if err != nil {
			return nil, err
		}
		var rows []tableRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("%s が [lo, hi] の配列でない: %w", t.constant, err)
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("%s が空", t.constant)
		}
		fmt.Fprintf(&b, "// %s は %s。昇順で重ならない。\nvar %s = [...]runeRange{\n", t.name, t.doc, t.name)
		for _, r := range rows {
			fmt.Fprintf(&b, "\t{0x%x, 0x%x},\n", r[0], r[1])
		}
		b.WriteString("}\n\n")
	}
	raw, err = LintBaseConstant(g, "_EMOJI_FLAG_REGION_CODES")
	if err != nil {
		return nil, err
	}
	var codes []string
	if err := json.Unmarshal(raw, &codes); err != nil {
		return nil, fmt.Errorf("_EMOJI_FLAG_REGION_CODES が文字列の配列でない: %w", err)
	}
	if len(codes) == 0 {
		return nil, errors.New("_EMOJI_FLAG_REGION_CODES が空")
	}
	b.WriteString("// emojiFlagRegionCodes は旗として認める地域の 2 字の組。\nvar emojiFlagRegionCodes = [...]string{\n")
	for _, c := range codes {
		fmt.Fprintf(&b, "\t%q,\n", c)
	}
	b.WriteString("}\n")
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("生成した Go のソースを整形できません: %w", err)
	}
	return out, nil
}
