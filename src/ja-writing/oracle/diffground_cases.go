package oracle

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

// 資料との照合 (ground)・事実の穴の候補 (fact_gaps)・引数を渡さない diff()・extract_numbers の記録。check が diff から使う口で、
// 記録は diff-base.json と別のファイルに持つ (diff-base.json は作り直しに約 5 分かかり、この 4 つは diff の CLI からは届かない)。
// ground・fact_gaps・diff の戻り値は、check が状態フォルダに書くときと同じ json.dumps(ensure_ascii=True, indent=1) にした文字列も記録する
// (正規の JSON の digest は鍵の順を見ないので、dict の鍵の順を本家と比べるため)。

// DiffGroundFile は記録のファイル (testdata/golden/ の中)。形は visibility.json と同じ。
const DiffGroundFile = "diff-ground.json"

// DiffGroundGolden は diff-ground.json の中身。
type DiffGroundGolden = VisibilityGolden

// DiffGroundFunctions は記録の対象の関数。dumps は json.dumps (直前の呼び出しが覚えた戻り値を、check が書くのと同じ形の文字列にする)。
var DiffGroundFunctions = []string{"ground", "fact_gaps", "diff", "extract_numbers", "dumps"}

// DiffGroundConstants は Go の値と比べる本家の定数 (Get で読む)。
var DiffGroundConstants = []string{"_NAME", "_URL_SCHEME", "_GROUND_WORD", "_CERTAINTY", "_GROUND_CERTAINTY", "_BARE_SETTING", "_SQUASHED", "_GROUND_UNIT",
	"_GROUND_SAME_UNIT", "_GROUND_NOT_JUDGED", "_GROUND_KIND_ORDER", "_GROUND_MASKED", "_CERTAINTY_WORDS",
	"_GAP_HEDGE", "_GAP_NEGATED_TAIL", "_GAP_VAGUE", "_GAP_COMPARED", "_GAP_QUESTION", "_GAP_KANJI_NUM", "_GAP_ASSUMPTION", "_GAP_QUOTE",
	"_GAP_OMISSION", "_GAP_NEED", "_GAP_SKIP_KINDS", "MATERIALS_FORMAT_VERSION"}

// DiffGroundSave は戻り値を覚える名前 (直後の dumps が読む)。
const DiffGroundSave = "ground_last"

// diffGroundDumps は直前に覚えた戻り値を json.dumps(ensure_ascii=True, indent=1) にする呼び出し。
func diffGroundDumps() Call {
	return Call{Module: "json", Function: "dumps", Args: []any{map[string]any{"$ref": DiffGroundSave}},
		Kwargs: map[string]any{"ensure_ascii": true, "indent": 1}, Digest: true}
}

// groundCall は ground(final, materials, original, labels) と、その戻り値の dumps。raises が真の呼び出し (本家が例外になる入力) は dumps を付けない。
func groundCall(final string, materials []string, original any, labels any, raises bool) []Call {
	ms := make([]any, len(materials))
	for i, m := range materials {
		ms[i] = m
	}
	c := dcall("ground", final, ms, original, labels)
	c.Keep = len(final)+len(original2(original))+totalLen(materials) < 600
	if raises {
		return []Call{c}
	}
	c.Save = DiffGroundSave
	return []Call{c, diffGroundDumps()}
}

func original2(v any) string {
	s, _ := v.(string)
	return s
}

func totalLen(texts []string) int {
	n := 0
	for _, t := range texts {
		n += len(t)
	}
	return n
}

// factGapsCall は fact_gaps(text, doc_type) と、その戻り値の dumps。
func factGapsCall(text string, docType any, raises bool) []Call {
	c := dcall("fact_gaps", text, docType)
	c.Keep = len(text) < 600
	if raises {
		return []Call{c}
	}
	c.Save = DiffGroundSave
	return []Call{c, diffGroundDumps()}
}

