// Package daemon は本物のモードの dispatcher (issue 427 の段階 3c-1)。1 回の Tick で:
//
//  1. 受付の箱を記録へ適用する (store.Apply)
//  2. 起動した PG の session を pro-con の記録 (live.Register) に登録する (session id と pid が一覧に出てから)
//  3. 分解済みのカードに、上限まで PG を割り当てる。回答を受けたカード (Resume が有る) は同じ session を再開し、それ以外は新しく起動する
//
// 書き手は daemon だけ (426 の決定 1)。PG の起動と再開は Launcher に任せ、テストでは偽物に差し替える。
// 起動・再開の前に印を記録へ書き、結果は次の Tick で session の一覧と照らして確かめる (claude の「失敗」と実際が食い違う / 途中で落ちる)。
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
	// Resume は stopID の session を止めてから (空なら止めない) 同じ session を text を渡して再開し、claude --bg が返す短い id を返す
	// (実行中の session に --resume するとコピーが起動するため。415 論点 11。再開で短い id が変わるかは未実測なので、返った id を使う)
	Resume(ctx context.Context, stopID, sessionID, text string) (newID string, err error)
}

// launchGrace は起動・再開の結果を確かめられないとき (claude が失敗と返した / daemon が途中で落ちた)、一覧に出るのを待つ長さ。
// 過ぎても出なければ起動し直す (待たずに起動し直すと、立っていた session の上にもう 1 本増える)。
const launchGrace = time.Minute

// Daemon は dispatcher の 1 つ。
type Daemon struct {
	Dir    string            // 本物のモードの状態の置き場 (記録・箱・pro-con が起動した session の記録)
	Limit  int               // 同時に動かす PG の上限
	Repos  map[string]string // repo の名前 → 絶対パス (カードの Repo から起動先を決める)
	Launch Launcher
	List   func(context.Context) ([]agents.Session, error)
	Now    func() time.Time
}

// Tick は 1 回ぶんの仕事をして、何をしたかの短い記録を返す (ログ用)。
// session の一覧を取れない Tick は、登録も割り当てもしない (一覧と照らさずに起動・再開すると、立っている session を見落として増やす /
// 別の session を止める)。
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
	ss, err := d.List(ctx)
	if err != nil {
		return append(notes, "session の一覧を取れない (登録と割り当ては次の Tick へ): "+err.Error()), nil
	}
	n, warn, err := d.register(ss)
	if err != nil {
		return notes, err
	}
	if n > 0 {
		notes = append(notes, fmt.Sprintf("PG の session を %d 本登録した", n))
	}
	notes = append(notes, warn...)
	more, err := d.dispatch(ctx, now, ss)
	return append(notes, more...), err
}

// register は作業中のカードの session (短い id) が一覧に出ていれば、session id と pid を添えて pro-con の記録に書く。
// 起動の直後は一覧にまだ出ないことがあるので、出るまで毎回見る。取り込むのは daemon が最後に起動・再開した (LaunchedAt) 後に
// 始まった session だけで、書き直せるのは記録の行がそれより前のもの (= daemon 自身の再開の後) だけ。それ以外は外から操作された疑いを知らせる。
// 判定の材料は記録に置く (daemon が起動し直しても失わない)。
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
			case s.Started().Before(c.LaunchedAt):
				warn = append(warn, fmt.Sprintf("%s の session %s は pro-con の最後の起動・再開より前に始まっている (同じ短い id の別の session の疑い)。登録しない", c.ID, s.ID))
				continue
			case ok && o.SessionID != s.SessionID:
				warn = append(warn, fmt.Sprintf("%s の短い id %s が別の session (%s) を指している。登録しない", c.ID, s.ID, s.SessionID))
				continue
			case ok && !o.StartedAt.Before(c.LaunchedAt):
				warn = append(warn, fmt.Sprintf("%s の session %s の pid が %d → %d に変わった。pro-con は再開していない (外から操作された疑い)。記録は書き直さない",
					c.ID, s.ID, o.PID, s.PID))
				continue
			}
			if err := live.Register(regPath, live.Owned{SessionID: s.SessionID, ID: s.ID, PID: s.PID, CardID: c.ID, StartedAt: s.Started()}); err != nil {
				return n, warn, err
			}
			n++
		}
	}
	return n, warn, nil
}

