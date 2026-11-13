package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 M (数値) の記録: extract_numbers(text)・段 _number_risks(sides, full, groups)・数の下ごしらえと「一つの」「一つずつ」の見方の関数と定数。
// 組は、群 F〜I までと同じ入れ物 (入力の集まりと diff の入力の組・全文差分・見出し・文・種類・許可・規則の手書きの組) に、群 M 専用の手書きの組
// (本家のコメントの例文・数の書き方の分かれ目) と固定の種 (PCG 1076/14) の組を足したもの。

// diffNumberConstants は群 M の Go の表と比べる本家の定数 (正規表現は文字列と flags)。
var diffNumberConstants = []string{
	"_ARABIC", "_EMPHASIS", "_THOUSANDS", "_UNITS", "_UNIT_NOT_BEFORE", "_UNIT", "_SPACED_UNIT", "_LETTER_UNIT", "_THOUSAND", "_SAME_UNIT",
	"_KANJI_COUNTERS", "_KANJI_NUMBER", "_KANJI_DIGITS", "_KANJI_UNITS", "_KANJI_BEFORE", "_KANJI_BEFORE_WORDS", "_TIME_OF_DAY_WORDS",
	"_TIME_OF_DAY_CHARS", "_ICHIJI_HOUR_NEXT", "_KANJI", "_WHOLE_PERIOD_WORDS", "_SAME_PERIOD", "_KANJI_CHAIN", "_KANJI_HOUR_BEFORE",
	"_JIPPUN_AFTER", "_JIPPUN_DE", "_JIPPUN_DE_NOT", "_ICHIJI_JIPPUN_AFTER", "_KANJI_CHAIN_REACH", "_KANJI_EXCLUDED", "_KANJI_NUMERALS",
	"_BUN_NOT_BEFORE", "_NIBUN_NOT_BEFORE", "_JUNIBUN_NOT_BEFORE", "_JOU_NOT_BEFORE", "_KANJI_RANGE_HEAD", "_KANJI_RANGE_REACH",
	"_NOTATION_JOIN", "_JOIN_RANGE", "_DATE_UNITS", "_TIME_UNITS", "_NEXT_UNIT", "_NOTATION_SPACE", "_WEAK_ONE_AFTER", "_WEAK_ONE_BEFORE",
	"_WEAK_ONE_REPEATED", "_WEAK_ONE_MO", "_WEAK_ONE_PAIRED", "_WEAK_ONE_ONLY", "_WEAK_SINGLE", "_WEAK_EACH", "_KANA_NUMBERS", "_KANA_NUMBER",
	"_KANA_NUMBER_NOT_NEXT", "_KANA_NUMBER_NEXT_STRICT", "_KANA_NUMBER_STRICT", "_KANA_NUMBER_NOT_KANJI_NEXT", "_KANA_NUMBER_BEFORE",
	"_KANA_NUMBER_KATAKANA", "_HIRAGANA_CHAR", "_KANA_ONLY", "_QUANTITY_WORDS", "_QUANTITY_SAME", "_QUANTITY_CLAUSE_WORDS", "_QUANTITY_FOR",
	"_QUANTITY_KEEP_ANY", "_QUANTITY_OTHER_NOUN", "_QUANTITY_TOPIC", "_QUANTITY_SUBJECT", "_QUANTITY_CLAUSE_END", "_QUANTITY_NEG",
	"_QUANTITY_LEAD", "_QUANTITY_SCOPE_END", "_QUANTITY_NOT_NEG", "_QUANTITY_LIMIT_OFF", "_QUANTITY_SENTENCE_END", "_QUANTITY_CLAUSE_SPLIT",
	"_QUANTITY_PARTICLE_END", "_QUANTITY_HEAD", "_QUANTITY_KEEP_BEFORE", "_QUANTITY_CHANGE_BEFORE", "_NEGATION_WORDS", "_ZU_PREV", "_ZU_NEXT",
	"_LIST_NUMBER", "_NUMBER_MASKED", "_LEAD_MODIFIERS", "_CLAUSE_END", "_CONTEXT_WORD",
}

// DiffNumberFunctions は群 M の関数。
var DiffNumberFunctions = []string{"extract_numbers", "_number_risks", "_plain_number", "_kanji_value", "_negative_zu", "_quantity_negated",
	"_quantity_word_counts", "_head_clauses", "_other_subject_masked", "_one_sentence_without", "_text_ratio"}

