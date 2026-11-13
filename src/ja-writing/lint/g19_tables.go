// G19 の正規表現と表 (本家 yomiyasu_lint.py の 5162-5226)。組んだ後の文字列のまま写した (手で写して書き間違えないため)。
// 本家の値と同じかは TestLintBaseMatchesPythonRecording が lint-base.json の定数と比べる (tables_oracle_test.go の tableConstantValue)。

package lint

import "ja-writing/internal/pyre"

// 台詞の数と割合・一人称と数字の千字あたりの数・地の文の字数の境目 (本家の DIALOGUE_MIN など)。
const (
	dialogueMin       = 10
	dialogueRatio     = 0.5
	firstPersonMax    = 1.0
	densityMin        = 8
	narrativeMinChars = 1000
)

// documentSkipKinds は台詞と地の文を探さない行の種類 (本家は frozenset。記録は昇順の配列)。
var documentSkipKinds = []string{"code", "frontmatter", "html_block", "reference_definition", "heading", "heading_underline", "thematic_break", "table"}

// quotePairs は本家の _QUOTE_PAIRS (開き → 閉じ)。
var quotePairs = map[rune]rune{'「': '」', '『': '』'}

var (
	speakerLine = newLazy(`^\s*(?:[-*+]\s+|\d+\.\s+)?([^\s：:「」『』（）()、。！？!?，,．.]{1,12})[ \t　]*[：:][ \t　]*(.+)$`, 0)
	notSpeaker  = newLazy(`^(?:SE|ＳＥ|M|Ｍ|BGM|ＢＧＭ|[0-9０-９]+|(?:[0-9０-９]+)?(?:コマ|ページ|P|Ｐ)目?[0-9０-９]*|場所|時間|時|`+
		`人物|登場人物|柱|ト書き?|注|メモ|備考)$`, pyre.IGNORECASE)
	edgeStageNote = newLazy(`^[（(][^（）()]*[）)]\s*|\s*[（(][^（）()]*[）)]$`, 0)
	firstPerson   = newLazy(
		`(?<![公無])私(?![立鉄的物有服用語設費学道塾刑権財欲情事信案邸宅募企営益生淑書])`+
			`|(?<![ぁ-ゖ手受引見])(?:わたくし|わたし(?!て|た(?!ち)|ます|まし|ませ)|ぼく(?![じしさめ])|おれ(?!い|る|た(?!ち)|て|ま|ば|ず|ろ|そ))`+
			`|僕|俺|自分|筆者`, 0)
	narrativeNumber = newLazy(`(?<![A-Za-z_0-9０-９.,．，])[0-9０-９]+(?:[.,．，][0-9０-９]+)*(?![_0-9０-９])`, 0)
	asciiIdentifier = newLazy(`(?<![A-Za-z0-9_])[A-Za-z_][A-Za-z0-9_]*(?:[./][A-Za-z0-9_]+)+|(?<![A-Za-z])[A-Za-z]+_[A-Za-z0-9_]+`, 0)
)
