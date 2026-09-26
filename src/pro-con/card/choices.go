package card

import (
	"errors"
	"fmt"
	"strings"

	"termsafe"
)

// 選択肢つきの質問 (issue 493)。PG が `card ask --json` で渡し、画面の `r` が radio / checkbox の回答フォームにする。
// 形は Claude Code の AskUserQuestion の入力にそろえる (PG に教える形が 1 つで済む)。推奨 (Recommended) だけが足した項目。
// 答えは自由文 (FormatAnswer) にして今の `card answer` で届ける: PG・PM・dispatcher は構造を知らなくてよい。

// Question は 1 つの問い。
type Question struct {
	Question    string   `json:"question"`
	Header      string   `json:"header,omitempty"` // 短い見出し (答えの文で問いを指す名前。空なら Question)
	Options     []Option `json:"options"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
}

// Option は選択肢の 1 つ。
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"` // 推奨。回答フォームで初めから選んでおく
}

// 問いと選択肢の数・長さの上限 (選択肢の文は PG が書くので untrusted。画面の枠に収まらない量を記録に入れない)。
const (
	MaxQuestions   = 4
	MinOptions     = 2
	MaxOptions     = 4
	maxQuestionLen = 300
	maxHeaderLen   = 30
	maxLabelLen    = 80
	maxDescLen     = 300
)

// NormalizeQuestions は PG が書いた問いの制御文字を落として前後の空白を除き、数・長さ・推奨の数を検査する。
// 記録に入れる前 (store の ask) と、箱に置く前 (cardcmd) の両方で呼ぶ。
func NormalizeQuestions(qs []Question) ([]Question, error) {
	if len(qs) == 0 || len(qs) > MaxQuestions {
		return nil, fmt.Errorf("問いは 1〜%d 個 (受け取ったのは %d 個)", MaxQuestions, len(qs))
	}
	out := make([]Question, len(qs))
	for i, q := range qs {
		n := i + 1
		q.Question, q.Header = clean(q.Question), clean(q.Header)
		switch {
		case q.Question == "":
			return nil, fmt.Errorf("問 %d の question が空", n)
		case runeLen(q.Question) > maxQuestionLen:
			return nil, fmt.Errorf("問 %d の question が長い (%d 字まで)", n, maxQuestionLen)
		case runeLen(q.Header) > maxHeaderLen:
			return nil, fmt.Errorf("問 %d の header が長い (%d 字まで)", n, maxHeaderLen)
		case len(q.Options) < MinOptions || len(q.Options) > MaxOptions:
			return nil, fmt.Errorf("問 %d の選択肢は %d〜%d 個 (受け取ったのは %d 個。「その他」は画面が足すので入れない)",
				n, MinOptions, MaxOptions, len(q.Options))
		}
		opts := make([]Option, len(q.Options))
		rec := 0
		for j, o := range q.Options {
			o.Label, o.Description = clean(o.Label), clean(o.Description)
			switch {
			case o.Label == "":
				return nil, fmt.Errorf("問 %d の選択肢 %d の label が空", n, j+1)
			case runeLen(o.Label) > maxLabelLen:
				return nil, fmt.Errorf("問 %d の選択肢 %d の label が長い (%d 字まで)", n, j+1, maxLabelLen)
			case runeLen(o.Description) > maxDescLen:
				return nil, fmt.Errorf("問 %d の選択肢 %d の description が長い (%d 字まで)", n, j+1, maxDescLen)
			}
			if o.Recommended {
				rec++
			}
			opts[j] = o
		}
		if !q.MultiSelect && rec > 1 {
			return nil, fmt.Errorf("問 %d は 1 つ選ぶ問いなので、推奨は 1 つまで (%d 個ある)", n, rec)
		}
		q.Options = opts
		out[i] = q
	}
	return out, nil
}

// clean は 1 行に潰す (改行・タブ・エスケープを落とす。フォームの 1 行に描くので)。
func clean(s string) string {
	return strings.Join(strings.Fields(termsafe.PlainLine(strings.ReplaceAll(s, "\n", " "))), " ")
}

