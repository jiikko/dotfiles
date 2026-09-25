// Package store は本物のモードのカードの記録 (issue 427 の段階 3a)。
//
// 書き手は dispatcher だけ (issue 426 の決定 1)。PM / PG / 画面は受付の箱 (inbox) に依頼を 1 件 1 ファイルで置き (Submit)、
// dispatcher が Apply でまとめて記録 (cards.json) へ適用する。採番と、状態の遷移の規則・不変条件 (card.Check) の検査も Apply の中の 1 か所。
//
// 失敗モード:
//   - 書きかけの依頼が箱に見える → Submit は一時ファイルに書いてから rename する (箱には完成したファイルしか現れない)
//   - 記録を書いた後、箱のファイルを消す前に落ちる → 次の Apply で同じ依頼を二重に適用しないよう、適用済みの依頼の ID を記録に控える
//   - 規則に反する依頼・壊れた依頼 → 記録に入れず、理由つきで inbox/rejected/ へ除ける (黙って捨てない)
//
// 🚨 Apply を 2 つの dispatcher から同時に呼ばない (記録の書き込みが後勝ちになる)。dispatcher の排他は 3c で入れる。
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
	"pro-con/wake"
)

const (
	StateFile   = "cards.json"
	InboxDir    = "inbox"
	RejectedDir = "rejected" // inbox の下
	// keepApplied は控えておく適用済みの依頼の ID の数 (落ちてから再開するまでに箱に残りうる数より十分大きい)。
	keepApplied = 1000
	// perApply は 1 回の Apply で適用する依頼の上限 (keepApplied より小さくする。Apply の中の注記)
	perApply = 500
)

// RunResource はテストの係 (dispatcher が直列に実行する列) のリソース名。今は 1 本の列だけ (426 の決定 5)。
const RunResource = "テスト"

// AttachPrefix は attach の間に人間が打った指示を履歴に残すときの前置き。
const AttachPrefix = "attach で人間が指示: "

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
	Command  string          `json:"command,omitempty"` // run: テストの係に実行を頼むコマンド (シェルの 1 行)
	Cwd      string          `json:"cwd,omitempty"`     // run: 頼んだシェルの作業ディレクトリ (dispatcher が PG の worktree と照らす)
	Answer   string          `json:"answer,omitempty"`
	From     string          `json:"from,omitempty"` // 回答した人 (人間 / PM)
	Ending   card.Ending     `json:"ending,omitempty"`
	Said     []card.Event    `json:"said,omitempty"` // attach: attach の間に人間が打った指示 (原文と打った時刻)
	At       time.Time       `json:"at"`
}

// State は記録の中身。
type State struct {
	NextID  int         `json:"nextId"`
	Cards   []card.Card `json:"cards"`
	Applied []string    `json:"applied"` // 適用済みの依頼の ID。古い順、最大 keepApplied
	// Rejected は除けた依頼の ID と理由。rejected/ へ移す前に落ちても、次の Apply は判定し直さずに移すだけにする
	// (判定し直すと、後の依頼で状態が変わった後に適用されて順序が入れ替わる)。古い順、最大 keepApplied
	Rejected []Rejected `json:"rejected,omitempty"`
}

