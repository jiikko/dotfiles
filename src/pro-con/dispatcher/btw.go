package dispatcher

// btw (415 要件 9 / issue 438): 人間が「今どうなってる?」と聞いたら、PG は止めず、その文脈も汚さずに答える
// (Claude Code の /btw と同じ性質)。PG へは届けない: dispatcher は SendMessage を呼べず、届けるには PG を止めて再開するしかない。
// 答えは別のプロセス (Ask。本物は haiku) が、カードの記録と PG の出力の末尾 (transcript) だけを材料に作る。
// 1 本ずつ裏で答え、Tick を止めない。dispatcher が答えの途中で落ちたら、次の dispatcher が答え直す (答えの印が記録に無いので)。

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
)

// btwJob は答えを作っている 1 本。
type btwJob struct {
	cardID string
	at     time.Time // 質問の時刻 (Btws の中の目印)
	done   chan string
	cancel context.CancelFunc
}

// btwTimeout は 1 本の答えの上限。
const btwTimeout = 2 * time.Minute

// btwOutputs は材料にする PG の出力の数 (末尾から)。
const btwOutputs = 8

// tickBtws は btw の 1 回ぶん: できた答えを記録に書き、まだ答えていない最も古い質問の答えを作り始める。
func (d *Dispatcher) tickBtws(ctx context.Context, now time.Time) ([]eventlog.Event, error) {
	if d.btw != nil {
		select {
		case answer := <-d.btw.done:
			job := d.btw
			d.btw = nil
			return d.answerBtw(now, job.cardID, job.at, answer)
		default:
			return nil, nil
		}
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	var target card.Card
	var q card.Btw
	for _, c := range st.Cards {
		for _, b := range c.Btws {
			if b.Answered.IsZero() && (q.At.IsZero() || b.At.Before(q.At)) {
				target, q = c, b
			}
		}
	}
	if q.At.IsZero() {
		return nil, nil
	}
	outputs := d.pgOutputs(target)
	if d.Ask == nil || len(outputs) == 0 { // 材料が記録だけなら、LLM を呼ばずに記録から答える
		return d.answerBtw(now, target.ID, q.At, localAnswer(target, now))
	}
	bctx, cancel := context.WithTimeout(ctx, btwTimeout)
	job := &btwJob{cardID: target.ID, at: q.At, done: make(chan string, 1), cancel: cancel}
	d.btw = job
	prompt := btwPrompt(target, q.Question, outputs, now)
	go func() {
		defer cancel()
		answer, err := d.Ask(bctx, prompt)
		if err != nil || strings.TrimSpace(answer) == "" {
			answer = fmt.Sprintf("答えを作れなかった (%v)。記録から: %s", err, localAnswer(target, now))
		}
		job.done <- strings.TrimSpace(answer)
	}()
	return nil, nil
}

// answerBtw は答えを記録に書く (質問の時刻で目印を引く)。
func (d *Dispatcher) answerBtw(now time.Time, cardID string, at time.Time, answer string) ([]eventlog.Event, error) {
	err := d.update(cardID, func(c *card.Card) {
		for i := range c.Btws {
			if c.Btws[i].At.Equal(at) && c.Btws[i].Answered.IsZero() {
				c.Btws[i].Answer, c.Btws[i].Answered = answer, now
				c.History = append(c.History, card.Event{At: now, Text: "btw の答え: " + answer})
				return
			}
		}
	})
	return []eventlog.Event{ev(eventlog.KindBtw, cardID, "", cardID+": btw に答えた")}, err
}

// cancelBtw は答えを作っている 1 本を取り消す (dispatcher が抜ける前。答えは記録に書かないので、次の dispatcher が答え直す)。
func (d *Dispatcher) cancelBtw() {
	if d.btw != nil {
		d.btw.cancel()
		d.btw = nil
	}
}

// pgOutputs はカードの PG の出力の末尾 (pro-con が起動した session のものだけ。無ければ nil)。
func (d *Dispatcher) pgOutputs(c card.Card) []string {
	if d.Transcript == nil || c.Session == "" {
		return nil
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return nil
	}
	o, ok := owned(c, reg)
	if !ok {
		return nil
	}
	t, err := d.Transcript(o.SessionID)
	if err != nil {
		return nil
	}
	return tailOf(t.Outputs, btwOutputs)
}

func tailOf(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}

// localAnswer は記録だけから作る答え (PG の出力が無い / 答えを作る口が無い / 作れなかった)。
func localAnswer(c card.Card, now time.Time) string {
	s := fmt.Sprintf("%s は %s (%s から)", c.ID, c.State.Label(), ago(now.Sub(c.Since)))
	if c.Stalled {
		s += "。watchdog が停滞と見ている"
	}
	if c.Wait.Question != "" {
		s += "。待っているもの: " + clipLine(c.Wait.Question)
	}
	if n := len(c.Pending()); n > 0 {
		s += fmt.Sprintf("。未達の追加オーダー %d 件", n)
	}
	for i := len(c.History) - 1; i >= 0; i-- { // 直近の出来事 (btw 自身の行は除く)
		if t := c.History[i].Text; !strings.HasPrefix(t, "btw") {
			s += "。直近の出来事: " + clipLine(t)
			break
		}
	}
	return s
}

func ago(d time.Duration) string {
	if d < time.Minute {
		return "1 分以内"
	}
	return fmt.Sprintf("%d 分前", int(d/time.Minute))
}

// btwPrompt は答えを作るプロセスへ渡す指示。材料 (カードの記録と PG の出力) だけから答えさせる。
func btwPrompt(c card.Card, question string, outputs []string, now time.Time) string {
	var b strings.Builder
	b.WriteString("次は作業カードの記録と、その作業担当 (PG) の直近の出力です。人間の質問に、この材料だけから日本語で 3 行以内で答えてください。" +
		"材料に無いことは推測せず「材料からは分からない」と書き、質問もしないこと。\n\n")
	fmt.Fprintf(&b, "質問: %s\n\nカード: %s「%s」\n記録から: %s\n", question, c.ID, c.Title, localAnswer(c, now))
	b.WriteString("\nPG の直近の出力 (古い順):\n")
	for _, o := range outputs {
		b.WriteString("- " + clipLine(o) + "\n")
	}
	return b.String()
}
