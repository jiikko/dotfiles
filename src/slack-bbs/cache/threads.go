package cache

import (
	"time"

	"slack-bbs/source"
)

// ThreadsVersion はスレッドのファイルの形のバージョン。Raw・Summary の Version とは別に数える (Version を上げると全部の板の分類を
// claude でやり直すことになる。スレッドの形だけを変えたときに、分類まで捨てない)。
const ThreadsVersion = 1

// Threads は板が前回読んだスレッドの返信。slack-cli の形のまま持ち、名前の解決と期間での切り分けは読み直すたびに
// board.Build がやり直す (改名と期間の窓の動きは使い回しても反映される)。
type Threads struct {
	Version int       `json:"version"`
	SavedAt time.Time `json:"saved_at"`
	Items   []Thread  `json:"threads"`
}

// Thread はスレッド 1 本。ReplyCount と LatestReply は、読んだときに history の親が持っていた値。
// 次の読み直しで親の値がどちらも同じなら、読み直さずに Msgs を使う (返信の編集は取り逃がす。全部を取り直す R で拾う)。
type Thread struct {
	Ch          string           `json:"ch"` // スレッドのチャンネル (グループの板ではメンバーのチャンネル)
	Ts          string           `json:"ts"` // 親の ts
	ReplyCount  int              `json:"reply_count"`
	LatestReply string           `json:"latest_reply"`
	Msgs        []source.Message `json:"messages"`
}

// LoadThreads は板 id のスレッドを読む。無い・バージョンが違うときは ok が偽 (誤りではない)。ok が偽なら Threads は空。
func (s Store) LoadThreads(id string) (Threads, bool, error) {
	var t Threads
	if ok, err := s.load(id, "threads", &t, func() bool { return t.Version == ThreadsVersion }); !ok {
		return Threads{}, false, err
	}
	return t, true, nil
}

// SaveThreads は板 id のスレッドを書く (Version は書くときに入れる)。
func (s Store) SaveThreads(id string, t Threads) error {
	t.Version = ThreadsVersion
	return s.save(id, "threads", t)
}
