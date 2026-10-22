package ledger

import (
	"errors"
	"slices"
	"time"
)

// Upsert は 1 台を足す / 置き換える (テストの準備用)。本番の Store には持たせない (行を丸ごと置き換える口は、開いた時の古い行で
// 別のプロセスの書き込みを消す。Store の注記)。ロックを取ってから読み直して書く
func (s FileStore) Upsert(h Host) error {
	h.Host = Key(h.Host)
	if h.Host == "" {
		return errors.New("ホスト名が空")
	}
	return s.modify(func(hosts []Host) []Host {
		h.UpdatedAt = s.now().UTC().Truncate(time.Second)
		if i := slices.IndexFunc(hosts, func(x Host) bool { return x.Host == h.Host }); i >= 0 {
			hosts[i] = h
			return hosts
		}
		return append(hosts, h)
	})
}
