// Package visibility は yomiyasu v3.1.0 の markdown_visibility.py (lint・diff・check が共有する、Markdown の読み方・語の一覧・入力の扱い) を Go で作り直したもの。
//
// 位置は全部 Python の str の添字と同じコードポイントの添字 (rune の添字)。byte の添字は使わない。文字列は WTF-8 (単独のサロゲートも 1 字) で受ける。
// 正規表現は本家と同じパターン・同じフラグで internal/pyre を通す。本家と同じ値を返すことは testdata/golden/visibility.json (本家の記録) との突き合わせで見る
// (oracle_test.go)。例外は *Error (Type が Python の例外の型名、Message が str(exc)) で返す。
//
// # 本家の関数と Go の名前の対応 (本家のファイルを更新したら、この表で見直す関数を引く)
//
// 公開 (lint・diff・check・hook が import する名前と、その下の find_words_folder・load_words・check_pattern):
//
//	analyze_markdown            AnalyzeMarkdown (戻り値の dict は *Analysis。lines・blocks の項目は *Line・*Block)
//	line_visible_text           (*Analysis).LineVisibleText
//	range_overlaps_protected    (*Analysis).RangeOverlapsProtected
//	inline_protected_spans      InlineProtectedSpans (戻り値は []Span)
//	reference_definition_end    ReferenceDefinitionEnd
//	unicode_whitespace          UnicodeWhitespace
//	decode_source               DecodeSource (str の入力)・DecodeSourceBytes (bytes の入力。UnicodeDecodeError)
//	DOC_TYPES / READER_LEVELS   DocTypes / ReaderLevels
//	normalize_doc_type          NormalizeDocType (None は nil)
//	normalize_reader            NormalizeReader
//	context_label               ContextLabel
//	WordsError                  *Error (Type が TypeWordsError)
//	find_words_folder           FindWordsFolder
//	load_words                  LoadWords (戻り値の dict は *Words)
//	resolve_words               ResolveWords
//	current_directory           CurrentDirectory
//	words_context               WordsContextOf (戻り値は WordsContext)
//	words_label                 WordsLabel
//	check_pattern               CheckPattern (戻り値は *CheckedPattern)
//	input_path_problem          InputPathProblem
//	absolute_path               AbsolutePath
//	__version__                 Version
//	WORDS_FOLDER など定数       WordsFolder・WordsFiles・WordsMaxLines・FlagReplacementMax・PatternsFile・PatternsMax・PatternNameMax・PatternMaxLength・
//	                            PatternMaxRepeat・PatternMaxCost・PatternsMaxCost・InputPathOutside・InputPathHidden・InputPathNoCwd
//
// 本家の私有の名前 (analyze.go・inline.go・refdef.go・pattern.go・words.go・paths.go):
//
//	MarkdownLine / MarkdownBlock             Line / Block
//	_containers / _columns                   containers / columns
//	_physical_lines / _table_cells           physicalLines / tableCells
//	_merged_masks                            mergedMasks (と masks.firstEndingAfter)
//	_escaped                                 escaped
//	_tag_space_end / _terminator_positions   tagSpaceEnd / terminatorPositions
//	_tag_end / _link_end                     tagEnd / linkEnd
//	_RowCursor                               rowCursor
//	_reference_gap / _reference_destination  referenceGap / referenceDestination
//	_reference_title_end                     referenceTitleEnd
//	_normalize_choice                        normalizeChoice
//	_words_home                              wordsHome
//	_shared_checkout                         sharedCheckout
//	_worktree_words_folder                   worktreeWordsFolder
//	_words_lines / _flag_entries             wordsLines / flagEntries
//	_words_text_rows                         wordsTextRows
//	_patterns_lines                          patternLines
//	_PatternError                            *Error (Type が TypePatternError)
//	_pattern_escape / _pattern_class         patternEscape / patternClass
//	_pattern_sequence                        patternSequence
//	_parts_inside                            partsInside
//	_path_matches                            pathExists・pathIsDir・pathIsFile (statMode)
//	_ASCII_PUNCT など正規表現と表            isASCIIPunct・htmlBlockTags・tagName・attributeName・autolink・containerMarker・bareURL・blankTitleLine・setext・thematic・
//	                                         inlineEvents・fenceOpen・atxHeading
//
// analyze_markdown の行ごとの分類 (柵・HTML ブロック・setext・表・リスト・引用の判定の枝) は analyzer.classify に分けてある (本家は 1 つの関数の中)。
// 本家にあって移さないもの: _REFERENCE_LABEL と _reference_definition (本家の中でも使われていない)・_CONTAINER_MARKER_DEFAULT (テストが _CONTAINER_MARKER を差し替えるための印)・
// Windows の分岐 (ドライブ・USERPROFILE。macOS だけが対象)。
package visibility
