package fake

import (
	"encoding/json"
	"time"

	"pro-con/card"
)

// ライブアップグレード (upgrade package) で模擬の状態を新しいプロセスへ引き継ぐ。本番の backend は状態をファイルに
// 持つので要らないが、模擬はメモリに持つので、入れ替えるたびに最初からになる (本番と同じ「入れ替えても続く」をハリボテで見せる)。

type scriptState struct {
	Lines    []string        `json:"lines"`
	Then     string          `json:"then"`
	Question string          `json:"question"`
	Choices  []card.Question `json:"choices,omitempty"` // 選択肢つきの質問 (issue 493)
	Exec     card.Exec       `json:"exec"`
	ExecDone bool            `json:"execDone"`
}

type simState struct {
	Now           time.Time              `json:"now"`
	Cards         []card.Card            `json:"cards"`
	Limit         int                    `json:"limit"`
	UsageOff      bool                   `json:"usage_off,omitempty"`
	Review        string                 `json:"review,omitempty"`
	Scripts       map[string]scriptState `json:"scripts"`
	Resource      map[string][]string    `json:"resource"`
	ExternalUntil map[string]time.Time   `json:"externalUntil"`
	NextID        int                    `json:"nextID"`
	NextIssue     int                    `json:"nextIssue"`
	NextSess      int                    `json:"nextSess"`
	// EventsSent は引き継がない (Save で書かず、Restore で偽のまま)。入れ替えた後の画面はログのタブを空から読み直すので、
	// 新しい Sim は模擬の出来事をもう 1 度返す (真で引き継ぐと、入れ替えた後のログのタブが空になる。issue 512)。欄の数を Sim と揃えるために置く
	EventsSent bool `json:"-"`
}

// Save は模擬の状態を書き出す。
func (s *Sim) Save() ([]byte, error) {
	st := simState{Now: s.now, Cards: s.cards, Limit: s.limit, UsageOff: s.usageOff, Review: s.review, Scripts: map[string]scriptState{}, Resource: s.resource,
		ExternalUntil: s.externalUntil, NextID: s.nextID, NextIssue: s.nextIssue, NextSess: s.nextSess}
	for id, sc := range s.scripts {
		st.Scripts[id] = scriptState{Lines: sc.lines, Then: sc.then, Question: sc.question, Choices: sc.choices, Exec: sc.exec, ExecDone: sc.execDone}
	}
	return json.Marshal(st)
}

// Restore は Save した状態に置き換える。読めなければ何も変えずにエラーを返す (呼び出し側は見本の最初から続ける)。
func (s *Sim) Restore(data []byte) error {
	var st simState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	scripts := make(map[string]*script, len(st.Scripts))
	for id, sc := range st.Scripts {
		scripts[id] = &script{lines: sc.Lines, then: sc.Then, question: sc.Question, choices: sc.Choices, exec: sc.Exec, execDone: sc.ExecDone}
	}
	*s = Sim{now: st.Now, cards: st.Cards, limit: st.Limit, usageOff: st.UsageOff, review: st.Review, scripts: scripts, resource: st.Resource,
		externalUntil: st.ExternalUntil, nextID: st.NextID, nextIssue: st.NextIssue, nextSess: st.NextSess}
	if s.resource == nil {
		s.resource = map[string][]string{}
	}
	if s.externalUntil == nil {
		s.externalUntil = map[string]time.Time{}
	}
	return nil
}
