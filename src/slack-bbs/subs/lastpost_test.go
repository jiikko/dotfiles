package subs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"slack-bbs/source"
)

var lpNow = time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)

func lpAll() []source.Channel {
	return []source.Channel{
		{ID: "C1", Name: "dev", IsMember: true, NumMembers: 42},
		{ID: "C2", Name: "ops", IsMember: true, NumMembers: 17},
		{ID: "C3", Name: "zz", IsMember: true, NumMembers: 3},
		{ID: "C5", Name: "random", NumMembers: 812},
	}
}

// 購読の画面は「参加している」「参加していない」のタブに分ける。グループと、購読していて一覧から消えたチャンネルは参加しているタブ。
// h / l でタブを移り、検索はそのタブの中で絞る (タブの数も絞った数)。
func TestPickerTabs(t *testing.T) {
	p := NewPicker(lpAll(), []Channel{{ID: "C9", Name: "gone"}, {ID: "grp00000001", Name: "g", Members: []Channel{{ID: "C1", Name: "dev"}}}})
	p.Resize(80, 20)
	names := func() []string {
		var out []string
		for _, i := range p.visible() {
			out = append(out, p.items[i].ch.Name)
		}
		return out
	}
	if got := names(); !slices.Equal(got, []string{"g", "gone", "dev", "ops", "zz"}) {
		t.Errorf("参加しているタブ: %v", got)
	}
	p.Key("l")
	if got := names(); !slices.Equal(got, []string{"random"}) {
		t.Errorf("参加していないタブ: %v", got)
	}
	if l := strings.Join(p.Lines(), "\n"); !strings.Contains(l, "参加している (5)") || !strings.Contains(l, "参加していない (1)") {
		t.Errorf("タブの数:\n%s", l)
	}
	// 画面の行数はタブの件数によらず端末の高さ (案内の 2 行がタブを移るたびに上下しない)
	if n := len(p.Lines()); n != 20 {
		t.Errorf("件数の少ないタブの行数 = %d, want 20", n)
	}
	p.Key("h")
	if n := len(p.Lines()); n != 20 {
		t.Errorf("参加しているタブの行数 = %d, want 20", n)
	}
	p.Key("/")
	p.Key("o")
	if got := names(); !slices.Equal(got, []string{"gone", "ops"}) || !strings.Contains(strings.Join(p.Lines(), "\n"), "参加していない (1)") {
		t.Errorf("検索はタブの中で絞る: %v\n%s", got, strings.Join(p.Lines(), "\n"))
	}
}

// 表の最終投稿の欄: 前に取った値 (今年は月日・去年より前は年月)、発言が無ければ「none」、取り直しの列にあってまだ無ければ「...」、
// 取れなかったら「err」、参加していないチャンネルは取らないので「-」 (印は日付と同じ列なので半角)。グループはメンバーで一番新しい日。人数の欄は人数 (グループはチャンネルの数)。
func TestPickerLastPostColumn(t *testing.T) {
	p := NewPicker(lpAll(), []Channel{{ID: "grp00000001", Name: "g", Members: []Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}}}})
	p.SetLastPosts(map[string]LastPost{
		"C1": {Ts: "1794400000.000000", At: lpNow}, // 2026/11/11
		"C2": {Ts: "1760000000.000000", At: lpNow}, // 2025/10/09
		"C3": {Ts: "", At: lpNow},
	})
	p.StartFetch(context.Background(), &LastPosts{Get: func(context.Context, string) (string, error) { return "", nil }, Now: func() time.Time { return lpNow }})
	p.Resize(140, 20) // 見出しの後ろの進み具合まで切らずに出す幅
	line := func(name string) string {
		for _, l := range p.Lines() {
			if strings.Contains(l, name) {
				return strings.Join(strings.Fields(l), " ")
			}
		}
		return ""
	}
	for name, want := range map[string]string{"#dev": "11/11 42", "#ops": "2025/10 17", "#zz": "none 3", "📂g": "11/11 2ch"} {
		if got := line(name); !strings.HasSuffix(got, want) {
			t.Errorf("%s の行 = %q, want 末尾 %q", name, got, want)
		}
	}
	p.SetLastPosts(nil)
	p.StartFetch(context.Background(), &LastPosts{Get: func(context.Context, string) (string, error) { return "", nil }, Now: func() time.Time { return lpNow }})
	if got := line("#dev"); !strings.HasSuffix(got, "... 42") {
		t.Errorf("取り直し中で値が無い行 = %q", got)
	}
	p.fetch.queue = p.fetch.queue[1:]
	p.fetch.failed["C1"] = true
	if got := line("#dev"); !strings.HasSuffix(got, "err 42") || !strings.Contains(p.Lines()[0], "最終投稿を取れなかった 1 件") {
		t.Errorf("取れなかった行 = %q / 見出し %q", got, p.Lines()[0])
	}
	p.Key("l")
	if got := line("#random"); !strings.HasSuffix(got, "- 812") {
		t.Errorf("参加していないチャンネルの行 = %q", got)
	}
}

