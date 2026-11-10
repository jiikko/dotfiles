package view

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/subs"
)

// BenchmarkLinesBigTopic は大きい話題 (500 発言) を選んだ画面の 1 回の描画。知らせが動く間は 1 秒 60 回描くので、本文の欄を
// 描くたびに折り返し直すと 1 回 2.5ms かかっていた (本文の欄の覚え・bodyCache で約 0.2ms)。
// go test ./view -run '^$' -bench LinesBigTopic -benchmem
func BenchmarkLinesBigTopic(b *testing.B) {
	var chans []subs.Channel
	for i := range 10 {
		chans = append(chans, subs.Channel{ID: fmt.Sprintf("C%d", i), Name: fmt.Sprintf("ch%d", i)})
	}
	load := func(_ context.Context, ch subs.Channel) (render.Page, error) {
		var ts []board.Topic
		for k := range 40 {
			n := 3
			if k == 0 {
				n = 500 // 最後の発言が一番新しいので一覧の先頭 (開いたときに選ばれる)
			}
			var msgs []board.Msg
			for j := range n {
				msgs = append(msgs, board.Msg{Ts: board.TsOf(int64(1_000_000 + (39-k)*100 + j/10)), Name: fmt.Sprintf("user%d", j%7),
					Text: strings.Repeat("本文の発言です。PR 1234 を「確認」した。", 3), New: true})
			}
			ts = append(ts, board.Topic{Title: fmt.Sprintf("話題 %d", k), State: board.StateOpen, Units: []board.Unit{{ID: msgs[0].Ts, Thread: true, Msgs: msgs}}})
		}
		return render.Page{Channel: ch.ID, ChannelName: ch.Name, Topics: ts}, nil
	}
	m := quiet(New(context.Background(), chans, load, nil, func() time.Time { return time.Unix(1_000_000, 0) }))
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	for i := range chans {
		m.Update(m.reload(i)())
	}
	m.Lines()
	if tp, ok := m.selected(); !ok || len(tp.Units[0].Msgs) != 500 {
		b.Fatal("大きい話題を選べていない (測る対象が違う)")
	}
	b.ResetTimer()
	for range b.N {
		m.Lines()
	}
}
