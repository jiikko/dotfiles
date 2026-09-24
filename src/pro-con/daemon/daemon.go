// Package daemon は本物のモードの dispatcher (issue 427 の段階 3c-1)。1 回の Tick で:
//
//  1. 受付の箱を記録へ適用する (store.Apply)
//  2. 起動した PG の session を pro-con の記録 (live.Register) に登録する (session id と pid が一覧に出てから)
//  3. 分解済みのカードに、上限まで PG を割り当てる。回答を受けたカード (Resume が有る) は同じ session を再開し、それ以外は新しく起動する
//
// 書き手は daemon だけ (426 の決定 1)。PG の起動と再開は Launcher に任せ、テストでは偽物に差し替える。
// 落ちた回数で止める・watchdog は 3c-2、常駐と排他は 3c-3。
package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// Launcher は PG の session を起動・再開する口。
type Launcher interface {
	// Start は repoPath で PG を起動し、claude --bg が返す短い id を返す。name は worktree と session の名前
	Start(ctx context.Context, repoPath, name, prompt string) (id string, err error)
	// Resume は止めてから同じ session を text を渡して再開する (実行中の session に --resume するとコピーが起動するため。415 論点 11)
	Resume(ctx context.Context, id, sessionID, text string) error
}

// Daemon は dispatcher の 1 つ。
type Daemon struct {
	Dir    string            // 本物のモードの状態の置き場 (記録・箱・pro-con が起動した session の記録)
	Limit  int               // 同時に動かす PG の上限
	Repos  map[string]string // repo の名前 → 絶対パス (カードの Repo から起動先を決める)
	Launch Launcher
	List   func(context.Context) ([]agents.Session, error)
	Now    func() time.Time

	// pending は daemon 自身が起動・再開して、まだ記録を書き直していないカード。pid が変わった session の記録を書き直してよいのは
	// これにあるカードだけ (外の shell で同じ session を再開したもの = pid が違う、を取り込まない。424 の敵対的レビューの P2)
	pending map[string]bool
}

// Tick は 1 回ぶんの仕事をして、何をしたかの短い記録を返す (ログ用)。
func (d *Daemon) Tick(ctx context.Context) ([]string, error) {
	now := d.Now()
	var notes []string
	res, err := store.Apply(d.Dir, now)
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		if r.Err != "" {
			notes = append(notes, fmt.Sprintf("箱の依頼 %s (%s) を除けた: %s", r.ID, r.Kind, r.Err))
		}
	}
	if d.List != nil {
		if ss, err := d.List(ctx); err != nil {
			notes = append(notes, "session の一覧を取れない (登録は次の Tick へ): "+err.Error())
		} else if n, warn, err := d.register(ss); err != nil {
			return notes, err
		} else {
			if n > 0 {
				notes = append(notes, fmt.Sprintf("PG の session を %d 本登録した", n))
			}
			notes = append(notes, warn...)
		}
	}
	more, err := d.dispatch(ctx, now)
	return append(notes, more...), err
}

// register は作業中のカードの session (短い id) が一覧に出ていれば、session id と pid を添えて pro-con の記録に書く。
// 起動の直後は一覧にまだ出ないことがあるので、出るまで毎回見る。
//   - 最初の登録: session の起動時刻がカードの起動 (作業中になった時刻) より前なら取り込まない (同じ短い id の古い session)
//   - 書き直し (pid が変わった): daemon 自身が再開した直後 (pending) だけ。それ以外は書き直さず、外から操作された疑いを知らせる
func (d *Daemon) register(ss []agents.Session) (int, []string, error) {
	st, err := store.Load(d.Dir)
	if err != nil {
		return 0, nil, err
	}
	regPath := filepath.Join(d.Dir, live.RegistryFile)
	reg, err := live.LoadRegistry(regPath)
	if err != nil {
		return 0, nil, err
	}
	var warn []string
	known := map[string]live.Owned{}
	for _, o := range reg {
		known[o.CardID] = o
	}
	n := 0
	for _, c := range st.Cards {
		if c.State != card.Running || c.Session == "" {
			continue
		}
		for _, s := range ss {
			if s.ID != c.Session || s.SessionID == "" || s.PID == 0 {
				continue
			}
			o, ok := known[c.ID]
			switch {
			case ok && o.SessionID == s.SessionID && o.PID == s.PID:
				continue // 登録済み
			case ok && !d.pending[c.ID]:
				warn = append(warn, fmt.Sprintf("%s の session %s の pid が %d → %d に変わった。pro-con は再開していない (外から操作された疑い)。記録は書き直さない",
					c.ID, s.ID, o.PID, s.PID))
				continue
			case !ok && !d.pending[c.ID] && s.Started().Before(c.Since):
				warn = append(warn, fmt.Sprintf("%s の session %s はカードの起動より前に始まっている (同じ短い id の別の session の疑い)。登録しない", c.ID, s.ID))
				continue
			}
			if err := live.Register(regPath, live.Owned{SessionID: s.SessionID, ID: s.ID, PID: s.PID, CardID: c.ID, StartedAt: s.Started()}); err != nil {
				return n, warn, err
			}
			delete(d.pending, c.ID)
			n++
		}
	}
	return n, warn, nil
}