// 最終投稿は参加しているチャンネルだけを、購読しているもの → 一覧の順に 1 件ずつ取り、取るたびに書く。1 時間より新しく取ったものは取らない。
// 取れなかったものは前の値のまま数を見出しに出す。前の取り直し・画面を閉じた後の結果は捨てる。取り消されたら列を止める。
func TestPickerFetchesLastPosts(t *testing.T) {
	var asked []string
	var saved map[string]LastPost
	fail := map[string]bool{"C2": true}
	lp := &LastPosts{
		Get: func(_ context.Context, id string) (string, error) {
			asked = append(asked, id)
			if fail[id] {
				return "", errors.New("x")
			}
			return "1794400000.000000", nil
		},
		Save: func(m map[string]LastPost) error { saved = m; return nil },
		Now:  func() time.Time { return lpNow },
	}
	p := NewPicker(lpAll(), []Channel{{ID: "C3", Name: "zz"}})
	p.Resize(140, 20)
	p.SetLastPosts(map[string]LastPost{"C1": {Ts: "1.000000", At: lpNow.Add(-time.Minute)}})
	cmd := p.StartFetch(context.Background(), lp)
	for cmd != nil {
		cmd = p.HandleLastPost(cmd().(LastPostMsg))
	}
	if !slices.Equal(asked, []string{"C3", "C2"}) {
		t.Errorf("取った順 = %v, want [C3 C2] (購読している C3 が先、新しく取った C1 と参加していない C5 は取らない)", asked)
	}
	if saved["C3"].Ts != "1794400000.000000" || saved["C1"].Ts != "1.000000" {
		t.Errorf("書いた値 = %v", saved)
	}
	if !strings.Contains(p.Lines()[0], "最終投稿を取れなかった 1 件") {
		t.Errorf("見出し = %q", p.Lines()[0])
	}
	// 前の取り直しの結果は捨てる。閉じてすぐ開き直した別の画面の取り直しの結果も捨てる (番号ではなく取り直しの実体で照合する)
	stale := p.StartFetch(context.Background(), lp)
	_ = p.StartFetch(context.Background(), lp)
	if stale != nil {
		if next := p.HandleLastPost(stale().(LastPostMsg)); next != nil {
			t.Error("前の取り直しの結果で列を進めた")
		}
	}
	a, b := NewPicker(lpAll(), nil), NewPicker(lpAll(), nil)
	fromA := a.StartFetch(context.Background(), lp)
	b.StartFetch(context.Background(), lp)
	if next := b.HandleLastPost(fromA().(LastPostMsg)); next != nil || len(b.fetch.failed) != 0 || b.Fetching() != 3 {
		t.Errorf("別の画面の取り直しの結果を受けた: next=%v failed=%v 残り=%d", next != nil, b.fetch.failed, b.Fetching())
	}
	// 取り消されたら列を止める
	ctx, cancel := context.WithCancel(context.Background())
	lp2 := &LastPosts{Get: func(ctx context.Context, _ string) (string, error) { return "", ctx.Err() }, Now: lp.Now}
	q := NewPicker(lpAll(), nil)
	cmd = q.StartFetch(ctx, lp2)
	cancel()
	if next := q.HandleLastPost(cmd().(LastPostMsg)); next != nil || q.Fetching() != 0 {
		t.Errorf("取り消した後も列が進む: next=%v 残り=%d", next != nil, q.Fetching())
	}
}

// 取り直した一覧で増えた参加しているチャンネルも取る (列が止まっていたら ResumeFetch で再開する)。
func TestPickerRefreshQueuesNewChannels(t *testing.T) {
	var asked []string
	lp := &LastPosts{Get: func(_ context.Context, id string) (string, error) { asked = append(asked, id); return "", nil }, Now: func() time.Time { return lpNow }}
	p := NewPicker(lpAll()[:1], nil)
	cmd := p.StartFetch(context.Background(), lp)
	for cmd != nil {
		cmd = p.HandleLastPost(cmd().(LastPostMsg))
	}
	p.Refresh(NewPicker(lpAll(), nil))
	run := func(c tea.Cmd) {
		for c != nil {
			c = p.HandleLastPost(c().(LastPostMsg))
		}
	}
	run(p.ResumeFetch())
	if !slices.Equal(asked, []string{"C1", "C2", "C3"}) {
		t.Errorf("取った順 = %v", asked)
	}
}
