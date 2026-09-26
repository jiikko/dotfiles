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
	"slices"
	"sort"
	"strings"
	"time"

	"termsafe"

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

// KindEvent は画面の出来事 (開いた・quit で閉じた・止めた / 止めなかった) の依頼の種類。
// 🚨 --view の画面は置かない (受付の箱にも書かない = 読むだけ)
const KindEvent = "event"

// KindMonitor は見張り (pro-con monitor。issue 475) が見つけたこと (取り込みの衝突・テストの順番の長さ) の依頼の種類。
// event と同じく記録 (カード) は変えず、dispatcher が出来事の記録へ書くだけ (見張りは読むだけ。書き手は dispatcher 1 つ = 426 の決定 1)
const KindMonitor = "monitor"

// noteOnly は記録を変えず、dispatcher が出来事の記録へ書くだけの依頼か (画面の出来事・見張りの知らせ)。
func noteOnly(kind string) bool { return kind == KindEvent || kind == KindMonitor }

// RunResource はテストの係 (dispatcher が直列に実行する列) のリソース名。今は 1 本の列だけ (426 の決定 5)。
const RunResource = "テスト"

// AttachPrefix は attach の間に人間が打った指示を履歴に残すときの前置き。
const AttachPrefix = "attach で人間が指示: "

// ReworkPrefix は差し戻しで PG を再開するときに、直してほしい点の前に付ける前置き (回答と区別できるように)。
const ReworkPrefix = "レビューで差し戻された。直してほしい点:\n"

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
	After    []string        `json:"after,omitempty"`  // plan: このカードより先に完了させるカード (issue 468)
	Points   int             `json:"points,omitempty"` // plan: 見積もりのポイント (card.PointScale のどれか。0 = 付けない。issue 490)
	Question string          `json:"question,omitempty"`
	Command  string          `json:"command,omitempty"` // run: テストの係に実行を頼むコマンド (シェルの 1 行)
	Cwd      string          `json:"cwd,omitempty"`     // run: 頼んだシェルの作業ディレクトリ (dispatcher が PG の worktree と照らす)
	Answer   string          `json:"answer,omitempty"`
	From     string          `json:"from,omitempty"`   // 回答した人 / 削除を依頼した人 (人間 / PM)
	Rework   string          `json:"rework,omitempty"` // rework: レビューで直してほしい点 (書いたまま)
	Ending   card.Ending     `json:"ending,omitempty"`
	Said     []card.Event    `json:"said,omitempty"` // attach: attach の間に人間が打った指示 (原文と打った時刻)
	Note     string          `json:"note,omitempty"` // event: 画面の出来事 (人が読む 1 文。dispatcher が出来事の記録へ書く) / attachment: 添付の一言
	Name     string          `json:"name,omitempty"` // attachment: 元のファイル名 (置き場の名前は依頼の ID と拡張子で決める)
	// File / Size は attachment の移し先の絶対パスと大きさ。Apply が箱のファイルから入れる (依頼に書かれた値は使わない)
	File     string         `json:"-"`
	Size     int64          `json:"-"`
	ParentID string         `json:"parentId,omitempty"` // add: 別件の追加オーダーの元のカード
	Order    card.OrderKind `json:"order,omitempty"`    // order: 追記 / 方針変更 (別件は add + ParentID)
	Text     string         `json:"text,omitempty"`     // order: 追加オーダーの本文 / handoff: 人に回す理由 (どちらも書いたまま)
	Cards    []string       `json:"cards,omitempty"`    // clear: 片付ける完了のカード (画面が見ていたもの。適用までに完了になったカードを巻き込まない)
	Seen     time.Time      `json:"seen,omitzero"`      // move: 頼んだ側が見ていたカードの Since (違えば列を移った後なので動かさない。空なら見ない)
	Delta    int            `json:"delta,omitempty"`    // move: -1 = 1 つ上 / +1 = 1 つ下と入れ替える (Repo が空でなければ、その repo のカードの中の隣。issue 470)
	Screen   string         `json:"screen,omitempty"`   // 置いた画面 (「a1b2c3 join review」。画面から置いた依頼だけ。履歴と出来事に残す。issue 481)
	Key      string         `json:"key,omitempty"`      // config: 設定の名前 (settings.go)
	Value    string         `json:"value,omitempty"`    // config: 設定の値 (空なら消す)
	At       time.Time      `json:"at"`
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

