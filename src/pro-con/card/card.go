// Package card は pro-con のドメイン (依頼カード・状態・不変条件) を持つ。
// UI にも backend にも依存しない。設計の正本は issues/ の 415 (PM / PG 分離)。
package card

import (
	"fmt"
	"time"
)

// State はカンバンの列。カードは常にどれか 1 つに居る (不変条件「依頼は失われない」)。
type State int

const (
	Requested State = iota // 依頼: 受付 PM がカードを作った直後
	Planned                // 分解済み: タスクに分けてキューに積んだ (PG の空き待ち)
	Running                // 作業中: PG が動いている (リソース待ち・枠待ちもここ)
	Waiting                // 質問待ち: 人間か PM の回答が要る (権限プロンプトもここ)
	Review                 // レビュー: PG が終えた。PM が diff と実行結果を読む
	Done                   // 完了
)

// Columns はカンバンの左から右の並び。
var Columns = []State{Requested, Planned, Running, Waiting, Review, Done}

func (s State) Label() string {
	switch s {
	case Requested:
		return "依頼"
	case Planned:
		return "分解済み"
	case Running:
		return "作業中"
	case Waiting:
		return "質問待ち"
	case Review:
		return "レビュー"
	case Done:
		return "完了"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

// Ending は issue に紐づかないカードの終わり方 (要件 10)。issue を持つカードは EndNone のままでよい。
type Ending int

const (
	EndNone         Ending = iota
	EndAnswered            // その場で回答した
	EndResearchOnly        // 調べて終わった
	EndRejected            // 却下した
	EndPendingIssue        // issue 化待ち (放置されると行方不明になる典型なので UI で目立たせる)
)

func (e Ending) Label() string {
	switch e {
	case EndNone:
		return ""
	case EndAnswered:
		return "回答済み"
	case EndResearchOnly:
		return "調査のみ"
	case EndRejected:
		return "却下"
	case EndPendingIssue:
		return "issue 化待ち"
	}
	return fmt.Sprintf("Ending(%d)", int(e))
}

// WaitKind は Running / Waiting のカードが何を待っているか。正当な待ちは watchdog の停滞判定から外す (要件 13)。
type WaitKind int

const (
	WaitNone       WaitKind = iota
	WaitQuestion            // PG が質問した (Waiting 列)
	WaitPermission          // 権限プロンプトで止まった (Waiting 列。claude agents --json の waitingFor)
	WaitResource            // 占有リソースの順番待ち (要件 12。Running 列のまま)
	WaitQuota               // 利用枠の回復待ち (Running 列のまま)
)

// NeedsAnswer は人間か PM の操作が要る待ちか (= Waiting 列に置く待ちか)。
func (w WaitKind) NeedsAnswer() bool {
	switch w {
	case WaitQuestion, WaitPermission:
		return true
	case WaitNone, WaitResource, WaitQuota:
		return false
	}
	return false
}

type Wait struct {
	Kind     WaitKind
	Resource string // WaitResource のときのリソース名 (device / xcode 等)
	Position int    // WaitResource のときの列の順番 (1 始まり)
	Question string // WaitQuestion / WaitPermission のときの質問文
}

// IssueRef は repo + 番号で issue を指す (パスで持たない。done への移動や改番で切れないように)。
type IssueRef struct {
	Repo   string
	Number int
	Status string // open / next / done (表示用。正本は issue ファイルの位置)
}

func (r IssueRef) String() string { return fmt.Sprintf("%s#%03d", r.Repo, r.Number) }

// OrderKind は追加オーダーの仕分け (要件 15)。
type OrderKind int

const (
	OrderAppend   OrderKind = iota // 追記: 同じ範囲の小さな追加。作業中の PG へ届ける
	OrderRedirect                  // 方針変更: PG を止めて指示を差し替える
	OrderSeparate                  // 別件: 新しいカードにする
)

func (k OrderKind) Label() string {
	switch k {
	case OrderAppend:
		return "追記"
	case OrderRedirect:
		return "方針変更"
	case OrderSeparate:
		return "別件"
	}
	return fmt.Sprintf("OrderKind(%d)", int(k))
}

type Order struct {
	Kind      OrderKind
	Text      string
	At        time.Time
	Delivered bool // PG へ届いたか (不変条件: 未達かどうかがカードで見える)
}

type Event struct {
	At   time.Time
	Text string
}

type Card struct {
	ID       string
	ParentID string // 1 つの依頼を分けたとき / 別件の追加オーダーの元
	Title    string
	Request  string // 依頼の原文 (人間が書いたまま)
	Prompt   string // PM に渡した指示の全文 (スコープの前置き + 原文)。TUI から出した依頼だけが持つ
	Repo     string
	Owner    string // 受付 PM / PM-A / PG-2 / 人間
	Session  string // 担当 PG の session id (claude --bg の id)
	State    State
	Since    time.Time // 今の State に入った時刻
	Wait     Wait
	Stalled  bool // watchdog が停滞と判定した
	Issues   []IssueRef
	Ending   Ending
	Orders   []Order
	History  []Event
	Log      []string // PG の出力の末尾 (本番は transcript から読む)
	// LastProgress は「実質的に進んだ」最後の時刻 (watchdog が見る。活動ではなく進捗)
	LastProgress time.Time
}

// Violation は不変条件の破れ。UI は件数を出し、0 件でないことを隠さない。
type Violation struct {
	CardID string
	Reason string
}

// Check は 415 の不変条件のうち、カードの集合だけで判定できるものを見る。
func Check(cards []Card) []Violation {
	var out []Violation
	ids := make(map[string]bool, len(cards))
	for _, c := range cards {
		if ids[c.ID] {
			out = append(out, Violation{c.ID, "カード ID が重複している"})
		}
		ids[c.ID] = true
	}
	for _, c := range cards {
		if c.ParentID != "" && !ids[c.ParentID] {
			out = append(out, Violation{c.ID, "親カード " + c.ParentID + " が存在しない"})
		}
		if c.State == Done && len(c.Issues) == 0 && c.Ending == EndNone {
			out = append(out, Violation{c.ID, "完了したが、紐づく issue も終わり方も無い"})
		}
		if c.State == Waiting && !c.Wait.Kind.NeedsAnswer() {
			out = append(out, Violation{c.ID, "質問待ちの列に居るが、回答の要る待ちではない"})
		}
		if c.State != Waiting && c.Wait.Kind.NeedsAnswer() {
			out = append(out, Violation{c.ID, "回答の要る待ちなのに質問待ちの列に居ない"})
		}
	}
	return out
}