// DiffGroundCases は diff-ground.json の case。
func DiffGroundCases(c Corpus, diffDir string, inputs []DiffInput) ([]VisCase, error) {
	var cases []VisCase
	consts := make([]Call, len(DiffGroundConstants))
	for i, name := range DiffGroundConstants {
		consts[i] = Call{Module: DiffModule, Function: name, Get: true, Digest: true, Keep: true}
	}
	cases = append(cases, VisCase{Name: "ground/constants", Calls: consts})

	type pair struct{ name, old, new string }
	var pairs []pair
	materials := map[string][]string{}
	for _, in := range inputs {
		var texts [2]string
		for i, name := range []string{"old.md", "new.md"} {
			body, err := os.ReadFile(filepath.Join(diffDir, in.Name, name))
			if err != nil {
				return nil, err
			}
			texts[i] = string(body)
		}
		for _, m := range in.Materials {
			body, err := os.ReadFile(filepath.Join(diffDir, in.Name, m))
			if err != nil {
				return nil, err
			}
			materials["input/"+in.Name] = append(materials["input/"+in.Name], string(body))
		}
		pairs = append(pairs, pair{"input/" + in.Name, texts[0], texts[1]})
	}
	for _, p := range DiffCorpusPairs(c) {
		o, err := c.Read(strings.TrimPrefix(p[1], "corpus/"))
		if err != nil {
			return nil, err
		}
		n, err := c.Read(strings.TrimPrefix(p[2], "corpus/"))
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, pair{"corpus/" + p[0], o, n})
	}
	// 組ごとに: 新規作成 (元の文を資料に)・推敲 (資料なし)・推敲 (隣の組の後の文と、組の資料を資料に)・逆向きの新規作成 (資料 2 つ)
	for i, p := range pairs {
		other := pairs[(i+1)%len(pairs)].new
		var calls []Call
		calls = append(calls, groundCall(p.new, []string{p.old}, nil, nil, false)...)
		calls = append(calls, groundCall(p.new, nil, p.old, []any{}, false)...)
		ms := append([]string{other}, materials[p.name]...)
		labels := []any{"隣"}
		for j := range materials[p.name] {
			labels = append(labels, fmt.Sprintf("資料%d", j+1))
		}
		calls = append(calls, groundCall(p.new, ms, p.old, labels, false)...)
		calls = append(calls, groundCall(p.old, []string{p.new, other}, nil, []any{nil, "b"}, false)...)
		cases = append(cases, VisCase{Name: "ground/pair/" + p.name, Calls: calls})
	}
	// 入力の集まりの全ファイル: 資料なしの新規作成 (取り出した項目が全部「資料にない」) と事実の穴の候補
	for _, f := range c.Files {
		text, err := c.Read(f.Path)
		if err != nil {
			return nil, err
		}
		calls := groundCall(text, nil, nil, nil, false)
		calls = append(calls, factGapsCall(text, nil, false)...)
		cases = append(cases, VisCase{Name: "corpus/" + f.Path, Calls: calls})
	}
	for _, p := range pairs {
		var calls []Call
		for _, dt := range []any{nil, "formal", "essay", "promo", "mail"} {
			calls = append(calls, factGapsCall(p.new, dt, false)...)
		}
		calls = append(calls, factGapsCall(p.old, nil, false)...)
		cases = append(cases, VisCase{Name: "gaps/pair/" + p.name, Calls: calls})
	}
	for _, h := range groundHandCases() {
		cases = append(cases, VisCase{Name: "ground/hand/" + h.name, Calls: groundCall(h.final, h.materials, h.original, h.labels, h.raises)})
	}
	cases = append(cases, groundRandomCases()...)
	for _, h := range factGapHandCases() {
		cases = append(cases, VisCase{Name: "gaps/hand/" + h.name, Calls: factGapsCall(h.text, h.docType, h.raises)})
	}
	cases = append(cases, factGapRandomCases()...)
	// 引数を渡さない diff() (check は diff("", "") を節の土台に使う) と extract_numbers の dict
	var diffCalls []Call
	for _, args := range [][2]string{{"", ""}, {"", "a\n"}, {"設定を開く。\n", "設定を開きます。\n"}} {
		d := dcall("diff", args[0], args[1])
		d.Save = DiffGroundSave
		diffCalls = append(diffCalls, d, diffGroundDumps())
	}
	for _, p := range pairs[:min(12, len(pairs))] {
		d := dcall("diff", p.old, p.new)
		d.Save = DiffGroundSave
		diffCalls = append(diffCalls, d, diffGroundDumps())
	}
	cases = append(cases, VisCase{Name: "diff/plain", Calls: diffCalls})
	var numberCalls []Call
	for _, h := range groundHandCases() {
		if !h.raises {
			c := dcall("extract_numbers", h.final)
			c.Keep = true
			numberCalls = append(numberCalls, c)
		}
	}
	numberCalls = append(numberCalls, dcall("extract_numbers", strings.Repeat("1", 4301)+"千件"))
	cases = append(cases, VisCase{Name: "numbers/hand", Calls: numberCalls})
	return cases, nil
}

