// Package fake は claude を起動せずに pro-con の画面とつなぎ込みを動かすための模擬 backend。
//
// 時間は壁時計ではなく刻みで進める (Poll 1 回 = 模擬時間で StepDuration)。テストは Step を直接呼んで
// 決定的に状態を進める。ここにある dispatcher / watchdog の判定は模擬であり、本番の daemon の実装ではない
// (本番の判定をここへ育てるなら、fake から切り出して backend 側の package に置く)。
package fake

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"pro-con/backend"
	"pro-con/card"
)

const (
	StepDuration = time.Minute
	// StallAfter は watchdog が「進捗なし」を停滞とみなすまでの模擬時間。
	StallAfter = 5 * time.Minute
	// ReviewAfter は模擬の PM がレビュー待ちのカードに手を付けるまでの時間。
	ReviewAfter = 6 * time.Minute
)

// script は 1 枚のカードで PG が何をするかの台本。
type script struct {
	lines    []string // 1 刻みに 1 行ずつ進捗として出る
	then     string   // 台本を出し切った後: "review" / "question" / "" (何もしない = 進捗が止まる)
	question string
}

type Sim struct {
	now      time.Time
	cards    []card.Card
	limit    int
	scripts  map[string]*script
	resource map[string][]string // リソース名 → 順番待ちのカード ID (先頭が占有中)
	// externalUntil は pro-con の外 (人間が直接使っている session) の占有がいつ終わるか。リソース名 → 時刻
	externalUntil map[string]time.Time
	nextID        int
	nextIssue     int // 模擬の PM が振る issue 番号 (見本の番号と重ならない帯から)
	nextSess      int
}

var _ backend.Backend = (*Sim)(nil)

// New は見本のカードを並べた Sim を返す。start は模擬時間の起点。
func New(start time.Time) *Sim {
	s := &Sim{now: start, limit: 2, scripts: map[string]*script{}, resource: map[string][]string{}, externalUntil: map[string]time.Time{}, nextID: 20, nextIssue: 930}
	s.seed()
	return s
}

func (s *Sim) ago(d time.Duration) time.Time { return s.now.Add(-d) }

func (s *Sim) newSession() string {
	s.nextSess++
	return fmt.Sprintf("%08x", 0x3feb6000+s.nextSess)
}

