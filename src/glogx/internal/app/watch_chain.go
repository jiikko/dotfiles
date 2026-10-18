package app

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// WatchChain は「fsnotify のイベント待ち + 保険のポーリング」の 2 本のチェーンの札と watcher を持つ
// (issues viewer の見張りと git log の見張りが共有する。機構を 2 実装に分けると、
// 札の扱いの修正を片方にだけ入れる形になる)。
//
// 何を見張り、何を指紋にし、いつ反映するかは各見張りが持つ (集合も指紋も別物)。
// ここが持つのは single-flight の札と、届いたチェーンの札だけを降ろす規律だけ。
//
// zero value は「何も張っていない」。gen は watcher を閉じるたびに各見張りが進め、閉じる前に
// 張ったチェーンの観測を弾くのに使う (メッセージに gen を載せるのは各見張りの仕事)。
type WatchChain struct {
	W   DirWatcher
	Gen int
	// チェーンは 2 本 (イベント待ち / 保険のポーリング)。それぞれ二重に張らない (maybeTick と同じ single-flight)。
	EvArmed   bool
	PollArmed bool
}

// ensureWatcher は watcher が無ければ作る。作れない環境 (fd 上限・未対応 platform) では false を返し、
// 呼び出し側はポーリングだけに縮退する。
func (c *WatchChain) EnsureWatcher() bool {
	if c.W == nil {
		w, err := NewDirWatcher()
		if err != nil {
			return false
		}
		c.W = w
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
func (c *WatchChain) EventCmd(debounce time.Duration, arm func() (msg func(closed bool) tea.Msg)) tea.Cmd {
	if c.EvArmed || c.W == nil {
		return nil
	}
	c.EvArmed = true
	w, msg := c.W, arm()
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
func (c *WatchChain) PollCmd(interval time.Duration, arm func() (msg func() tea.Msg)) tea.Cmd {
	if c.PollArmed {
		return nil
	}
	c.PollArmed = true
	msg := arm()
	return tea.Tick(interval, func(time.Time) tea.Msg { return msg() })
}

// release は観測を届けたチェーンの札を降ろす。🚨 降ろすのは**届けた方だけ**: 両方降ろすと、まだ
// w.Events でブロックしている goroutine が生きているのに evArmed が false になり、single-flight を
// すり抜けて 2 本目が張られる = 観測 1 回ごとに goroutine が 1 本ずつ積み上がる。
func (c *WatchChain) Release(fromEvent bool) {
	if fromEvent {
		c.EvArmed = false
	} else {
		c.PollArmed = false
	}
}

// dropDeadWatcher はイベント経路が閉じた (watcher が死んだ: fd 回収・NFS 等) ときに watcher を畳む。
// ポーリングは続けるので無音にはならない。世代を進めるか (飛んでいる観測を捨てるか) は各見張りが決める。
func (c *WatchChain) DropDeadWatcher() {
	c.EvArmed = false
	if c.W != nil {
		_ = c.W.Close()
		c.W = nil
	}
}

// close は watcher を閉じる (後始末)。札と世代は各見張りが状態ごと作り直す。
func (c *WatchChain) Close() {
	if c.W != nil {
		_ = c.W.Close()
	}
}

// DirWatcher は fsnotify.Watcher のうち見張りが使う面だけを切った seam (issue の見張りと
// git log の見張り (gitlog_watch.go) で共用する)。
//
// 🚨 実装を差し替えるためではなく、**CI で不変条件を観測できるようにするため**にある。
// CI (ubuntu-slim) では fsnotify.NewWatcher が通らず watch.w が nil になるので、実 watcher を
// 前提にしたテストはすべて skip され、「消えて戻ったディレクトリを再 Add する」のような配線の
// 退行が CI では一度も検査されない。フェイクを差せば実 fsnotify 無しで startWatch /
// eventCmd の本体をそのまま走らせられる。
// Events/Errors をメソッドにしているのは fsnotify.Watcher がチャネルを**フィールド**で公開して
// いてインタフェースに乗らないため (fsWatcher が薄く包む)。
type DirWatcher interface {
	Add(string) error
	Close() error
	WatchList() []string
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

// fsWatcher は *fsnotify.Watcher を DirWatcher へ合わせるだけのアダプタ (ロジックを持たない)。
type fsWatcher struct{ w *fsnotify.Watcher }

func (f fsWatcher) Add(dir string) error { return f.w.Add(dir) }

func (f fsWatcher) Close() error { return f.w.Close() }

func (f fsWatcher) WatchList() []string { return f.w.WatchList() }

func (f fsWatcher) Events() <-chan fsnotify.Event { return f.w.Events }

func (f fsWatcher) Errors() <-chan error { return f.w.Errors }

// NewDirWatcher は watcher を作る唯一の経路 (テストがフェイクへ差し替える口)。
//
// 🚨 production ではここを分岐させない。差し替えはテストだけの都合。
// 🚨 差し替えは package 変数の書き換えなので、この seam を使うテストで t.Parallel() を呼ばないこと
// (-race が「テスト基盤のデータレース」として落ち、検証対象と無関係な形で失敗する)。
// 🚨 包む前に nil を弾く: interface に nil ポインタを入れると `w == nil` が false になり
// (typed nil)、「watcher を作れない環境ではポーリングへ縮退する」という startWatch / eventCmd /
// pollInterval / handleWatch の nil ガード全部が panic に変わる。fsnotify v1.10.1 は成功時に
// 必ず非 nil を返すので今は起きないが、その契約 1 つに縮退の正しさを乗せない。
var NewDirWatcher = func() (DirWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errors.New("fsnotify.NewWatcher が nil を返した")
	}
	return fsWatcher{w: w}, nil
}

// drainWatchEvents は quiet の間イベントが来なくなるまで吸う (バーストの畳み込み)。
func drainWatchEvents(w DirWatcher, quiet time.Duration) {
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-w.Events():
			if !ok {
				return
			}
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(quiet) // まだ書いている: 静まるまで待つ
		case <-w.Errors():
		case <-timer.C:
			return
		}
	}
}