// DiffNumberConstantNames は群 M の定数の名前 (Go の突き合わせが、この名前の Get を群 M の定数に回す)。
func DiffNumberConstantNames() []string { return diffNumberConstants }

// diffNumberStageCalls は組 1 つを、本家の _build_full_diff (覚える)・_sentence_pairs (覚える)・_number_risks と、両方の文書の extract_numbers に通す呼び出し。
func diffNumberStageCalls(old, new string, keep bool) []Call {
	build := dcall("_build_full_diff", old, new, nil)
	build.Save, build.Quiet = "full", true
	pairs := dcall("_sentence_pairs", diffFullRef(1), diffFullRef(0))
	pairs.Save, pairs.Quiet = "groups", true
	risks := dcall("_number_risks", diffFullRef(1), diffFullRef(0), diffRef("groups"))
	risks.Keep = keep
	calls := []Call{build, pairs, risks}
	for _, text := range []string{old, new} {
		c := dcall("extract_numbers", text)
		c.Keep = keep
		calls = append(calls, c)
	}
	return calls
}

// diffNumberBank は数の書き方ごとの文 (extract_numbers と _number_risks に通す)。算用数字・全角・桁区切り・単位・漢数字・平仮名の数・範囲・日付・時刻・「一つ」を含む。
var diffNumberBank = []string{
	"移行は3日で終えます。", "移行は 3 日で終えます。", "移行は3日間で終えます。", "参加者は１２３人です。", "料金は1,234円です。", "料金は1,234.5円です。", "番号は1,2,3です。", "番号は1,23,456円です。",
	"ビルドは1.2.3です。", "v2 を使います。", "変数 x_3 を使います。", "IPv6 は fe80::1 です。", "03日に開始します。", "0.5倍に減らします。", "2千件を処理します。", "3千円かかります。", "30sで終わります。",
	"100Mbpsで通信します。", "容量は8GiBです。", "容量は16 GB です。", "割合は95 %です。", "3Dで表示します。", "5Gに対応します。", "3階に集まります。", "3階層に分けます。", "3条に従います。",
	"3条件を満たします。", "第3条に従います。", "3か月かかります。", "3ヶ月かかります。", "3カ月かかります。", "3ヵ月かかります。", "3か所に置きます。", "3箇所に置きます。", "3種類あります。", "3つあります。",
	"**3**日かかります。", "３日かかります。", "３．５人です。", "１，２３４円です。", "3日，5日に行います。", "参加者は，30人です。", "10．5倍です。", "1.5.3 ビルドです。", "3.5.1.2 はビルドの番号です。",
	"三日かかります。", "十二月に行います。", "二〇二六年に行います。", "三十分かかります。", "十分に確かめました。", "十分間待ちます。", "数十分かかります。", "一時的に止めます。", "一時十分に集まります。", "一時間十分かかります。",
	"十時十分に集まります。", "午前一時に集まります。", "深夜一時に集まります。", "朝九時に集まります。", "毎朝八時に集まります。", "統一します。", "一般に公開します。", "最大三回まで試します。", "年二回行います。",
	"月一回行います。", "週三日働きます。", "一日三回飲みます。", "今日一日かかります。", "日一日と暖かくなります。", "去年一年かかりました。", "今週一週間かかります。", "当日一日限りです。", "休日一日だけです。",
	"二分しました。", "三分の一を使います。", "百分率で表します。", "四分位で見ます。", "五分五分です。", "九分九厘終わりました。", "十二分に備えます。", "二分しか使えません。", "一条の光が差します。",
	"三条あります。", "第三条に従います。", "三階に集まります。", "二つ選びます。", "三か所に置きます。", "三〜五日かかります。", "三から五日かかります。", "三、四日かかります。", "三・四日かかります。",
	"一つの窓口で受けます。", "一つずつ確認します。", "もう一つ確認します。", "一つも見つかりません。", "一つのみ選べます。", "1つの窓口で受けます。", "1つずつ確認します。", "もう1つ確認します。",
	"ひとつ確認します。", "ふたつ確認します。", "ひとり参加します。", "ふたり参加します。", "いつつ並べます。", "ここのつ並べます。", "家にいつつ働きます。", "ひとつまみ入れます。", "ひとり言を言います。",
	"ひとつきり持ちます。", "むっつり黙ります。", "家にいつつも働きます。", "一人ひとりに配ります。", "一人一回ずつ使います。", "一回三十分ずつ使います。", "一回十分で終わります。",
	"3〜5日かかります。", "3から5日かかります。", "3-5日かかります。", "2026-10-13 に行います。", "2026/10/13 に行います。", "10:30 に集まります。", "10時30分に集まります。", "2026年10月13日に行います。",
	"10月13に行います。", "3/4 を使います。", "4月3日に行います。", "12:25 に集まります。", "25分かかります。", "10:30〜11:15 に行います。", "5〜10人参加します。", "手順3-5を行います。", "3 s かかります。",
	"3 日かかります。", "約120人参加します。", "120人参加します。", "100人以上参加します。", "合計5日かかります。", "最大3日かかります。", "およそ2週間かかります。", "計3回行います。",
	"1. 手順を読む。", "2) 手順を読む。", "(1) 手順を読む。", "（１）手順を読む。", "> 1. 引用の中です。", "`3日` と書きます。", "https://example.com/v3/abc を見ます。", "![3日の図](path/3日.png) を見ます。",
	"3週間かかります。", "3年かかります。", "3時間かかります。", "5分かかります。", "10秒かかります。", "100%に達します。", "50TBを使います。", "2MBを使います。", "300msかかります。", "12pxにします。",
	"3回行います。", "3件あります。", "3個あります。", "3名参加します。", "3倍にします。", "3割減らします。", "3度試します。", "3点あります。", "3本あります。", "3枚あります。", "3台あります。", "3社参加します。",
	"3歳です。", "3字です。", "3行です。", "3章を読みます。", "3節を読みます。", "3項を読みます。", "3号を読みます。", "3月3日です。", "3時3分です。",
}