type groundHand struct {
	name      string
	final     string
	materials []string
	original  any
	labels    any
	raises    bool
}

// groundHandCases は照合の分かれ目を 1 つずつ通す手書きの入力。
func groundHandCases() []groundHand {
	none := any(nil)
	return []groundHand{
		{name: "empty", final: ""},
		{name: "empty-materials", final: "", materials: []string{""}, original: ""},
		{name: "label-mismatch", final: "3日です。", materials: []string{"3日"}, labels: []any{}, raises: true},
		{name: "label-extra", final: "3日です。", materials: nil, labels: []any{"a"}, raises: true},
		{name: "int-limit-final", final: strings.Repeat("1", 4301) + "千件です。", raises: true},
		{name: "int-limit-material", final: "3日です。", materials: []string{strings.Repeat("2", 4301) + "千件"}, raises: true},
		{name: "int-limit-original", final: "3日です。", original: strings.Repeat("2", 4301) + "千件", raises: true},
		{name: "compose-number", final: "再試行は最大3回です。\n保存期間は14日です。\n", materials: []string{"再試行は3回まで。\n"}},
		{name: "compose-weak-one", final: "一つの設定を変えます。もう一つ足します。一つずつ確かめます。\n"},
		{name: "rewrite-number", final: "再試行は最大3回です。\n", original: "再試行は2回です。\n", materials: []string{"再試行は3回。"}, labels: []any{"設計書"}},
		{name: "unit-sec", final: "待ち時間は30秒です。", materials: []string{"timeout は 30s です。"}},
		{name: "unit-sec-word", final: "待ち時間は30sec、間隔は5min、遅れは200ms。", materials: []string{"30秒、5分、200ミリ秒"}},
		{name: "follow-sec", final: "待ち時間は30 secです。間隔は5 minです。", materials: []string{"待ち時間は30秒。間隔は5分。"}},
		{name: "follow-sec-rev", final: "待ち時間は30秒です。", materials: []string{"待ち時間は30 sec。"}},
		{name: "follow-ms", final: "遅れは200 msecです。", materials: []string{"遅れは200ミリ秒"}},
		{name: "squash-nfkc-word", final: "テストを動かします。file を開きます。", materials: []string{"ﾃｽﾄ と ﬁle"}},
		{name: "unit-msec", final: "遅れは200msecです。", materials: []string{"遅れは 200 ms です"}},
		{name: "unit-secs-mins", final: "30secs と 2mins", materials: []string{"30秒と2分"}},
		{name: "unit-s-in-word", final: "3s と 3sample と 3min.", materials: []string{"3sample は 3秒"}},
		{name: "fraction-zero", final: "版は1.0です。値は2.50です。", materials: []string{"版は1、値は2.5"}},
		{name: "fraction-zero-rev", final: "値は1です。", materials: []string{"値は1.00です"}},
		{name: "spaced-unit", final: "容量は16 GBです。期間は3 日です。", materials: []string{"容量は16GB。期間は3日。"}},
		{name: "spaced-unit-rev", final: "容量は16GBです。期間は3日です。", materials: []string{"容量は16 GB。期間は3 日。"}},
		{name: "range", final: "期間は3〜5日です。", materials: []string{"期間は3日から5日です。"}},
		{name: "range-rev", final: "期間は3日から5日です。", materials: []string{"期間は3〜5日です。"}},
		{name: "date", final: "公開は2026-10-13です。", materials: []string{"公開は2026年10月13日です。"}},
		{name: "date-rev", final: "公開は2026年10月13日です。", materials: []string{"公開は2026-10-13です。"}},
		{name: "time", final: "開始は10:30です。", materials: []string{"開始は10時30分です。"}},
		{name: "time-rev", final: "開始は10時30分です。", materials: []string{"開始は10:30。"}},
		{name: "time-sec", final: "記録は10:30:15です。", materials: []string{"記録は10時30分15秒。"}},
		{name: "step-vs-week", final: "手順2で2週間かかります。表3は3%です。", materials: []string{"手順2、表3"}},
		{name: "notation-original", final: "期間は3〜5日です。", original: "期間は3日から5日でした。"},
		{name: "fullwidth", final: "値は１，０００件です。ＡＰＩを使います。", materials: []string{"値は1000件。APIを使う。"}},
		{name: "thousands", final: "上限は1,000件と2,5件。", materials: []string{"1000件、2件、5件"}},
		{name: "kanji", final: "三日で終わります。十二月に出します。", materials: []string{"3日、12月"}},
		{name: "kanji-word-overlap", final: "三日間の作業と十二月号。", materials: []string{"作業"}},
		{name: "names", final: "Java と PostgreSQL と SQL を使います。", materials: []string{"JavaScript と PostgreSQL を使う"}},
		{name: "names-substring", final: "Go1 と v2 と example.com/guide を見ます。", materials: []string{"see example.com/guide/x", "Go1.22"}},
		{name: "names-bold", final: "本番環境で Postgres を使う。", materials: []string{"**本番**環境で **Post**gres"}},
		{name: "names-digit-before", final: "200ms と 8GiB と 3D と 5G と x1y2。", materials: []string{"x1y2"}},
		{name: "names-unicode-digit", final: "①ab と ٣cd と ab", materials: []string{"cd"}},
		{name: "url", final: "詳しくは https://example.com/guide を見ます。ftp://files.example.org/a も。", materials: []string{"example.com/guide"}},
		{name: "url-long-scheme", final: "a" + strings.Repeat(".b", 40) + "://x.example", materials: []string{"x.example"}},
		{name: "html-frontmatter", final: "---\ntitle: Foo Bar\n---\n\n<div class=\"note\">設定 Alpha 3日</div>\n\n本文の Beta と <span>Gamma</span>。\n", materials: []string{"Beta"}},
		{name: "code", final: "```\nretries = 3\nServer Name\n```\n\n`inline 5` と Delta。\n", materials: []string{"Delta"}},
		{name: "certainty", final: "公開は予定です。仕様は確定する。未定義の値。はずれ。はずす。はずです。確定申告。確定です。\n", materials: []string{"未確認です。検討中。調査中。見込み。可能性。かもしれない。確認済み。暫定。未定。"}},
		{name: "certainty-dropped", final: "公開は3日です。", materials: []string{"公開は3日の予定です。たぶん3日。", "おそらく3日。気がする。"}},
		{name: "certainty-kept", final: "公開は3日の予定です。", materials: []string{"公開は3日の予定です。"}},
		{name: "value-certainty-hedged-final", final: "公開は3日かもしれません。", materials: []string{"公開は3日だろう。"}},
		{name: "value-certainty-repeated", final: "3日かもしれない。3日です。", materials: []string{"たしか3日だと思う。\n3日でしょう。\n"}},
		{name: "value-certainty-notation", final: "期間は3〜5日です。", materials: []string{"期間はたぶん3日から5日です。"}},
		{name: "value-certainty-name-only", final: "Foo を使います。", materials: []string{"Foo を使うかもしれない。"}},
		{name: "bare-setting", final: "上限は30です。", materials: []string{"timeout = 30\nretries: 3 # comment\n  - limit = 30.0 ;x\nname=30x\n"}},
		{name: "bare-setting-many", final: "値は7です。", materials: []string{"a=7\nb=7\n", "c: 7\nd: 7\n"}},
		{name: "bare-setting-code", final: "上限は7です。", materials: []string{"```\na = 7\nb = 7\nc = 7\n```\n", "```\nd = 7\n```\n"}},
		{name: "bare-setting-code-two", final: "上限は7です。", materials: []string{"```\na = 7\n```\n", "```\nd = 7\ne: 7\nf = 7\n```\n"}},
		{name: "bare-setting-fullwidth", final: "値は7です。", materials: []string{"ａ＝７\n"}},
		{name: "evidence-many", final: "値は7件です。名前は Alpha です。", materials: []string{"7件\n7件\nAlpha\n", "7件\nAlpha\nAlpha\n"}, labels: []any{"a", none}},
		{name: "evidence-long-line", final: "値は7件です。", materials: []string{strings.Repeat("あ", 70) + "7件" + strings.Repeat("い", 10) + "\r\n"}},
		{name: "not-in-final", final: "値は7件です。", materials: []string{"値は7件、8件、9日、一つ、3.0回、3回\n"}},
		{name: "not-in-final-notation", final: "公開は2026年10月13日です。", materials: []string{"公開は2026-10-13、もう一つ"}},
		{name: "rewrite-from-materials", final: "サーバは東京にあります。費用は5万円です。", original: "サーバがあります。", materials: []string{"サーバは東京。", "費用は5万円。"}, labels: []any{none, "見積"}},
		{name: "compose-words", final: "自動で再試行します。", materials: []string{"再試行"}},
		{name: "same-name-lines", final: "Foo を使う。\nFoo を使う。\nFoo\n", materials: []string{"Bar"}},
		{name: "table-quote", final: "| 項目 | 値 |\n|---|---|\n| 上限 | 30件 |\n\n> 引用の 5日 と Quux\n", materials: []string{"30件"}},
		{name: "heading-list", final: "# 見出し Zed\n\n- 項目 3回\n- 1. Foo\n", materials: []string{"Zed"}},
		{name: "cr", final: "値は7件です。\r\n名前は Alpha。\r\n", materials: []string{"7件\r\nAlpha\r\n"}},
		{name: "surrogate-ish", final: "値は7件😀です。", materials: []string{"7件😀"}},
		{name: "kana-number", final: "ひとつの案とふたつの案。ひとりで。", materials: []string{"2つ"}},
		{name: "thousand-mark", final: "2千件です。", materials: []string{"2000件"}},
		{name: "version-dot", final: "v1.2.3 と 1.2.3 と IPv6", materials: []string{"1.2"}},
		{name: "list-number", final: "1. 最初\n2) 次\n(3) 三つ目\n", materials: []string{"1"}},
		{name: "letter-unit", final: "100Mbps と 30s と 8GiB", materials: []string{"100Mbps、30秒"}},
		{name: "compose-not-weak-number", final: "部品は1つです。", materials: nil},
		{name: "emphasis-unit", final: "待ち時間は30**sec**です。", materials: []string{"待ち時間は30秒。"}},
		{name: "emphasis-unit-rev", final: "待ち時間は30秒です。", materials: []string{"待ち時間は30**sec**。"}},
		{name: "emphasis-unit-ms", final: "遅れは200**ms**です。間隔は5**min**です。", materials: []string{"遅れは200ミリ秒。間隔は5分。"}},
		{name: "bare-setting-code-fullwidth", final: "値は3です。", materials: []string{"```\nＡ＝３\n```\n"}},
		{name: "bare-setting-code-fullwidth-colon", final: "値は7です。", materials: []string{"```\nｂ：７\n```\n"}},
		{name: "bare-setting-code-fraction", final: "上限は30です。", materials: []string{"```\nlimit = 30.0\n```\n"}},
		{name: "bare-setting-code-fraction-two", final: "上限は5です。", materials: []string{"```\nlimit = 5.00\n```\n", "```\nmax: 5.0\n```\n"}},
		{name: "value-certainty-range-unit", final: "期間は3〜5sです。", materials: []string{"期間はたぶん3秒から5秒です。"}},
		{name: "value-certainty-range-unit-rev", final: "期間は3〜5秒です。", materials: []string{"期間はたぶん3sから5sです。"}},
		{name: "range-cluster-unit", final: "3-5sです。", materials: []string{"3から5秒5 分"}},
		{name: "range-cluster-unit-min", final: "3〜5minです。", materials: []string{"3から5分5 秒"}},
	}
}

