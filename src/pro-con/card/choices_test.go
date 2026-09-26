package card

import (
	"errors"
	"strings"
	"testing"
)

func twoOpts() []Option { return []Option{{Label: "A"}, {Label: "B"}} }

// PG が書いた問いは untrusted: 数・長さ・推奨の数を検査し、制御文字と改行を落として 1 行にする。
func TestNormalizeQuestionsChecksShapeAndCleans(t *testing.T) {
	bad := map[string][]Question{
		"問いが 0 個":         nil,
		"問いが 5 個":         {{Question: "1", Options: twoOpts()}, {Question: "2", Options: twoOpts()}, {Question: "3", Options: twoOpts()}, {Question: "4", Options: twoOpts()}, {Question: "5", Options: twoOpts()}},
		"選択肢が 1 個":        {{Question: "q", Options: []Option{{Label: "A"}}}},
		"選択肢が 5 個":        {{Question: "q", Options: []Option{{Label: "1"}, {Label: "2"}, {Label: "3"}, {Label: "4"}, {Label: "5"}}}},
		"問いの文が空":          {{Question: " \x1b[31m ", Options: twoOpts()}},
		"label が空":        {{Question: "q", Options: []Option{{Label: "A"}, {Label: "\t"}}}},
		"1 つ選ぶ問いに推奨が 2 つ": {{Question: "q", Options: []Option{{Label: "A", Recommended: true}, {Label: "B", Recommended: true}}}},
		"label が長い":       {{Question: "q", Options: []Option{{Label: "A"}, {Label: strings.Repeat("あ", maxLabelLen+1)}}}},
	}
	for name, qs := range bad {
		if _, err := NormalizeQuestions(qs); err == nil {
			t.Errorf("%s を通した", name)
		}
	}
	got, err := NormalizeQuestions([]Question{{Question: " 色は\n\x1b[31m赤\x1b[0m? ", Header: "色", MultiSelect: true,
		Options: []Option{{Label: "A", Recommended: true}, {Label: "B\x07", Description: "説明\r\nの続き", Recommended: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	if q := got[0]; q.Question != "色は 赤?" || q.Options[1].Label != "B" || q.Options[1].Description != "説明 の続き" {
		t.Fatalf("制御文字・改行が残っている: %#v", q)
	}
}

// 質問待ちの文には、前置きの後に問いと選択肢 (推奨の印つき) が並ぶ (card show・通知・PM が選択肢を読める)。
func TestAskWaitPutsChoicesIntoQuestionText(t *testing.T) {
	w, err := AskWait("2 点決めてください", []Question{{Question: "形", Options: []Option{{Label: "丸", Recommended: true}, {Label: "角", Description: "四角"}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "2 点決めてください\n\n1. 形 (1 つ選ぶ)\n   - 丸 (推奨)\n   - 角: 四角"
	if w.Kind != WaitQuestion || w.Question != want || len(w.Questions) != 1 {
		t.Fatalf("質問待ちが違う:\n%q\n%q", w.Question, want)
	}
	if _, err := AskWait(" ", nil); err == nil {
		t.Fatal("空の質問を通した")
	}
	if w, err := AskWait("自由文だけ", nil); err != nil || w.Question != "自由文だけ" || w.Questions != nil {
		t.Fatalf("自由文だけの質問が変わった: %#v %v", w, err)
	}
}

// 答えは問いごとに「番号. 名前: 選んだもの」の 1 行。答えていない問いは ErrUnanswered で止める。
func TestFormatAnswer(t *testing.T) {
	qs := []Question{
		{Question: "形", Header: "形", Options: []Option{{Label: "丸"}, {Label: "角"}}},
		{Question: "直すもの", MultiSelect: true, Options: []Option{{Label: "色"}, {Label: "幅"}, {Label: "余白"}}},
	}
	got, err := FormatAnswer(qs, []Pick{{Chosen: []int{1}}, {Chosen: []int{0, 2}, Other: " 影 "}}, " 急がない ")
	if want := "1. 形: 角\n2. 直すもの: 色 / 余白 / その他: 影\n補足: 急がない"; err != nil || got != want {
		t.Fatalf("答えの文が違う:\n%q\n%q (%v)", got, want, err)
	}
	if _, err := FormatAnswer(qs, []Pick{{Chosen: []int{0}}, {Other: "  "}}, ""); !errors.Is(err, ErrUnanswered) {
		t.Fatalf("答えていない問いを通した: %v", err)
	}
	if _, err := FormatAnswer(qs, []Pick{{Chosen: []int{0}, Other: "x"}, {Chosen: []int{0}}}, ""); err == nil {
		t.Fatal("1 つ選ぶ問いで 2 つ選んだ答えを通した")
	}
}