// diffNumberSwaps は文の数の書き方を変える・数を変える置き換え (元の文字列, 変えた文字列)。
var diffNumberSwaps = [][2]string{
	{"3日", "5日"}, {"3日", "3週間"}, {"3日", "三日"}, {"3日", "3 日"}, {"3日", "3日間"}, {"3日", "数日"}, {"3日", ""}, {"3日", "3日と5日"}, {"3か月", "3ヶ月"}, {"3か月", "三か月"}, {"3か月", "3か所"},
	{"5日", "3〜5日"}, {"3〜5日", "3日から5日"}, {"3〜5日", "5日"}, {"3から5日", "3〜5日"}, {"3-5日", "3日から5日"}, {"2026-10-13", "2026年10月13日"}, {"2026/10/13", "2026年10月13日"},
	{"10:30", "10時30分"}, {"10時30分", "10:30"}, {"12:25", "25分"}, {"10:30〜11:15", "10時15分から11時30分"}, {"3/4", "4月3日"}, {"5〜10人", "5人から10人"},
	{"三日", "3日"}, {"三日", "五日"}, {"二つ", "三つ"}, {"一つ", "二つ"}, {"一つの", "複数の"}, {"一つずつ", "まとめて"}, {"一つずつ", "順に"}, {"一つずつ", "同時に"}, {"一つの", "同じ"}, {"1つの", "三つの"},
	{"ひとつ", "ふたつ"}, {"ひとり", "3人"}, {"ひとり", "一人"}, {"ふたり", "3人"}, {"1つ", "ひとつ"}, {"1つずつ", "一つずつ"}, {"1,234円", "1234円"}, {"1,234円", "1,235円"}, {"１２３人", "123人"},
	{"３日", "3日"}, {"3人", "3名"}, {"3人", "5人"}, {"約120人", "120人"}, {"120人", "約120人"}, {"100人以上", "100人"}, {"3回", "4回"}, {"3回", "毎回"}, {"3%", "30%"},
	{"午前一時", "午前1時"}, {"午前一時", "午前二時"}, {"十時十分", "10時10分"}, {"朝九時", "朝9時"}, {"三十分", "30分"}, {"三十分", "五十分"}, {"十分に", "十分間に"}, {"年二回", "年2回"}, {"年二回", "年一回"},
	{"今日一日", "今日1日"}, {"今日一日", "今日は"}, {"日一日と", "日ごとに"}, {"二分しか", "5分しか"}, {"三分の一", "3分の1"}, {"一条の光", "1条の光"}, {"3階", "3階層"}, {"3条", "3条件"},
	{"v2", "v3"}, {"x_3", "x_4"}, {"30s", "30秒"}, {"100Mbps", "100 Mbps"}, {"16 GB", "16GB"}, {"95 %", "95%"}, {"8GiB", "8GB"},
}

