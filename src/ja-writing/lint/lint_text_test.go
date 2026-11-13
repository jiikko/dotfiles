package lint

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"ja-writing/visibility"
)

// 群の状態の欄は 20 個あり、どれも別の型の別の欄 (群を入れる人が自分の欄だけを書き足せる)。
func TestGroupStatesAreSeparateFields(t *testing.T) {
	s := &lintState{}
	states := s.groupStates()
	seenTypes := map[reflect.Type]bool{}
	seenPointers := map[any]bool{}
	for i, st := range states {
		typ := reflect.TypeOf(st).Elem()
		if seenTypes[typ] || seenPointers[st] {
			t.Errorf("G%d の状態が別の群の欄と重なっている", i+1)
		}
		seenTypes[typ], seenPointers[st] = true, true
	}
	if len(seenTypes) != 20 {
		t.Errorf("状態の型が %d 種類 (20 のはず)", len(seenTypes))
	}
}

func TestAddKeepsDetectionOrderAndFindingsOfFilters(t *testing.T) {
	s := &lintState{}
	s.add(newFinding("trailing_colon", 3, "warn", "", ""))
	s.add(newFinding("heading_colon", 1, "warn", "", ""))
	s.add(newFinding("trailing_colon", 2, "warn", "", ""))
	var lines []int
	for _, f := range s.findingsOf("trailing_colon") {
		lines = append(lines, f.Line)
	}
	if !slices.Equal(lines, []int{3, 2}) || len(s.findings) != 3 {
		t.Errorf("検出の順を保って規則で絞れない: %v (全 %d 件)", lines, len(s.findings))
	}
	essay := "essay"
	if (lintContext{docType: &essay}).docTypeName() != "essay" || (lintContext{}).docTypeName() != "" {
		t.Error("docTypeName: 種類なしは空、指定すればその名前")
	}
}

// 文書の指標 (本家の analyze_markdown_metrics): 字が 0 のとき比率は int の 0 (--json の出力が 0 で、0.0 でない)。1 字以上なら float。
func TestMetricsZeroIsIntAndNonZeroIsFloat(t *testing.T) {
	empty, err := lintText("", lintContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := dumpResult(empty)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\"list_ratio\": 0,\n    \"bold_count\": 0,\n    \"bold_per_1000\": 0\n"; !strings.Contains(out, want) {
		t.Errorf("空の入力の metrics が int の 0 でない:\n%s", out)
	}
	text, err := lintText("あ\n", lintContext{})
	if err != nil {
		t.Fatal(err)
	}
	if out, _ = dumpResult(text); !strings.Contains(out, "\"list_ratio\": 0.0,") || !strings.Contains(out, "\"bold_per_1000\": 0.0") {
		t.Errorf("1 字の入力の metrics が float でない:\n%s", out)
	}
}

// 行のループの入口 (本家の 5656-5676): 前の行の文 (previousSentence) は、続く行にだけ渡し、コードの囲み・区切り線の行で消す。見える文字が無い行は検査しない (nil)。
// 比喩の「効く」の窓は、直近の 3 行 (この行を除く)。
func TestBeginRowTracksPreviousSentenceAndKikuWindow(t *testing.T) {
	text := "一行目です。\n\n二行目です。\n\n```\ncode\n```\n三行目です。\n\n***\n\n四行目です。\n五行目です。\n六行目です。\n"
	an, err := visibility.AnalyzeMarkdown(text, true)
	if err != nil {
		t.Fatal(err)
	}
	s := &lintState{an: an, doc: newDocument(an)}
	type seen struct{ line, before, window string }
	var got []seen
	for _, row := range an.Lines {
		if r := s.beginRow(row); r != nil {
			got = append(got, seen{r.plainText, r.sentenceBefore, strings.Join(r.kikuBefore, "/")})
		}
	}
	// 区切り線 (***) は見える文字が残るので検査する行になる。太字の記号を除いた文は空で、窓にも空の行として入る。
	want := []seen{
		{"一行目です。", "", ""},
		{"二行目です。", "一行目です。", "一行目です。"},
		{"三行目です。", "", "一行目です。/二行目です。"},  // コードの囲みで前の文を消す。窓は消さない
		{"", "", "一行目です。/二行目です。/三行目です。"}, // 区切り線で消す
		{"四行目です。", "", "二行目です。/三行目です。/"},
		{"五行目です。", "四行目です。", "三行目です。//四行目です。"},
		{"六行目です。", "五行目です。", "/四行目です。/五行目です。"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("\n got  %v\n want %v", got, want)
	}
}

// 本家の行のループの順 (表と見出しと引用でない行): 文末のコロン → 語彙 → 直訳 → フィラー → 文の型 → 指示の残り → 短い文 → 否定の対比 → 読点。
func TestBodyStepsFollowTheOriginalOrder(t *testing.T) {
	var names []string
	for _, step := range bodySteps {
		names = append(names, step.name)
	}
	want := []string{"g07TrailingColon", "g08Vocabulary", "g08Translated", "g09Filler", "g09InfoSentences", "g10InstructionResidue", "g11ShortSentences", "g12Negation", "g13PauseComma"}
	if !slices.Equal(names, want) {
		t.Errorf("本家の順と違う: %v", names)
	}
}

func TestG08WindowKeepsTheLastThreeLines(t *testing.T) {
	s := &lintState{}
	var windows []string
	for _, line := range []string{"a", "b", "c", "d", "e"} {
		windows = append(windows, strings.Join(s.g08Window(line), ""))
	}
	if want := []string{"", "a", "ab", "abc", "bcd"}; !slices.Equal(windows, want) {
		t.Errorf("窓 %v、期待 %v", windows, want)
	}
}

// <details> のタグ名は ASCII の字だけの綴りが details のとき真 (re.I は ſ・ı も当てるが、str.lower() はそれを ASCII にしない)。
func TestIsDetailsName(t *testing.T) {
	for name, want := range map[string]bool{"details": true, "DETAILS": true, "Details": true, "script": false, "deta" + string(rune(0x131)) + "ls": false, "detail" + string(rune(0x17f)): false} {
		if got := isDetailsName(name); got != want {
			t.Errorf("isDetailsName(%q) = %v", name, got)
		}
	}
}
