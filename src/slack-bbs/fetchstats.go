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
	same, edited               int // edited は same のうち、両方に在る返信の本文が違うもの (返信の数と最後の返信では気づけない変化)
	notCached, noParent        int
	noPrev, prevErr            int // 前回の板が無い (版が違うものを含む)・読めなかった板の数
}

// unitKey は単位を板の中で引く鍵。グループの単位は ID が "<ch>/<ts>" なので、チャンネルと親の ts に分けて持つ。
func unitKey(board string, u board.Unit) [2]string {
	ch := u.Ch
	if ch == "" {
		ch = board
	}
	return [2]string{ch, u.ParentTs()}
}

// compareFetch は f のスレッドのうち、前回の板 (st の板 id の Raw) に返信の数と最後の返信が同じ単位があるものを数える。
// 同じものは、差分の取得なら読み直さずに前回の返信を使えたスレッド。そのうち、今回の単位 (units) と前回の単位の両方に在る返信の
// 本文が違うものは edited に数える (使い回すと取り逃がす変化)。親は history から毎回読み直すので比べない。
// 比べる本文は board.Msg.Text (名指しを表示名に直した後の値) なので、名指しされた人の改名や bot の blocks_text の組み立ての違いも
// edited に入る (原文は Raw に残らず分けられない)。出力では「本文が変わった」と書き、編集だけとは言わない。
//
// Raw の単位は返信を期間で省いて持つので (board.Unit.Omitted)、返信の数は「出した発言 + 省いた数 - 親」で数える。
// 読めなかった・上限で切れた単位は返信を全部持たないので、同じとは数えない。
func compareFetch(f fetchStats, id string, units []board.Unit, st cache.Store) fetchReport {
	r := fetchReport{boards: 1, histMsgs: f.histMsgs, need: f.need, history: f.history, threads: f.threads, mentions: f.mentions,
		noParent: f.need - len(f.marks)}
	prev := map[[2]string]board.Unit{}
	switch raw, ok, err := st.LoadRaw(id); {
	case err != nil:
		r.prevErr = 1
	case !ok:
		r.noPrev = 1
	default:
		for _, u := range raw.Units {
			if u.Thread {
				prev[unitKey(id, u)] = u
			}
		}
	}
	cur := map[[2]string]board.Unit{}
	for _, u := range units {
		cur[unitKey(id, u)] = u
	}
	for _, m := range f.marks {
		k := [2]string{m.ch, m.ts}
		u, ok := prev[k]
		switch {
		case !ok:
			r.notCached++
		case !u.Unreadable && !u.Truncated && u.Last() == m.latest && len(u.Msgs)+u.Omitted-1 == m.replies:
			r.same++
			if repliesChanged(u, cur[k]) {
				r.edited++
			}
		}
	}
	return r
}

// repliesChanged は a と b の両方に在る返信 (同じ ts。親を除く) のうち、本文が違うものがあるか。
func repliesChanged(a, b board.Unit) bool {
	text := map[string]string{}
	for _, m := range a.Msgs {
		if m.Ts != a.ParentTs() {
			text[m.Ts] = m.Text
		}
	}
	for _, m := range b.Msgs {
		if t, ok := text[m.Ts]; ok && t != m.Text {
			return true
		}
	}
	return false
}

func (r *fetchReport) add(g fetchReport) {
	r.boards += g.boards
	r.histMsgs += g.histMsgs
	r.need += g.need
	r.history += g.history
	r.threads += g.threads
	r.mentions += g.mentions
	r.same += g.same
	r.edited += g.edited
	r.notCached += g.notCached
	r.noParent += g.noParent
	r.noPrev += g.noPrev
	r.prevErr += g.prevErr
}

func (r fetchReport) String() string {
	s := fmt.Sprintf("取得 %.1f 秒 (内訳の合計) = history %d 件 %.1f 秒 + スレッド %d 本 (読めなかったものを含む) %.1f 秒 + 言及の検索 %.1f 秒。"+
		"スレッドのうち前回の板と返信の数・最後の返信が同じ %d 本 (うち返信の本文が変わった %d 本) / 前回の板に無い %d 本 / 親が history に無い %d 本",
		(r.history + r.threads + r.mentions).Seconds(), r.histMsgs, r.history.Seconds(), r.need, r.threads.Seconds(), r.mentions.Seconds(),
		r.same, r.edited, r.notCached, r.noParent)
	if r.noPrev > 0 {
		s += fmt.Sprintf(" (前回の板が無い板 %d)", r.noPrev)
	}
	if r.prevErr > 0 {
		s += fmt.Sprintf(" (前回の板を読めなかった板 %d)", r.prevErr)
	}
	return s
}