func (s *Sim) seed() {
	ev := func(d time.Duration, t string) card.Event { return card.Event{At: s.ago(d), Text: t} }
	s.cards = []card.Card{
		{ID: "C-001", Title: "glogx の diff で日本語ファイル名が化ける", Request: "glogx で diff 開いたら日本語のファイル名が化けてた。直せる？",
			Repo: "dotfiles", Owner: "受付 PM", State: card.Requested, Since: s.ago(3 * time.Minute),
			History: []card.Event{ev(3*time.Minute, "受付 PM がカードを作った")}},
		{ID: "C-002", Title: "statusline の色が見えづらい件の状況", Request: "statusline の色の件どうなってる？",
			Repo: "dotfiles", Owner: "受付 PM", State: card.Requested, Since: s.ago(1 * time.Minute),
			History: []card.Event{ev(1*time.Minute, "受付 PM がカードを作った")}},
		{ID: "C-003", Title: "tmux-toast の通知が重なる", Request: "toast が 2 つ同時に出ると重なって読めない",
			Repo: "dotfiles", Owner: "PM-A", State: card.Planned, Since: s.ago(12 * time.Minute),
			Issues:  []card.IssueRef{{Repo: "dotfiles", Number: 921, Status: "open"}},
			History: []card.Event{ev(20*time.Minute, "受付 PM がカードを作った"), ev(12*time.Minute, "PM-A が issue 921 に分解した")}},
		{ID: "C-004", Title: "av1ify のログ出力を整理", Request: "av1ify のログがうるさい", ParentID: "C-010",
			Repo: "dotfiles", Owner: "PM-A", State: card.Planned, Since: s.ago(8 * time.Minute),
			Issues:  []card.IssueRef{{Repo: "dotfiles", Number: 922, Status: "open"}},
			History: []card.Event{ev(8*time.Minute, "C-010 の追加オーダー (別件) から分けた")}},
		{ID: "C-005", Title: "pro-con のカンバン幅の調整", Request: "カンバンの列が狭いときに崩れないようにして",
			Repo: "dotfiles", Owner: "PG", Session: s.newSession(), State: card.Running, Since: s.ago(15 * time.Minute),
			LastProgress: s.ago(1 * time.Minute),
			Issues:       []card.IssueRef{{Repo: "dotfiles", Number: 415, Status: "next"}},
			Log:          []string{"Read src/pro-con/ui/board.go", "Edit ui/board.go: 列幅の下限を 14 に"},
			History:      []card.Event{ev(15*time.Minute, "PG が着手した")}},
		{ID: "C-006", Title: "実機 E2E で再生テスト", Request: "実機で seek のテストを回して",
			Repo: "obaket", Owner: "PG", Session: s.newSession(), State: card.Running, Since: s.ago(6 * time.Minute),
			LastProgress: s.ago(2 * time.Minute),
			Wait:         card.Wait{Kind: card.WaitResource, Resource: "device", Position: 2},
			Issues:       []card.IssueRef{{Repo: "obaket", Number: 912, Status: "next"}},
			Log:          []string{"make e2e-device を実行しようとした → device の順番待ち"},
			History:      []card.Event{ev(6*time.Minute, "PG が着手した"), ev(2*time.Minute, "device の順番待ちに並んだ")}},
		{ID: "C-007", Title: "zsh の起動が遅い", Request: "新しい zsh が開くのに 1 秒くらいかかる",
			Repo: "dotfiles", Owner: "PM-A", Session: "3feb5f01", State: card.Waiting, Since: s.ago(9 * time.Minute),
			Wait:    card.Wait{Kind: card.WaitQuestion, Question: "direnv の hook を外すと 300ms 速くなります。外してよいですか？ それとも遅延ロードにしますか？"},
			Issues:  []card.IssueRef{{Repo: "dotfiles", Number: 923, Status: "next"}},
			Log:     []string{"zprof: direnv hook 312ms", "質問を書いて終了した"},
			History: []card.Event{ev(30*time.Minute, "PG が着手した"), ev(9*time.Minute, "PG が質問した")}},
		{ID: "C-008", Title: "glogx の起動時間短縮", Request: "glogx 開くのちょっと遅い",
			Repo: "dotfiles", Owner: "PM-A", Session: "3feb5f02", State: card.Review, Since: s.ago(25 * time.Minute),
			Issues:  []card.IssueRef{{Repo: "dotfiles", Number: 924, Status: "next"}},
			Log:     []string{"make -C src/glogx test: ok", "push: worktree-pg-924"},
			History: []card.Event{ev(25*time.Minute, "PG が終えてブランチへ push した。レビュー待ち")}},
		{ID: "C-009", Title: "direnv は入れるべき？", Request: "direnv って入れた方がいい？",
			Repo: "dotfiles", Owner: "受付 PM", State: card.Done, Since: s.ago(60 * time.Minute), Ending: card.EndAnswered,
			History: []card.Event{ev(60*time.Minute, "受付 PM がその場で回答した")}},
		{ID: "C-010", Title: "av1ify の失敗時に再開できない", Request: "av1ify が途中で落ちたらやり直しになる",
			Repo: "dotfiles", Owner: "PM-A", State: card.Done, Since: s.ago(90 * time.Minute),
			Issues:  []card.IssueRef{{Repo: "dotfiles", Number: 901, Status: "done"}},
			History: []card.Event{ev(90*time.Minute, "PM-A がレビューして master へ載せた")}},
		{ID: "C-011", Title: "glogx にコミット検索が欲しい", Request: "glogx でコミットメッセージを検索したい",
			Repo: "dotfiles", Owner: "受付 PM", State: card.Done, Since: s.ago(40 * time.Minute), Ending: card.EndPendingIssue,
			History: []card.Event{ev(40*time.Minute, "受付 PM が「あとで issue にする」とした")}},
	}
	s.resource["device"] = []string{"C-099", "C-006"} // 先頭 C-099 は人間が直接使っている session の占有という想定
	s.externalUntil["device"] = s.now.Add(2 * StepDuration)
	s.scripts["C-005"] = &script{lines: []string{"go test ./ui: ok"}} // then 無し = ここで進捗が止まり watchdog が拾う
	s.scripts["C-006"] = &script{lines: []string{"make e2e-device: 12/40", "make e2e-device: 40/40 ok"}, then: "review"}
	s.scripts["C-003"] = &script{lines: []string{"Read bin/tmux-toast"}, then: "question",
		question: "重なったときは縦に積みますか？ 古い方を消しますか？"}
	s.scripts["C-004"] = &script{lines: []string{"Edit bin/av1ify: log を stderr の 1 行に", "make test: ok"}, then: "review"}
	s.scripts["C-007"] = &script{lines: []string{"direnv を遅延ロードに変更", "zprof: 88ms"}, then: "review"}
}