// diffNumberHandPairs は数値の恐れの手書きの組 (名前, 元, 後)。本家のコメントの例と、組の文の比べ・打ち消し・まとまり・移した値・「一つ」の分かれ目を狙う。
func diffNumberHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("移行は3日で終える。"), new: doc("移行は3日で終える。")},
		{name: "replaced", old: doc("移行は3日で終える。"), new: doc("移行は5日で終える。")},
		{name: "replaced-unit", old: doc("移行は3日で終える。"), new: doc("移行は3週間で終える。")},
		{name: "added-in-pair", old: doc("移行は3日で終える。"), new: doc("移行は3日で終える。予備は5日を見込む。")},
		{name: "added-same-sentence", old: doc("移行は終える。"), new: doc("移行は5日で終える。")},
		{name: "lost-in-pair", old: doc("移行は3日で終える。"), new: doc("移行は終える。")},
		{name: "lost-sentence", old: doc("手順を読む。", "移行は3日で終える。"), new: doc("手順を読む。")},
		{name: "added-sentence", old: doc("手順を読む。"), new: doc("手順を読む。", "移行は3日で終える。")},
		{name: "order-swap", old: doc("設計は3日、試験は2人で行う。"), new: doc("設計は2人、試験は3日で行う。")},
		{name: "order-same-unit", old: doc("部屋にふたり、廊下に3人いる。"), new: doc("部屋に3人、廊下にふたりいる。")},
		{name: "order-lead", old: doc("まず設計に3日、次に試験に5時間かける。"), new: doc("設計に5時間、試験に3日かける。")},
		{name: "order-clause", old: doc("上限は20、応答は504とする。"), new: doc("応答は504、上限は20とする。")},
		{name: "order-table", old: doc("| a | b |", "|---|---|", "| 3日 | 5日 |"), new: doc("| a | b |", "|---|---|", "| 5日 | 3日 |")},
		{name: "order-time", old: doc("10時から設計レビュー、14時からテストを行う。"), new: doc("14時から設計レビュー、10時からテストを行う。")},
		{name: "order-not-swapped", old: doc("会議は3回、2時間ずつ行う。"), new: doc("会議は2時間ずつ、3回行う。")},
		{name: "order-modifier", old: doc("移行は約2週間、検証で約3日かかる。"), new: doc("検証は約3日、移行で約2週間かかる。")},
		{name: "order-three", old: doc("A は3日、B は5日、C は7日。"), new: doc("A は7日、B は3日、C は5日。")},
		{name: "cancel-range", old: doc("移行は3〜5日で終える。"), new: doc("移行は3日から5日で終える。")},
		{name: "cancel-date", old: doc("開始は2026-10-13 です。"), new: doc("開始は2026年10月13日です。")},
		{name: "cancel-time", old: doc("開始は10:30 です。"), new: doc("開始は10時30分です。")},
		{name: "cancel-space", old: doc("移行は3 日で終える。"), new: doc("移行は3日で終える。")},
		{name: "cancel-not-unit", old: doc("手順3を行う。"), new: doc("手順は3日で行う。")},
		{name: "cancel-time-wrong", old: doc("開始は12:25 です。"), new: doc("開始は25分です。")},
		{name: "cancel-date-wrong", old: doc("開始は3/4 です。"), new: doc("開始は4月3日です。")},
		{name: "cancel-time-order", old: doc("10:30〜11:15 に行う。"), new: doc("10時15分から11時30分に行う。")},
		{name: "partner-range-shrink", old: doc("移行は3〜5日で終える。", "", "予備日は3日です。"), new: doc("移行は5日で終える。", "", "予備日は3日です。")},
		{name: "partner-range-grow", old: doc("移行は5日で終える。", "", "予備日は3日です。"), new: doc("移行は3〜5日で終える。", "", "予備日は3日です。")},
		{name: "partner-around", old: doc("発生日時：3〜5日", "影響：なし"), new: doc("発生日時は5日で、影響はなし。")},
		{name: "moved-split", old: doc("30秒のタイムアウトに達し、ゲートウェイが504を返した。"), new: doc("30秒のタイムアウトに達した。ゲートウェイは504を返した。")},
		{name: "moved-merge", old: doc("30秒のタイムアウトに達した。ゲートウェイは504を返した。"), new: doc("30秒のタイムアウトに達し、ゲートウェイが504を返した。")},
		{name: "moved-not-same-unit", old: doc("移行は3日で終える。"), new: doc("移行は5日で終える。予備として3日を見込む。")},
		{name: "moved-similar", old: doc("設定を確認する。", "移行は3日で終える。"), new: doc("設定を確認し、移行を3日で終える。")},
		{name: "weak-each-to-together", old: doc("ファイルを一つずつ確認する。"), new: doc("ファイルをまとめて確認する。")},
		{name: "weak-each-to-order", old: doc("ファイルを一つずつ確認する。"), new: doc("ファイルを順に確認する。")},
		{name: "weak-each-to-multi", old: doc("質問は一つずつどうぞ。"), new: doc("質問はいくつでもどうぞ。")},
		{name: "weak-single-to-multi", old: doc("一つのファイルに書く。"), new: doc("複数のファイルに書く。")},
		{name: "weak-single-to-same", old: doc("一つのファイルに書く。"), new: doc("同じファイルに書く。")},
		{name: "weak-single-separate", old: doc("設定は一つのファイルに書きます。"), new: doc("設定は別々の場所に置きます。")},
		{name: "weak-single-room", old: doc("一つの部屋で行う。"), new: doc("部屋で、別々の時間に行う。")},
		{name: "weak-negated", old: doc("ファイルを一つずつ確認する。"), new: doc("並列にせず、順に確認する。")},
		{name: "weak-limit", old: doc("一つの部屋に入れる。"), new: doc("部屋に入れる。上限はない。")},
		{name: "weak-limit-exceed", old: doc("一つの部屋に入れる。"), new: doc("部屋ごとの上限の十人を超えない。")},
		{name: "weak-two-sentences", old: doc("一つずつ確認します。すべての確認が終わったら報告します。"), new: doc("順に確認し、すべて済んだら報告します。")},
		{name: "weak-topic", old: doc("申請は一つずつ審査します。"), new: doc("申請は逐一見て、複数の担当者が確かめます。")},
		{name: "weak-other-noun", old: doc("ファイルを一つずつ確認する。"), new: doc("ファイルを見て、複数の誤りを探す。")},
		{name: "weak-moto", old: doc("もう一つの利点は速さです。"), new: doc("速さも利点です。")},
		{name: "weak-ichiji-mo", old: doc("一つも見つからない。"), new: doc("見つからない。")},
		{name: "weak-hitotsu", old: doc("ひとつ確認する。"), new: doc("確認する。")},
		{name: "weak-hitotsu-futatsu", old: doc("ひとつ確認する。"), new: doc("ふたつ確認する。")},
		{name: "weak-hitori", old: doc("3人が参加する。"), new: doc("ひとりが参加する。")},
		{name: "weak-ichinichi", old: doc("今日一日かかる。"), new: doc("今日は終わる。")},
		{name: "weak-bun", old: doc("二分しか使えない。"), new: doc("5分しか使えない。")},
		{name: "kanji-to-arabic", old: doc("三か月かかる。十二月に行う。"), new: doc("3か月かかる。12月に行う。")},
		{name: "kanji-change", old: doc("三か月かかる。"), new: doc("五か月かかる。")},
		{name: "masked-code", old: doc("```", "3日", "```", "手順を読む。"), new: doc("```", "5日", "```", "手順を読む。")},
		{name: "masked-code-lost", old: doc("移行は3日で終える。"), new: doc("移行は `3日` で終える。")},
		{name: "masked-url", old: doc("https://example.com/v3/guide を読む。"), new: doc("https://example.com/v4/guide を読む。")},
		{name: "masked-image", old: doc("![3日の図](a/3日.png)"), new: doc("![5日の図](a/3日.png)")},
		{name: "masked-frontmatter", old: doc("---", "version: 3", "---", "本文です。"), new: doc("---", "version: 4", "---", "本文です。")},
		{name: "masked-html", old: doc("<div data-n=\"3\">本文</div>"), new: doc("<div data-n=\"4\">本文</div>")},
		{name: "list-number", old: doc("1. 手順を読む。", "2. 手順を行う。"), new: doc("1. 手順を読む。", "3. 手順を行う。")},
		{name: "quote", old: doc("> 移行は3日で終える。"), new: doc("> 移行は5日で終える。")},
		{name: "hunk-run", old: doc("手順は3日。", "", "試験は5人。"), new: doc("手順は3日。試験は5人。")},
		{name: "run-shifted", old: doc("一段落目は3日。", "", "二段落目は5日。", "", "三段落目は7日。"), new: doc("一段落目は3日。二段落目は5日。", "", "三段落目は9日。")},
		{name: "multi-hunk", old: doc("A は3日。", "同じ行。", "B は5日。"), new: doc("A は4日。", "同じ行。", "B は6日。")},
		{name: "thousands", old: doc("料金は1,234円。"), new: doc("料金は1234円。")},
		{name: "thousand-char", old: doc("2千件を処理する。"), new: doc("2000件を処理する。")},
		{name: "fullwidth", old: doc("参加者は１２３人。"), new: doc("参加者は123人。")},
		{name: "big-number", old: doc("値は123456789012345678901234567890円。"), new: doc("値は12345678901234567890123456789円。")},
		{name: "duplicate-numbers", old: doc("3日と3日と5日を使う。"), new: doc("3日と5日を使う。")},
		{name: "unit-letter", old: doc("遅延は30sです。"), new: doc("遅延は30秒です。")},
		{name: "ratio-bound", old: doc("甲は3日。", "乙の長い説明の文です。移行は5日かかる。"), new: doc("移行は5日かかる。", "甲は4日。")},
		{name: "empty-old", old: "", new: doc("移行は3日。")},
		{name: "empty-new", old: doc("移行は3日。"), new: ""},
		{name: "crlf", old: "移行は3日で終える。\r\n試験は5人。\r\n", new: "移行は5日で終える。\r\n試験は5人。\r\n"},
	}
}