// groundBank は無作為の組み合わせに使う文 (数値の書き方・単位・名前・語・確度の語を混ぜる)。
var groundBank = []string{
	"再試行は最大3回です。", "保存期間は14日です。", "期間は3〜5日です。", "期間は3日から5日です。", "公開は2026-10-13です。", "公開は2026年10月13日です。",
	"開始は10:30です。", "開始は10時30分です。", "待ち時間は30秒です。", "待ち時間は30sです。", "遅れは200msです。", "遅れは200ミリ秒です。",
	"容量は16 GBです。", "容量は16GBです。", "値は1.0です。", "値は1です。", "値は2.50です。", "上限は1,000件です。", "三日で終わります。",
	"Java を使います。", "JavaScript を使います。", "PostgreSQL を使います。", "SQL を書きます。", "https://example.com/guide を見ます。",
	"公開は予定です。", "仕様は未定です。", "たぶん間に合います。", "おそらく3日です。", "3日かもしれません。", "確認済みです。",
	"timeout = 30", "retries: 3", "一つの設定を変えます。", "もう一つ足します。", "自動で再試行します。", "サーバは東京にあります。",
	"| 上限 | 30件 |", "> 引用の 5日", "# 見出し 3章", "- 項目 3回", "`code 5` と Delta", "<span>Gamma 9</span>",
	"手順2で2週間かかります。", "表3は3%です。", "100Mbps です。", "8GiB です。", "2千件です。", "ひとつの案です。", "第3条を見ます。",
}

