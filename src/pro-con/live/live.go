// Package live は読み取り専用の本物の backend (issue 424)。**pro-con が起動した session だけ** (registry.go の記録にあるもの) を
// `claude agents --json` と transcript から読み、session 1 本をカード 1 枚として出す。書き込みは持たない (Apply は拒否)。
// Desktop や他の shell で立ち上げた session は出さない (選べると、pro-con の外の session に入力・停止できてしまう)。
//
// 🚨 session 1 本 = カード 1 枚で、415 の不変条件「カード = 人間の依頼 1 件」とは別の単位 (依頼を分けてもいない)。
// 本物の PM / PG の backend (427) ができたら、そちらのカードに置き換わる。
package live

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pro-con/agents"
	"pro-con/backend"
	"pro-con/card"
)

// Interval は一覧と transcript を読み直す間隔。claude agents --json は 1 回 0.15 秒ほどかかるので、画面の tick (1 秒) では呼ばない。
const Interval = 3 * time.Second

// ErrReadOnly は書き込みの操作 (回答・依頼・追加オーダー 等) を拒否するときのエラー。
var ErrReadOnly = errors.New("読み取り専用の backend (本物の PM / PG はまだ無い。issue 427)。模擬で試すなら pro-con --mock")

// Backend は読み取り専用の本物の backend。
type Backend struct {
	repos    []backend.Repo
	registry string // pro-con が起動した session の記録 (registry.go)
	list     func(context.Context) ([]agents.Session, error)
	findPath func(sessionID string) (string, error)
	read     func(path string) (Transcript, error)
	now      func() time.Time

	mu    sync.Mutex
	snap  backend.Snapshot
	owned int  // 記録にある session の数 (ヘッダーの説明に出す)
	ready bool // 最初の読み取りが済んだか
	done  chan struct{}
	cache map[string]cached // transcript のパス → 大きさ・更新時刻と読んだ結果 (変わっていなければ読み直さない)
	paths map[string]string // sessionId → transcript のパス
}

type cached struct {
	size  int64
	mtime time.Time
	t     Transcript
}

// New は本物の claude と ~/.claude/projects を読む backend を作る。stateDir は本物のモードの状態の置き場 (記録はその下)。
// Start で読み直しを始める。
func New(repos []backend.Repo, home, stateDir string) *Backend {
	projects := filepath.Join(home, ".claude", "projects")
	now := time.Now()
	return &Backend{
		repos:    repos,
		registry: filepath.Join(stateDir, RegistryFile),
		snap:     backend.Snapshot{Now: now, DaemonTick: now},
		done:     make(chan struct{}),
		list:     func(ctx context.Context) ([]agents.Session, error) { return agents.List(ctx, agents.ExecRunner) },
		findPath: func(id string) (string, error) {
			// ディレクトリ名は cwd から Claude Code が組む (規則を真似ず、sessionId で探す)
			ms, err := filepath.Glob(filepath.Join(projects, "*", id+".jsonl"))
			if err != nil || len(ms) == 0 {
				return "", os.ErrNotExist
			}
			return ms[0], nil
		},
		read:  ReadTail,
		now:   time.Now,
		cache: map[string]cached{},
		paths: map[string]string{},
	}
}

// Start は裏で読み直しを始める (最初の読み取りも裏で行う。claude agents --json は最大 3 秒待つので、画面を出す前に待たない)。
// ctx が終わったら止める。止まったかは Wait で待てる。
func (b *Backend) Start(ctx context.Context) {
	go func() {
		defer close(b.done)
		b.Refresh(ctx)
		t := time.NewTicker(Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				b.Refresh(ctx)
			}
		}
	}()
}

// Wait は Start の読み直しが止まるのを待つ (終了のとき。止まる前に抜けると、claude の子プロセスが残りうる)。
func (b *Backend) Wait() { <-b.done }

// Refresh は一覧と transcript を読み直して Snapshot を作り直す。🚨 1 つの goroutine からだけ呼ぶ (Start の中)。
// transcript のキャッシュ (cache / paths) は lock の外で触っている。一覧を取れなかったら、前の Snapshot を残して
// 取れなかった理由を Violations に出す (0 本と区別する。画面は件数を隠さない)。
func (b *Backend) Refresh(ctx context.Context) {
	reg, err := LoadRegistry(b.registry)
	if err != nil {
		b.fail("pro-con が起動した session の記録を読めない (" + b.registry + "): " + err.Error())
		return
	}
	ss, err := b.list(ctx)
	now := b.now()
	if err != nil {
		b.fail("session の一覧を取れない: " + err.Error())
		return
	}
	cards := make([]card.Card, 0, len(ss))
	var cons []backend.Consumer
	for _, s := range ss {
		if !owns(reg, s.SessionID, s.ID, s.PID) {
			continue // pro-con が起動していない session は出さない
		}
		c := b.toCard(s, b.transcript(s.SessionID))
		cards = append(cards, c)
		if s.Kind == "background" {
			cons = append(cons, backend.Consumer{Session: s.ID, CardID: c.ID, Status: s.Status, PID: s.PID})
		}
	}
	b.mu.Lock()
	b.snap = backend.Snapshot{Now: now, Cards: cards, Consumers: cons, Limit: len(cons), DaemonTick: now, Violations: card.Check(cards)}
	b.owned, b.ready = len(reg), true
	b.mu.Unlock()
}

// fail は読み取りに失敗したとき、前のカードを残して理由を出す (0 本と区別する。画面は件数を隠さない)。
func (b *Backend) fail(reason string) {
	b.mu.Lock()
	b.snap.Violations = []card.Violation{{Reason: reason}}
	b.ready = true
	b.mu.Unlock()
}