// diffNumberFuzz は固定の種 (PCG 1076/14) から作った、数の書き方・数・「一つ」の語を変える組。文の並びに数の文を足す・消す・入れ替える・つなぐ・分ける。
func diffNumberFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 14))
	pick := func() string { return diffNumberBank[r.IntN(len(diffNumberBank))] }
	variant := func(s string) string {
		for range 6 {
			sw := diffNumberSwaps[r.IntN(len(diffNumberSwaps))]
			if strings.Contains(s, sw[0]) {
				return strings.Replace(s, sw[0], sw[1], 1)
			}
		}
		return pick()
	}
	var cases []diffFullCase
	for n := range 1400 {
		var lines [][]string
		for range 1 + r.IntN(4) {
			var line []string
			for range 1 + r.IntN(3) {
				line = append(line, pick())
			}
			lines = append(lines, line)
		}
		cur := make([][]string, len(lines))
		for i, l := range lines {
			cur[i] = append([]string{}, l...)
		}
		for ops := 1 + r.IntN(3); ops > 0 && len(cur) > 0; ops-- {
			i := r.IntN(len(cur))
			if len(cur[i]) == 0 {
				continue
			}
			j := r.IntN(len(cur[i]))
			switch r.IntN(8) {
			case 0, 1:
				cur[i][j] = variant(cur[i][j])
			case 2:
				cur[i] = append(cur[i][:j], cur[i][j+1:]...)
			case 3:
				cur[i] = append(cur[i][:j], append([]string{pick()}, cur[i][j:]...)...)
			case 4:
				k := r.IntN(len(cur))
				cur[i], cur[k] = cur[k], cur[i]
			case 5:
				cur[i][j] = pick()
			case 6:
				// 文を 2 つの文に分ける・つなげる (読点でつなぐ)
				if j+1 < len(cur[i]) {
					first := strings.TrimSuffix(cur[i][j], "。")
					cur[i] = append(cur[i][:j], append([]string{first + "、" + cur[i][j+1]}, cur[i][j+2:]...)...)
				}
			default:
				cur[i][j] = variant(cur[i][j]) + variant(pick())
			}
		}
		join := func(ls [][]string, sep string) string {
			var parts []string
			for _, l := range ls {
				if len(l) > 0 {
					parts = append(parts, strings.Join(l, sep))
				}
			}
			return diffJoinParts(parts, r.IntN(3))
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: join(lines, ""), new: join(cur, "")})
	}
	return cases
}

