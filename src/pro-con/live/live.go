// Package live は本物の backend (issue 424 / 427 の段階 3d)。カードは daemon が書く記録 (store) から読み、作業中のカードには
// **pro-con が起動した session** (registry.go の記録にあるもの) の様子 (PG の出力の末尾・pid) を `claude agents --json` と transcript から足す。
// Desktop や他の shell で立ち上げた session は出さない (選べると、pro-con の外の session に入力・停止できてしまう)。
//
// 書き込みは受付の箱に置くだけ (store.Submit)。記録へ適用するのは daemon (426 の決定 1)。受けるのは新しい依頼と回答だけで、
// 追加オーダー・btw・片付けはまだ受けない (Accepts。画面は押した時点で断る)。
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
	"pro-con/store"
)

// Interval は一覧と transcript を読み直す間隔。claude agents --json は 1 回 0.15 秒ほどかかるので、画面の tick (1 秒) では呼ばない。
const Interval = 3 * time.Second

// ErrNotYet は本物のモードでまだ受けない操作 (追加オーダー・btw・片付け)。
var ErrNotYet = errors.New("本物のモードではまだ使えない操作 (issue 427)。模擬で試すなら pro-con --mock")

// Backend は本物の backend。
type Backend struct {
	repos    []backend.Repo
	dir      string // 本物のモードの状態の置き場 (カードの記録・受付の箱・pro-con が起動した session の記録)
	registry string // pro-con が起動した session の記録 (registry.go)
	list     func(context.Context) ([]agents.Session, error)
	findPath func(sessionID string) (string, error)
	read     func(path string) (Transcript, error)
	now      func() time.Time

	mu      sync.Mutex
	snap    backend.Snapshot
	pending int  // 受付の箱の適用待ちの数 (daemon が動いていないと溜まる。ヘッダーに出す)
	ready   bool // 最初の読み取りが済んだか
	done    chan struct{}
	cache   map[string]cached // transcript のパス → 大きさ・更新時刻と読んだ結果 (変わっていなければ読み直さない)
	paths   map[string]string // sessionId → transcript のパス
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
		dir:      stateDir,
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

// Refresh はカードの記録を読み、作業中のカードに pro-con が起動した session の様子を足して Snapshot を作り直す。
// 🚨 1 つの goroutine からだけ呼ぶ (Start の中)。transcript のキャッシュ (cache / paths) は lock の外で触っている。
// 記録を読めなければ前の Snapshot を残して理由を出す (0 枚と区別する)。session の一覧を取れないときは、カードは出して理由を足す。
func (b *Backend) Refresh(ctx context.Context) {
	st, err := store.Load(b.dir)
	if err != nil {
		b.fail("カードの記録を読めない: " + err.Error())
		return
	}
	reg, err := LoadRegistry(b.registry)
	if err != nil {
		b.fail("pro-con が起動した session の記録を読めない (" + b.registry + "): " + err.Error())
		return
	}
	now := b.now()
	var extra []card.Violation
	ss, err := b.list(ctx)
	if err != nil {
		extra = append(extra, card.Violation{Reason: "session の一覧を取れない (PG の様子は古いまま): " + err.Error()})
	}
	owned := map[string]agents.Session{} // 短い id → pro-con が起動した session
	for _, s := range ss {
		if s.ID != "" && owns(reg, s.SessionID, s.ID, s.PID) {
			owned[s.ID] = s
		}
	}
	cards := append([]card.Card(nil), st.Cards...)
	var cons []backend.Consumer
	for i, c := range cards {
		cards[i].Request = clip(c.Request, requestRunes) // 貼り付けた巨大な依頼を、詳細の描画のたびに折り返さない
		s, ok := owned[c.Session]
		if c.Session == "" || !ok {
			continue // 記録に無い session (外のもの) の様子は足さない
		}
		if t := b.transcript(s.SessionID); len(t.Outputs) > 0 {
			cards[i].Log = tail(t.Outputs, 3)
			if t.LastAt.After(cards[i].LastProgress) {
				cards[i].LastProgress = t.LastAt
			}
		}
		if c.State == card.Running {
			cons = append(cons, backend.Consumer{Session: s.ID, CardID: c.ID, Status: s.Status, PID: s.PID})
		}
	}
	pending, _ := filepath.Glob(filepath.Join(b.dir, store.InboxDir, "*.json"))
	b.mu.Lock()
	b.snap = backend.Snapshot{Now: now, Cards: cards, Consumers: cons, Limit: len(cons), DaemonTick: now,
		Violations: append(card.Check(cards), extra...)}
	b.pending, b.ready = len(pending), true
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

func (b *Backend) Poll() backend.Snapshot { return b.Snapshot() }

func (b *Backend) Snapshot() backend.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.snap
	s.Cards = append([]card.Card(nil), b.snap.Cards...)
	return s
}

// Apply は受付の箱に依頼を置く (記録へ適用するのは daemon)。受けるのは新しい依頼と回答だけ。
func (b *Backend) Apply(cmd backend.Command) (string, error) {
	var r store.Request
	switch c := cmd.(type) {
	case backend.NewRequest:
		if strings.TrimSpace(c.Text) == "" && c.Issue == nil {
			return "", backend.ErrEmptyText
		}
		prompt := backend.PMPrompt(c.Repo, c.Text)
		title := clip(firstLine(c.Text), 40)
		if c.Issue != nil {
			prompt = backend.PMPrompt(c.Repo, backend.IssuePrompt(*c.Issue, c.Text))
			title = fmt.Sprintf("#%03d %s", c.Issue.Number, c.Issue.Title)
		}
		r = store.Request{Kind: "add", Title: title, Request: c.Text, Prompt: prompt, Repo: c.Repo.Name, Owner: "PM"}
	case backend.Answer:
		if strings.TrimSpace(c.Text) == "" {
			return "", backend.ErrEmptyText
		}
		r = store.Request{Kind: "answer", CardID: c.CardID, Answer: c.Text, From: firstNonEmpty(c.From, "人間")}
	default:
		return "", ErrNotYet
	}
	if _, err := store.Submit(b.dir, r); err != nil {
		return "", err
	}
	return "受付の箱に置いた (daemon が適用する。pro-con daemon が動いていなければ進まない)", nil
}

// Accepts は本物のモードで受ける操作 (backend.Accepter)。新しい依頼と回答だけ。
func (b *Backend) Accepts(op backend.Op) bool { return op == backend.OpNew || op == backend.OpAnswer }

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
	case b.pending > 0:
		return fmt.Sprintf("live: 受付の箱に適用待ち %d 件 (pro-con daemon が動いていない? 模擬は pro-con --mock)", b.pending)
	}
	return "live: 本物のカード (依頼と回答は受付の箱へ。追加オーダー・btw・片付けはまだ。模擬は pro-con --mock)"
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

func tail[T any](xs []T, n int) []T {
	if len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}