// Rejected は除けた依頼 1 件。
type Rejected struct {
	ID  string `json:"id"`
	Why string `json:"why"`
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
	_ = wake.Poke(dir) // dispatcher をすぐ起こす。居なくても箱のファイルは残り、ポーリングが拾う (package wake)
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

// Apply は受付の箱の依頼を置いた順に記録へ適用する (dispatcher だけが呼ぶ)。記録を書いてから箱のファイルを片付ける。
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
	if len(names) > perApply { // 残りは次の Apply。1 回で控え (keepApplied) より多く適用すると、片付ける前に落ちたとき控えから外れた分を二重に適用する
		names = names[:perApply]
	}
	applied := map[string]bool{}
	for _, id := range st.Applied {
		applied[id] = true
	}
	judged := map[string]string{} // 前の Apply で除けたが、rejected/ へ移す前に落ちた依頼
	for _, r := range st.Rejected {
		judged[r.ID] = r.Why
	}
	var results []Result
	var done []string             // 片付ける箱のファイル
	reject := map[string]string{} // 除ける箱のファイル → 理由
	for _, name := range names {
		id := strings.TrimSuffix(filepath.Base(name), ".json")
		if applied[id] { // 前の Apply で記録には入ったが、ファイルを片付ける前に落ちた
			done = append(done, name)
			continue
		}
		if why, ok := judged[id]; ok {
			reject[name] = why
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
			reject[name] = res.Err
			st.Rejected = append(st.Rejected, Rejected{ID: id, Why: res.Err})
		} else {
			done = append(done, name)
			st.Applied = append(st.Applied, id)
		}
		results = append(results, res)
	}
	if len(st.Applied) > keepApplied {
		st.Applied = st.Applied[len(st.Applied)-keepApplied:]
	}
	if len(st.Rejected) > keepApplied {
		st.Rejected = st.Rejected[len(st.Rejected)-keepApplied:]
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
	// 除けた依頼は理由を先に置いてから rejected/ へ移す。移せなくても dispatcher は止めない (箱に残り、次の Apply が控えを見て移し直す)。
	// 移せなかったことは結果に出す
	for name, why := range reject {
		id := strings.TrimSuffix(filepath.Base(name), ".json")
		rj := filepath.Join(box, RejectedDir)
		err := os.MkdirAll(rj, 0o700)
		if err == nil {
			err = os.WriteFile(filepath.Join(rj, id+".reason"), []byte(why+"\n"), 0o600)
		}
		if err == nil {
			err = os.Rename(name, filepath.Join(rj, filepath.Base(name)))
		}
		if err != nil {
			results = append(results, Result{ID: id, Err: "rejected/ へ移せない (次の Apply で移し直す): " + err.Error()})
		}
	}
	return results, nil
}

// Update は dispatcher の中でカードを直接進める (PG の起動で作業中へ、など。箱を通さない dispatcher 自身の操作)。
// f が st を書き換え、不変条件に新しい違反が出なければ記録を書く。🚨 呼ぶのは dispatcher だけ (Apply と同じ書き手)。
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
			Owner: firstNonEmpty(r.Owner, "PM"), State: card.Requested, Since: now, FromRequest: r.ID,
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
		if c.State == card.Running && to != card.Running {
			c.DropRun() // 作業中の列を離れたら、テストの係への頼みは取り下げる
		}
		c.State, c.Since = to, now
		c.Stalled = false // 停滞は作業中の列でだけ意味を持つ (watchdog が作業中のカードだけを見る)。止める印 (StopWanted) は作業中へ戻る settle が外す
		c.History = append(c.History, card.Event{At: now, Text: why})
	}
	switch r.Kind {
	case "plan": // PM がタスクに分けてキューに積んだ
		if c.State != card.Requested {
			return fmt.Errorf("依頼の列に無い (今は %s)", c.State.Label())
		}
		c.Issues = append(c.Issues, r.Issues...)
		if c.Repo == "" && len(r.Issues) > 0 {
			// global で受けた依頼は、PM が分けた issue の repo で作業する (付けないと、dispatcher が起動先を決められずに止まる)
			c.Repo = r.Issues[0].Repo
		}
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
		c.Resume = r.Answer // dispatcher が同じ session を再開するときに渡す (426 の決定 2)
		move(card.Planned, firstNonEmpty(r.From, "人間")+" が回答した: "+clip(r.Answer, 80)+" (PG の空きが出たら同じ session を resume)")
	case "run": // PG がテストの係にコマンドの実行を頼んで turn を終えた (426 の決定 5)。結果は dispatcher が再開のときに渡す
		if c.State != card.Running {
			return fmt.Errorf("作業中の列に無い (今は %s)", c.State.Label())
		}
		if c.Run != "" || c.Exec.Active() {
			return fmt.Errorf("前に頼んだコマンドの結果をまだ返していない (%s)", clip(firstNonEmpty(c.Run, c.Exec.Command), 60))
		}
		if strings.TrimSpace(r.Command) == "" {
			return errors.New("コマンドが空")
		}
		c.Run, c.RunAt, c.RunCwd = r.Command, now, r.Cwd
		c.Wait = card.Wait{Kind: card.WaitResource, Resource: RunResource}
		c.History = append(c.History, card.Event{At: now, Text: "テストの係に頼んだ: " + clip(r.Command, 80)})
	case "attach": // 画面が attach から戻り、その間に人間が PG へ打った指示を残す (issue 428)。どの列でも受け、状態は変えない
		if len(r.Said) == 0 {
			return errors.New("指示が無い")
		}
		for _, e := range r.Said {
			if strings.TrimSpace(e.Text) == "" {
				return errors.New("空の指示がある")
			}
			// 原文のまま残す (要約・切り詰めをしない)。時刻は打った時刻 (適用した時刻ではない)
			c.History = append(c.History, card.Event{At: e.At, Text: AttachPrefix + e.Text})
		}
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
