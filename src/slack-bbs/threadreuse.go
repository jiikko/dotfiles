package main

import (
	"time"

	"slack-bbs/cache"
	"slack-bbs/source"
)

// threadReuse は板の読み直しで、前回読んだスレッドを使い回すか (read) と、読んだスレッドを残すか (write)。
// ゼロ値はどちらもしない (毎回全部のスレッドを slack-cli で読む)。
type threadReuse struct {
	st    cache.Store
	read  bool
	write bool
}

// browseThreads は読む画面の読み直しのスレッドの扱い。全部を取り直す (R) と -no-cache のときだけ前回のスレッドを使わない
// (ユーザーの決定 2026-11-15: 起動・r・自動の読み直しは差分で取り、返信の編集の取り逃がしは R で拾う)。どちらでも読んだものは残す。
func browseThreads(o options, st cache.Store, full bool) threadReuse {
	return threadReuse{st: st, read: !full && !o.noCache, write: true}
}

// threadIndex は前回読んだスレッドを (チャンネル, 親の ts) で引く表。nil なら何も使い回さない。
type threadIndex map[[2]string]cache.Thread

// reusable は history の親 parent のスレッドを、前回の返信のまま使えるか。返信の数と最後の返信が前回と同じときだけ使う。
// 返信の編集 (最後の返信を含む) はこの 2 つが変わらないので取り逃がす (ユーザーの決定 2026-11-15: 受け入れ、全部を取り直す R で拾う)。
// 親と「チャンネルにも投稿」の返信の編集は、history に新しい値が届くので freshen が拾う。
// 前回の返信が上限 (source.ThreadLimit) で切れていたら使わない (切れた続きを読み直す)。
func (ix threadIndex) reusable(ch string, parent source.Message) (cache.Thread, bool) {
	t, ok := ix[[2]string{ch, parent.Ts}]
	if !ok || len(t.Msgs) == 0 || len(t.Msgs) >= source.ThreadLimit {
		return cache.Thread{}, false
	}
	return t, t.ReplyCount == parent.ReplyCount && t.LatestReply == parent.LatestReply
}

// consistent は、読んだスレッドの発言 msgs (親を含む) が history の親 parent の値と合うか (返信の数と最後の返信が同じ)。
// history を読んでからスレッドを読むまでの間に返信が動くと合わない。合わないまま残すと、後で返信が消えて親の値が前回に戻ったとき、
// 消えた返信を使い回す (2026-11-27 に実際のスレッド 12 本で、どれも合うことを確かめた)。
func consistent(msgs []source.Message, parent source.Message) bool {
	return len(msgs) == parent.ReplyCount+1 && msgs[len(msgs)-1].Ts == parent.LatestReply
}

// freshen は使い回す発言 msgs のうち、今回の history (ts で引く) にも在る発言を history の値に差し替えた写しを返す。
// 親と「チャンネルにも投稿」の返信は history に新しい本文が届く (board.Build はスレッドの側の発言を出すので、差し替えないと古い本文が残る)。
func freshen(msgs []source.Message, history map[string]source.Message) []source.Message {
	out := make([]source.Message, len(msgs))
	for i, m := range msgs {
		if h, ok := history[m.Ts]; ok {
			m = h
		}
		out[i] = m
	}
	return out
}

// load は板 id が前回読んだスレッドを読む。読めなければ何も使い回さず (全部を読み直す)、板の注記に出す文を返す。
func (tr threadReuse) load(id string) (threadIndex, string) {
	if !tr.read {
		return nil, ""
	}
	t, ok, err := tr.st.LoadThreads(id)
	if err != nil {
		return nil, "前回読んだスレッドを読めなかった (全部のスレッドを読み直した): " + err.Error()
	}
	if !ok {
		return nil, ""
	}
	ix := threadIndex{}
	for _, th := range t.Items {
		ix[[2]string{th.Ch, th.Ts}] = th
	}
	return ix, ""
}

// save は板 id が今回読んだスレッドを残す (前回の分は置き換える。今回の期間で読まなかったスレッドは残さない)。
// 書けなければ板の注記に出す文を返す (次の読み直しが全部のスレッドを読むだけで、板は出せる)。
func (tr threadReuse) save(id string, at time.Time, read []cache.Thread) string {
	if !tr.write {
		return ""
	}
	if err := tr.st.SaveThreads(id, cache.Threads{SavedAt: at, Items: read}); err != nil {
		return "読んだスレッドをキャッシュに書けなかった (次の読み直しは全部のスレッドを読む): " + err.Error()
	}
	return ""
}
