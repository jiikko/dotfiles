package main

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// watchChain は「fsnotify のイベント待ち + 保険のポーリング」の 2 本のチェーンの札と watcher を持つ
// (issues viewer の見張りと git log の見張りが共有する。issue 666: 以前は同じ機構が 2 実装あり、
// 札の扱いの修正を片方にだけ入れる形になっていた)。
//
// 何を見張り、何を指紋にし、いつ反映するかは各見張りが持つ (集合も指紋も別物。issue 271)。
// ここが持つのは single-flight の札と、届いたチェーンの札だけを降ろす規律だけ。
//
// zero value は「何も張っていない」。gen は watcher を閉じるたびに各見張りが進め、閉じる前に
// 張ったチェーンの観測を弾くのに使う (メッセージに gen を載せるのは各見張りの仕事)。
type watchChain struct {
	w   dirWatcher
	gen int
	// チェーンは 2 本 (イベント待ち / 保険のポーリング)。それぞれ二重に張らない (maybeTick と同じ single-flight)。
	evArmed   bool
	pollArmed bool
}

// ensureWatcher は watcher が無ければ作る。作れない環境 (fd 上限・未対応 platform) では false を返し、
// 呼び出し側はポーリングだけに縮退する。
func (c *watchChain) ensureWatcher() bool {
	if c.w == nil {
		w, err := newDirWatcher()
		if err != nil {
			return false
		}
		c.w = w
	}
	return true
}

// eventCmd は fsnotify のイベントを 1 回待ち、debounce の静穏までバーストを畳んでから msg(false) を返す。
// イベント経路が閉じた (watcher が死んだ) ら msg(true)。既に張っている・watcher が無いなら nil。
//
// arm は**張るときだけ**呼ばれ、メッセージを作る関数を返す。観測対象の組み立て (ReadDir を伴う) を
// 張らないときに払わないための形 (以前の各見張りは札を見て先に抜けていた)。
//
// ブロックする Cmd にするのは、外から Msg を送る口 (Program ハンドル) を持たずに済むため
// (bubbletea では Cmd の goroutine で待つのが定石)。🚨 msg は goroutine で呼ばれるので、
// arm の中で値のコピーだけを捕捉すること (Issue のポインタ等を読むと View 側と競合する)。
func (c *watchChain) eventCmd(debounce time.Duration, arm func() (msg func(closed bool) tea.Msg)) tea.Cmd {
	if c.evArmed || c.w == nil {
		return nil
	}
	c.evArmed = true
	w, msg := c.w, arm()
	return func() tea.Msg {
		select {
		case _, ok := <-w.Events():
			if !ok {
				return msg(true)
			}
		case _, ok := <-w.Errors():
			if !ok {
				return msg(true)
			}
			// エラーは握って観測へ倒す (指紋が正本なので、測り直せば辻褄は合う)
		}
		drainWatchEvents(w, debounce)
		return msg(false)
	}
}

// pollCmd は保険のポーリング (interval 後に msg を返す)。既に張っているなら nil。arm は eventCmd と同じ (張るときだけ呼ぶ)。
func (c *watchChain) pollCmd(interval time.Duration, arm func() (msg func() tea.Msg)) tea.Cmd {
	if c.pollArmed {
		return nil
	}
	c.pollArmed = true
	msg := arm()
	return tea.Tick(interval, func(time.Time) tea.Msg { return msg() })
}

// release は観測を届けたチェーンの札を降ろす。🚨 降ろすのは**届けた方だけ**: 両方降ろすと、まだ
// w.Events でブロックしている goroutine が生きているのに evArmed が false になり、single-flight を
// すり抜けて 2 本目が張られる = 観測 1 回ごとに goroutine が 1 本ずつ積み上がる。
func (c *watchChain) release(fromEvent bool) {
	if fromEvent {
		c.evArmed = false
	} else {
		c.pollArmed = false
	}
}

// dropDeadWatcher はイベント経路が閉じた (watcher が死んだ: fd 回収・NFS 等) ときに watcher を畳む。
// ポーリングは続けるので無音にはならない。世代を進めるか (飛んでいる観測を捨てるか) は各見張りが決める。
func (c *watchChain) dropDeadWatcher() {
	c.evArmed = false
	if c.w != nil {
		_ = c.w.Close()
		c.w = nil
	}
}

// close は watcher を閉じる (後始末)。札と世代は各見張りが状態ごと作り直す。
func (c *watchChain) close() {
	if c.w != nil {
		_ = c.w.Close()
	}
}
