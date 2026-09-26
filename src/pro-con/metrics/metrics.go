// Package metrics はカードごとの所要の記録 (issue 516)。閉じたカード 1 枚を、あとで比べられる数の 1 行 (Row) にする。
//
// 行は状態の置き場の metrics.jsonl に溜める (読み書きは store/metrics.go。書き手は dispatcher だけ)。カードの記録・書庫は
// 完了から 1 週間で消える (497) ので、振り返りはこの行だけで足りるようにする (題名・ポイント・時刻・列ごとの時間・回数・枠)。
//
// 列ごとの時間はカードの足跡 (card.Trail) から、回数は履歴の文から出す (新しい時計・数えるための欄を足さない)。
// 足跡はこの issue で足したので、それより前に作ったカードの行には列ごとの時間が無い (Stay / Human が nil。足跡が依頼の列から
// 始まっていないカード = 入れた時点で動いていたカードも同じ)。
package metrics

import (
	"strings"
	"time"

	"pro-con/card"
)

// EndDeleted は削除したカードの終わり方 (card.Ending には無い。削除は記録から外すので)。
const EndDeleted = "削除"

// EndIssue は issue を持ち、終わり方の無いカードの終わり方。
const EndIssue = "issue で完了"

// Row は閉じたカード 1 枚の所要。時刻は閉じるまでに一度も通らなかったらゼロ (JSON では省く)。
type Row struct {
	Card   string   `json:"card"`
	Title  string   `json:"title"`
	Repo   string   `json:"repo,omitempty"`
	Issues []string `json:"issues,omitempty"`
	Points int      `json:"points,omitempty"` // 見積もりのポイント (0 = 見積もり無し)
	Ending string   `json:"ending"`           // 終わり方 (card.Ending の名前 / EndIssue / EndDeleted)

	RequestedAt time.Time `json:"requestedAt"`         // 依頼を受けた
	PlannedAt   time.Time `json:"plannedAt,omitzero"`  // 最初に分けた (着手待ちへ)
	StartedAt   time.Time `json:"startedAt,omitzero"`  // 最初に PG が起きた (作業中へ)
	ReviewAt    time.Time `json:"reviewAt,omitzero"`   // 最初にレビューに来た
	ClosedAt    time.Time `json:"closedAt"`            // 閉じた (完了にした・削除した)
	RecordedAt  time.Time `json:"recordedAt,omitzero"` // この行を書いた (閉じた時刻と離れていれば埋め戻した行)

	// Stay は列ごとに居た秒数、Human はそのうち人の番だった秒数。足跡の無いカードは nil (0 秒と区別する)
	Stay  *Stays `json:"staySec,omitempty"`
	Human *Stays `json:"humanSec,omitempty"`

	Counts Counts `json:"counts"`

	// Usage は PG の枠。取れなかったら nil で、UsageMissing に理由を書く (0 と取り違えない)
	Usage        *Usage `json:"usage,omitempty"`
	UsageMissing string `json:"usageMissing,omitempty"`
}

// Stays は列ごとの秒数 (完了の列は数えない)。
type Stays struct {
	Requested int64 `json:"requested"`
	Planned   int64 `json:"planned"`
	Running   int64 `json:"running"`
	Waiting   int64 `json:"waiting"`
	Review    int64 `json:"review"`
}

// Total は列の秒数の和。
func (s Stays) Total() int64 { return s.Requested + s.Planned + s.Running + s.Waiting + s.Review }

func (s *Stays) add(st card.State, sec int64) {
	switch st {
	case card.Requested:
		s.Requested += sec
	case card.Planned:
		s.Planned += sec
	case card.Running:
		s.Running += sec
	case card.Waiting:
		s.Waiting += sec
	case card.Review:
		s.Review += sec
	case card.Done:
	}
}

// Counts は履歴から数えた回数。
type Counts struct {
	Resumes         int `json:"resumes"`         // PG の再開
	Reworks         int `json:"reworks"`         // 差し戻し
	Questions       int `json:"questions"`       // PG の質問
	AnsweredByPM    int `json:"answeredByPM"`    // うち PM が答えた
	AnsweredByHuman int `json:"answeredByHuman"` // うち人が答えた
	PMQuestions     int `json:"pmQuestions"`     // PM が依頼について人に聞いた
	Runs            int `json:"runs"`            // テストの係に頼んだ
}

// FromCard は閉じたカード c の行を作る。closed は閉じた時刻 (完了のカードは完了にした時刻 = Since、削除は外した時刻)、
// ending は終わり方 (空なら c から決める)。roles は人の番の数え方 (役を起こさない設定。閉じた時点のものを使う)。
// 枠 (Usage / UsageMissing) は呼ぶ側が埋める。
func FromCard(c card.Card, closed time.Time, ending string, roles card.Roles) Row {
	r := Row{Card: c.ID, Title: c.Title, Repo: c.Repo, Points: c.Points, Ending: ending, ClosedAt: closed, RequestedAt: c.Since}
	for _, i := range c.Issues {
		r.Issues = append(r.Issues, i.String())
	}
	if r.Ending == "" {
		r.Ending = c.Ending.Label()
		if c.Ending == card.EndNone {
			r.Ending = EndIssue
		}
	}
	if len(c.History) > 0 {
		r.RequestedAt = c.History[0].At
	}
	// 足跡が依頼の列から始まっていなければ使わない (この issue を入れた時点で動いていたカードは途中から始まる。途中からの時間を
	// 全部の時間として出さない)
	if len(c.Trail) > 0 && c.Trail[0].State == card.Requested {
		r.RequestedAt = c.Trail[0].At
		r.Stay, r.Human = &Stays{}, &Stays{}
		first := map[card.State]*time.Time{card.Planned: &r.PlannedAt, card.Running: &r.StartedAt, card.Review: &r.ReviewAt}
		for i, s := range c.Trail {
			end := closed
			if i+1 < len(c.Trail) {
				end = c.Trail[i+1].At
			}
			sec := max(int64(end.Sub(s.At)/time.Second), 0)
			r.Stay.add(s.State, sec)
			if s.HumanUnder(roles) {
				r.Human.add(s.State, sec)
			}
			if p := first[s.State]; p != nil && p.IsZero() {
				*p = s.At
			}
		}
	}
	r.Counts = countHistory(c.History)
	return r
}

// countHistory は履歴の文から回数を数える (書く側と同じ文の定数で。card/trail.go)。
func countHistory(h []card.Event) Counts {
	var n Counts
	for _, e := range h {
		t := e.Text
		switch {
		case strings.HasPrefix(t, card.ResumedPrefix):
			n.Resumes++
		case strings.HasPrefix(t, card.ReworkedPrefix):
			n.Reworks++
		case strings.HasPrefix(t, card.AskedPrefix):
			n.Questions++
		case strings.HasPrefix(t, card.PMAskedPrefix):
			n.PMQuestions++
		case strings.HasPrefix(t, card.RunAskedPrefix):
			n.Runs++
		default:
			// 「<答えた者> が回答した: 」。答えた者は空白を含まない 1 語 (回答の本文や attach の指示の中の同じ文を数えない)
			if i := strings.Index(t, card.AnsweredMark); i > 0 && !strings.ContainsAny(t[:i], " \n") {
				if t[:i] == card.PMName {
					n.AnsweredByPM++
				} else {
					n.AnsweredByHuman++
				}
			}
		}
	}
	return n
}
