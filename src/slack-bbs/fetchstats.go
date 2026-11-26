package main

import (
	"fmt"
	"time"

	"slack-bbs/board"
	"slack-bbs/cache"
	"slack-bbs/source"
)

// fetchStats は 1 つの板の取得の量と所要。所要は slack-cli の列の待ち (throttle の間隔・429 の待ち) を含む実時間で、
// 差分の取得にしたときに減らせる量を見積もる材料にする (-dry-run が出す)。
type fetchStats struct {
	histMsgs                   int
	need                       int // 読み直したスレッドの本数 (読めなかったものを含む)
	history, threads, mentions time.Duration
	marks                      []threadMark // 親が history に在るスレッドだけ (親が無いものは前回と比べる材料が無い)
}

// threadMark は読み直したスレッドの、history の親が持っていた返信の数と最後の返信。
type threadMark struct {
	ch, ts  string
	replies int
	latest  string
}

func (f *fetchStats) add(g fetchStats) {
	f.histMsgs += g.histMsgs
	f.need += g.need
	f.history += g.history
	f.threads += g.threads
	f.mentions += g.mentions
	f.marks = append(f.marks, g.marks...)
}

func threadMarks(ch string, history []source.Message, need []string) []threadMark {
	parents := map[string]source.Message{}
	for _, m := range history {
		if m.IsThreadParent() {
			parents[m.Ts] = m
		}
	}
	var out []threadMark
	for _, ts := range need {
		if p, ok := parents[ts]; ok {
			out = append(out, threadMark{ch: ch, ts: ts, replies: p.ReplyCount, latest: p.LatestReply})
		}
	}
	return out
}

// fetchReport は fetchStats を前回の板 (キャッシュの Raw) と比べた結果。
type fetchReport struct {
	boards                     int
	histMsgs, need             int
	history, threads, mentions time.Duration
	same, notCached, noParent  int
	prevErr                    int // 前回の板を読めなかった板の数
}

// compareFetch は f のスレッドのうち、前回の板 (st の板 id の Raw) に返信の数と最後の返信が同じ単位があるものを数える。
// 同じものは、差分の取得なら読み直さずに前回の返信を使えたスレッド。
//
// Raw の単位は返信を期間で省いて持つので (board.Unit.Omitted)、返信の数は「出した発言 + 省いた数 - 親」で数える。
// 読めなかった・上限で切れた単位は返信を全部持たないので、同じとは数えない。
func compareFetch(f fetchStats, id string, st cache.Store) fetchReport {
	r := fetchReport{boards: 1, histMsgs: f.histMsgs, need: f.need, history: f.history, threads: f.threads, mentions: f.mentions,
		noParent: f.need - len(f.marks)}
	prev := map[[2]string]board.Unit{}
	raw, ok, err := st.LoadRaw(id)
	if err != nil {
		r.prevErr = 1
	}
	if ok {
		for _, u := range raw.Units {
			if !u.Thread {
				continue
			}
			ch := u.Ch
			if ch == "" {
				ch = id
			}
			prev[[2]string{ch, u.ParentTs()}] = u
		}
	}
	for _, m := range f.marks {
		u, ok := prev[[2]string{m.ch, m.ts}]
		switch {
		case !ok:
			r.notCached++
		case !u.Unreadable && !u.Truncated && u.Last() == m.latest && len(u.Msgs)+u.Omitted-1 == m.replies:
			r.same++
		}
	}
	return r
}

func (r *fetchReport) add(g fetchReport) {
	r.boards += g.boards
	r.histMsgs += g.histMsgs
	r.need += g.need
	r.history += g.history
	r.threads += g.threads
	r.mentions += g.mentions
	r.same += g.same
	r.notCached += g.notCached
	r.noParent += g.noParent
	r.prevErr += g.prevErr
}

func (r fetchReport) String() string {
	s := fmt.Sprintf("取得 %.1f 秒 (内訳の合計) = history %d 件 %.1f 秒 + スレッド %d 本 %.1f 秒 + 言及の検索 %.1f 秒。"+
		"スレッドのうち前回の板と返信の数・最後の返信が同じ %d 本 / 前回の板に無い %d 本 / 親が history に無い %d 本",
		(r.history + r.threads + r.mentions).Seconds(), r.histMsgs, r.history.Seconds(), r.need, r.threads.Seconds(), r.mentions.Seconds(),
		r.same, r.notCached, r.noParent)
	if r.prevErr > 0 {
		s += fmt.Sprintf(" (前回の板を読めなかった板 %d)", r.prevErr)
	}
	return s
}
