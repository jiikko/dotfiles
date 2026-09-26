// Package storetest は、store / dispatcher / 画面の bench が同じ記録で測るための組み立て (issue 528)。テストからだけ import する。
package storetest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// Env は本物の cards.json の写しのパス。あれば BusyDay はそれを置く (本物の記録は repo に入れない)。
const Env = "PRO_CON_BENCH_CARDS"

// BusyDay は dir に「完了して 24 時間以内のカードが溜まった日」の記録を置き、測るときの今の時刻を返す
// (どのカードも完了から 24 時間たっていない時刻。過ぎていると最初の Tick が書庫へ移し、溜まった記録を測れない)。
// Env があればその写しを、無ければ 2026-09-27 の本物 (60 枚・履歴 1,076 行・402KB) に形を寄せた合成の記録
// (60 枚 × 履歴 18 行) を書く。
func BusyDay(tb testing.TB, dir string) time.Time {
	tb.Helper()
	if p := os.Getenv(Env); p != "" {
		data, err := os.ReadFile(p)
		if err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, store.StateFile), data, 0o600); err != nil {
			tb.Fatal(err)
		}
		st, err := store.Load(dir)
		if err != nil {
			tb.Fatal(err)
		}
		var now time.Time
		for _, c := range st.Cards {
			if c.Since.After(now) {
				now = c.Since
			}
		}
		return now.Add(time.Minute)
	}
	now := time.Date(2026, 9, 27, 2, 0, 0, 0, time.UTC)
	hist := make([]card.Event, 18)
	for i := range hist {
		hist[i] = card.Event{At: now, Text: "PG を起動した (session xxxxxxxx)。テストの係に頼んだ: make test。" + strings.Repeat("結果を読んで次へ進む。", 8)}
	}
	req := strings.Repeat("issues/epic/415 の進め方のとおりに直して、before / after を実測して issue に書く。", 20)
	var cs []card.Card
	for i := range 60 {
		c := card.Card{ID: fmt.Sprintf("C-%03d", i+1), Title: "溜まった日のカード", Request: req, Prompt: req, Repo: "dotfiles", Owner: "PM",
			State: card.Done, Ending: card.EndAnswered, Since: now.Add(-time.Hour), History: hist,
			Issues: []card.IssueRef{{Repo: "dotfiles", Number: 528}}}
		if i >= 56 { // 動いているのは 4 枚
			c.State, c.Ending = card.Requested, 0
		}
		cs = append(cs, c)
	}
	if err := store.Update(dir, func(s *store.State) error { s.Cards, s.NextID = cs, len(cs)+1; return nil }); err != nil {
		tb.Fatal(err)
	}
	return now
}