// diffNumberOrder は固定の種 (PCG 1076/15) から作った、節ごとの数の値と単位を入れ替える組 (数の並びの候補を狙う)。同じ単位の入れ替え・違う単位の入れ替え・
// 直前の語が違う入れ替え・同じ語に付く 2 つの数の語順だけの言い換え・節の頭の数・約や計の付いた数を混ぜる。
func diffNumberOrder() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 15))
	labels := []string{"設計", "試験", "移行", "検証", "準備", "本番", "調査", "報告", "部屋", "廊下"}
	values := []string{"3日", "5日", "2時間", "30分", "4人", "10件", "1週間", "2回", "ふたり", "三日", "約3日", "計5人", "最大2時間", "一つ", "20", "504"}
	particles := []string{"は", "に", "で", "が"}
	var cases []diffFullCase
	for n := range 400 {
		count := 2 + r.IntN(2)
		var ls, vs []string
		for range count {
			ls = append(ls, labels[r.IntN(len(labels))])
			vs = append(vs, values[r.IntN(len(values))])
		}
		particle := particles[r.IntN(len(particles))]
		build := func(vals []string) string {
			parts := make([]string, count)
			for i := range count {
				parts[i] = ls[i] + particle + vals[i]
			}
			return strings.Join(parts, "、") + "とする。"
		}
		swapped := append([]string{}, vs...)
		switch r.IntN(3) {
		case 0:
			swapped[0], swapped[1] = swapped[1], swapped[0]
		case 1:
			swapped = append(swapped[1:], swapped[0])
		default:
			swapped[count-1], swapped[0] = swapped[0], swapped[count-1]
		}
		old, new := build(vs), build(swapped)
		if r.IntN(5) == 0 {
			// 同じ語に付く 2 つの数の語順だけの言い換え
			old, new = ls[0]+"は"+vs[0]+"、"+vs[1]+"ずつ行う。", ls[0]+"は"+vs[1]+"ずつ、"+vs[0]+"行う。"
		}
		cases = append(cases, diffFullCase{name: "order-" + strconv.Itoa(n), old: diffDoc(old), new: diffDoc(new)})
	}
	return cases
}