func (s *Sim) find(id string) *card.Card {
	for i := range s.cards {
		if s.cards[i].ID == id {
			return &s.cards[i]
		}
	}
	return nil
}

func (s *Sim) setState(c *card.Card, st card.State, why string) {
	c.State = st
	c.Since = s.now
	c.History = append(c.History, card.Event{At: s.now, Text: why})
}

// busyConsumers は PG の枠を使っているカード (作業中の列に居るもの) の数。
// 質問待ちの PG は質問を書いて終了する想定 (415 論点 8 の候補 A) なので枠を使わない。
func (s *Sim) busyConsumers() int {
	n := 0
	for _, c := range s.cards {
		if c.State == card.Running {
			n++
		}
	}
	return n
}

// Step は模擬時間を 1 刻み進める。
func (s *Sim) Step() {
	s.now = s.now.Add(StepDuration)
	s.stepResources()
	s.stepProgress()
	s.stepDispatch()
	s.stepWatchdog()
	s.stepIntake()
	s.stepReview()
}

// stepResources はリソースの列を進める。先頭が pro-con の外の占有なら externalUntil で返す。
func (s *Sim) stepResources() {
	for name, q := range s.resource {
		if len(q) == 0 {
			continue
		}
		if s.find(q[0]) == nil { // pro-con の外の占有 (直接使っている session の想定)
			if !s.now.Before(s.externalUntil[name]) {
				q = q[1:]
			}
		}
		for i, id := range q {
			c := s.find(id)
			if c == nil || c.Wait.Kind != card.WaitResource {
				continue
			}
			if i == 0 {
				c.Wait = card.Wait{}
				c.History = append(c.History, card.Event{At: s.now, Text: name + " を占有した"})
			} else {
				c.Wait.Position = i + 1
			}
		}
		s.resource[name] = q
	}
}

func (s *Sim) releaseResources(id string) {
	for name, q := range s.resource {
		s.resource[name] = slices.DeleteFunc(q, func(x string) bool { return x == id })
	}
}

func (s *Sim) stepProgress() {
	for i := range s.cards {
		c := &s.cards[i]
		if c.State != card.Running || c.Wait.Kind != card.WaitNone {
			continue
		}
		sc := s.scripts[c.ID]
		if sc == nil {
			continue
		}
		s.deliverOrders(c)
		if len(sc.lines) > 0 {
			c.Log = append(c.Log, sc.lines[0])
			sc.lines = sc.lines[1:]
			c.LastProgress = s.now
			if c.Stalled {
				c.Stalled = false
				c.History = append(c.History, card.Event{At: s.now, Text: "watchdog: 進捗が戻った"})
			}
			continue
		}
		switch sc.then {
		case "review":
			s.releaseResources(c.ID)
			s.setState(c, card.Review, "PG が終えてブランチへ push した。レビュー待ち")
		case "question":
			c.Wait = card.Wait{Kind: card.WaitQuestion, Question: sc.question}
			c.Log = append(c.Log, "質問を書いて終了した")
			sc.then = ""
			s.setState(c, card.Waiting, "PG が質問した")
		}
	}
}

// stepDispatch は枠が空いていれば分解済みのカードを上から順に PG へ渡す (模擬の dispatcher)。
func (s *Sim) stepDispatch() {
	for i := range s.cards {
		if s.busyConsumers() >= s.limit {
			return
		}
		c := &s.cards[i]
		if c.State != card.Planned {
			continue
		}
		why := "PG が着手した"
		if c.Session == "" {
			c.Session = s.newSession()
		} else {
			why = "同じ session を resume した"
		}
		c.Owner = "PG"
		c.LastProgress = s.now
		if s.scripts[c.ID] == nil {
			s.scripts[c.ID] = &script{lines: []string{"Read issue", "Edit", "make test: ok"}, then: "review"}
		}
		s.setState(c, card.Running, why)
	}
}

