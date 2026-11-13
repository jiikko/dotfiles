// G18 の正規表現と表 (本家 yomiyasu_lint.py の 5008-5085)。組んだ後の文字列のまま写した (手で写して書き間違えないため)。
// 本家の値と同じかは TestLintBaseMatchesPythonRecording が lint-base.json の定数と比べる (tables_oracle_test.go の tableConstantValue)。

package lint

// shapeRule は G18 の 3 規則の名前と message (本家の _SHAPE_RULE_MESSAGES。この順で 1 行に出す)。
var shapeRules = []struct{ name, message string }{
	{"say_hedge", "文末の「と言えるでしょう」の候補です。文脈を見て、誰が何をどうするかを書き直すか確かめてください。推量の「でしょう」は残します。ある区分に当たるかを判断する文（指摘の出ないもの）は直しません。"},
	{"copy_comma_noun", "修飾語と短い名詞の間に読点を打って名詞で終える、キャッチコピーの型の候補です。文脈を見て、誰が何をどうするかを書き直すか確かめてください。"},
	{"bare_verb_chain", "主語も目的語もない動詞を読点で並べて止める文の候補です。文脈を見て、誰が何をどうするかを書き直すか確かめてください。"},
}

var shapeGateWords = []string{"えるでしょう"}

var shapeQuoteMarks = []string{"「", "『", "[", "“", `"`}

// copyNotModifier と copyINouns は本家の frozenset (記録は昇順の並びで比べる)。
var copyNotModifier = []string{"はい", "いいえ", "いい", "また", "まあ", "あの", "その", "この", "どの", "あのね",
	"ええ", "いや", "おい", "なお", "ただ", "しかし", "ちなみ", "つまり", "さて", "では"}

var copyINouns = []string{"思い", "想い", "願い", "違い", "匂い", "出会い", "味わい", "祝い", "誓い", "迷い", "笑い", "住まい",
	"佇まい", "問い", "扱い", "装い", "幸い"}

var (
	shapeCommaGate   = newLazy(`[ないたのうくぐすつぬぶむるか][*_]*、(?:[^、。\n]{1,14}。|[^。\n]{0,30}、[^、。\n]{0,30}[うくぐすつぬぶむる][*_]*。)`, 0)
	shapeHeadingMark = newLazy(`^\s*#{1,6}(?:\s+|$)|\s+#+\s*$`, 0)
	shapeCurlyQuoted = newLazy(`“[^“”]*”|\"[^\"]*\"`, 0)
	shapeSentence    = newLazy(`[^。！？!?]+(?:[。！？!?]+|$)`, 0)

	sayHedge            = newLazy(`(.{0,4})と(?:言|い)えるでしょう[。！!]?$`, 0)
	sayHedgeNounEnd     = newLazy(`[一-龯々〆ヵヶァ-ヴーｦ-ﾟA-Za-z0-9０-９Ａ-Ｚａ-ｚ]`, 0)
	sayHedgeNaAdjective = newLazy(`(?:的|重要|有効|必要|適切|十分|妥当|有用|大切|確実|可能|重大|有益|顕著|明確|不可欠|理想|最適|容易|困難|安全|安心)$`, 0)
	sayHedgeJudge       = newLazy(`(?:に|へ)(?:当た|あた)る(?:と(?:言|い)えるでしょう)|に該当する(?:と(?:言|い)えるでしょう)`, 0)

	copyCommaNoun        = newLazy(`(?:^|、)([^、]+?[ないたの])、([一-龯々〆ヵヶぁ-ゖァ-ヴーA-Za-z0-9０-９Ａ-Ｚａ-ｚ]{1,7})。$`, 0)
	copyModifierParticle = newLazy(`を|[はがで](?![ぁ-ゖ])`, 0)
	copyNounNot          = newLazy(`(?:[るたすだてでねよわかぞぜさうくぐつぬぶむずじ]|ない|ません|しい|たい)$|[はがにでの]`, 0)

	bareVerbPart     = newLazy(`[一-龯々ぁ-ゖァ-ヴー]*[うくぐすつぬぶむる]か?$`, 0)
	bareVerbLast     = newLazy(`[一-龯々ぁ-ゖァ-ヴー]*[うくぐすつぬぶむる]$`, 0)
	bareVerbPolite   = newLazy(`(?:ます|です|ません)か?$`, 0)
	bareVerbNotVerb  = newLazy(`(?:^[一二三四五六七八九十百千0-9０-９いくひとふたみよ]+つ|しく)か?$`, 0)
	bareVerbParticle = newLazy(`を|[はがに](?![ぁ-ゖ])`, 0)
)
