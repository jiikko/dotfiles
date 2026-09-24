// Package backend は UI と「状態を持つ側」の境界。UI はこの package の型だけを知り、
// 実装 (今は fake。本番は状態ファイル + claude agents --json を読む daemon クライアント) を知らない。
//
// 状態の正本は backend 側にあり、UI は Snapshot を読むことと Command を送ることしかしない
// (415 要件 14: TUI が落ちても何も失わない)。
package backend

import (
	"errors"
	"os/exec"
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
	Now        time.Time
	Cards      []card.Card
	Consumers  []Consumer
	Limit      int       // PG の同時実行数の上限
	DaemonTick time.Time // daemon (と watchdog) が最後に回った時刻。古ければ UI が警告する
	Violations []card.Violation
}

// Consumer は PG (consumer) 1 体。
type Consumer struct {
	Session string
	CardID  string // 空なら待機中
	Status  string // busy / waiting / idle (claude agents --json の status と同じ語)
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

// Btw は作業を止めずに状況を聞く (要件 9)。回答は Apply の戻り値の文面で返る。
type Btw struct {
	CardID   string
	Question string
}

func (Answer) isCommand()   {}
func (AddOrder) isCommand() {}
func (Btw) isCommand()      {}

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
