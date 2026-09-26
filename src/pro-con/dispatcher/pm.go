package dispatcher

// PM (人間の依頼を受けてカードを分ける Claude の session) を起こして、依頼の列に来たカードと PG の質問を知らせる (issue 437。設計と失敗モードの表は 437 の本文)。
// PM は 1 つ (415 の論点 6)。起こし方は PG の回答と同じ Launcher.Resume (止めてから --resume)。記録が無ければ起動する。
// 起こし方・起こし直し・終了で止めるのは役の状態機械 (role.go)。ここは PM に知らせる物と知らせの文だけ。

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// PMCardID は起動の記録で PM の行に付けるカード ID。カード ID (C-%03d) とは重ならない。
const PMCardID = "PM"

// pmReviveLimit は、生きていない PM を crash の窓 (defaultCrashWindow) の間に起こし直す回数の上限。
const pmReviveLimit = 3

// pmRole は PM の役。
var pmRole = &role{
	cardID: PMCardID, name: card.PMName, file: store.PMStateFile, prefix: "pc-pm-",
	intro: "あなたは pro-con の PM です。次の指示書に従う (`pro-con card guide` でいつでも読み直せる)。",
	told:  "依頼の列のカードと PG の質問を知らせた",
	guide: func(d *Dispatcher) string { return d.PMGuide },
	off:   func(d *Dispatcher) bool { return d.PMOff },
	key:   pmKey, labels: pmLabels,
	notice: func(_ *Dispatcher, cards []card.Card, untold, pending []string) string {
		return pmNotice(cards, untold, pending)
	},
}

// pmKey は PM に知らせる物の鍵。依頼の列のカードはカード ID、PG の質問はカード ID と質問待ちに入った時刻
// (回答で列を離れてまた質問したら、離れたのを見ていなくても別の鍵になる = また知らせる)。
// 権限の確認と落ちて止めた PG、PM が人に回した質問、PM 自身が人に聞いている問い (498) は PM には片付けられない (人の番。452) ので知らせない
// (残すと PM を起こす理由になり続ける)。PM の問いに人が答えると依頼の列へ戻り、鍵はカード ID のまま知らせ直す (列を離れた間に知らせ済みから外れる)。
func pmKey(c card.Card) (string, bool) {
	switch {
	case c.Archived || c.HandedOff() || c.Wait.FromPM():
		return "", false
	case c.State == card.Requested:
		return c.ID, true
	case c.State == card.Waiting && c.Wait.Kind == card.WaitQuestion:
		return c.ID + "@" + c.Since.UTC().Format(time.RFC3339Nano), true
	}
	return "", false
}

// pmLabels は出来事に書く、知らせた物の短い名前 (質問は「C-001 の質問」)。
func pmLabels(keys []string) string {
	var out []string
	for _, k := range keys {
		if id, _, ok := strings.Cut(k, "@"); ok {
			k = id + " の質問"
		}
		out = append(out, k)
	}
	return strings.Join(out, ", ")
}

// pmNotice は PM に渡す知らせ。新しい依頼を ID・題・repo で、新しい PG の質問と PM の質問への人の回答 (498) をそれに文を足して並べ、知らせ済みで残っているものは ID だけ添える。
// 指示は書かない (指示の正本は pm-guide.md)。
func pmNotice(cards []card.Card, untold, pending []string) string {
	var b strings.Builder
	b.WriteString("pro-con: 次のカードを指示書のとおりに扱って (上ほど優先)。\n")
	var restReq, restAsk []string
	for _, c := range card.Board(cards) {
		k, ok := pmKey(c)
		if !ok || !slices.Contains(pending, k) {
			continue
		}
		fresh := slices.Contains(untold, k)
		switch {
		case c.State == card.Requested && fresh && c.PMAnswer != "":
			fmt.Fprintf(&b, "- PM の質問に人が回答した依頼 %s「%s」(repo: %s): %s\n", c.ID, c.Title, orNone(c.Repo), c.PMAnswer)
		case c.State == card.Requested && fresh:
			fmt.Fprintf(&b, "- 新しい依頼 %s「%s」(repo: %s)\n", c.ID, c.Title, orNone(c.Repo))
		case c.State == card.Requested:
			restReq = append(restReq, c.ID)
		case fresh:
			fmt.Fprintf(&b, "- PG の質問 %s「%s」(repo: %s): %s\n", c.ID, c.Title, orNone(c.Repo), c.Wait.Question)
		default:
			restAsk = append(restAsk, c.ID)
		}
	}
	if len(restReq) > 0 {
		fmt.Fprintf(&b, "- まだ依頼の列に残っている: %s\n", strings.Join(restReq, ", "))
	}
	if len(restAsk) > 0 {
		fmt.Fprintf(&b, "- まだ回答していない PG の質問: %s\n", strings.Join(restAsk, ", "))
	}
	b.WriteString("中身は `pro-con card show <カード>` で読む。\n")
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "指定なし"
	}
	return s
}