func groundPick(r *rand.Rand, n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(groundBank[r.IntN(len(groundBank))])
		if r.IntN(3) == 0 {
			b.WriteString("\n")
		} else if r.IntN(4) == 0 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// groundRandomCases は固定の種 (PCG 1077/1) で文を組み合わせた照合の入力 (新規作成と推敲、資料 0〜3 つ)。
func groundRandomCases() []VisCase {
	r := rand.New(rand.NewPCG(1077, 1))
	var cases []VisCase
	for block := range 30 {
		var calls []Call
		for range 20 {
			final := groundPick(r, 1+r.IntN(5))
			var ms []string
			for range r.IntN(4) {
				ms = append(ms, groundPick(r, 1+r.IntN(4)))
			}
			var original any
			if r.IntN(2) == 0 {
				original = groundPick(r, 1+r.IntN(4))
			}
			var labels any
			if r.IntN(2) == 0 {
				l := make([]any, len(ms))
				for i := range l {
					if r.IntN(2) == 0 {
						l[i] = fmt.Sprintf("出典%d", i)
					}
				}
				labels = l
			}
			calls = append(calls, groundCall(final, ms, original, labels, false)...)
		}
		cases = append(cases, VisCase{Name: fmt.Sprintf("ground/random/%02d", block), Calls: calls})
	}
	return cases
}

type factGapHand struct {
	name    string
	text    string
	docType any
	raises  bool
}

// factGapHandCases は事実の穴の候補の分かれ目を 1 つずつ通す手書きの入力。
func factGapHandCases() []factGapHand {
	many := strings.Repeat("品質が向上しました。\n", 25)
	long := "この変更で" + strings.Repeat("とても", 30) + "大幅に速くなりました。"
	return []factGapHand{
		{name: "empty", text: ""},
		{name: "kinds", text: "土台づくりを進めました。\n信頼性が向上しました。\n処理を大幅に見直しました。\n時間を短縮しました。\n"},
		{name: "order-in-sentence", text: "大幅に改善しました。\n"},
		{name: "negated", text: "大きく変わりません。\n一度に大きく変えず進めます。\n品質が向上することはない。\n劇的にない。\n"},
		{name: "compared", text: "本体より短くしました。\n本体より品質が向上しました。\n"},
		{name: "number", text: "時間を3割短縮しました。\n時間を三割短縮しました。\n時間を短縮しました。\n所要は5分です。\n"},
		{name: "kanji-num", text: "処理を三秒短縮しました。\n容量を百万倍に増やしました。\n速度を五％向上しました。\n"},
		{name: "next-number", text: "時間を短縮しました。所要は5分です。\n時間を短縮しました。\n\n所要は5分です。\n時間を短縮しました。\n\n\n所要は5分です。\n"},
		{name: "hedge", text: "大幅に改善するかもしれません。\n大幅に改善する予定です。\n大幅に改善すれば良い。\n改善しましたなら、\n"},
		{name: "question", text: "大幅に改善しましたか。\n大幅に改善しました？」\n大幅に改善したのか？\n"},
		{name: "vague", text: "一部の画面を大幅に改善しました。\n数社で大幅に改善しました。\n近日、大幅に改善します。\n"},
		{name: "omission", text: "詳細は省略しますが、大幅に改善しました。\n今後も大幅に改善していきます。\n"},
		{name: "assumption", text: "もし大幅に改善したら。\n仮に大幅に改善しました。\n"},
		{name: "certainty", text: "たぶん大幅に改善しました。\nおそらく品質が向上しました。\n"},
		{name: "quote", text: "「大幅に改善しました」と書く。\n『品質が向上しました』を見る。\n大幅に改善しました。\n"},
		{name: "inline-code", text: "`大幅に` 改善しました。\n品質が `向上` しました。\n"},
		{name: "skip-kinds", text: "# 大幅に改善しました\n\n| 品質が向上しました | x |\n|---|---|\n| 大幅に | y |\n\n> 大幅に改善しました。\n\n```\n大幅に改善しました。\n```\n\n---\n\n大幅に改善しました。\n"},
		{name: "frontmatter", text: "---\nnote: 大幅に改善しました。\n---\n\n品質が向上しました。\n"},
		{name: "list", text: "- 大幅に改善しました。\n1. 品質が向上しました。\n"},
		{name: "many", text: many},
		{name: "long", text: long},
		{name: "by-kind-order", text: "時間を短縮しました。\n土台を作りました。\n時間を短縮しました。\n信頼性が向上しました。\n"},
		{name: "figure-variants", text: "礎を築きました。\n基礎を固めました。\n礎石を置きました。\n道を開きました。\n道を開けました。\n鍵を握ります。\n静かに壊れました。\n黙って無視しました。\n"},
		{name: "change-variants", text: "短くしました。\n短くしたのです。\n短縮した。\n短縮する。\n縮小しています。\n延ばしました。\n"},
		{name: "doc-essay", text: "大幅に改善しました。\n", docType: "essay"},
		{name: "doc-promo", text: "大幅に改善しました。\n", docType: "promo"},
		{name: "doc-formal", text: "大幅に改善しました。\n", docType: "formal"},
		{name: "doc-empty", text: "大幅に改善しました。\n", docType: ""},
		{name: "int-limit", text: "大幅に改善しました。" + strings.Repeat("1", 4301) + "千件。\n", raises: true},
		{name: "int-limit-next", text: "大幅に改善しました。\n" + strings.Repeat("1", 4301) + "千件。\n", raises: true},
		{name: "cr", text: "大幅に改善しました。\r\n品質が向上しました。\r\n"},
		{name: "footnote-number", text: "a。[^1]: 短くしました。\n"},
		{name: "footnote-number-change", text: "[^2]: 時間を短縮しました。\n本文です。[^2]\n"},
		{name: "second-match", text: "大幅に変えず桁違い。\n"},
		{name: "second-match-other", text: "大きく変えず格段に速くなりました。\n"},
	}
}

// factGapBank は無作為の組み合わせに使う文。
var factGapBank = []string{
	"大幅に改善しました。", "品質が向上しました。", "土台づくりを進めました。", "時間を短縮しました。", "所要は5分です。", "三割減りました。",
	"大きく変わりません。", "本体より短くしました。", "かもしれません。", "一部の画面です。", "今後も続けます。", "もし必要なら。",
	"「大幅に改善しました」", "`大幅`", "# 見出し", "> 引用です。", "| 表 | 値 |", "たぶんそうです。", "圧倒的な性能です。", "シームレスに動きます。",
	"混乱を最小限に抑えました。", "改善しましたか？", "最適化しました。", "ばらつきを減らしました。", "格段に速くなりました。",
}

// factGapRandomCases は固定の種 (PCG 1077/2) で文を組み合わせた事実の穴の候補の入力。
func factGapRandomCases() []VisCase {
	r := rand.New(rand.NewPCG(1077, 2))
	var cases []VisCase
	for block := range 10 {
		var calls []Call
		for range 20 {
			var b strings.Builder
			for range 1 + r.IntN(8) {
				b.WriteString(factGapBank[r.IntN(len(factGapBank))])
				switch r.IntN(3) {
				case 0:
					b.WriteString("\n")
				case 1:
					b.WriteString("\n\n")
				}
			}
			var dt any
			if r.IntN(4) == 0 {
				dt = []any{"formal", "essay", "mail"}[r.IntN(3)]
			}
			calls = append(calls, factGapsCall(b.String(), dt, false)...)
		}
		cases = append(cases, VisCase{Name: fmt.Sprintf("gaps/random/%02d", block), Calls: calls})
	}
	return cases
}
