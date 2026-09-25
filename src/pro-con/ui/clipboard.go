package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"termsafe"

	"pro-con/card"
)

// pbcopy はテキストを OS のクリップボードへ入れる (macOS 専用の repo なので pbcopy だけ)。
// ハングしても TUI を止めないよう timeout を付ける。Model.copy から呼ぶ (テストは差し替える)。
func pbcopy(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(text)
	cmd.WaitDelay = time.Second // ctx の kill 後に stdin の書き込みで Wait が止まり続けないように
	return cmd.Run()
}

// cardText は y でコピーするカードの中身。🚨 カードの本文は将来 PG の出力 (自分以外が書いた文字列) を含むので、
// termsafe.PlainBlock を通して制御文字・エスケープを落とす (貼った先の端末で発火させない)。改行だけ残す。
func (m *Model) cardText(c card.Card) string {
	var refs []string
	for _, r := range c.Issues {
		refs = append(refs, r.String()+" ("+r.Status+")")
	}
	link := strings.Join(refs, ", ")
	if link == "" {
		link = "なし"
		if c.Ending != card.EndNone {
			link += " (" + c.Ending.Label() + ")"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", c.ID, c.Title)
	fmt.Fprintf(&b, "repo: %s / 状態: %s / issue: %s\n", orDash(c.Repo), c.State.Label(), link)
	fmt.Fprintf(&b, "\n依頼の原文:\n%s\n", c.Request)
	if c.Wait.Question != "" {
		fmt.Fprintf(&b, "\n質問:\n%s\n", c.Wait.Question)
	}
	return termsafe.PlainBlock(b.String())
}

// yank は選択中のカードのタイトルと内容 (整形した参照) をコピーする (Y。docs/glogx-ui-guide.md の Y = 整形)。
func (m *Model) yank() {
	c, ok := m.selectedCard()
	if !ok {
		m.refuse("コピーするカードが選ばれていない")
		return
	}
	if err := m.copy(m.cardText(c)); err != nil {
		m.fail("コピーに失敗した: " + err.Error())
		return
	}
	m.done(c.ID + " のタイトルと内容をクリップボードへコピーした")
}
