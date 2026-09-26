package fake

import (
	"errors"
	"fmt"
	"time"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/diskuse"
	"pro-con/eventlog"
	"pro-con/store"
)

// 設定画面 (issue 456) の模擬。変える所は模擬の枠を変え、見る所のプロセスは模擬の PG を出す。ディスクは測らない (模擬の記録は小さく、
// 本物の worktree を数えると模擬が本物の置き場を読むことになる)。

var (
	_ backend.Inspector = (*Sim)(nil)
	_ backend.EventLog  = (*Sim)(nil)
)

// ErrNoDisk は模擬のモードでディスクを測ろうとしたとき。
var ErrNoDisk = errors.New("模擬のモードでは測らない (本物のモードで pro-con du と同じものを出す)")

// Procs は模擬の dispatcher・PM・PG (pid は無い)。
func (s *Sim) Procs() ([]backend.Proc, error) {
	rows := []backend.Proc{{Role: "dispatcher", State: "動いている (模擬)"}, {Role: "PM", State: "動いている (模擬)", Session: "pm000001"}}
	for _, c := range s.cards {
		if c.State == card.Running {
			rows = append(rows, backend.Proc{Role: "PG", State: c.State.Label(), Card: c.ID, Session: c.Session, Age: s.now.Sub(c.Since), Command: c.Exec.Command})
		}
	}
	return rows, nil
}

// DiskUsage は模擬では測らない。
func (s *Sim) DiskUsage() (diskuse.Usage, error) { return diskuse.Usage{}, ErrNoDisk }

// setConfig は模擬の枠を変える (検査は pro-con config と同じ store.CheckSetting。本物と違い、次の刻みを待たずに効く)。
func (s *Sim) setConfig(c backend.SetConfig) (string, error) {
	apply, err := store.CheckSetting(c.Key, c.Value)
	if err != nil {
		return "", err
	}
	var set store.Settings
	apply(&set)
	switch {
	case c.Key == backend.ConfigLimit && set.Limit > 0:
		s.limit = set.Limit
	case c.Key == backend.ConfigUsage:
		s.usageOff = set.UsageOff
	case c.Key == backend.ConfigReview:
		s.review = set.Review
	}
	return fmt.Sprintf("%s を %s にした (模擬)", c.Key, c.Value), nil
}

// Events は模擬の出来事 (ログのタブの見本。issue 512)。最初の 1 回だけ返し、後から足さない (模擬の刻みは出来事を書かない)。
func (s *Sim) Events() ([]eventlog.Event, error) {
	if s.eventsSent {
		return nil, nil
	}
	s.eventsSent = true
	ev := func(ago time.Duration, kind, cardID, reason string) eventlog.Event {
		return eventlog.Event{At: s.ago(ago), Kind: kind, Card: cardID, Reason: reason}
	}
	out := []eventlog.Event{
		ev(40*time.Minute, eventlog.KindScreen, "", "画面 3f0a1c 持ち主 (pid 0): 開いた (開いている画面 1) (模擬)"),
		ev(40*time.Minute, eventlog.KindSupervisor, "", "supervisor が dispatcher を起こす (pid 0) (模擬)"),
		ev(40*time.Minute, eventlog.KindDispatcher, "", "dispatcher が起きた (pid 0・画面か supervisor が起こした) (模擬)"),
		ev(40*time.Minute, eventlog.KindMonitor, "", "見張りを起こす (模擬)"),
		ev(39*time.Minute, eventlog.KindLaunch, "PM", "PM を再開して依頼の列のカードと PG の質問を知らせた (pm000001: C-011) (模擬)"),
		ev(38*time.Minute, eventlog.KindApply, "C-011", "C-011: 分解済みへ (模擬)"),
		ev(37*time.Minute, eventlog.KindLaunch, "C-005", "C-005 に PG を起動した (3feb6001) (模擬)"),
		ev(37*time.Minute, eventlog.KindRegister, "", "PG の session を 1 本登録した (模擬)"),
	}
	for i := 30; i > 25; i-- { // 同じ失敗の繰り返し (1 行に畳まれる)
		out = append(out, ev(time.Duration(i)*time.Minute/2, eventlog.KindLaunch, "C-007", "C-007 の PG を起動できない: repo の場所が設定に無い (模擬)"))
	}
	return append(out,
		ev(12*time.Minute, eventlog.KindSupervisor, "", "dispatcher が落ちた (signal: killed。10m0s の間に 1 回目)。10s 後に起こし直す (模擬)"),
		ev(12*time.Minute-10*time.Second, eventlog.KindDispatcher, "", "dispatcher が起きた (pid 0・画面か supervisor が起こした) (模擬)"),
		ev(5*time.Minute, eventlog.KindUpgrade, "", "dispatcher が新版に切り替わった (模擬)"),
		ev(2*time.Minute, eventlog.KindStop, "C-003", "C-003: 閉じたので PG の session を止めた (worktree とブランチは残す) (模擬)"),
	), nil
}