// stepWatchdog は「作業中なのに実質的な進捗が止まっている」カードを停滞にする。正当な待ちは数えない。
func (s *Sim) stepWatchdog() {
	for i := range s.cards {
		c := &s.cards[i]
		if c.State != card.Running || c.Wait.Kind != card.WaitNone || c.Stalled {
			continue
		}
		if s.now.Sub(c.LastProgress) >= StallAfter {
			c.Stalled = true
			c.History = append(c.History, card.Event{At: s.now,
				Text: fmt.Sprintf("watchdog: %d 分進捗なし → PG に「待っているものの状態を確認して」と促した", int(StallAfter/time.Minute))})
		}
	}
}

// stepIntake は受付 PM の模擬。依頼の列のカードを古い順に 1 枚ずつ仕分ける。
func (s *Sim) stepIntake() {
	for i := range s.cards {
		c := &s.cards[i]
		if c.State != card.Requested || s.now.Sub(c.Since) < 4*time.Minute {
			continue
		}
		if strings.Contains(c.Request, "どうなってる") {
			c.Ending = card.EndAnswered
			s.setState(c, card.Done, "受付 PM がその場で回答した")
		} else {
			c.Owner = "PM-A"
			s.nextIssue++
			c.Issues = append(c.Issues, card.IssueRef{Repo: c.Repo, Number: s.nextIssue, Status: "open"})
			s.setState(c, card.Planned, fmt.Sprintf("PM-A が issue %03d に分解した", s.nextIssue))
		}
		return
	}
}

// stepReview は担当 PM のレビューの模擬。レビュー待ちが ReviewAfter を越えたカードを 1 刻みに 1 枚だけ完了にする
// (レビューは 1 枚ずつしか進まない = 並列数を上げてもここが詰まる、を見本でも見せる)。
func (s *Sim) stepReview() {
	for i := range s.cards {
		c := &s.cards[i]
		if c.State != card.Review || s.now.Sub(c.Since) < ReviewAfter {
			continue
		}
		for j := range c.Issues {
			c.Issues[j].Status = "done"
		}
		s.setState(c, card.Done, "PM-A が diff と実行結果を読んで master へ載せた")
		return
	}
}

func (s *Sim) deliverOrders(c *card.Card) {
	for j := range c.Orders {
		if !c.Orders[j].Delivered && c.Orders[j].Kind == card.OrderAppend {
			c.Orders[j].Delivered = true
			c.History = append(c.History, card.Event{At: s.now, Text: "追加オーダーを PG のターンの区切りで届けた"})
		}
	}
}

// Poll は 1 刻み進めてから状態を返す。
func (s *Sim) Poll() backend.Snapshot {
	s.Step()
	return s.Snapshot()
}

// Snapshot は状態のコピーを返す (UI が書き換えても Sim に影響しない)。
func (s *Sim) Snapshot() backend.Snapshot {
	cards := make([]card.Card, len(s.cards))
	for i, c := range s.cards {
		c.Issues = slices.Clone(c.Issues)
		c.Orders = slices.Clone(c.Orders)
		c.History = slices.Clone(c.History)
		c.Log = slices.Clone(c.Log)
		cards[i] = c
	}
	var cons []backend.Consumer
	for _, c := range s.cards {
		if c.State == card.Running {
			st := "busy"
			if c.Wait.Kind != card.WaitNone {
				st = "waiting"
			}
			cons = append(cons, backend.Consumer{Session: c.Session, CardID: c.ID, Status: st})
		}
	}
	return backend.Snapshot{Now: s.now, Cards: cards, Consumers: cons, Limit: s.limit, DaemonTick: s.now, Violations: card.Check(cards)}
}

func (s *Sim) Apply(cmd backend.Command) (string, error) {
	switch c := cmd.(type) {
	case backend.Answer:
		return s.answer(c)
	case backend.AddOrder:
		return s.addOrder(c)
	case backend.Btw:
		return s.btw(c)
	}
	return "", backend.ErrUnknownKind
}