// dispatch は分解済みのカードに、作業中が上限に達するまで PG を割り当てる (古い順)。
//   - 起動・再開の前に、印 (Launching) と時刻を記録に書く。結果が分かったら印を外して作業中にする
//   - 前の Tick の起動・再開の結果が分からないまま (印が残っている) のカードは、一覧で確かめる。立っていれば取り込み、
//     launchGrace を過ぎても出なければ起動し直す。待っている間は上限に数える
//   - 起動・再開の前提 (repo の場所 / 前の session の記録) が無いカードは、何も起動せずに履歴へ書く
func (d *Daemon) dispatch(ctx context.Context, now time.Time, ss []agents.Session) ([]string, error) {
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
	// 印の残ったカード (前の起動・再開の結果が分からない) は、上限の判定より先に片付ける。上限の後ろに置くと、実際に立っている PG を
	// 数えずに別のカードを起動する (上限を下げて起動し直したときも)
	var notes []string
	var fresh []card.Card
	for _, c := range queue {
		if c.Launching == "" {
			fresh = append(fresh, c)
			continue
		}
		if id, ok := adopt(c, ss, reg); ok {
			if err := d.settle(c.ID, now, c.Launching, id); err != nil {
				return notes, err
			}
			running++
			notes = append(notes, fmt.Sprintf("%s の PG の%sを一覧で確かめた (%s)", c.ID, c.Launching, id))
			continue
		}
		if now.Sub(c.LaunchedAt) < launchGrace {
			running++ // まだ一覧に出ていないだけかもしれない
			continue
		}
		fresh = append(fresh, c) // 待っても出なかった。起動・再開し直す (古い順は保つ)
	}
	for _, c := range fresh {
		if running >= d.Limit {
			break
		}
		how, run, err := d.prepare(c, ss, reg)
		if err != nil {
			if err := d.note(c.ID, now, how+"できない: "+err.Error()); err != nil {
				return notes, err
			}
			notes = append(notes, fmt.Sprintf("%s の PG を%sできない: %v", c.ID, how, err))
			continue
		}
		if err := d.mark(c.ID, now, how); err != nil {
			return notes, err
		}
		running++ // 失敗と返っても立っているかもしれないので、確かめるまで上限に数える
		id, launchErr := run(ctx)
		if launchErr != nil {
			if err := d.note(c.ID, now, how+"に失敗したと返った (立っているかもしれないので、一覧で確かめてから起動し直す): "+launchErr.Error()); err != nil {
				return notes, err
			}
			notes = append(notes, fmt.Sprintf("%s の PG の%sに失敗: %v", c.ID, how, launchErr))
			continue
		}
		if err := d.settle(c.ID, now, how, id); err != nil {
			return notes, err
		}
		notes = append(notes, fmt.Sprintf("%s に PG を%sした (%s)", c.ID, how, id))
	}
	return notes, nil
}

// resumes は回答を受けて、同じ session を再開するカードか。
func resumes(c card.Card) bool { return c.Resume != "" && c.Session != "" }

// prepare は起動・再開の前提を確かめて、実行する関数を返す。再開は、前の session が pro-con の記録にあり、
// 今の一覧でその短い id が同じ session を指している (別の session を止めない) ときだけ。
func (d *Daemon) prepare(c card.Card, ss []agents.Session, reg []live.Owned) (string, func(context.Context) (string, error), error) {
	if resumes(c) {
		o, ok := owned(c, reg)
		if !ok {
			return "再開", nil, fmt.Errorf("前の session (%s) が pro-con の記録に無い", c.Session)
		}
		stop := "" // 一覧に無い session は止めない (無い id への stop が失敗すると、再開に届かないまま繰り返す)
		for _, s := range ss {
			if s.ID != c.Session {
				continue
			}
			if s.SessionID != o.SessionID {
				return "再開", nil, fmt.Errorf("短い id %s が今は別の session (%s) を指している", c.Session, s.SessionID)
			}
			stop = c.Session
		}
		return "再開", func(ctx context.Context) (string, error) {
			return d.Launch.Resume(ctx, stop, o.SessionID, c.Resume)
		}, nil
	}
	path, ok := d.Repos[c.Repo]
	if !ok {
		return "起動", nil, fmt.Errorf("repo %q の場所が設定に無い", c.Repo)
	}
	return "起動", func(ctx context.Context) (string, error) { return d.Launch.Start(ctx, path, sessionName(c), Prompt(c)) }, nil
}

func sessionName(c card.Card) string { return "pc-" + strings.ToLower(c.ID) }

func owned(c card.Card, reg []live.Owned) (live.Owned, bool) {
	for _, o := range reg {
		if o.CardID == c.ID && o.ID == c.Session {
			return o, true
		}
	}
	return live.Owned{}, false
}

// adopt は結果の分からない起動・再開の session が一覧に出ているかを見る。印を書いた後 (LaunchedAt 以降) に始まったものだけ:
// 起動はこのカードの session の名前、再開は前の session と同じ session id。
func adopt(c card.Card, ss []agents.Session, reg []live.Owned) (string, bool) {
	o, hasOwned := owned(c, reg)
	for _, s := range ss {
		if s.ID == "" || s.Started().Before(c.LaunchedAt) {
			continue
		}
		if c.Launching == "起動" && s.Name == sessionName(c) {
			return s.ID, true
		}
		if c.Launching == "再開" && hasOwned && s.SessionID == o.SessionID {
			return s.ID, true
		}
	}
	return "", false
}

// mark は起動・再開を始める印を記録に書く (結果を待つ前に)。
func (d *Daemon) mark(id string, now time.Time, how string) error {
	return d.update(id, func(c *card.Card) { c.Launching, c.LaunchedAt = how, now })
}

// settle は起動・再開が済んだカードを作業中にする。
func (d *Daemon) settle(id string, now time.Time, how, session string) error {
	return d.update(id, func(c *card.Card) {
		c.State, c.Since, c.Owner, c.Session = card.Running, now, "PG", session
		c.LastProgress, c.Resume, c.Launching = now, "", ""
		c.History = append(c.History, card.Event{At: now, Text: "PG を" + how + "した (session " + session + ")"})
	})
}

func (d *Daemon) note(id string, now time.Time, text string) error {
	return d.update(id, func(c *card.Card) { c.History = append(c.History, card.Event{At: now, Text: text}) })
}

func (d *Daemon) update(id string, f func(*card.Card)) error {
	return store.Update(d.Dir, func(s *store.State) error {
		for i := range s.Cards {
			if s.Cards[i].ID == id {
				f(&s.Cards[i])
			}
		}
		return nil
	})
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