// Result は依頼 1 件の適用の結果。Err が空なら適用した。Note は記録に残す出来事 (カードを消したとき。消したカードの履歴には残せない /
// 画面の出来事 (event) の文)
type Result struct {
	ID, Kind, CardID string
	Screen           string // 依頼を置いた画面 (Request.Screen。画面以外が置いた依頼は空)
	Err              string
	Note             string
	At               time.Time // event: 画面が出来事を置いた時刻 (dispatcher が出来事の記録へ書く。適用した時刻ではない)
}

// Submit は依頼を受付の箱に置き、依頼の ID を返す。どのプロセスから呼んでもよい。
func Submit(dir string, r Request) (string, error) { return submitWith(dir, r, nil) }

// submitWith は依頼を箱に置く。stage は依頼より先に箱へ置くもの (添付のファイル) を、振った依頼の ID で置く (nil なら無し)。
func submitWith(dir string, r Request, stage func(box, id string) error) (string, error) {
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
	if stage != nil {
		if err := stage(box, r.ID); err != nil {
			return "", err
		}
	}
	if err := writeAtomic(filepath.Join(box, r.ID+".json"), data); err != nil {
		return "", err
	}
	_ = wake.Poke(dir) // dispatcher をすぐ起こす。居なくても箱のファイルは残り、ポーリングが拾う (package wake)
	return r.ID, nil
}

// Pending は受付の箱の適用待ちの依頼の数 (画面の出来事 = event と見張りの知らせ = monitor は数えない: 画面を開くたびに・見張りが見るたびに置くので、
// dispatcher の最初の Tick まで「適用待ち」が出て、dispatcher が止まっているように見える)。読めない・壊れたファイルは依頼として数える (除けられるまで待ちには違いない)。
func Pending(dir string) int {
	n := 0
	for _, r := range inbox(dir) {
		if !noteOnly(r.Kind) {
			n++
		}
	}
	return n
}

// PendingRequests は受付の箱の適用待ちの依頼 (読めないものは除く。読むだけ)。
func PendingRequests(dir string) []Request {
	var out []Request
	for _, r := range inbox(dir) {
		if r.Kind != "" {
			out = append(out, r)
		}
	}
	return out
}