// answer は「まだ質問待ちなら書く」の比較付き更新。負けた側には ErrNotWaiting を返す。
func (s *Sim) answer(a backend.Answer) (string, error) {
	if strings.TrimSpace(a.Text) == "" {
		return "", backend.ErrEmptyText
	}
	c := s.find(a.CardID)
	if c == nil {
		return "", backend.ErrNotFound
	}
	if c.State != card.Waiting {
		return "", backend.ErrNotWaiting
	}
	c.Wait = card.Wait{}
	c.History = append(c.History, card.Event{At: s.now, Text: a.From + " が回答した: " + a.Text})
	s.setState(c, card.Planned, "回答を受けて再開待ち (PG の空きが出たら同じ session を resume)")
	return c.ID + " に回答した。PG の空きが出たら再開する", nil
}

func (s *Sim) addOrder(o backend.AddOrder) (string, error) {
	if strings.TrimSpace(o.Text) == "" {
		return "", backend.ErrEmptyText
	}
	c := s.find(o.CardID)
	if c == nil {
		return "", backend.ErrNotFound
	}
	switch o.Kind {
	case card.OrderSeparate:
		s.nextID++
		// 別件は新しい依頼として受け付ける (依頼の列から、PM の分解 = issue への紐づけを通す)。
		// 分解を飛ばして Planned に置くと、issue も終わり方も無いまま完了まで進んで不変条件を破る
		child := card.Card{ID: fmt.Sprintf("C-%03d", s.nextID), ParentID: c.ID, Title: o.Text, Request: o.Text,
			Repo: c.Repo, Owner: "受付 PM", State: card.Requested, Since: s.now,
			History: []card.Event{{At: s.now, Text: c.ID + " の追加オーダー (別件) から分けた。同じファイルを触るなら " + c.ID + " の後に着手"}}}
		c.History = append(c.History, card.Event{At: s.now, Text: "追加オーダー (別件) を " + child.ID + " に分けた"})
		s.cards = append(s.cards, child)
		return "別件として " + child.ID + " を作った", nil
	case card.OrderAppend, card.OrderRedirect:
		if c.State == card.Done {
			return "", backend.ErrNotActive
		}
		ord := card.Order{Kind: o.Kind, Text: o.Text, At: s.now}
		msg := "追記を受けた。PG のターンの区切りで届ける"
		if o.Kind == card.OrderRedirect {
			ord.Delivered = true
			c.Stalled = false
			c.LastProgress = s.now
			c.History = append(c.History, card.Event{At: s.now, Text: "方針変更: その時点の diff を記録 → claude stop → 指示を差し替えて resume"})
			msg = "方針変更を反映した (止めて差し替えて再開)"
		}
		c.Orders = append(c.Orders, ord)
		c.History = append(c.History, card.Event{At: s.now, Text: "追加オーダー (" + o.Kind.Label() + "): " + o.Text})
		return msg, nil
	}
	return "", backend.ErrUnknownKind
}

// btw は PG を止めずに状況を返す。本番は PG の最新出力を元に別プロセスで答える (415 要件 9)。
func (s *Sim) btw(b backend.Btw) (string, error) {
	c := s.find(b.CardID)
	if c == nil {
		return "", backend.ErrNotFound
	}
	last := "(出力なし)"
	if len(c.Log) > 0 {
		last = c.Log[len(c.Log)-1]
	}
	state := c.State.Label()
	if c.Stalled {
		state += " (停滞)"
	}
	return fmt.Sprintf("[btw・模擬] %s は %s。%s から。直近の出力: %s", c.ID, state, fmtAgo(s.now.Sub(c.Since)), last), nil
}

func fmtAgo(d time.Duration) string {
	if d < time.Minute {
		return "今"
	}
	return fmt.Sprintf("%d 分前", int(d/time.Minute))
}

// AttachCommand は本物の claude attach の代わりに、自分自身の fake-attach を起動する。
// 画面の受け渡しと戻り (tea.ExecProcess) は本物と同じ経路を通る。
func (s *Sim) AttachCommand(sessionID string) (*exec.Cmd, error) {
	if sessionID == "" {
		return nil, backend.ErrNoSession
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return exec.Command(self, "fake-attach", sessionID), nil
}
