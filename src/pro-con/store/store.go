// Package store は本物のモードのカードの記録 (issue 427 の段階 3a)。
//
// 書き手は daemon だけ (issue 426 の決定 1)。PM / PG / 画面は受付の箱 (inbox) に依頼を 1 件 1 ファイルで置き (Submit)、
// daemon が Apply でまとめて記録 (cards.json) へ適用する。採番と、状態の遷移の規則・不変条件 (card.Check) の検査も Apply の中の 1 か所。
//
// 失敗モード:
//   - 書きかけの依頼が箱に見える → Submit は一時ファイルに書いてから rename する (箱には完成したファイルしか現れない)
//   - 記録を書いた後、箱のファイルを消す前に落ちる → 次の Apply で同じ依頼を二重に適用しないよう、適用済みの依頼の ID を記録に控える
//   - 規則に反する依頼・壊れた依頼 → 記録に入れず、理由つきで inbox/rejected/ へ除ける (黙って捨てない)
//
// 🚨 Apply を 2 つの daemon から同時に呼ばない (記録の書き込みが後勝ちになる)。daemon の排他は 3c で入れる。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pro-con/card"
)

const (
	StateFile   = "cards.json"
	InboxDir    = "inbox"
	RejectedDir = "rejected" // inbox の下
	// keepApplied は控えておく適用済みの依頼の ID の数 (落ちてから再開するまでに箱に残りうる数より十分大きい)。
	keepApplied = 1000
)

// Request は受付の箱に置く依頼 1 件。Kind ごとに使う欄が違う (apply を参照)。
type Request struct {
	ID       string          `json:"id"` // 箱のファイル名から Submit が付ける。二重適用を防ぐ鍵
	Kind     string          `json:"kind"`
	CardID   string          `json:"cardId,omitempty"`
	Title    string          `json:"title,omitempty"`
	Request  string          `json:"request,omitempty"` // 依頼の原文 (人間が書いたまま)
	Prompt   string          `json:"prompt,omitempty"`  // PM に渡した指示の全文
	Repo     string          `json:"repo,omitempty"`
	Owner    string          `json:"owner,omitempty"`
	Issues   []card.IssueRef `json:"issues,omitempty"`
	Question string          `json:"question,omitempty"`
	Answer   string          `json:"answer,omitempty"`
	From     string          `json:"from,omitempty"` // 回答した人 (人間 / PM)
	Ending   card.Ending     `json:"ending,omitempty"`
	At       time.Time       `json:"at"`
}

// State は記録の中身。
type State struct {
	NextID  int         `json:"nextId"`
	Cards   []card.Card `json:"cards"`
	Applied []string    `json:"applied"` // 適用済み (と除けた) 依頼の ID。古い順、最大 keepApplied
}

// Result は依頼 1 件の適用の結果。Err が空なら適用した。
type Result struct {
	ID, Kind, CardID string
	Err              string
}

