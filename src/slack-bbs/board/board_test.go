package board

import (
	"slices"
	"testing"

	"slack-bbs/source"
)

const since = "1000.000000"

// 期間 (since 以降) の判定と、スレッドを取り直す親の選び方。
func TestNeedThreadsPicksActiveThreadsOnly(t *testing.T) {
	hist := []source.Message{
		{Ts: "1100.000000", Text: "期間内のスレッド外の発言"},
		{Ts: "1050.000000", ThreadTs: "1050.000000", ReplyCount: 2, LatestReply: "1060.000000"}, // 期間内の親
		{Ts: "900.000000", ThreadTs: "900.000000", ReplyCount: 3, LatestReply: "1200.000000"},   // 古い親・新しい返信 (続いている)
		{Ts: "800.000000", ThreadTs: "800.000000", ReplyCount: 1, LatestReply: "950.000000"},    // 動きが期間前で止まった
		{Ts: "700.000000", Text: "期間前のスレッド外の発言"},
	}
	got := NeedThreads(hist, since)
	if want := []string{"1050.000000", "900.000000"}; !slices.Equal(got, want) {
		t.Fatalf("取り直す親 = %v, want %v", got, want)
	}
}

// 続いているスレッドは、親と期間前の文脈 (直近 OldContext 件) と期間内の返信で 1 つの単位になる。
// チャンネルにも投稿した返信は history の側に出ず、スレッドの側にだけ 1 回出る。
func TestBuildContinuingThreadAndBroadcastDedup(t *testing.T) {
	thread := []source.Message{{Ts: "900.000000", ThreadTs: "900.000000", User: "U1", Text: "親"}}
	for i := range 7 {
		thread = append(thread, source.Message{Ts: TsOf(int64(910 + i)), ThreadTs: "900.000000", User: "U2", Text: "前"})
	}
	thread = append(thread, source.Message{Ts: "1200.000000", ThreadTs: "900.000000", User: "U2", Text: "<@ME> 見て"})
	hist := []source.Message{
		{Ts: "1200.000000", ThreadTs: "900.000000", User: "U2", Text: "<@ME> 見て"}, // チャンネルにも投稿した返信
		{Ts: "1100.000000", User: "U3", Text: "単発"},
		{Ts: "900.000000", ThreadTs: "900.000000", User: "U1", ReplyCount: 8, LatestReply: "1200.000000"},
	}
	units, err := Build(Input{History: hist, Threads: map[string][]source.Message{"900.000000": thread}, Since: since, Me: "ME",
		Names: map[string]string{"U1": "tanaka", "ME": "me"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("単位の数 = %d, want 2: %+v", len(units), units)
	}
	th := units[0]
	if th.ID != "900.000000" || !th.Thread || !th.Continuing {
		t.Fatalf("先頭は続いているスレッドのはず: %+v", th)
	}
	if th.Omitted != 7-OldContext || len(th.Msgs) != 1+OldContext+1 {
		t.Errorf("省略 %d / 発言 %d (want %d / %d)", th.Omitted, len(th.Msgs), 7-OldContext, 1+OldContext+1)
	}
	last := th.Msgs[len(th.Msgs)-1]
	if !last.New || !last.ToMe || last.Text != "@me 見て" {
		t.Errorf("期間内の返信: %+v", last)
	}
	if th.Msgs[1].New {
		t.Errorf("期間前の文脈が期間内として扱われた: %+v", th.Msgs[1])
	}
	if th.Msgs[0].Name != "tanaka" {
		t.Errorf("表示名: %q", th.Msgs[0].Name)
	}
	if units[1].ID != "1100.000000" {
		t.Errorf("2 つ目は単発の発言のはず: %+v", units[1])
	}
}

// 自分が期間前に書いたスレッドへの期間内の返信は、自分の発言が文脈から省かれても「要対応 (返信)」になる。
func TestReplyLabelSurvivesOmittedContext(t *testing.T) {
	thread := []source.Message{{Ts: "900.000000", ThreadTs: "900.000000", User: "U1", Text: "親"},
		{Ts: "901.000000", ThreadTs: "900.000000", User: "ME", Text: "自分の返信"}}
	for i := range OldContext {
		thread = append(thread, source.Message{Ts: TsOf(int64(910 + i)), ThreadTs: "900.000000", User: "U2", Text: "前"})
	}
	thread = append(thread, source.Message{Ts: "1200.000000", ThreadTs: "900.000000", User: "U2", Text: "どう？"})
	hist := []source.Message{{Ts: "900.000000", ThreadTs: "900.000000", User: "U1", ReplyCount: 7, LatestReply: "1200.000000"}}
	units, err := Build(Input{History: hist, Threads: map[string][]source.Message{"900.000000": thread}, Since: since, Me: "ME"})
	if err != nil {
		t.Fatal(err)
	}
	if !units[0].MineOmitted {
		t.Fatalf("自分の発言が省かれたことが記録されていない: %+v", units[0])
	}
	topics, _ := Assemble(units, Response{Topics: []RespTopic{{Title: "t", State: StateOpen, Members: []string{"900.000000"}}}})
	if !slices.ContainsFunc(topics[0].Labels, func(l Label) bool { return l.Source == SourceReply }) {
		t.Errorf("自分のスレッドへの返信が要対応にならない: %+v", topics[0].Labels)
	}
}

// 自分が最後に返した後に返信が無ければ「要対応 (返信)」にしない。
func TestReplyLabelClearedByMyLaterReply(t *testing.T) {
	u := Unit{ID: "900.000000", Thread: true, Msgs: []Msg{
		{Ts: "900.000000", User: "ME", Mine: true},
		{Ts: "1100.000000", User: "U2", New: true},
		{Ts: "1150.000000", User: "ME", Mine: true, New: true},
	}}
	if ls := mechanicalLabels([]Unit{u}); len(ls) != 0 {
		t.Errorf("返し済みのスレッドに要対応が付いた: %+v", ls)
	}
}

func units3() []Unit {
	return []Unit{
		{ID: "a", Msgs: []Msg{{Ts: "a", New: true}}},
		{ID: "b", Msgs: []Msg{{Ts: "b", New: true}, {Ts: "b2", New: true}}},
		{ID: "c", Msgs: []Msg{{Ts: "c", New: true}}},
	}
}

// LLM が落とした単位は未分類へ、無い ID と 2 回目の ID は捨てる。全部の単位がちょうど 1 回出る。
func TestAssembleEveryUnitExactlyOnce(t *testing.T) {
	resp := Response{Topics: []RespTopic{
		{Title: "x", State: StateDecided, Members: []string{"a", "ghost", "b"}},
		{Title: "y", State: StateOpen, Members: []string{"b"}}, // b は 2 回目 → 捨てる。単位が残らないので話題ごと捨てる
	}}
	topics, d := Assemble(units3(), resp)
	count := map[string]int{}
	for _, tp := range topics {
		for _, u := range tp.Units {
			count[u.ID]++
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if count[id] != 1 {
			t.Errorf("単位 %s が %d 回出た (topics=%+v)", id, count[id], topics)
		}
	}
	if len(count) != 3 {
		t.Errorf("入力に無い単位が出た: %v", count)
	}
	want := Dropped{UnknownMembers: 1, DuplicateMembers: 1, EmptyTopics: 1, Unclassified: 1}
	if d != want {
		t.Errorf("捨てた数 = %+v, want %+v", d, want)
	}
	if !topics[len(topics)-1].Unclassified || topics[len(topics)-1].Units[0].ID != "c" {
		t.Errorf("落ちた単位が未分類の話題に入っていない: %+v", topics[len(topics)-1])
	}
}

// 要点とラベルの根拠は、その話題の中の発言に限る。根拠が残らないものは捨てる。知らない状態は議論中にする。
func TestAssembleKeepsOnlyGroundedPointsAndLabels(t *testing.T) {
	resp := Response{Topics: []RespTopic{
		{Title: "x", State: "weird", Members: []string{"b"},
			Points: []RespPoint{{Text: "ok", Refs: []string{"b2", "zzz"}}, {Text: "他の話題の発言", Refs: []string{"a"}}},
			Labels: []RespLabel{{Kind: KindMustRead, Reason: "r", Refs: []string{"b"}}, {Kind: "urgent", Reason: "r", Refs: []string{"b"}}, {Kind: KindAction, Reason: "r", Refs: []string{"c"}}}},
		{Title: "y", State: StateInfo, Members: []string{"a", "c"}},
	}}
	topics, d := Assemble(units3(), resp)
	var x Topic
	for _, tp := range topics {
		if tp.Title == "x" {
			x = tp
		}
	}
	if x.State != StateOpen {
		t.Errorf("知らない状態 = %q", x.State)
	}
	if len(x.Points) != 1 || !slices.Equal(x.Points[0].Refs, []string{"b2"}) {
		t.Errorf("要点 = %+v", x.Points)
	}
	if len(x.Labels) != 1 || x.Labels[0].Kind != KindMustRead || x.Labels[0].Source != SourceLLM {
		t.Errorf("ラベル = %+v", x.Labels)
	}
	if d.Points != 1 || d.Labels != 2 || d.BadState != 1 {
		t.Errorf("捨てた数 = %+v", d)
	}
}

// 並びは 要対応 → 必読 → その他、同じ段は最後の発言が新しい順。
func TestAssembleOrder(t *testing.T) {
	us := []Unit{
		{ID: "1", Msgs: []Msg{{Ts: "1", New: true}}},
		{ID: "2", Msgs: []Msg{{Ts: "2", New: true}}},
		{ID: "3", Msgs: []Msg{{Ts: "3", New: true, ToMe: true}}},
		{ID: "4", Msgs: []Msg{{Ts: "4", New: true}}},
	}
	resp := Response{Topics: []RespTopic{
		{Title: "old", State: StateInfo, Members: []string{"1"}},
		{Title: "new", State: StateInfo, Members: []string{"4"}},
		{Title: "must", State: StateInfo, Members: []string{"2"}, Labels: []RespLabel{{Kind: KindMustRead, Refs: []string{"2"}}}},
		{Title: "mention", State: StateInfo, Members: []string{"3"}},
	}}
	topics, _ := Assemble(us, resp)
	var got []string
	for _, tp := range topics {
		got = append(got, tp.Title)
	}
	if want := []string{"mention", "must", "new", "old"}; !slices.Equal(got, want) {
		t.Errorf("並び = %v, want %v", got, want)
	}
}

func TestTsBefore(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1700000000.000001", "1700000000.000002", true},
		{"1700000000.000002", "1700000000.000001", false},
		{"999999999.9", "1000000000.0", true}, // 桁数の違う秒を文字列で比べない
		{"", "1.0", true},                     // 欄の無い latest_reply は最も古い
		{"1.0", "1.0", false},
	} {
		if got := TsBefore(c.a, c.b); got != c.want {
			t.Errorf("TsBefore(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestPlain(t *testing.T) {
	names := map[string]string{"U1": "tanaka"}
	got := Plain("<@U1> <@U9|bob> <#C1|dev> <!here> <https://x.example/a|資料> <https://x.example/b> &lt;tag&gt; &amp;", names)
	want := "@tanaka @bob #dev @here 資料 (https://x.example/a) https://x.example/b <tag> &"
	if got != want {
		t.Errorf("Plain = %q\nwant   %q", got, want)
	}
}

// 期間内に「チャンネルにも投稿」された返信は、親が遡る範囲より前で history に無くても、スレッドを取り直して出す。
func TestBroadcastReplyWithParentOutsideLookback(t *testing.T) {
	hist := []source.Message{{Ts: "1100.000000", ThreadTs: "500.000000", User: "U2", Text: "<@ME> 至急"}}
	if got := NeedThreads(hist, since); !slices.Equal(got, []string{"500.000000"}) {
		t.Fatalf("取り直す親 = %v", got)
	}
	thread := []source.Message{{Ts: "500.000000", ThreadTs: "500.000000", User: "U1", Text: "親"},
		{Ts: "1100.000000", ThreadTs: "500.000000", User: "U2", Text: "<@ME> 至急"}}
	units, err := Build(Input{History: hist, Threads: map[string][]source.Message{"500.000000": thread}, Since: since, Me: "ME"})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || !units[0].Continuing || len(units[0].Msgs) != 2 || !units[0].Msgs[1].ToMe {
		t.Fatalf("単位: %+v", units)
	}
	// スレッドを読めなかったときも、期間内に見えていた返信は落とさない
	units, err = Build(Input{History: hist, Since: since, Me: "ME"})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || !units[0].Unreadable || len(units[0].Msgs) != 1 || units[0].Msgs[0].Ts != "1100.000000" {
		t.Errorf("読めなかったスレッドの単位: %+v", units)
	}
}

// 親が history にあるスレッドを読めなかったら、親だけを出して読めなかった印を付ける。
func TestUnreadableThreadIsMarked(t *testing.T) {
	hist := []source.Message{{Ts: "1050.000000", ThreadTs: "1050.000000", User: "U1", ReplyCount: 2, LatestReply: "1060.000000"}}
	units, err := Build(Input{History: hist, Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || !units[0].Unreadable || len(units[0].Msgs) != 1 {
		t.Errorf("単位: %+v", units)
	}
}

// 同じ ts の発言がスレッドとスレッド外の両方に来たら、黙って 2 回出さずに誤りにする。
func TestBuildRejectsDuplicateTs(t *testing.T) {
	hist := []source.Message{
		{Ts: "1060.000000", User: "U2", Text: "どちらにも出る"},
		{Ts: "1050.000000", ThreadTs: "1050.000000", User: "U1", ReplyCount: 1, LatestReply: "1060.000000"},
	}
	thread := []source.Message{{Ts: "1050.000000", ThreadTs: "1050.000000"}, {Ts: "1060.000000", ThreadTs: "1050.000000"}}
	if _, err := Build(Input{History: hist, Threads: map[string][]source.Message{"1050.000000": thread}, Since: since}); err == nil {
		t.Error("重複した ts を受け入れた")
	}
}

// 外から読み戻した単位の形の検査: 空の発言・重複した ID を受けない (Assemble が空の単位で panic しない前提)。
func TestValidateUnits(t *testing.T) {
	for name, us := range map[string][]Unit{
		"空の発言":    {{ID: "a"}},
		"重複した ID": {{ID: "a", Msgs: []Msg{{Ts: "1"}}}, {ID: "a", Msgs: []Msg{{Ts: "2"}}}},
		"空の ID":   {{ID: "", Msgs: []Msg{{Ts: "1"}}}},
	} {
		if err := ValidateUnits(us); err == nil {
			t.Errorf("%s を受け入れた", name)
		}
	}
}

// 名指しも、自分がその後に同じ単位で書いていれば要対応にしない。
func TestMentionClearedByMyLaterReply(t *testing.T) {
	u := Unit{ID: "900.000000", Thread: true, Msgs: []Msg{
		{Ts: "900.000000", User: "U1"},
		{Ts: "1100.000000", User: "U2", New: true, ToMe: true},
		{Ts: "1150.000000", User: "ME", Mine: true, New: true},
	}}
	if ls := mechanicalLabels([]Unit{u}); len(ls) != 0 {
		t.Errorf("返した後の名指しに要対応が付いた: %+v", ls)
	}
}
