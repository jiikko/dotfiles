package live

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/store"
)

// submitted は受付の箱の依頼を適用し、その結果を返す (dispatcher の代わり)。
func submitted(t *testing.T, b *Backend) []store.Result {
	t.Helper()
	res, err := store.Apply(b.dir, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// 追加オーダー (追記・方針変更) と btw は、そのカードへの依頼として箱に置く。別件は元のカードの子の新しい依頼 (PM への指示つき) にする (issue 438)。
func TestApplyOrdersAndBtw(t *testing.T) {
	b, _ := testBackend(t, sessions, nil)
	runningCard(t, b, "C-001", "bbbbbbbb")
	b.Refresh(context.Background())
	for _, cmd := range []backend.Command{
		backend.AddOrder{CardID: "C-001", Kind: card.OrderAppend, Text: "README も"},
		backend.AddOrder{CardID: "C-001", Kind: card.OrderRedirect, Text: "青に"},
		backend.AddOrder{CardID: "C-001", Kind: card.OrderSeparate, Text: "別の画面も\n詳しく"},
		backend.Btw{CardID: "C-001", Question: "今どう?"},
	} {
		if _, err := b.Apply(cmd); err != nil {
			t.Fatalf("%T: %v", cmd, err)
		}
	}
	for _, cmd := range []backend.Command{backend.AddOrder{CardID: "C-001", Text: " "}, backend.Btw{CardID: "C-001"}} {
		if _, err := b.Apply(cmd); !errors.Is(err, backend.ErrEmptyText) {
			t.Fatalf("空の %T を置いた: %v", cmd, err)
		}
	}
	if _, err := b.Apply(backend.AddOrder{CardID: "C-404", Kind: card.OrderSeparate, Text: "x"}); !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("無いカードの別件を置いた: %v", err)
	}
	for _, r := range submitted(t, b) {
		if r.Err != "" {
			t.Fatalf("箱の依頼が除けられた: %+v", r)
		}
	}
	st, _ := store.Load(b.dir)
	c, child := st.Cards[0], st.Cards[1]
	if len(c.Orders) != 2 || c.Orders[0].Kind != card.OrderAppend || c.Orders[1].Kind != card.OrderRedirect || len(c.Btws) != 1 {
		t.Fatalf("オーダーと btw がカードに積まれていない: %+v %+v", c.Orders, c.Btws)
	}
	if child.ParentID != "C-001" || child.Repo != "dotfiles" || child.Title != "別の画面も" || !strings.Contains(child.Prompt, "/w/dotfiles") || !strings.Contains(child.Prompt, "C-001") {
		t.Fatalf("別件が元のカードの子の依頼になっていない: %+v", child)
	}
}

// 片付けは、画面が見ている完了のカード (そのタブの repo の分) だけを頼む。無ければ箱に何も置かない。
func TestApplyClearDoneListsShownCards(t *testing.T) {
	b, _ := testBackend(t, sessions, nil)
	for _, repo := range []string{"dotfiles", "other", "dotfiles"} {
		if _, err := store.Submit(b.dir, store.Request{Kind: "add", Title: "t", Repo: repo}); err != nil {
			t.Fatal(err)
		}
	}
	submitted(t, b)
	if msg, err := b.Apply(backend.ClearDone{}); err != nil || !strings.Contains(msg, "無い") {
		t.Fatalf("完了が無いのに片付けを頼んだ: %q %v", msg, err)
	}
	if err := store.Update(b.dir, func(st *store.State) error {
		for i := range st.Cards[:2] {
			st.Cards[i].State, st.Cards[i].Ending = card.Done, card.EndAnswered
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b.Refresh(context.Background())
	if _, err := b.Apply(backend.ClearDone{Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	if res := submitted(t, b); len(res) != 1 || res[0].Kind != "clear" || res[0].Err != "" {
		t.Fatalf("%+v", res)
	}
	st, _ := store.Load(b.dir)
	if !st.Cards[0].Archived || st.Cards[1].Archived || st.Cards[2].Archived {
		t.Fatalf("タブの repo の完了のカードだけを片付けていない: %v %v %v", st.Cards[0].Archived, st.Cards[1].Archived, st.Cards[2].Archived)
	}
}
