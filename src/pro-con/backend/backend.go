// Package backend は UI と「状態を持つ側」の境界。UI はこの package の型だけを知り、
// 実装 (今は fake。本番は状態ファイル + claude agents --json を読む dispatcher クライアント) を知らない。
//
// 状態の正本は backend 側にあり、UI は Snapshot を読むことと Command を送ることしかしない
// (415 要件 14: TUI が落ちても何も失わない)。
package backend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"pro-con/card"
)

// Backend は UI から見た状態の持ち主。
type Backend interface {
	// Poll は最新の状態を返す。fake はここで模擬の時間を 1 刻み進める。
	Poll() Snapshot
	// Snapshot は時間を進めずに今の状態を返す (操作の直後に画面を更新するため)。
	Snapshot() Snapshot
	// Apply は UI からの操作を 1 つ適用する。結果の文面は UI の通知行に出す。
	Apply(Command) (string, error)
	// AttachCommand は PG の session を対話で開くコマンドを返す (本番は `claude attach <id>`)。
	// UI はこれを tea.ExecProcess で起動し、抜けたら自分の画面へ戻る (415 論点 7)。
	AttachCommand(sessionID string) (*exec.Cmd, error)
}

// Snapshot はある瞬間の全状態。UI はこれだけから画面を組む。
type Snapshot struct {
	Now            time.Time
	Cards          []card.Card
	Consumers      []Consumer
	Limit          int       // 今の PG の同時実行数 (利用枠の残量で上限より絞ることがある)
	LimitMax       int       // 上限 (dispatcher の --limit)
	LimitWhy       string    // Limit を絞った / 利用枠を読めない理由 (無ければ空)
	DispatcherTick time.Time // dispatcher (と watchdog) が最後に回った時刻。zero なら 1 度も回っていない。古ければ UI が警告する
	Screens        int       // 開いている画面の数 (自分を含む。package presence。0 なら数えられなかった)
	Violations     []card.Violation
}

// Consumer は PG (consumer) 1 体。
type Consumer struct {
	Session string
	CardID  string // 空なら待機中
	Status  string // busy / waiting / idle (claude agents --json の status と同じ語)
	PID     int    // 実体のプロセス (本物だけ。模擬は 0)
}

// Notifier は状態が変わったと知らせる backend (画面は tick を待たずに描き直す。任意)。
type Notifier interface{ Changed() <-chan struct{} }

// Describer はヘッダーに出す自分の説明を持つ backend (模擬か本物かを画面で見分けるため。任意)。
type Describer interface{ Describe() string }

// Op は画面から backend へ出す書き込みの操作の種類。
type Op string

const (
	OpNew    Op = "new"    // 新しい依頼 (n / i)
	OpAnswer Op = "answer" // 質問への回答 (r)
	OpOrder  Op = "order"  // 追加オーダー (+)
	OpBtw    Op = "btw"    // btw (w)
	OpClear  Op = "clear"  // 完了のレーンを片付ける (x)
)

// Accepter は一部の操作しか受けない backend (任意。持たない backend は全部受ける)。
// 画面は受けない操作のキーを押した時点で断り、案内の行でも暗くする (入力欄を開いてから送った時点で断ると、書いた文が無駄になる)。
type Accepter interface{ Accepts(Op) bool }

// Stopper は、画面を終了するときに backend が動かしているもの (本物のモード: dispatcher と、pro-con が起動した PG) を止める口。
// 持たない backend (模擬) は、画面を閉じるだけで終わる。
// 🚨 同じ置き場で複数の画面を開いてよい。止めるのは最後に閉じる画面だけで、ほかの画面が開いていれば KeptRunning を返す (失敗ではない)。
type Stopper interface {
	StopAll(ctx context.Context) error
}

// KeptRunning は、ほかの画面が開いているので dispatcher と PG を止めずに閉じたこと (StopAll の結果。失敗ではない)。
type KeptRunning struct{ Others int }

func (k KeptRunning) Error() string {
	return fmt.Sprintf("ほかに %d 画面が開いているので、dispatcher と PG は止めずに閉じた", k.Others)
}

// Command は UI からの操作。値として送り、backend が 1 か所で適用する。
type Command interface{ isCommand() }

