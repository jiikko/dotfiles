package dispatcher

// 取り込みの係 (issue 487): PG が終えたカード (レビューの列) をレビューし、master へ取り込むか差し戻す Claude の session。
// 分担は 2026-09-25 のユーザーの決定 (437): 依頼の分解と PG の質問への回答は PM、レビュー・差し戻し・完了・master への取り込みは取り込みの係。
// 起こし方・起こし直し・終了で止めるのは役の状態機械 (role.go)。ここは知らせる物と知らせの文だけ。指示の正本は integrator-guide.md。

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// IntegratorCardID は起動の記録で取り込みの係の行に付けるカード ID。カード ID (C-%03d) とも PMCardID とも重ならない。
const IntegratorCardID = "INT"

// integratorRole は取り込みの係の役。
var integratorRole = &role{
	cardID: IntegratorCardID, name: "取り込みの係", file: store.IntegratorStateFile, prefix: "pc-int-",
	intro: "あなたは pro-con の取り込みの係です。次の指示書に従う (`pro-con card guide --integrator` でいつでも読み直せる)。",
	told:  "レビューの列のカードを知らせた",
	guide: func(d *Dispatcher) string { return d.IntegratorGuide },
	off:   func(d *Dispatcher) bool { return d.IntegratorOff },
	key:   integratorKey, notice: integratorNotice, labels: integratorLabels,
}

// integratorKey は取り込みの係に知らせる物の鍵: レビューの列のカードの ID とレビューの列に入った時刻
// (差し戻して PG が直し、またレビューの列に戻ったら、離れたのを見ていなくても別の鍵になる = また知らせる)。
// 係が人に回した (card handoff) カードは、またレビューの列に入り直すまで知らせない。
func integratorKey(c card.Card) (string, bool) {
	if c.Archived || c.State != card.Review || c.HandedOff() { // 人に回したカードは係に片付けられない (残すと係を起こす理由になり続ける)
		return "", false
	}
	return c.ID + "@" + c.Since.UTC().Format(time.RFC3339Nano), true
}

// integratorLabels は出来事に書く、知らせたカードの ID。
func integratorLabels(keys []string) string {
	var out []string
	for _, k := range keys {
		id, _, _ := strings.Cut(k, "@")
		out = append(out, id)
	}
	return strings.Join(out, ", ")
}

// integratorNotice は取り込みの係に渡す知らせ。新しくレビューの列に来たカードを ID・題・repo・PG の worktree で並べ、
// 知らせ済みで残っているものは ID だけ添える。指示は書かない (指示の正本は integrator-guide.md)。
func integratorNotice(d *Dispatcher, cards []card.Card, untold, pending []string) string {
	var b strings.Builder
	b.WriteString("pro-con: 次のカードを指示書のとおりに扱って。\n")
	var rest []string
	for _, c := range card.Board(cards) { // 人がレーンで並べた順 (上ほど優先。issue 470)
		k, ok := integratorKey(c)
		if !ok || !slices.Contains(pending, k) {
			continue
		}
		if !slices.Contains(untold, k) {
			rest = append(rest, c.ID)
			continue
		}
		wt := ""
		if path, ok := d.Repos[c.Repo]; ok {
			wt = WorktreePath(path, c)
		}
		fmt.Fprintf(&b, "- レビュー待ち %s「%s」(repo: %s / PG の worktree: %s)\n", c.ID, c.Title, orNone(c.Repo), orNone(wt))
	}
	if len(rest) > 0 {
		fmt.Fprintf(&b, "- まだレビューの列に残っている: %s\n", strings.Join(rest, ", "))
	}
	b.WriteString("中身は `pro-con card show <カード>` で読む。\n")
	return b.String()
}
