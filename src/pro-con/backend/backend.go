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

	"pro-con/agents"
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
	Screens        []Screen  // 開いている画面 (持ち主と join。自分を含む。開いた順。package presence。空なら数えられなかった / 模擬)
	DispatcherHeld bool      // 人が dispatcher を止めた印がある (pro-con dispatcher --stop。画面は起こさない。issue 459)
	DispatcherGone bool      // 最後に動いた dispatcher のプロセスが居ない (ロックの pid。Tick の古さを待たずに出す。issue 483)
	// Startup は dispatcher の起動時の確かめの要約 (起動から 10 分だけ。issue 483)。StartupAlert は復旧した・判定できないものがある
	Startup      string
	StartupAlert bool
	Roles        card.Roles       // dispatcher が起こさない役 (最後に回ったときの値)。人の番の目印 (card.Turn) に使う
	RoleStates   []card.RoleState // dispatcher が起こす役 (PM・取り込みの係) の最後に回ったときの様子。ゲージと依頼の列の印 (issue 476)
	Violations   []card.Violation
}

// DispatcherStale はこれより長く回っていなければ dispatcher が止まっている疑いとする (Tick は数秒ごと)。
// 止まった dispatcher の最後の様子 (役の様子・絞りの理由) は、画面も card list も今の様子として出さない。
const DispatcherStale = 2 * time.Minute

// Screen は開いている画面 1 つ (package presence の印。見ているだけの画面 = --view は入らない。issue 481)。
type Screen struct {
	ID     string // 短い画面 ID
	Join   bool   // 加わった画面 (pro-con --join)。偽なら持ち主
	Label  string // --as <ラベル> (任意)
	TTY    string // 端末 (取れなければ空)
	PID    int
	Opened time.Time
	Self   bool // この画面
}

// ScreenTally は開いている画面を、持ち主と join に分けて数える (自分を含む)。
func (s Snapshot) ScreenTally() (owners, joins int) {
	for _, sc := range s.Screens {
		if sc.Join {
			joins++
		} else {
			owners++
		}
	}
	return owners, joins
}

// Consumer は PG (consumer) 1 体。
type Consumer struct {
	Session string
	CardID  string // 空なら待機中
	Status  string // busy / waiting / idle (claude agents --json の status と同じ語)
	PID     int    // 実体のプロセス (本物だけ。模擬は 0)
}

// Idle は PG が turn を終えて次の入力を待っているか (一覧の status が idle)。
func (c Consumer) Idle() bool { return c.Status == agents.StatusIdle }

// SlotsUsed は一覧 (Consumers) の PG のうち枠を使っている数 (dispatcher と同じ判定 card.HoldsPGSlot。issue 455)。
// テストの係の結果を待って idle の PG は、一覧には居るが数えない。
// 🚨 母数は dispatcher と違う: dispatcher は一覧に居ない作業中の PG と、起動の結果が分からない (Launching) カードも数える
func (s Snapshot) SlotsUsed() int {
	cards := map[string]card.Card{}
	for _, c := range s.Cards {
		cards[c.ID] = c
	}
	n := 0
	for _, pg := range s.Consumers {
		if card.HoldsPGSlot(cards[pg.CardID], pg.Idle()) {
			n++
		}
	}
	return n
}

// AttachRecorder は attach の間に人間が PG へ打った指示をカードの履歴へ残す backend (任意。持たない backend は残さない)。
// 画面は attach から戻ったら、明け渡した時刻 from と戻った時刻 to を渡して裏で呼ぶ。返すのは残すよう頼んだ指示の数。
// 🚨 要約しない (人間の発言を原文で残す。issue 428)。
type AttachRecorder interface {
	RecordAttach(cardID, sessionID string, from, to time.Time) (int, error)
}

// Activity は PG の活動 1 つ (応答の文か道具の呼び出し。transcript から読む。issue 467)。
// 思考 (thinking) は Claude Code が中身をほぼ保存しないので無い。道具の結果も持たない (長い。何をしたかは呼び出しで分かる)。
type Activity struct {
	At      time.Time `json:"at"`
	Session string    `json:"session"`        // どの session の活動か (pro-con の短い id。再開で入れ替わると変わる)
	Tool    string    `json:"tool,omitempty"` // 道具の名前 (Bash / Edit …)。空なら応答の文
	Text    string    `json:"text"`           // 応答の文 (改行を残した markdown。486)、または道具の呼び出しの要点 (1 行)。どちらも制御文字なし
}