func runeLen(s string) int { return len([]rune(s)) }

// Name は答えの文で問いを指す名前 (見出しが無ければ問いの文)。
func (q Question) Name() string {
	if q.Header != "" {
		return q.Header
	}
	return q.Question
}

// Kind は問いの選び方の説明 (フォームと質問の文に出す)。
func (q Question) Kind() string {
	if q.MultiSelect {
		return "複数選べる"
	}
	return "1 つ選ぶ"
}

// QuestionsText は問いを読むための自由文にする。Wait.Question に前置きと並べて入れる
// (card show・詳細・通知・PM への指示など、質問の文を読む口がそのまま選択肢を読める)。
func QuestionsText(qs []Question) string {
	var b strings.Builder
	for i, q := range qs {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%d. %s (%s)", i+1, q.Question, q.Kind())
		for _, o := range q.Options {
			b.WriteString("\n   - " + o.Label)
			if o.Recommended {
				b.WriteString(" (推奨)")
			}
			if o.Description != "" {
				b.WriteString(": " + o.Description)
			}
		}
	}
	return b.String()
}

// AskWait は質問待ちを作る。選択肢つきなら問いを検査し、前置きの後に問いを文にして並べる (Question を読む口が選択肢も読める)。
// 選択肢が無ければ前置きがそのまま質問の文。どちらも空なら誤り。
func AskWait(question string, qs []Question) (Wait, error) {
	q := strings.TrimSpace(question)
	if len(qs) == 0 {
		if q == "" {
			return Wait{}, errors.New("質問が空")
		}
		return Wait{Kind: WaitQuestion, Question: question}, nil
	}
	qs, err := NormalizeQuestions(qs)
	if err != nil {
		return Wait{}, err
	}
	text := QuestionsText(qs)
	if q != "" {
		text = q + "\n\n" + text
	}
	return Wait{Kind: WaitQuestion, Question: text, Questions: qs}, nil
}

// Pick は 1 つの問いへの答え。Chosen は選んだ選択肢の添字 (radio なら 0〜1 個)。Other は「その他」に書いた文 (空なら選んでいない)。
type Pick struct {
	Chosen []int
	Other  string
}

// ErrUnanswered は答えていない問いがある (フォームは送らずに開いたままにする)。
var ErrUnanswered = errors.New("答えていない問いがある")

// FormatAnswer はフォームの答えを `card answer` の自由文にする。問いごとに「N. 名前: 選んだもの」を 1 行、補足があれば最後に 1 行。
// 1 文字の答えがどの問いへのものか取り違えないよう、番号と名前と選んだ選択肢の名前を書く。
func FormatAnswer(qs []Question, picks []Pick, note string) (string, error) {
	if len(picks) != len(qs) {
		return "", fmt.Errorf("答えの数 %d が問いの数 %d と違う", len(picks), len(qs))
	}
	lines := make([]string, 0, len(qs)+1)
	for i, q := range qs {
		var got []string
		for _, j := range picks[i].Chosen {
			if j < 0 || j >= len(q.Options) {
				return "", fmt.Errorf("問 %d の選択肢 %d は無い", i+1, j+1)
			}
			got = append(got, q.Options[j].Label)
		}
		if o := strings.TrimSpace(picks[i].Other); o != "" {
			got = append(got, "その他: "+o)
		}
		if len(got) == 0 {
			return "", fmt.Errorf("%w: 問 %d (%s)", ErrUnanswered, i+1, q.Name())
		}
		if !q.MultiSelect && len(got) > 1 {
			return "", fmt.Errorf("問 %d は 1 つ選ぶ問い (%d 個選んでいる)", i+1, len(got))
		}
		lines = append(lines, fmt.Sprintf("%d. %s: %s", i+1, q.Name(), strings.Join(got, " / ")))
	}
	if n := strings.TrimSpace(note); n != "" {
		lines = append(lines, "補足: "+n)
	}
	return strings.Join(lines, "\n"), nil
}