// diffJoinParts は行をつなげる (0: 改行 1 つ、1: 空行を挟む、2: 改行 1 つ)。
func diffJoinParts(parts []string, mode int) string {
	if len(parts) == 0 {
		return ""
	}
	sep := "\n"
	if mode == 1 {
		sep = "\n\n"
	}
	return strings.Join(parts, sep) + "\n"
}

// 数の下ごしらえの関数 (_plain_number・_kanji_value・_negative_zu・_head_clauses など) に通す手書きの文字列。
var diffNumberDigits = []string{"0", "00", "03", "0.5", "00.50", "1", "10", "007", "1.2.3", "100", "0.0", "000.000", ""}

var diffNumberKanji = []string{"一", "二", "九", "十", "十二", "二十", "二十一", "百", "百二十", "百二十三", "千", "千二百三十四", "三千", "三千五", "二〇二六", "〇", "〇〇", "〇一", "一〇", "二三", "十十", "百十", "千百", "百千", "十〇", "一十", "九百九十九", "二千二十六", "二〇〇〇〇〇〇〇〇〇〇〇〇〇〇〇〇"}

var diffNumberNegations = []string{
	"並列にせず、順に確認する。", "並列にして確認する。", "並列ではなく、順に確認する。", "並列でなく、順に確認する。", "まとめて受け付けず、順に確認する。", "複数に分けない。", "複数の窓口で受け付けるので待ちません。",
	"まとめて送って取りこぼしがない。", "まとめて送ってかまいません。", "同時でも問題ない。", "並行しても差し支えない。", "まとめて送らなければならない。", "同時にしないといけない。", "同時にしないと間に合わない。",
	"同時にせざるを得ない。", "一つしかない。", "ほかない。", "並列かもしれない。", "並列に違いない。", "おこなう。", "まかなう。", "ともなう。", "おはなしする。", "みなす。", "こなす。", "少ない。", "危ない。", "まず確認する。", "必ず確認する。",
	"とりあえず確認する。", "あえず。", "かならず確認する。", "確認せず。", "確認せずに進める。", "確認せず、進める。", "読まず嫌い。", "使わずに済ませる。", "少しずつ進める。", "わずかに進める。", "はずがない。", "一つずつ進める。",
	"上限はない。", "上限なし。", "上限を設けない。", "上限をなくす。", "上限を撤廃する。", "上限を超えない。", "上限は3件。", "上限を決めていない。", "上限を外す。", "上限を取り払う。", "上限を解除する。", "上限について特になし。",
	"上限は特にありません。", "ないと困る。", "ないとしたら。", "ないとする。", "同時に動かす。", "並行して動かさない。", "並列にしても構わない。", "並列に動かさず、順に動かす。", "並列に動かすのでよいか。",
}

