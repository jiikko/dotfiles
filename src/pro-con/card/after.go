package card

import (
	"slices"
	"time"
)

// Blockers は c.After のうち、まだ完了していないカードの ID。空なら起動してよい。完了は終わり方を問わない
// (却下された前のカードも待たない。後のカードは今の master を読んで作る)。記録 (cards) に無い相手は待たない: 記録から外れるのは
// 完了して書庫へ移ったカード (issue 478) と削除したカードだけで、どちらも戻らない (まだ振っていない番号は plan の適用が除ける)。
func Blockers(cards []Card, c Card) []string {
	var out []string
	for _, id := range c.After {
		if i := slices.IndexFunc(cards, func(x Card) bool { return x.ID == id }); i >= 0 && cards[i].State != Done {
			out = append(out, id)
		}
	}
	return out
}

// HeldBy は順番が今 c の起動を止めている前のカード (dispatcher の割り当てと詳細の「今の待ち」が読む)。順番が止めるのは初めての起動だけで、
// 一度起動したカードの再開・起動の結果が分からないカード (立っているかもしれない) は止めない。
func HeldBy(cards []Card, c Card) []string {
	if c.Session != "" || c.Launching != "" {
		return nil
	}
	return Blockers(cards, c)
}

// Drop は id のカードを外した集合を返す。そのカードを前に持つカードの After からも外し、履歴に残す
// (削除は完了と同じく待ちを解く。外さないと相手の無い順番が残り、不変条件の違反で以後の適用が止まる)。cards は書き換えない。
func Drop(cards []Card, id string, now time.Time) []Card {
	out := make([]Card, 0, len(cards))
	for _, c := range cards {
		if c.ID == id {
			continue
		}
		if slices.Contains(c.After, id) {
			c.After = slices.DeleteFunc(slices.Clone(c.After), func(a string) bool { return a == id })
			c.History = append(slices.Clone(c.History), Event{At: now, Text: id + " が削除されたので、その完了を待たない"})
		}
		out = append(out, c)
	}
	return out
}

// afterViolations は順番が循環しているカード (自分に戻る) を出す。🚨 相手が記録に無いことは違反にしない: 前のカードが書庫へ移ると
// 記録から外れ、違反にすると書庫への移動 (store.Archive) がまとめて止まる。
func afterViolations(cards []Card) []Violation {
	byID := make(map[string]Card, len(cards))
	for _, c := range cards {
		byID[c.ID] = c
	}
	var out []Violation
	for _, c := range cards {
		if reaches(byID, c.After, c.ID) {
			out = append(out, Violation{c.ID, "順番が循環している (前のカードを辿ると自分に戻る)"})
		}
	}
	return out
}

// reaches は from から After を辿って target に着くか。
func reaches(byID map[string]Card, from []string, target string) bool {
	seen := map[string]bool{}
	stack := slices.Clone(from)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == target {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, byID[id].After...)
	}
	return false
}
