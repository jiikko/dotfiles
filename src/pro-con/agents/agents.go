// Package agents は `claude agents --json` で、今動いている Claude Code の session を一覧する。
// 対話 session (Desktop / 端末) と裏で動く session (claude --bg) の両方が出る。
//
// 出力の形は Claude Code 2.1.281 で実測した (issue 415 の論点 2)。版の保証は無いので、読めなかったときは
// 「0 本」として扱わず、エラーとして返す (UI は失敗の理由をそのまま出す)。
package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Session struct {
	ID        string `json:"id"` // 裏の session の短い id (claude attach <id> に渡す)。対話 session には無い
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`   // interactive / background
	Status    string `json:"status"` // idle / busy / waiting
	// State は session の状態 (working / blocked / failed / stopped / done。425 の実測)。止まったかは Stopped で判じる (pid と合わせて見る)
	State      string `json:"state"`
	WaitingFor string `json:"waitingFor"`
	Name       string `json:"name"`
	Cwd        string `json:"cwd"`
	PID        int    `json:"pid"`
	StartedAt  int64  `json:"startedAt"` // epoch ms
}

func (s Session) Started() time.Time { return time.UnixMilli(s.StartedAt) }

// Runner は `claude agents --json` を実行し、stdout と stderr を分けて返す (テストで差し替える)。
type Runner func(ctx context.Context) (stdout, stderr []byte, err error)

// Timeout は 1 回の一覧の上限 (普段の実測は 0.14〜0.16 秒。PG を起動・再開している最中に 3 秒を超えたことがある = 427 の 3f)。
const Timeout = 10 * time.Second

var ErrEmptyOutput = errors.New("claude agents --json の出力が空")

// List は session の一覧を返す。コマンドの失敗・timeout・空の出力・壊れた JSON はすべてエラー (0 本と区別する)。
func List(ctx context.Context, run Runner) ([]Session, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out, errOut, err := run(ctx)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("claude agents --json が %v 以内に終わらない", Timeout)
	}
	if err != nil {
		if msg := firstLine(errOut); msg != "" {
			return nil, fmt.Errorf("claude agents --json が失敗: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("claude agents --json が失敗: %w", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, ErrEmptyOutput
	}
	var ss []Session
	if err := json.Unmarshal(out, &ss); err != nil {
		return nil, fmt.Errorf("claude agents --json の出力を読めない (版が変わった?): %w", err)
	}
	return ss, nil
}

// StatusIdle は turn を終えて次の入力を待つ session の Status (425 の実測: 正常に終えた turn / API エラーで落ちた turn)。
const StatusIdle = "idle"

// StatusBusy は turn の途中の session の Status。
const StatusBusy = "busy"

// StatusWaiting は turn の途中で人の入力を待って止まった session の Status (権限の確認 / AskUserQuestion)。
// 2.1.283 で実測 (2026-09-26。C-054): 起動の直後 = status なし・pid なし・working / 作業中 = busy・working・waitingFor なし /
// 権限の確認 = waiting・blocked・"permission prompt" / AskUserQuestion = waiting・blocked・"input needed" /
// attach して答えた直後 = busy・working・waitingFor なし / turn を終えた = idle・done・waitingFor なし。
// 🚨 state で判じない: turn を終えた session の state は版で変わった (2.1.281 = blocked、2.1.283 = done) ので、入力待ちの印にならない
const StatusWaiting = "waiting"

// Waiting は人の入力を待って止まっているか (attach して答えるまで動かない)。PG と役 (PM / 取り込みの係) の入力待ちはこれだけで判じる。
func (s Session) Waiting() bool { return s.Status == StatusWaiting }

// waitingFor の実測の値 (StatusWaiting の実測を見る)。
const (
	waitingPermission = "permission prompt"
	waitingInput      = "input needed"
)

// WaitingLabel は入力待ちの中身の短い名前 (waitingFor から。知らない値はそのまま添える)。判定には使わない (文面だけ)。
func (s Session) WaitingLabel() string {
	switch s.WaitingFor {
	case waitingPermission:
		return "権限の確認"
	case waitingInput:
		return "AskUserQuestion の質問"
	case "":
		return "入力待ち"
	}
	return "入力待ち: " + s.WaitingFor
}

// StateStopped は `claude stop` で止めた session の State。
const StateStopped = "stopped"

// stateWorking は動いている (か、落ちて Claude Code が自動で再開する途中の) session の State。
const stateWorking = "working"

// stateDone は作業を終えた session の State。
const stateDone = "done"

// stateFailed は turn が落ちた session の State。pid があれば API エラーで止まった turn (プロセスは生きている)、
// pid 無しならプロセスごと消えた (マシンのクラッシュ・再起動の後。2.1.282 で実測 2026-09-26: 再起動から 30 分たっても
// pid 無し・failed のままで自動で再開しない。issue 482)
const stateFailed = "failed"

// Stopped は止まっている session か: プロセスが無く (pid 無し)、state が止まった形 (stopped / done / failed) の許可リストにある。
// 🚨 state の名前だけで決めない: 作業を終えた session (done) を claude stop すると、state は done のまま pid が無くなる
// (2.1.282 で実測 2026-09-25。dogfooding で、stopped だけを見ていた判定が止まったものを止め直し続けた)。
// 🚨 「working でない」で決めない: 再開の途中を表す state の名前が変わる / state の欄が無くなる版では、再開の途中の PG を
// 止まったと読み、閉じても終了でも止めない (issue 466)。許可リストに無い pid 無しは UnknownState (止めに行く側)
// 425 の実測: 正常に終えた (pid あり・blocked) / API エラー (pid あり・failed) / claude stop (pid 無し・stopped) /
// kill -9 (数秒 pid 無し・working のまま自動で再開)
func (s Session) Stopped() bool {
	return s.PID == 0 && (s.State == StateStopped || s.State == stateDone || s.State == stateFailed)
}

// UnknownState は、pid 無しで止まったとも自動の再開の途中 (working) とも判定できない session か (知らない state・state の欄が無い)。
// Stopped は偽 (止めに行く)。呼び出し側は止めるときに警告を出す
func (s Session) UnknownState() bool { return s.PID == 0 && !s.Stopped() && s.State != stateWorking }

// ExecRunner は本物の claude (claude は実体のパス) を呼ぶ (止めた session は出ない)。
func ExecRunner(claude string) func(context.Context) ([]byte, []byte, error) {
	return func(ctx context.Context) ([]byte, []byte, error) { return execAgents(ctx, claude) }
}

// ExecRunnerAll は止めた session も出す (`--all`。止めたことを確かめるのに使う。2.1.282 で実測 2026-09-25: `claude stop` した
// session は `--all` なしでは出ず、`--all` では state: stopped・pid 無しで残る。25 秒後も自動で再開しない)。
func ExecRunnerAll(claude string) func(context.Context) ([]byte, []byte, error) {
	return func(ctx context.Context) ([]byte, []byte, error) { return execAgents(ctx, claude, "--all") }
}

func execAgents(ctx context.Context, claude string, extra ...string) ([]byte, []byte, error) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, claude, append([]string{"agents", "--json"}, extra...)...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