var diffNumberHeads = [][2]string{
	{"質問は一つずつどうぞ。", "質問"}, {"設定は一つのファイルに書く。", "ファイル"}, {"ファイルを開き、結果をまとめて共有する。", "ファイル"}, {"ファイルを開く。結果を共有する。", "ファイル"},
	{"窓口は、部屋で、受け付ける。", "部屋"}, {"窓口は、部屋で、受け付ける。", "窓口"}, {"窓口は、部屋で、受け付ける。", "受け付"}, {"議題を確認し、結果はまとめて共有する。", "共有"}, {"議題を確認し、結果はまとめて共有する。", "議題"},
	{"同じ窓口で受ける。", "窓口"}, {"「一つの」窓口。", "窓口"}, {"窓口は複数あります。", "窓口"}, {"", ""}, {"窓口。", "窓口"}, {"窓口?!", "窓口"},
}

var diffNumberTopics = [][2]string{
	{"申請は一つずつ審査します。", "申請は逐一見て、複数の担当者が確かめます。"}, {"申請は一つずつ審査します。", "担当者は複数の申請を見ます。"}, {"申請は一つずつ審査します。", "申請は、担当者が複数を見ます。"},
	{"鍵は一つずつ配ります。", "合鍵は各自が別々に持ちます。"}, {"", "複数が見ます。"}, {"は一つずつ。", "複数が見ます。"}, {"申請は一つずつ審査します。", ""}, {"リリースは一つずつ出す。", "リリースごとに別々の名前を付けます。"},
	{"問い合わせは一つずつ見ます。", "問い合わせが複数来ます。"}, {"設定は一つのファイルに書く。", "設定が複数の担当者に渡る。"},
}

var diffNumberRatios = [][2]string{
	{"", ""}, {"a", ""}, {"あいう", "あいう"}, {"あいう", "あいえ"}, {"あいうえお", "かきくけこ"}, {"移行は3日で終える。", "移行は5日で終える。"}, {"abcd", "bcde"},
}

// diffNumberStringCalls は数の下ごしらえの関数の呼び出し (結果そのものを記録に残す)。
func diffNumberStringCalls() []Call {
	var calls []Call
	add := func(fn string, args ...any) {
		c := dcall(fn, args...)
		c.Keep = true
		calls = append(calls, c)
	}
	for _, s := range diffNumberDigits {
		add("_plain_number", s)
	}
	for _, s := range diffNumberKanji {
		add("_kanji_value", s)
	}
	for _, s := range diffNumberNegations {
		add("_negative_zu", s)
		add("_quantity_word_counts", s)
		add("_quantity_negated", s, 0)
		if i := strings.Index(s, "、"); i >= 0 {
			add("_quantity_negated", s, len([]rune(s[:i])))
		}
		add("_quantity_negated", s, len([]rune(s)))
		add("_quantity_negated", s, 3)
	}
	for _, h := range diffNumberHeads {
		add("_head_clauses", h[0], h[1])
		add("_one_sentence_without", h[0], h[1])
	}
	for _, t := range diffNumberTopics {
		add("_other_subject_masked", t[0], t[1])
	}
	for _, p := range diffNumberRatios {
		add("_text_ratio", p[0], p[1])
	}
	// 2,000 字を超える組 (前後の同じ字から求める形) と、ちょうど 2,000 字の組 (difflib の ratio)
	long := strings.Repeat("あ", 2001)
	exact := strings.Repeat("あ", 2000)
	add("_text_ratio", long, long+"い")
	add("_text_ratio", exact, exact[:len(exact)-3]+"いうえ")
	add("_text_ratio", exact, long)
	return calls
}

// diffNumberCases は群 M の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffNumberCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "number/constants", Calls: diffConstantCalls(diffNumberConstants)}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "number/" + b.name, Calls: diffNumberStageCalls(b.old, b.new, false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
	}{{"number/hand/", diffNumberHandPairs(), true}, {"number/order/", diffNumberOrder(), false}, {"number/fuzz/", diffNumberFuzz(), false}} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffNumberStageCalls(f.old, f.new, g.keep)})
		}
	}
	var calls []Call
	for _, s := range diffNumberBank {
		c := dcall("extract_numbers", s)
		c.Keep = true
		calls = append(calls, c)
	}
	cases = append(cases, VisCase{Name: "number/bank", Calls: calls})
	return append(cases, VisCase{Name: "number/strings", Calls: diffNumberStringCalls()})
}
