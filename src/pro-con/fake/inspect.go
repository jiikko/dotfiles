package fake

import (
	"errors"
	"fmt"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/diskuse"
	"pro-con/store"
)

// 設定画面 (issue 456) の模擬。変える所は模擬の枠を変え、見る所のプロセスは模擬の PG を出す。ディスクは測らない (模擬の記録は小さく、
// 本物の worktree を数えると模擬が本物の置き場を読むことになる)。

var _ backend.Inspector = (*Sim)(nil)

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
	case c.Key == backend.ConfigReview:
		s.review = set.Review
	}
	return fmt.Sprintf("%s を %s にした (模擬)", c.Key, c.Value), nil
}