// transcript は sessionId の transcript の末尾 (大きさ・更新時刻が前と同じなら読み直さない)。見つからなければ空。
func (b *Backend) transcript(id string) Transcript {
	p, ok := b.paths[id]
	if !ok {
		var err error
		if p, err = b.findPath(id); err != nil {
			return Transcript{}
		}
		b.paths[id] = p
	}
	st, err := os.Stat(p)
	if err != nil {
		delete(b.paths, id)
		return Transcript{}
	}
	if c, ok := b.cache[p]; ok && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.t
	}
	t, err := b.read(p)
	if err != nil {
		return Transcript{}
	}
	b.cache[p] = cached{size: st.Size(), mtime: st.ModTime(), t: t}
	return t
}

// toCard は session 1 本をカードにする。
func (b *Backend) toCard(s agents.Session, t Transcript) card.Card {
	c := card.Card{
		ID:           "S-" + prefix(s.SessionID, 8),
		Title:        firstNonEmpty(t.Title, s.Name, clip(t.LastPrompt, 40), "(題名なし)"),
		Request:      clip(t.LastPrompt, requestRunes), // 貼り付けた巨大な依頼を詳細の描画のたびに折り返さない
		Repo:         b.repoOf(s.Cwd),
		Owner:        map[string]string{"interactive": "対話", "background": "裏"}[s.Kind],
		Since:        firstTime(t.LastAt, s.Started()),
		LastProgress: t.LastAt,
		Log:          tail(t.Outputs, 3),
	}
	if s.Kind == "background" {
		c.Session = s.ID // claude attach <id> に渡す短い id (対話 session は Desktop で開くので持たない)
	}
	for _, p := range tail(t.Prompts, 5) {
		c.History = append(c.History, card.Event{At: p.At, Text: "人間: " + clip(p.Text, 120)})
	}
	switch s.Status {
	case "busy":
		c.State = card.Running
	case "waiting":
		c.State = card.Waiting
		c.Wait = card.Wait{Kind: card.WaitQuestion, Question: "入力待ち (" + firstNonEmpty(s.WaitingFor, "理由不明") + ")"}
		if s.WaitingFor == "permission prompt" {
			c.Wait.Kind = card.WaitPermission
		}
	default: // idle = turn を終えて人の番
		c.State = card.Review
	}
	return c
}

// repoOf は cwd を含む repo の名前 (一番長く一致したもの)。どれにも入らなければ cwd の末尾のディレクトリ名。
func (b *Backend) repoOf(cwd string) string {
	best, bestLen := "", -1
	for _, r := range b.repos {
		if r.Path != "" && (cwd == r.Path || strings.HasPrefix(cwd, r.Path+string(filepath.Separator))) && len(r.Path) > bestLen {
			best, bestLen = r.Name, len(r.Path)
		}
	}
	if best != "" {
		return best
	}
	return filepath.Base(cwd)
}

func (b *Backend) Poll() backend.Snapshot { return b.Snapshot() }

func (b *Backend) Snapshot() backend.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.snap
	s.Cards = append([]card.Card(nil), b.snap.Cards...)
	return s
}

func (b *Backend) Apply(backend.Command) (string, error) { return "", ErrReadOnly }

// AttachCommand は裏の session を claude attach で開く。対話 session (Desktop) には attach の口が無い。
// 🚨 撃つ直前に一覧を取り直し、記録と照合し直す (最大 3 秒前の一覧の短い id のまま撃つと、その間に入れ替わった外の session へ attach しうる)。
// 照合し直してから claude attach が id を解決するまでの窓は閉じられない (claude attach は短い id で引き直す。pid では固定できない)。
// 一覧の取り直しで最大 3 秒待つので画面は裏で呼び (ui の attach)、窓は「照合の終わり → 画面の Update → 端末の明け渡し」まで延びる。
func (b *Backend) AttachCommand(sessionID string) (*exec.Cmd, error) {
	if sessionID == "" {
		return nil, errors.New("対話の session は Desktop で開く (attach できるのは claude --bg の session だけ)")
	}
	reg, err := LoadRegistry(b.registry)
	if err != nil {
		return nil, fmt.Errorf("pro-con が起動した session の記録を読めない: %w", err)
	}
	ss, err := b.list(context.Background())
	if err != nil {
		return nil, err
	}
	for _, s := range ss {
		if s.ID == sessionID && owns(reg, s.SessionID, s.ID, s.PID) {
			return exec.Command("claude", "attach", sessionID), nil
		}
	}
	return nil, errors.New("pro-con が起動した session ではない (または終わった): " + sessionID)
}

// requestRunes は依頼の原文を詳細に出す上限 (文字数)。
const requestRunes = 2000

// Describe はヘッダーに出す、この backend の説明。
func (b *Backend) Describe() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case !b.ready:
		return "live: 読み込み中… (模擬は pro-con --mock)"
	case b.owned == 0:
		return "live: 読み取り専用。pro-con が起動した session はまだ無い (起動は issue 427。模擬は pro-con --mock)"
	}
	return fmt.Sprintf("live: 読み取り専用。pro-con が起動した session %d 本 (模擬は pro-con --mock)", b.owned)
}

// ReadOnly は書き込みの操作を受け付けないか (画面は該当の案内を暗くする)。
func (b *Backend) ReadOnly() bool { return true }

func prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func firstTime(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func tail[T any](xs []T, n int) []T {
	if len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}
