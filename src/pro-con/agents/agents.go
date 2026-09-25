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
	// State は session の状態 (working / blocked / failed / stopped。425 の実測)。stopped だけが「止めた」で、それ以外は
	// プロセスが生きているか、落ちて Claude Code が自動で再開する途中 (pid が 0 で working)
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
			return nil, fmt.Errorf("claude agents --json が失敗: %v: %s", err, msg)
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

// StateStopped は `claude stop` で止めた session の State。
const StateStopped = "stopped"

// Stopped は止めた session か (自動で再開しない)。
func (s Session) Stopped() bool { return s.State == StateStopped }

// ExecRunner は本物の claude を呼ぶ (止めた session は出ない)。
func ExecRunner(ctx context.Context) ([]byte, []byte, error) { return execAgents(ctx) }

// ExecRunnerAll は止めた session も出す (`--all`。止めたことを確かめるのに使う。2.1.282 で実測 2026-09-25: `claude stop` した
// session は `--all` なしでは出ず、`--all` では state: stopped・pid 無しで残る。25 秒後も自動で再開しない)。
func ExecRunnerAll(ctx context.Context) ([]byte, []byte, error) { return execAgents(ctx, "--all") }

func execAgents(ctx context.Context, extra ...string) ([]byte, []byte, error) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "claude", append([]string{"agents", "--json"}, extra...)...)
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