// Submit は依頼を受付の箱に置き、依頼の ID を返す。どのプロセスから呼んでもよい。
func Submit(dir string, r Request) (string, error) {
	box := filepath.Join(dir, InboxDir)
	if err := os.MkdirAll(box, 0o700); err != nil {
		return "", err
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	if r.At.IsZero() {
		r.At = time.Now()
	}
	// 名前の順 = 置いた順 (時刻を先頭に固定幅で)。同じ時刻でも乱数で衝突しない
	r.ID = fmt.Sprintf("%020d-%s", r.At.UnixNano(), hex.EncodeToString(b[:]))
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(filepath.Join(box, r.ID+".json"), data); err != nil {
		return "", err
	}
	return r.ID, nil
}

// Load は記録を読む。無ければ空の記録。壊れていたらエラー (空と区別する)。
func Load(dir string) (State, error) {
	data, err := os.ReadFile(filepath.Join(dir, StateFile))
	if errors.Is(err, os.ErrNotExist) {
		return State{NextID: 1}, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("カードの記録 (%s) を読めない: %w", filepath.Join(dir, StateFile), err)
	}
	if st.NextID < 1 {
		st.NextID = 1
	}
	return st, nil
}

// Apply は受付の箱の依頼を置いた順に記録へ適用する (daemon だけが呼ぶ)。記録を書いてから箱のファイルを片付ける。
func Apply(dir string, now time.Time) ([]Result, error) {
	st, err := Load(dir)
	if err != nil {
		return nil, err
	}
	box := filepath.Join(dir, InboxDir)
	names, err := filepath.Glob(filepath.Join(box, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	applied := map[string]bool{}
	for _, id := range st.Applied {
		applied[id] = true
	}
	var results []Result
	var done, rejected []string // 片付ける箱のファイル / 除ける箱のファイル
	for _, name := range names {
		id := strings.TrimSuffix(filepath.Base(name), ".json")
		if applied[id] { // 前の Apply で記録には入ったが、ファイルを片付ける前に落ちた
			done = append(done, name)
			continue
		}
		res := Result{ID: id}
		data, err := os.ReadFile(name)
		var r Request
		if err == nil {
			err = json.Unmarshal(data, &r)
		}
		if err == nil {
			r.ID = id // ファイル名が正本 (中身の id は信じない)
			res.Kind, res.CardID = r.Kind, r.CardID
			var next State
			if next, res.CardID, err = apply(st, r, now); err == nil {
				st = next
			}
		}
		if err != nil {
			res.Err = err.Error()
			rejected = append(rejected, name)
		} else {
			done = append(done, name)
		}
		st.Applied = append(st.Applied, id)
		results = append(results, res)
	}
	if len(st.Applied) > keepApplied {
		st.Applied = st.Applied[len(st.Applied)-keepApplied:]
	}
	if len(results) > 0 {
		data, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := writeAtomic(filepath.Join(dir, StateFile), data); err != nil {
			return nil, err
		}
	}
	for _, name := range done {
		_ = os.Remove(name) // 消せなくても、次の Apply は Applied を見て飛ばす
	}
	if len(rejected) > 0 {
		rj := filepath.Join(box, RejectedDir)
		if err := os.MkdirAll(rj, 0o700); err != nil {
			return results, err
		}
		for _, name := range rejected {
			_ = os.Rename(name, filepath.Join(rj, filepath.Base(name)))
		}
		byID := map[string]string{}
		for _, r := range results {
			if r.Err != "" {
				byID[r.ID] = r.Err
			}
		}
		for id, why := range byID { // 除けた理由を横に置く (人が読む)
			_ = os.WriteFile(filepath.Join(rj, id+".reason"), []byte(why+"\n"), 0o600)
		}
	}
	return results, nil
}

// Update は daemon の中でカードを直接進める (PG の起動で作業中へ、など。箱を通さない daemon 自身の操作)。
// f が st を書き換え、不変条件に新しい違反が出なければ記録を書く。🚨 呼ぶのは daemon だけ (Apply と同じ書き手)。
func Update(dir string, f func(*State) error) error {
	st, err := Load(dir)
	if err != nil {
		return err
	}
	next := st
	next.Cards = append([]card.Card(nil), st.Cards...)
	if err := f(&next); err != nil {
		return err
	}
	if err := newViolation(st.Cards, next.Cards); err != nil {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, StateFile), data)
}

// newViolation は before に無かった不変条件の違反が after に出たらエラー (件数ではなく中身で比べる)。
func newViolation(before, after []card.Card) error {
	seen := map[card.Violation]bool{}
	for _, v := range card.Check(before) {
		seen[v] = true
	}
	for _, v := range card.Check(after) {
		if !seen[v] {
			return fmt.Errorf("不変条件を破る (%s: %s)", v.CardID, v.Reason)
		}
	}
	return nil
}

// apply は依頼 1 件を st に当てた次の状態と、対象のカード ID を返す。規則か不変条件に反したらエラー (st は変えない)。
func apply(st State, r Request, now time.Time) (State, string, error) {
	next := st
	next.Cards = append([]card.Card(nil), st.Cards...)
	var id string
	switch r.Kind {
	case "add":
		if strings.TrimSpace(r.Title) == "" && strings.TrimSpace(r.Request) == "" {
			return st, "", errors.New("add: 題名も依頼の原文も空")
		}
		id = fmt.Sprintf("C-%03d", next.NextID)
		next.NextID++
		c := card.Card{ID: id, Title: firstNonEmpty(r.Title, clip(r.Request, 40)), Request: r.Request, Prompt: r.Prompt, Repo: r.Repo,
			Owner: firstNonEmpty(r.Owner, "PM"), State: card.Requested, Since: now,
			History: []card.Event{{At: now, Text: "依頼を受けた"}}}
		next.Cards = append(next.Cards, c)
	default:
		i := indexOf(next.Cards, r.CardID)
		if i < 0 {
			return st, r.CardID, fmt.Errorf("%s: カード %q が無い", r.Kind, r.CardID)
		}
		id = r.CardID
		c := next.Cards[i]
		if err := transition(&c, r, now); err != nil {
			return st, id, fmt.Errorf("%s: %w", r.Kind, err)
		}
		next.Cards[i] = c
	}
	// 件数ではなく「新しく出た違反」で判定する (ある違反を消しつつ別の違反を作る依頼を通さない)
	if err := newViolation(st.Cards, next.Cards); err != nil {
		return st, id, fmt.Errorf("%s: %w", r.Kind, err)
	}
	return next, id, nil
}

// transition はカードの状態を 1 つ進める。遷移の規則 (どの状態からどの依頼を受けるか) はここだけに書く。
func transition(c *card.Card, r Request, now time.Time) error {
	move := func(to card.State, why string) {
		c.State, c.Since = to, now
		c.History = append(c.History, card.Event{At: now, Text: why})
	}
	switch r.Kind {
	case "plan": // PM がタスクに分けてキューに積んだ
		if c.State != card.Requested {
			return fmt.Errorf("依頼の列に無い (今は %s)", c.State.Label())
		}
		c.Issues = append(c.Issues, r.Issues...)
		move(card.Planned, "タスクに分けてキューに積んだ")
	case "ask": // PG が質問を書いて turn を終えた
		if c.State != card.Running {
			return fmt.Errorf("作業中の列に無い (今は %s)", c.State.Label())
		}
		if strings.TrimSpace(r.Question) == "" {
			return errors.New("質問が空")
		}
		c.Wait = card.Wait{Kind: card.WaitQuestion, Question: r.Question}
		move(card.Waiting, "質問: "+clip(r.Question, 80))
	case "answer": // 人間か PM の回答。まだ質問待ちのときだけ受ける (二重回答で 2 回 resume しない。415 論点 8)
		if !c.Answerable() {
			return fmt.Errorf("質問待ちではない (今は %s。既に回答済みの可能性)", c.State.Label())
		}
		if strings.TrimSpace(r.Answer) == "" {
			return errors.New("回答が空")
		}
		c.Wait = card.Wait{}
		c.Resume = r.Answer // daemon が同じ session を再開するときに渡す (426 の決定 2)
		move(card.Planned, firstNonEmpty(r.From, "人間")+" が回答した: "+clip(r.Answer, 80)+" (PG の空きが出たら同じ session を resume)")
	case "review": // PG が終えた
		if c.State != card.Running {
			return fmt.Errorf("作業中の列に無い (今は %s)", c.State.Label())
		}
		move(card.Review, "PG が終えた。レビュー待ち")
	case "close": // PM がレビューを通して完了にした。依頼の列からも閉じられる (その場で回答した・却下した。Ending を付ける)
		if c.State != card.Review && c.State != card.Requested {
			return fmt.Errorf("レビューの列に無い (今は %s)", c.State.Label())
		}
		c.Issues = append(c.Issues, r.Issues...)
		if r.Ending != card.EndNone {
			c.Ending = r.Ending
		}
		move(card.Done, "完了にした")
	default:
		return fmt.Errorf("未知の依頼 %q", r.Kind)
	}
	return nil
}

func indexOf(cs []card.Card, id string) int {
	for i, c := range cs {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// writeAtomic は一時ファイルに書いてから rename する (途中で落ちても壊れたファイルを残さない)。
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
