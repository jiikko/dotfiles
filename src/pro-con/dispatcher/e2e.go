package dispatcher

// e2e モード (2026-09-25 にユーザーが依頼): 画面・dispatcher・受付の箱・記録は本物のまま、PG の claude だけを台本どおりに動く偽物にする
// (利用枠を使わない)。Claude が `pro-con e2e` で画面を操作して、質問 → 回答 → テストの係 → レビュー → 終了を通しで確かめるため。
//
// 偽の PM (FakePM) が依頼の列のカードを、その場で分解済みにする (本物では PM の Claude の仕事)。
// 置き場 (Root) の中身: state/ (本物のモードの状態の置き場) / repo/ (偽の repo。PG の worktree はその下) / state/e2e-sessions.json (偽の session の一覧)。
// 偽の PG の台本 (1 つだけ):
//  1. 起動されたら「続けてよいですか」と質問する
//  2. 回答で再開されたら、テストの係に `echo e2e-ok` を頼む
//  3. テストの係の結果で再開されたら、レビューに出す
//
// 本物の claude と同じく、再開は別の session id・別の短い id の session を立てる (427 の 3f で実測した形に合わせる)。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/store"
)

// E2E は e2e モードの置き場。
type E2E struct{ Root string }

// E2ERepo は e2e モードの repo の名前。
const E2ERepo = "e2e"

func (e E2E) StateDir() string     { return filepath.Join(e.Root, "state") }
func (e E2E) RepoDir() string      { return filepath.Join(e.Root, "repo") }
func (e E2E) sessionsPath() string { return filepath.Join(e.StateDir(), "e2e-sessions.json") }

// e2eFile は偽の session の一覧 (画面と dispatcher が読む) と、session → カードの対応 (偽の PG が自分のカードを知るため)。
type e2eFile struct {
	Sessions []agents.Session  `json:"sessions"`
	Cards    map[string]string `json:"cards"` // 短い id → カードの ID
	Seq      int               `json:"seq"`
}

var e2eMu sync.Mutex // 同じプロセスの中の読み書きをまとめる (書き手は dispatcher だけ。画面は読むだけ)

func (e E2E) load() (e2eFile, error) {
	f := e2eFile{Cards: map[string]string{}}
	data, err := os.ReadFile(e.sessionsPath())
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("e2e の session の一覧が壊れている: %w", err)
	}
	if f.Cards == nil {
		f.Cards = map[string]string{}
	}
	return f, nil
}

func (e E2E) save(f e2eFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.StateDir(), 0o700); err != nil {
		return err
	}
	tmp := e.sessionsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.sessionsPath()) // 画面が書きかけを読まない
}

// FakePM は偽の PM: 依頼の列のカードを、その場で分解済みにする (本物では PM の Claude が issue に分けてキューに積む)。
// dispatcher の Tick の頭で呼ぶ (箱に置いた plan は同じ Tick の Apply で入る)。
func (e E2E) FakePM() error {
	st, err := store.Load(e.StateDir())
	if err != nil {
		return err
	}
	for _, c := range st.Cards {
		if c.State == card.Requested {
			if _, err := store.Submit(e.StateDir(), store.Request{Kind: "plan", CardID: c.ID,
				Issues: []card.IssueRef{{Repo: E2ERepo, Number: 1, Status: "open"}}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// List は偽の session の一覧 (`claude agents --json` の代わり)。
func (e E2E) List(context.Context) ([]agents.Session, error) {
	e2eMu.Lock()
	defer e2eMu.Unlock()
	f, err := e.load()
	return f.Sessions, err
}

// Launcher は台本どおりに動く偽の PG (Launcher)。
func (e E2E) Launcher() Launcher { return e2eLauncher{e} }

type e2eLauncher struct{ e E2E }

func (l e2eLauncher) Start(_ context.Context, repoPath, name, _ string) (string, error) {
	wt := filepath.Join(repoPath, ".claude", "worktrees", name) // claude --bg -w <name> と同じ置き場
	if err := os.MkdirAll(wt, 0o700); err != nil {
		return "", err
	}
	id, err := l.newSession(wt, name, cardOfName(name))
	if err != nil {
		return "", err
	}
	return id, l.act(cardOfName(name), wt, store.Request{Kind: "ask", Question: "続けてよいですか (e2e の偽の PG)"})
}

func (l e2eLauncher) Resume(_ context.Context, stopID, _, cwd, text string) (string, error) {
	e2eMu.Lock()
	f, err := l.e.load()
	e2eMu.Unlock()
	if err != nil {
		return "", err
	}
	cardID := f.Cards[stopID]
	if cardID == "" { // 止めずに再開 (前の session は一覧に無い): cwd の worktree からカードを知る
		cardID = cardOfName(filepath.Base(cwd))
	}
	if stopID != "" {
		if err := l.Stop(context.Background(), stopID); err != nil {
			return "", err
		}
	}
	id, err := l.newSession(cwd, "", cardID) // 再開は別の session を立てる (本物の claude の形)
	if err != nil {
		return "", err
	}
	req := store.Request{Kind: "run", Command: "echo e2e-ok", Cwd: cwd}
	if strings.Contains(text, "テストの係の結果") {
		req = store.Request{Kind: "review"}
	}
	return id, l.act(cardID, cwd, req)
}

func (l e2eLauncher) Stop(_ context.Context, id string) error {
	e2eMu.Lock()
	defer e2eMu.Unlock()
	f, err := l.e.load()
	if err != nil {
		return err
	}
	kept := f.Sessions[:0]
	for _, s := range f.Sessions {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	f.Sessions = kept
	return l.e.save(f)
}

func (l e2eLauncher) newSession(cwd, name, cardID string) (string, error) {
	e2eMu.Lock()
	defer e2eMu.Unlock()
	f, err := l.e.load()
	if err != nil {
		return "", err
	}
	f.Seq++
	id := fmt.Sprintf("e2e%05d", f.Seq)
	f.Sessions = append(f.Sessions, agents.Session{
		ID: id, SessionID: fmt.Sprintf("e2e-session-%05d", f.Seq), Kind: "background", Status: "idle", Name: name,
		Cwd: cwd, PID: 900000 + f.Seq, StartedAt: time.Now().UnixMilli(),
	})
	f.Cards[id] = cardID
	return id, l.e.save(f)
}

// act は偽の PG が `pro-con card ...` を打つ代わりに、受付の箱へ依頼を置く。
func (l e2eLauncher) act(cardID, cwd string, r store.Request) error {
	r.CardID = cardID
	if r.Kind == "run" {
		r.Cwd = cwd
	}
	_, err := store.Submit(l.e.StateDir(), r)
	return err
}

// cardOfName は PG の session の名前 (pc-c-001) からカードの ID (C-001) を戻す。
func cardOfName(name string) string {
	return strings.ToUpper(strings.TrimPrefix(name, "pc-"))
}