// Answer は質問待ちのカードへの回答 (要件 11)。
type Answer struct {
	CardID string
	Text   string
	From   string // 人間 / PM
}

// AddOrder は作業中のカードへの追加オーダー (要件 15)。
type AddOrder struct {
	CardID string
	Kind   card.OrderKind
	Text   string
}

// Repo は依頼のスコープにする repo。Name はカードの Repo と突き合わせる名前、Path は PM に渡す絶対パス。
// ゼロ値は「repo を指定しない」(global のタブから出した依頼)。
type Repo struct {
	Name string
	Path string
}

// NewRequest は TUI から PM への新しい依頼。Repo は依頼を出したタブの repo (global ならゼロ値)。
// Issue があれば「この issue (epic) をやって」の依頼で、Text は補足 (空でよい)。
type NewRequest struct {
	Repo  Repo
	Text  string
	Issue *IssueTarget
}

// IssueTarget は依頼の対象の issue。Epic が非空なら epic 全体 (Path は親 issue、Children は未完了の子)。
type IssueTarget struct {
	Number   int
	Title    string
	Path     string
	Epic     string
	Children []string // 未完了の子 issue のパス (epic のとき)
}

// IssuePrompt は issue を選んで出した依頼の本文 (PMPrompt の text に渡す)。補足は人間が書いたまま末尾に置く。
func IssuePrompt(t IssueTarget, extra string) string {
	var b strings.Builder
	if t.Epic != "" {
		fmt.Fprintf(&b, "epic %s に取り組んでください。親 issue: %s (#%03d %s)。", t.Epic, t.Path, t.Number, t.Title)
		if len(t.Children) > 0 {
			b.WriteString("未完了の子 issue を順に進めてください:")
			for _, c := range t.Children {
				b.WriteString("\n- " + c)
			}
		}
	} else {
		fmt.Fprintf(&b, "issue %s (#%03d %s) に取り組んでください。", t.Path, t.Number, t.Title)
	}
	if strings.TrimSpace(extra) != "" {
		b.WriteString("\n\n補足:\n" + extra)
	}
	return b.String()
}

// PMPrompt は PM に渡す指示の全文。repo のタブから出した依頼には、その repo の中だけが対象である旨を前置きする。
// 本文は人間が書いたまま末尾に置く (前置きで言い換えない)。
func PMPrompt(r Repo, text string) string {
	if r.Name == "" {
		return "この依頼は repo を指定していません。どの repo の作業かを判断し、カードの repo を決めてください。" +
			"複数の repo にまたがるなら、repo ごとにカードを分けてください。\n\n依頼:\n" + text
	}
	return "この依頼のスコープは repo " + r.Name + " (" + r.Path + ") の中だけです。" +
		"この repo の外のファイルを読んだり変更したりしないでください。issue とカードもこの repo に作ってください。" +
		"\n\n依頼:\n" + text
}

// Btw は作業を止めずに状況を聞く (要件 9)。回答は Apply の戻り値の文面で返る。
type Btw struct {
	CardID   string
	Question string
}

// ClearDone は完了のレーンを片付ける (Repo が空なら全 repo)。カードは消さずに Archived にする
// (「依頼したタスクがどこに行ったか分からなくなる」を避ける。記録は状態ファイルに残る)。
type ClearDone struct {
	Repo string
}

func (ClearDone) isCommand()  {}
func (Answer) isCommand()     {}
func (AddOrder) isCommand()   {}
func (Btw) isCommand()        {}
func (NewRequest) isCommand() {}

var (
	ErrNotFound = errors.New("カードが見つからない")
	// ErrNotWaiting は回答の比較付き更新に負けたとき (もう質問待ちではない)。
	// PM と人間が同じ質問へ同時に答えても、2 回 resume しない (415 論点 8)。
	ErrNotWaiting  = errors.New("このカードは質問待ちではない (既に回答済みの可能性)")
	ErrEmptyText   = errors.New("本文が空")
	ErrNoSession   = errors.New("このカードには PG の session が無い")
	ErrNotActive   = errors.New("このカードは作業中ではない")
	ErrUnknownKind = errors.New("未知の操作")
)