// Line は Text に道具の名前を前置きした 1 行 (「Bash: go test ./...」)。応答の文はそのまま。
func (a Activity) Line() string {
	if a.Tool == "" {
		return a.Text
	}
	return a.Tool + ": " + a.Text
}

// ActivityReader は作業中のカードの PG の活動を読む backend (任意。持たない backend (模擬) は出力の末尾だけを出す)。
// 返すのはそのカードのために pro-con が起動した session (再開で入れ替わった前の session も) の活動を古い順に、末尾の上限まで。
// 🚨 読むだけ (--view でも使う。受付の箱・記録・PG の session に何も書かない)。transcript を読むので画面は裏で呼ぶ。
type ActivityReader interface {
	Activity(cardID string) ([]Activity, error)
}

// ReadOnly は読み取りだけの backend (pro-con --view)。画面は終了の見出しを「見ているだけ」にする (止める口・書く口は持たない)。
type ReadOnly interface{ ReadOnly() }

// Joiner は加わった画面 (pro-con --join) の backend。読み書きするが、dispatcher を起こさず、quit で閉じても止めない (issue 481)。
type Joiner interface{ Joined() }

// Rejected は、この画面が受付の箱に置いた依頼を dispatcher が除けたこと (理由は dispatcher が書いたまま。issue 481)。
type Rejected struct {
	Kind   string // 依頼の種類 (answer / delete …)
	CardID string // 対象のカード (無ければ空)
	Why    string
}

// RejectReader は、この画面が置いた依頼のうち除けられたものを渡す backend (任意)。渡した分は忘れる (画面は 1 度だけ出す)。
// 🚨 除けた理由は dispatcher が記録に書いたものだけを渡す (カードの変化から推測しない = 誤った知らせになる)
type RejectReader interface{ TakeRejected() []Rejected }

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
	OpDelete Op = "delete" // カードを削除する (d)
	OpMove   Op = "move"   // レーンの中の並び (優先度) を入れ替える (K / J)
	OpResume Op = "resume" // 人が止めた dispatcher を起こす (c)
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

// KeptRunning は、ほかの持ち主の画面が開いている / この画面が join なので dispatcher と PG を止めずに閉じたこと
// (StopAll の結果。失敗ではない)。Others はほかに開いている持ち主の画面の数。
type KeptRunning struct { //nolint:errname // 失敗ではなく StopAll の結果 (error の経路で返すだけ)。KeptRunningError と名付けると失敗に読める
	Others int
	Join   bool // この画面が join (pro-con --join)
}

func (k KeptRunning) Error() string {
	if k.Join {
		return "join の画面なので、dispatcher と PG は止めずに閉じた"
	}
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

// DeleteCard はカードを消す (issue 451)。依頼の列のカードはすぐ消え、それ以外は PG の session を止めてから消える
// (それまでカードは「削除中」で残る)。🚨 PG の worktree とブランチは消さない。
type DeleteCard struct {
	CardID string
	From   string // 人間 / PM
}

// MoveCard はカードをレーンの中で 1 つ上 (Delta = -1) / 下 (+1) の隣と入れ替える (issue 470。上ほど優先)。Repo は画面のタブ
// (空なら global = 全 repo の並びの中の隣)。隣は backend が適用の時点の並びで決める (押した回数だけ動く)。
type MoveCard struct {
	CardID string
	Repo   string
	Delta  int
	Seen   time.Time // 画面が見ていたカードの Since。適用までに列を移っていたら動かさない (見ていない列で入れ替えない)
}

func (MoveCard) isCommand() {}

// ResumeDispatcher は人が止めた dispatcher を起こす (止めた印を外して起こす。issue 459)。
type ResumeDispatcher struct{}

func (ResumeDispatcher) isCommand() {}

func (DeleteCard) isCommand() {}
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
	ErrDeleting    = errors.New("このカードは削除の依頼を受けている (PG を止めてから消す)")
)