// inbox は箱のファイルを置いた順に読む。読めない・壊れたファイルは Kind が空の Request。
func inbox(dir string) []Request {
	names, _ := filepath.Glob(filepath.Join(dir, InboxDir, "*.json"))
	sort.Strings(names)
	out := make([]Request, 0, len(names))
	for _, name := range names {
		var r Request
		if data, err := os.ReadFile(name); err != nil || json.Unmarshal(data, &r) != nil {
			r = Request{}
		}
		out = append(out, r)
	}
	return out
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
	var set *Settings // config の依頼が来たときだけ読む (settings.go)
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
			res.Kind, res.CardID, res.Screen = r.Kind, r.CardID, r.Screen
			if noteOnly(r.Kind) {
				res.At = r.At
			}
			if r.Kind == KindConfig {
				var f func(*Settings)
				if f, err = CheckSetting(r.Key, r.Value); err == nil { // 🚨 検査に落ちた依頼では読み書きしない (壊れた設定をゼロ値で黙って書き直さない)
					if set == nil {
						s, lerr := LoadSettings(dir) // 壊れていたらゼロ値から書き直す (直す口がこの依頼しか無い)
						set = &s
						if lerr != nil {
							res.Note = "壊れていた " + SettingsFile + " を書き直した。"
						}
					}
					f(set)
					res.Note += configNote(r.Key, r.Value)
				}
			} else {
				var staged string
				if r.Kind == KindAttachment {
					staged, err = stageAttachment(dir, &r)
				}
				var next State
				if err == nil {
					next, res.CardID, res.Note, err = apply(st, r, now)
				}
				if err == nil && r.Kind == KindAttachment { // 記録に当てられると決まってから移す (無いカードの置き場を作らない)
					err = adoptAttachment(staged, r.File)
				}
				if err == nil {
					tagScreen(st, &next, r.Screen)
					st = next
				}
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
	if set != nil {
		// 🚨 記録 (適用済みの控え) より先に書く: 間で落ちても、次の Apply は同じ依頼を同じ順に当て直すだけ (後に書くと、控えにだけ入って設定が消える)
		if err := saveSettings(dir, *set); err != nil {
			return nil, err
		}
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
		if idPattern.MatchString(id) { // 除けた添付のファイル (形の合う id だけ。* などを含む手で置いた名前で、他の依頼のファイルを消さない)
			staged, _ := filepath.Glob(filepath.Join(box, StageDir, id+"*"))
			for _, f := range staged {
				_ = os.Remove(f) // 消せなくても SweepAttachments が stageTTL の後に消す
			}
		}
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

// tagScreen は依頼 1 件の適用で足された履歴の行に、依頼を置いた画面を付ける (screen が空なら何もしない)。
// 足された行は、前の状態のそのカードの履歴に無い行 (attach の指示は時刻の位置に差し込まれるので、末尾の差では拾えない)。
func tagScreen(before State, next *State, screen string) {
	if screen == "" {
		return
	}
	type key struct {
		at   time.Time
		text string
	}
	old := map[string]map[key]bool{}
	for _, c := range before.Cards {
		m := map[key]bool{}
		for _, e := range c.History {
			m[key{e.At, e.Text}] = true
		}
		old[c.ID] = m
	}
	for i, c := range next.Cards {
		var hist []card.Event // 書き換えるカードだけ複製する (next.Cards は before と履歴の配列を共有している)
		for j, e := range c.History {
			if e.Screen != "" || old[c.ID][key{e.At, e.Text}] {
				continue
			}
			if hist == nil {
				hist = slices.Clone(c.History)
			}
			hist[j].Screen = screen
		}
		if hist != nil {
			next.Cards[i].History = hist
		}
	}
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

// apply は依頼 1 件を st に当てた次の状態と、対象のカード ID と、記録に残す出来事 (無ければ空) を返す。
// 規則か不変条件に反したらエラー (st は変えない)。
func apply(st State, r Request, now time.Time) (State, string, string, error) {
	next := st
	next.Cards = append([]card.Card(nil), st.Cards...)
	var id, note string
	switch r.Kind {
	case "add":
		if strings.TrimSpace(r.Title) == "" && strings.TrimSpace(r.Request) == "" {
			return st, "", "", errors.New("add: 題名も依頼の原文も空")
		}
		id = fmt.Sprintf("C-%03d", next.NextID)
		next.NextID++
		c := card.Card{ID: id, ParentID: r.ParentID, Title: firstNonEmpty(r.Title, clip(r.Request, 40)), Request: r.Request, Prompt: r.Prompt, Repo: r.Repo,
			Owner: firstNonEmpty(r.Owner, "PM"), State: card.Requested, Since: now, FromRequest: r.ID,
			History: []card.Event{{At: now, Text: "依頼を受けた"}}}
		if p := indexOf(next.Cards, r.ParentID); r.ParentID != "" { // 親が無ければ不変条件 (親カードが存在しない) が弾く
			c.History[0].Text = r.ParentID + " の追加オーダー (別件) から分けた"
			if p >= 0 {
				next.Cards[p].History = append(next.Cards[p].History, card.Event{At: now, Text: "追加オーダー (別件) を " + id + " に分けた: " + clip(r.Request, 80)})
			}
		}
		next.Cards = append(next.Cards, c)
	case "delete":
		i := indexOf(next.Cards, r.CardID)
		if i < 0 {
			return st, r.CardID, "", fmt.Errorf("delete: カード %q が無い", r.CardID)
		}
		id = r.CardID
		var err error
		if note, err = remove(&next, i, r, now); err != nil {
			return st, id, "", fmt.Errorf("delete: %w", err)
		}
	case "move": // レーンの中の並び (= 優先度) を 1 つ上 / 下と入れ替える (issue 470)。隣は適用の時点の並びで決める (押した回数だけ動く)
		i := indexOf(next.Cards, r.CardID)
		if i < 0 {
			return st, r.CardID, "", fmt.Errorf("move: カード %q が無い", r.CardID)
		}
		id = r.CardID
		if !r.Seen.IsZero() && !r.Seen.Equal(next.Cards[i].Since) { // 見ていない列で入れ替えない (回答で分解済みへ移った・起動した後)
			return st, id, "", fmt.Errorf("move: 押した後に %s の列へ移ったので動かさない", next.Cards[i].State.Label())
		}
		other, err := card.Move(next.Cards, r.CardID, r.Repo, r.Delta)
		if err != nil {
			return st, id, "", fmt.Errorf("move: %w", err)
		}
		c := &next.Cards[i]
		how := card.MovedText(r.Delta, other)
		if b := card.Blockers(next.Cards, *c); c.State == card.Planned && len(b) > 0 && c.Session == "" {
			how += "。" + strings.Join(b, ", ") + " の完了を待つので、それまでは起動しない" // 並びより順番 (issue 468) が勝つ
		}
		c.History = append(c.History, card.Event{At: now, Text: how})
	case KindEvent: // 画面の出来事。記録 (カード) は変えず、dispatcher が出来事の記録へ書くだけ (書き手を dispatcher 1 つに保つ。issue 445)
		if strings.TrimSpace(r.Note) == "" {
			return st, "", "", errors.New("event: 出来事の文が空")
		}
		return st, "", r.Note, nil
	case KindMonitor: // 見張りの知らせ。カードの ID は出来事に付けるだけ (片付けた後のカードでも除けない = 知らせは記録に当てない)
		if strings.TrimSpace(r.Note) == "" {
			return st, "", "", errors.New("monitor: 知らせの文が空")
		}
		return st, r.CardID, r.Note, nil
	case "clear": // 画面が完了のレーンを片付けた (x)。消さずに Archived にする。見ていた後に完了でなくなったカード・無いカードは飛ばす
		if len(r.Cards) == 0 {
			return st, "", "", errors.New("clear: 片付けるカードが無い")
		}
		for _, cid := range r.Cards {
			if i := indexOf(next.Cards, cid); i >= 0 && next.Cards[i].State == card.Done && !next.Cards[i].Archived {
				next.Cards[i].Archived = true
				next.Cards[i].History = append(next.Cards[i].History, card.Event{At: now, Text: "完了のレーンから片付けた"})
			}
		}
	default:
		i := indexOf(next.Cards, r.CardID)
		if i < 0 {
			return st, r.CardID, "", fmt.Errorf("%s: カード %q が無い", r.Kind, r.CardID)
		}
		if r.Kind == "plan" {
			if err := checkAfter(next, r.After); err != nil {
				return st, r.CardID, "", fmt.Errorf("plan: %w", err)
			}
		}
		id = r.CardID
		c := next.Cards[i]
		if err := transition(&c, r, now); err != nil {
			return st, id, "", fmt.Errorf("%s: %w", r.Kind, err)
		}
		next.Cards[i] = c
	}
	// 件数ではなく「新しく出た違反」で判定する (ある違反を消しつつ別の違反を作る依頼を通さない)
	if err := newViolation(st.Cards, next.Cards); err != nil {
		return st, id, "", fmt.Errorf("%s: %w", r.Kind, err)
	}
	return next, id, note, nil
}

// remove は削除の依頼 (issue 451)。依頼の列のカード (PG が付いていない) はすぐ記録から外す。それ以外は印 (DeleteAt) を付けるだけで、
// dispatcher が PG の session を止めたのを確かめてから外す (dispatcher/close.go)。返すのは記録に残す出来事。
// 🚨 PG の worktree とブランチには触らない (取り込み前の作業が入っている)。
func remove(st *State, i int, r Request, now time.Time) (string, error) {
	c := st.Cards[i]
	if kids := card.Children(st.Cards, c.ID); len(kids) > 0 {
		return "", fmt.Errorf("子カード %s の親なので消せない (先に子カードを消す)", strings.Join(kids, ", "))
	}
	by := firstNonEmpty(r.From, "人間")
	if c.Deleting() {
		return "", nil // 既に削除の依頼を受けている (二重に押した)
	}
	if c.State == card.Requested && c.Session == "" && c.Launching == "" {
		st.Cards = card.Drop(st.Cards, c.ID, now)
		return fmt.Sprintf("「%s」を削除した (依頼の列。%s が依頼)", c.Title, by), nil
	}
	// テストの係への頼みはここでは取り下げない (実行中の印を消すと、前の dispatcher が残した実行を止められない)。dispatcher が止めてから取り下げる
	c.DeleteAt, c.DeleteBy = now, by
	c.History = append(c.History, card.Event{At: now, Text: by + " が削除を依頼した (PG の session を止めてから消す。worktree とブランチは残す)"})
	st.Cards[i] = c
	return fmt.Sprintf("「%s」の削除の依頼を受けた (%s。PG の session を止めてから消す)", c.Title, by), nil
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
	if c.Deleting() { // PG を止めて消すのを待っている。質問・完了・実行の頼みで列を動かさない (動かすと再開・実行の口が開く)
		return errors.New("削除の依頼を受けている")
	}
	switch r.Kind {
	case "plan": // PM がタスクに分けてキューに積んだ
		if c.State != card.Requested {
			return fmt.Errorf("依頼の列に無い (今は %s)", c.State.Label())
		}
		if err := card.CheckPoints(r.Points); err != nil { // 箱に手で置かれた依頼もここで止める (cardcmd の検査を通らない)
			return err
		}
		c.Issues = append(c.Issues, r.Issues...)
		if c.Repo == "" && len(r.Issues) > 0 {
			// global で受けた依頼は、PM が分けた issue の repo で作業する (付けないと、dispatcher が起動先を決められずに止まる)
			c.Repo = r.Issues[0].Repo
		}
		// 相手が記録に有るか・循環しないかは不変条件 (card.Check) が見る
		for _, a := range r.After {
			if !slices.Contains(c.After, a) {
				c.After = append(c.After, a)
			}
		}
		why := "タスクに分けてキューに積んだ"
		if r.Points != 0 {
			c.Points = r.Points
			why += fmt.Sprintf(" (見積もり %dpt)", r.Points)
		}
		if len(c.After) > 0 {
			why += " (" + strings.Join(c.After, ", ") + " の後に起動する)"
		}
		move(card.Planned, why)
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
		// dispatcher が同じ session を再開するときに渡す (426 の決定 2)。まだ渡せていない文 (再開を claude が受け付けずに人の番へ回った
		// 差し戻し・テストの結果など。462) があれば消さずに前に残す
		if c.Resume != "" {
			c.Resume += "\n\n回答: " + r.Answer
		} else {
			c.Resume = r.Answer
		}
		move(card.Planned, firstNonEmpty(r.From, "人間")+" が回答した: "+clip(r.Answer, 80)+" (PG の空きが出たら同じ session を resume)")
	case "rework": // 取り込みの係 (487) がレビューで差し戻した (issue 446)。回答と同じく分解済みへ戻し、dispatcher が同じ session を再開する
		if c.State != card.Review {
			return fmt.Errorf("レビュー待ちではない (今は %s)", c.State.Label())
		}
		if strings.TrimSpace(r.Rework) == "" {
			return errors.New("直してほしい点が空")
		}
		c.Resume = ReworkPrefix + r.Rework + "\n直したら、もう一度 `pro-con card review " + c.ID + "` を実行してから turn を終える。"
		move(card.Planned, "差し戻した: "+r.Rework) // 原文のまま残す (要約・切り詰めをしない)
	case "handoff": // PM が PG の質問を / 取り込みの係がレビュー待ちを人に回した (487)。人の番の目印 (452) ができるまでは履歴に残すだけで、列も質問も変えない
		if (c.State != card.Waiting || c.Wait.Kind != card.WaitQuestion) && c.State != card.Review {
			return fmt.Errorf("PG の質問待ちでもレビュー待ちでもない (今は %s)", c.State.Label())
		}
		if strings.TrimSpace(r.Text) == "" {
			return errors.New("人に回す理由が空")
		}
		c.History = append(c.History, card.Event{At: now, Text: card.HandoffText(firstNonEmpty(r.From, "PM"), r.Text)}) // 原文のまま
	case "run": // PG がテストの係にコマンドの実行を頼んで turn を終えた (426 の決定 5)。結果は dispatcher が再開のときに渡す
		if c.State != card.Running {
			return fmt.Errorf("作業中の列に無い (今は %s)", c.State.Label())
		}
		if c.AwaitsRun() {
			return fmt.Errorf("前に頼んだコマンドの結果をまだ返していない (%s)", clip(firstNonEmpty(c.Run, c.Exec.Command), 60))
		}
		if strings.TrimSpace(r.Command) == "" {
			return errors.New("コマンドが空")
		}
		c.Run, c.RunAt, c.RunCwd = r.Command, now, r.Cwd
		c.Wait = card.Wait{Kind: card.WaitResource, Resource: RunResource}
		c.History = append(c.History, card.Event{At: now, Text: "テストの係に頼んだ: " + clip(r.Command, 80)})
	case "order": // 人間が作業中のカードへ追加オーダーを出した (要件 15)。積むだけで、PG へ届けるのは dispatcher (orders.go)
		if r.Order != card.OrderAppend && r.Order != card.OrderRedirect {
			return fmt.Errorf("追記か方針変更ではない (%s。別件は新しい依頼にする)", r.Order.Label())
		}
		if c.State == card.Done { // レビュー待ちは受ける (PG の review と同じ Apply で来たオーダーを捨てない。dispatcher が PG へ戻して届ける)
			return errors.New("完了したカードには出せない。別件で出す")
		}
		if strings.TrimSpace(r.Text) == "" {
			return errors.New("本文が空")
		}
		c.Orders = append(c.Orders, card.Order{Kind: r.Order, Text: r.Text, At: now})
		c.History = append(c.History, card.Event{At: now, Text: "追加オーダー (" + r.Order.Label() + "): " + r.Text}) // 原文のまま
	case "btw": // 人間が PG を止めずに状況を聞いた (要件 9)。答えるのは dispatcher (btw.go)。どの列でも受ける
		if strings.TrimSpace(r.Question) == "" {
			return errors.New("質問が空")
		}
		c.Btws = append(c.Btws, card.Btw{Question: r.Question, At: now})
		c.History = append(c.History, card.Event{At: now, Text: "btw: " + r.Question})
	case "attach": // 画面が attach から戻り、その間に人間が PG へ打った指示を残す (issue 428)。どの列でも受け、状態は変えない
		if len(r.Said) == 0 {
			return errors.New("指示が無い")
		}
		for _, e := range r.Said {
			if strings.TrimSpace(e.Text) == "" {
				return errors.New("空の指示がある")
			}
			if e.At.IsZero() { // 時刻の位置へ差し込むので、時刻の無い指示は履歴の先頭へ行ってしまう
				return errors.New("時刻の無い指示がある")
			}
		}
		for _, e := range r.Said {
			// 原文のまま残す (要約・切り詰めをしない)。時刻は打った時刻 (適用した時刻ではない)
			c.History = insertByTime(c.History, card.Event{At: e.At, Text: AttachPrefix + e.Text})
		}
	case KindAttachment: // PG が作業の証拠を付けた (issue 453)。ファイルは Apply が移す。状態は変えない
		if c.State == card.Done {
			return errors.New("完了したカードには付けない")
		}
		if len(c.Attachments) >= MaxAttachPerCard {
			return fmt.Errorf("添付は 1 枚のカードに %d 件まで", MaxAttachPerCard)
		}
		if r.File == "" {
			return errors.New("添付のファイルが無い")
		}
		// 一言と名前は PG が書いた文字列。履歴と詳細にそのまま出るので、ここで制御文字・エスケープを落とす
		a := card.Attachment{Path: r.File, Name: termsafe.PlainLine(r.Name), Note: termsafe.PlainLine(r.Note), Kind: AttachKindOf(r.Name), Size: r.Size, At: r.At}
		c.Attachments = append(c.Attachments, a)
		c.History = append(c.History, card.Event{At: now, Text: "添付 (" + string(a.Kind) + "): " + firstNonEmpty(a.Note, a.Name)})
	case "review": // PG が終えた
		if c.State != card.Running {
			return fmt.Errorf("作業中の列に無い (今は %s)", c.State.Label())
		}
		move(card.Review, "PG が終えた。レビュー待ち")
	case "close": // 取り込みの係 (487) がレビューを通して完了にした。PM は依頼の列からも閉じられる (その場で回答した・却下した。Ending を付ける)
		if c.State != card.Review && c.State != card.Requested {
			return fmt.Errorf("レビューの列に無い (今は %s)", c.State.Label())
		}
		if n := len(c.Pending()); n > 0 { // 未達のまま完了に埋めない (dispatcher がレビュー待ちから PG へ戻して届ける。issue 438)
			return fmt.Errorf("PG へ届いていない追加オーダーが %d 件ある (dispatcher が PG へ戻して届ける。届いてからもう一度閉じる)", n)
		}
		c.Issues = append(c.Issues, r.Issues...)
		if r.Ending != card.EndNone {
			c.Ending = r.Ending
		}
		c.StopAfterClose = c.Session != "" // PG の session は dispatcher が止める (worktree とブランチは PM が取り込むので残す)
		move(card.Done, "完了にした")
	default:
		return fmt.Errorf("未知の依頼 %q", r.Kind)
	}
	return nil
}

// checkAfter は plan --after の相手が、振った番号のカードか (記録に在る・書庫へ移った・削除した)。まだ振っていない番号 (打ち間違い) は除ける。
// 🚨 記録に在るかだけで判定しない: 完了から 24 時間で書庫へ移ったカードの後に積めなくなる (issue 478)。循環は card.Check が見る
func checkAfter(st State, after []string) error {
	for _, a := range after {
		if indexOf(st.Cards, a) >= 0 {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(a, "C-%d", &n); err != nil || fmt.Sprintf("C-%03d", n) != a || n < 1 || n >= st.NextID {
			return fmt.Errorf("順番の前のカード %s が無い (まだ振っていない番号)", a)
		}
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

// insertByTime は e を、時刻が e より後の出来事の直前へ差し込む (同じ時刻なら後ろへ。issue 489)。
// 履歴は起きた順の記録で、attach の間の指示だけが過去の時刻 (打った時刻) で後から届く。末尾へ足すと、その間に足した
// 出来事 (テストの係の結果・質問) より後ろに並び、表示の順と時刻が食い違う。後ろから探すのは、ふつうは末尾の数件で止まるため。
// 🚨 apply はカードを浅くコピーするので、h の配列は適用前の state と共有している。Clip して、ずらす先を新しい配列にする
func insertByTime(h []card.Event, e card.Event) []card.Event {
	i := len(h)
	for i > 0 && h[i-1].At.After(e.At) {
		i--
	}
	return slices.Insert(slices.Clip(h), i, e)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