// dispatch は分解済みのカードに、作業中が上限に達するまで PG を割り当てる (古い順)。起動・再開に失敗したカードは分解済みのまま残し、
// 失敗を履歴に書く (次の Tick でまた試す。回数で止めるのは 3c-2)。
func (d *Daemon) dispatch(ctx context.Context, now time.Time) ([]string, error) {
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	running := 0
	var queue []card.Card
	for _, c := range st.Cards {
		switch c.State {
		case card.Running:
			running++
		case card.Planned:
			queue = append(queue, c)
		case card.Requested, card.Waiting, card.Review, card.Done:
		}
	}
	sort.SliceStable(queue, func(i, j int) bool { return queue[i].Since.Before(queue[j].Since) })
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return nil, err
	}
	var notes []string
	for _, c := range queue {
		if running >= d.Limit {
			break
		}
		id, how, launchErr := d.launch(ctx, c, reg)
		if err := store.Update(d.Dir, func(s *store.State) error {
			for i := range s.Cards {
				if s.Cards[i].ID != c.ID {
					continue
				}
				if launchErr != nil {
					s.Cards[i].History = append(s.Cards[i].History, card.Event{At: now, Text: how + "に失敗した: " + launchErr.Error()})
					return nil
				}
				s.Cards[i].State, s.Cards[i].Since, s.Cards[i].Owner, s.Cards[i].Session = card.Running, now, "PG", id
				s.Cards[i].LastProgress, s.Cards[i].Resume = now, ""
				s.Cards[i].History = append(s.Cards[i].History, card.Event{At: now, Text: "PG を" + how + "した (session " + id + ")"})
			}
			return nil
		}); err != nil {
			return notes, err
		}
		if launchErr != nil {
			notes = append(notes, fmt.Sprintf("%s の PG の%sに失敗: %v", c.ID, how, launchErr))
			continue
		}
		running++
		if d.pending == nil {
			d.pending = map[string]bool{}
		}
		d.pending[c.ID] = true // 次の登録で、この起動・再開の session を記録に書いてよい
		notes = append(notes, fmt.Sprintf("%s に PG を%sした (%s)", c.ID, how, id))
	}
	return notes, nil
}

// launch は 1 枚のカードの PG を起動か再開する。回答を受けたカード (Resume が有り、前の session が記録にある) は再開する。
func (d *Daemon) launch(ctx context.Context, c card.Card, reg []live.Owned) (id, how string, err error) {
	if c.Resume != "" && c.Session != "" {
		for _, o := range reg {
			if o.CardID == c.ID && o.ID == c.Session {
				return c.Session, "再開", d.Launch.Resume(ctx, c.Session, o.SessionID, c.Resume)
			}
		}
		return "", "再開", fmt.Errorf("前の session (%s) が pro-con の記録に無い", c.Session)
	}
	path, ok := d.Repos[c.Repo]
	if !ok {
		return "", "起動", fmt.Errorf("repo %q の場所が設定に無い", c.Repo)
	}
	id, err = d.Launch.Start(ctx, path, "pc-"+strings.ToLower(c.ID), Prompt(c))
	return id, "起動", err
}

// Prompt は PG に渡す最初の指示。PG の規律 (426 の決定 2・3) を前に置き、依頼の中身を後ろに置く。
func Prompt(c card.Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "あなたは pro-con の PG (作業担当) です。担当はカード %s「%s」。\n", c.ID, c.Title)
	b.WriteString("規律:\n")
	b.WriteString("- 作業は自分の worktree で行い、commit は自分のブランチまで push する (master へは push しない)\n")
	fmt.Fprintf(&b, "- 質問があるときは AskUserQuestion を使わず、`pro-con card ask %s \"<質問>\"` を実行してから turn を終える (回答は再開のときに届く)\n", c.ID)
	fmt.Fprintf(&b, "- 終えたら `pro-con card review %s` を実行してから turn を終える\n", c.ID)
	if len(c.Issues) > 0 {
		var refs []string
		for _, r := range c.Issues {
			refs = append(refs, r.String())
		}
		fmt.Fprintf(&b, "\n関わる issue: %s\n", strings.Join(refs, ", "))
	}
	if c.Prompt != "" {
		b.WriteString("\n指示:\n" + c.Prompt + "\n")
	} else if c.Request != "" {
		b.WriteString("\n依頼の原文:\n" + c.Request + "\n")
	}
	return b.String()
}
