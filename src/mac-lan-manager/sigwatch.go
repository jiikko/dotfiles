package main

import (
	"os"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
)

// sigWatch は「止める」合図 (INT / TERM、Run が戻った後は HUP も) を、画面を閉じた後の待ち (finishBG) のために覚える。
//
// 合図 × いつ の期待:
//
//		          画面の中                      ssh に端末を渡している間  閉じてから待ち始めるまで  待っている間
//		INT/TERM  bubbletea が終える・待たない  捨てる                    死なない・待たない        待つのをやめる
//		HUP       既定どおり死ぬ                既定どおり死ぬ            死なない・待たない        待つのをやめる
//
//	  - INT / TERM は Run の前から聞く。後から聞くと、bubbletea が Run を終えた最初の合図が見えず裏の ssh を最大 20 秒待つ。
//	    聞いていない区間 (Run の後・m.Close の最中) があると、そこへ来た INT で既定どおり死に、dns-sd が残る
//	  - ssh に端末を渡している間 (tea.Exec) の合図は捨てる: ssh は同じプロセスグループに居るので、ssh への Ctrl-C がこちらにも
//	    届く。bubbletea も捨てる。覚えると「止める」が立ったまま残り、後で q で終えたときに待たない
//	  - HUP は Run が戻ってから聞く (前に聞くと既定の終了が外れ、端末を閉じても画面が残る)
//	  - QUIT・TSTP は聞かない: ssh に端末を渡している間と待っている間は端末が cooked なので、Ctrl-\ / Ctrl-Z は同じ
//	    プロセスグループの裏の ssh・dns-sd ごと既定どおりに効く
type sigWatch struct {
	inExec atomic.Bool
	stop   chan struct{}
	once   sync.Once
}

// newSigWatch は sigs を読み続ける。sigs は呼び出し側が signal.Notify で張る (テストは直接送る)。
func newSigWatch(sigs <-chan os.Signal) *sigWatch {
	w := &sigWatch{stop: make(chan struct{})}
	go func() {
		for range sigs {
			if !w.inExec.Load() {
				w.once.Do(func() { close(w.stop) })
			}
		}
	}()
	return w
}

// Done は「止める」合図を受けたら閉じる。
func (w *sigWatch) Done() <-chan struct{} { return w.stop }

// Guard は c を、走っている間だけ「ssh に端末を渡している間」にする。立て下ろしを c の Run の入口と出口に置くのは、bubbletea が
// 合図を捨てる区間 (releaseTerminal〜RestoreTerminal) の内側に収めるため。Update で tea.Exec を返すときに立てて execDoneMsg で
// 下ろす形にしない: その前後の ms の窓で bubbletea は終えるのにこちらは捨て、裏の ssh を待つ。
// 逆の向きの窓 (releaseTerminal の始め〜Run の入口、Run の出口〜RestoreTerminal) は残る: そこへ外から kill が来ると bubbletea は
// 捨て、こちらは覚えて後で待たない。bubbletea の ignoreSignals は外から読めず一致させられないので、軽い方 (待たずに終える。
// 「待たずに終えた」と出る) を残す
func (w *sigWatch) Guard(c tea.ExecCommand) tea.ExecCommand {
	return &guardedExec{ExecCommand: c, w: w}
}

type guardedExec struct {
	tea.ExecCommand
	w *sigWatch
}

func (g *guardedExec) Run() error {
	g.w.inExec.Store(true)
	defer g.w.inExec.Store(false)
	return g.ExecCommand.Run()
}
