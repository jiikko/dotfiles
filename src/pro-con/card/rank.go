package card

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Rank は人が入れ替えたレーンの中の並びの鍵 (issue 470。レーンの中は上ほど優先度が高い)。For が今の Since と同じ間だけ効く:
// レーンを移ると Since が変わるので外す処理を書かなくても外れ、移った先では入った順 (末尾) に着く。
// 🚨 列を変える書き手 (store の遷移・dispatcher の起動 / 再開 / 止めた後の戻し) ごとに外す処理を足さない。Since を進めるだけでよい
type Rank struct {
	Key time.Time `json:",omitzero"`
	For time.Time `json:",omitzero"`
}

// LaneKey はレーンの中の並びの鍵。入れ替えていなければ、その列に入った時刻 (入った順)。
func (c Card) LaneKey() time.Time {
	if !c.Rank.Key.IsZero() && c.Rank.For.Equal(c.Since) {
		return c.Rank.Key
	}
	return c.Since
}

// LaneCompare はレーンの中の並び (鍵、同じなら ID)。画面のカンバン・dispatcher の起動の順・入れ替えの隣が、同じ比べ方を使う。
func LaneCompare(a, b Card) int {
	if c := a.LaneKey().Compare(b.LaneKey()); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}

// ErrLaneEdge は入れ替える隣が無い (レーンの先頭で上へ・末尾で下へ。巻かない)。
var ErrLaneEdge = errors.New("レーンの端なので、それ以上は動かせない")

// Move は id のカードを、同じレーン (同じ列・片付けていない。repo が空でなければその repo のカードだけ = 画面のタブで見えている並び) の中で、
// delta (-1 = 上 / +1 = 下) の隣と入れ替える。cards を書き換え、入れ替えた相手の ID を返す。
// 隣は呼んだ時点の並びで決める (画面の古い並びで決めない: キーを速く続けて押しても、押した回数だけ動く)。
func Move(cards []Card, id, repo string, delta int) (string, error) {
	if delta != -1 && delta != 1 {
		return "", fmt.Errorf("動かす向きは上 (-1) か下 (+1): %d", delta)
	}
	i := slices.IndexFunc(cards, func(c Card) bool { return c.ID == id })
	if i < 0 {
		return "", fmt.Errorf("カード %q が無い", id)
	}
	if cards[i].Archived {
		return "", errors.New("片付けたカードは動かせない")
	}
	var lane []int // レーンの全部 (repo を問わない)。鍵を振り直すのは全部、隣を探すのは repo の中
	for j, c := range cards {
		if c.State == cards[i].State && !c.Archived {
			lane = append(lane, j)
		}
	}
	slices.SortStableFunc(lane, func(a, b int) int { return LaneCompare(cards[a], cards[b]) })
	// 同じ鍵 (同じ Tick で列に入ったカード) を入れ替えても並びが変わらないので、並びを保ったまま鍵をばらす
	keys := make([]time.Time, len(lane))
	for k, j := range lane {
		keys[k] = cards[j].LaneKey()
		if k > 0 && !keys[k].After(keys[k-1]) {
			keys[k] = keys[k-1].Add(time.Nanosecond)
		}
	}
	var view []int // lane の中の位置 (repo の中のカードだけ)
	pos := -1
	for k, j := range lane {
		if repo == "" || cards[j].Repo == repo {
			if j == i {
				pos = len(view)
			}
			view = append(view, k)
		}
	}
	if pos < 0 { // repo の外のカードを、その repo のタブから動かそうとした
		return "", fmt.Errorf("カード %s は repo %s のカードではない", id, repo)
	}
	if pos+delta < 0 || pos+delta >= len(view) {
		return "", ErrLaneEdge
	}
	a, b := view[pos], view[pos+delta]
	keys[a], keys[b] = keys[b], keys[a]
	for k, j := range lane {
		if !keys[k].Equal(cards[j].LaneKey()) {
			cards[j].Rank = Rank{Key: keys[k], For: cards[j].Since}
		}
	}
	return cards[lane[b]].ID, nil
}

// MovedText は入れ替えを履歴に残す文 (store と模擬で同じ文)。
func MovedText(delta int, other string) string {
	if delta < 0 {
		return "優先度を上げた (" + other + " の上へ)"
	}
	return "優先度を下げた (" + other + " の下へ)"
}
