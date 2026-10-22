// Package fswatch は glogx の見張り (git log と issues viewer) が共有する「fsnotify のイベント待ち + 保険のポーリング」の札と世代、
// watcher の seam。
package fswatch

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// Chain は「fsnotify のイベント待ち + 保険のポーリング」の 2 本のチェーンの札と watcher を持つ
// (issues viewer の見張りと git log の見張りが共有する。機構を 2 実装に分けると、
// 札の扱いの修正を片方にだけ入れる形になる)。
//
// 何を見張り、何を指紋にし、いつ反映するかは各見張りが持つ (集合も指紋も別物)。
// ここが持つのは single-flight の札と、届いたチェーンの札だけを降ろす規律だけ。
//
// zero value は「何も張っていない」。Gen は watcher を閉じるたびに進め (Advance / DropDeadWatcher)、閉じる前に
// 張ったチェーンの観測を弾くのに使う (メッセージに Gen を載せるのは各見張りの仕事)。
//
// フィールドは別 package のテストが札を置く・読むために公開しているだけで、本番のコードが札と世代を書くのは
// このファイルのメソッドだけ (glogx の structure_ratchet_test.go の TestWatchChainStateWrittenOnlyByItsMethods が止める)。
type Chain struct {
	W   DirWatcher
	Gen int
	// チェーンは 2 本 (イベント待ち / 保険のポーリング)。それぞれ二重に張らない (maybeTick と同じ single-flight)。
	EvArmed   bool
	PollArmed bool
}

// EnsureWatcher は watcher が無ければ作る。作れない環境 (fd 上限・未対応 platform) では false を返し、
// 呼び出し側はポーリングだけに縮退する。
func (c *Chain) EnsureWatcher() bool {
	if c.W == nil {
		w, err := NewDirWatcher()
		if err != nil {
			return false
		}
		c.W = w
	}
	return true
}

// EventCmd は fsnotify のイベントを 1 回待ち、debounce の静穏までバーストを畳んでから msg(false) を返す。
// イベント経路が閉じた (watcher が死んだ) ら msg(true)。既に張っている・watcher が無いなら nil。
//
// arm は**張るときだけ**呼ばれ、メッセージを作る関数を返す。観測対象の組み立て (ReadDir を伴う) を
// 張らないときに払わないための形 (以前の各見張りは札を見て先に抜けていた)。
//
// ブロックする Cmd にするのは、外から Msg を送る口 (Program ハンドル) を持たずに済むため
// (bubbletea では Cmd の goroutine で待つのが定石)。🚨 msg は goroutine で呼ばれるので、
// arm の中で値のコピーだけを捕捉すること (Issue のポインタ等を読むと View 側と競合する)。
func (c *Chain) EventCmd(debounce time.Duration, arm func() (msg func(closed bool) tea.Msg)) tea.Cmd {
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

// PollCmd は保険のポーリング (interval 後に msg を返す)。既に張っているなら nil。arm は EventCmd と同じ (張るときだけ呼ぶ)。
func (c *Chain) PollCmd(interval time.Duration, arm func() (msg func() tea.Msg)) tea.Cmd {
	if c.PollArmed {
		return nil
	}
	c.PollArmed = true
	msg := arm()
	return tea.Tick(interval, func(time.Time) tea.Msg { return msg() })
}

// Release は観測を届けたチェーンの札を降ろす。🚨 降ろすのは**届けた方だけ**: 両方降ろすと、まだ
// w.Events でブロックしている goroutine が生きているのに evArmed が false になり、single-flight を
// すり抜けて 2 本目が張られる = 観測 1 回ごとに goroutine が 1 本ずつ積み上がる。
func (c *Chain) Release(fromEvent bool) {
	if fromEvent {
		c.EvArmed = false
	} else {
		c.PollArmed = false
	}
}

// DropDeadWatcher はイベント経路が閉じた (watcher が死んだ: fd 回収・NFS 等) ときに watcher を畳む。
// ポーリングは続けるので無音にはならない。世代を進めるか (飛んでいる観測を捨てるか) は各見張りが決める。
// 進めるときはポーリングの札も降ろす: 旧世代の tick は届いても捨てられるので、張り直さないとポーリングが止まる。
// 🚨 札だけを降ろして世代を進めない形にしない: 旧い tick と張り直した tick が同じ世代で届き、ポーリングが二重になる。
func (c *Chain) DropDeadWatcher(advanceGen bool) {
	c.EvArmed = false
	if c.W != nil {
		_ = c.W.Close()
		c.W = nil
	}
	if advanceGen {
		c.Gen++
		c.PollArmed = false
	}
}

// Close は watcher を閉じる (後始末)。札と世代は触らない (作り直すなら Advance)。
func (c *Chain) Close() {
	if c.W != nil {
		_ = c.W.Close()
	}
}

// Advance は watcher を閉じ、世代を 1 つ進めた空の札を返す (閉じる前に張ったチェーンの観測を、作り直した後の状態へ効かせない)。
func (c *Chain) Advance() Chain {
	c.Close()
	return Chain{Gen: c.Gen + 1}
}

// DirWatcher は fsnotify.Watcher のうち見張りが使う面だけを切った seam (issue の見張りと
// git log の見張り (gitlog_watch.go) で共用する)。
//
// 🚨 実装を差し替えるためではなく、**不変条件をイベントの届き方に依存せずに観測できるようにするため**にある。
// 実 fsnotify のイベントは届く時刻と束ね方が OS とファイルシステムで変わり、作れない環境 (fd の上限・未対応の platform) もある。
// フェイクを差せば実 fsnotify 無しで startWatch / eventCmd の本体をそのまま走らせ、「消えて戻ったディレクトリを再 Add する」
// のような配線の退行を決定的に検査できる。
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
		case <-w.Errors(): //nolint:errcheck // watcher のエラーは読み捨てる (止めずに保険のポーリングに任せる)
		case <-timer.C:
			return
		}
	}
}
