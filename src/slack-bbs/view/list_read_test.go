package view

import (
	"context"
	"errors"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/subs"
)

// day0 は日付の見出しのテストの「今日」(ローカル時刻の 11/10 (月) 12:00)。
var day0 = time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)

func tsAt(t time.Time) string { return board.TsOf(t.Unix()) }

// topicAt は at に他人が 1 回発言した話題 (単位の ID は発言の ts)。
func topicAt(title string, at time.Time, labels ...board.Label) board.Topic {
	ts := tsAt(at)
	return board.Topic{Title: title, State: board.StateInfo, Labels: labels,
		Units: []board.Unit{{ID: ts, Msgs: []board.Msg{{Ts: ts, Name: "tanaka", Text: title, New: true}}}}}
}

// datedModel は topics を出す 1 チャンネルの画面 (読み込み済み)。
func datedModel(t *testing.T, topics func() []board.Topic) *Model {
	t.Helper()
	load := func(context.Context, subs.Channel) (render.Page, error) {
		return render.Page{Channel: "C1", ChannelName: "dev", Since: day0, Until: day0, Lookback: day0, Topics: topics()}, nil
	}
	m := quiet(New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}}, load, nil, func() time.Time { return day0 }))
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	run(m, m.Init())
	return m
}

var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// leftPlain は左の一覧の中身の行 (色・縁・余白・落ち影を外す。空行は除く)。
func leftPlain(m *Model) []string {
	var out []string
	pad := cardPadX(m.w)
	for _, l := range m.leftLines()[1 : 1+m.cardRows()] { // 上辺と、下辺・下の落ち影の間
		r := []rune(sgrRe.ReplaceAllString(l, ""))
		if l := strings.TrimRight(string(r[1+pad:len(r)-2-pad]), " "); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// 話題は最後の発言の日ごとに見出しの下へ組み、新しい日が上。同じ日の中は板の並び (要対応が先) を保ち、未分類は最後に別の見出しで出す。
func TestListGroupsTopicsByDay(t *testing.T) {
	action := board.Label{Kind: board.KindAction, Source: board.SourceMention}
	m := datedModel(t, func() []board.Topic {
		other := topicAt("未分類", day0.AddDate(0, 0, -3))
		other.Unclassified = true
		return []board.Topic{ // board.Assemble の並び: 要対応 → 新しい順
			topicAt("一昨日の要対応", day0.AddDate(0, 0, -2), action),
			topicAt("今日の夕方", day0.Add(5*time.Hour)),
			topicAt("今日の朝", day0.Add(-3*time.Hour)),
			topicAt("昨日", day0.AddDate(0, 0, -1)),
			other,
		}
	})
	got := strings.Join(leftPlain(m), "\n")
	want := []string{"11/10 (火) 今日", "今日の夕方", "今日の朝", "11/09 (月) 昨日", "昨日", "11/08 (日)", "一昨日の要対応", "未分類", "未分類"}
	at := 0
	for _, w := range want {
		i := strings.Index(got[at:], w)
		if i < 0 {
			t.Fatalf("一覧に %q が順に出ない:\n%s", w, got)
		}
		at += i + len(w)
	}
}

// カーソルは見出しに止まらない: 開いた直後は先頭の話題、j / k / g / G は見出しを飛ばす。
func TestCursorSkipsDayHeaders(t *testing.T) {
	m := datedModel(t, func() []board.Topic {
		return []board.Topic{topicAt("今日", day0), topicAt("昨日", day0.AddDate(0, 0, -1)), topicAt("前の日", day0.AddDate(0, 0, -2))}
	})
	m.Lines()
	sel := func() string { tp, _ := m.selected(); return tp.Title }
	if sel() != "今日" {
		t.Fatalf("開いた直後の選択 = %q", sel())
	}
	for _, step := range []struct{ key, want string }{{"j", "昨日"}, {"j", "前の日"}, {"j", "前の日"}, {"k", "昨日"}, {"k", "今日"}, {"k", "今日"}, {"G", "前の日"}, {"g", "今日"}} {
		press(m, step.key)
		if sel() != step.want {
			t.Fatalf("%s の後の選択 = %q, want %q", step.key, sel(), step.want)
		}
	}
	if m.list.Offset != 0 {
		t.Errorf("先頭の話題へ戻っても見出しが窓の外: offset=%d", m.list.Offset)
	}
}

// 既読: 開いた直後は全部未読で、カーソルを動かして選んだ話題・l で開いた話題が既読になる (開いた直後に乗っただけでは既読にしない)。
// 既読の印は Save で残し、次に開いたとき Load から戻る。u は既読と未読を入れ替える。
func TestReadMarks(t *testing.T) {
	topics := []board.Topic{topicAt("a", day0), topicAt("b", day0.Add(-time.Hour)), topicAt("c", day0.Add(-2*time.Hour))}
	m := datedModel(t, func() []board.Topic { return topics })
	saved := map[string]map[string]string{}
	m.SetReads(&Reads{
		Load: func(ch string) (map[string]string, error) { return maps.Clone(saved[ch]), nil },
		Save: func(ch string, r map[string]string) error { saved[ch] = maps.Clone(r); return nil },
	})
	if n := m.unreadCount(0); n != 3 {
		t.Fatalf("開いた直後の未読 = %d, want 3", n)
	}
	if !strings.Contains(header(m), "#dev  未読 3") {
		t.Errorf("タブに未読の数が無い: %q", header(m))
	}
	press(m, "j") // b を選ぶ
	if n := m.unreadCount(0); n != 2 || topicUnread(topics[1], m.chans[0].read, nil) {
		t.Fatalf("j で選んだ b が既読にならない: 未読 %d", n)
	}
	press(m, "k") // a へ戻る (選んだので既読)
	press(m, "u") // a を未読に戻す
	if !topicUnread(topics[0], m.chans[0].read, nil) || !strings.Contains(m.LastToast(), "未読に戻した") {
		t.Fatalf("u で未読に戻らない: %q", m.LastToast())
	}
	press(m, "u")
	if topicUnread(topics[0], m.chans[0].read, nil) {
		t.Fatal("u で既読にならない")
	}
	if len(saved["C1"]) != 2 {
		t.Errorf("保存した印 = %v, want a と b", saved["C1"])
	}

	// 開き直すと印が戻る。読んだ単位に新しい他人の発言が付いたら、また未読になる
	m2 := datedModel(t, func() []board.Topic { return topics })
	m2.SetReads(&Reads{Load: func(ch string) (map[string]string, error) { return maps.Clone(saved[ch]), nil }, Save: func(string, map[string]string) error { return nil }})
	if n := m2.unreadCount(0); n != 1 {
		t.Fatalf("開き直した後の未読 = %d, want 1 (c だけ)", n)
	}
	b := topics[1]
	b.Units[0].Msgs = append(b.Units[0].Msgs, board.Msg{Ts: tsAt(day0.Add(time.Hour)), Name: "suzuki", Text: "返信", New: true})
	if !topicUnread(b, m2.chans[0].read, nil) {
		t.Error("読んだ後に返信が付いた話題が未読にならない")
	}
	mine := topics[1]
	mine.Units = []board.Unit{{ID: b.Units[0].ID, Msgs: append(topics[1].Units[0].Msgs[:1:1], board.Msg{Ts: tsAt(day0.Add(time.Hour)), Text: "自分の返信", New: true, Mine: true})}}
	if topicUnread(mine, m2.chans[0].read, nil) {
		t.Error("自分の返信だけで未読になった")
	}
}

// 既読の印を読めない・書けないときは、黙らずに知らせる (読めなければ未読から始める)。
func TestReadStoreErrorsAreToasted(t *testing.T) {
	m := datedModel(t, func() []board.Topic { return []board.Topic{topicAt("a", day0), topicAt("b", day0.Add(-time.Hour))} })
	m.SetReads(&Reads{
		Load: func(string) (map[string]string, error) { return nil, errors.New("壊れている") },
		Save: func(string, map[string]string) error { return errors.New("書けない") },
	})
	if !strings.Contains(m.LastToast(), "既読を読めなかった") || m.unreadCount(0) != 2 {
		t.Fatalf("読めないとき: 知らせ %q 未読 %d", m.LastToast(), m.unreadCount(0))
	}
	press(m, "j")
	if !strings.Contains(m.LastToast(), "既読を保存できなかった") {
		t.Errorf("書けないとき: 知らせ %q", m.LastToast())
	}
}

// R は始めたことを 1 枚で知らせ、読み込み中のチャンネルを飛ばしたらその数を添える。全部が読み込み中なら読み直せないと知らせる。
func TestReloadAllToasts(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	pending := m.Init() // C1 は読み込み中
	press(m, "R")       // C2 を読み直す。結果を渡さないので、C1 も C2 も読み込み中のまま
	if got := m.LastToast(); got != "1 チャンネルを読み直しています… (読み込み中の 1 チャンネルは読み直さない)" {
		t.Errorf("一部が読み込み中の R の知らせ = %q", got)
	}
	press(m, "R")
	if !strings.Contains(m.LastToast(), "どのチャンネルも読み込み中なので、読み直せない") {
		t.Errorf("全部が読み込み中の R の知らせ = %q", m.LastToast())
	}
	run(m, pending)
}

// 知らせは滑り込んだ後、画面の右下 (キーの案内の上) に出て、画面の幅と行の数を崩さない。
func TestToastIsDrawnBottomRight(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	pending := m.Init()
	press(m, "r")
	for range 20 {
		m.Update(tickMsg{})
	}
	lines := m.Lines()
	if len(lines) != 20 {
		t.Fatalf("行の数 = %d", len(lines))
	}
	at := -1
	for i, ln := range lines {
		if w := termwidth.Of(ln); w > 90 {
			t.Errorf("%d 行目の幅 %d > 90", i, w)
		}
		if strings.Contains(sgrRe.ReplaceAllString(ln, ""), "読み込み中なので") {
			at = i
		}
	}
	if at < 10 || at == len(lines)-1 {
		t.Errorf("知らせが右下 (キーの案内の上) に無い: %d 行目", at)
	}
	if !strings.Contains(sgrRe.ReplaceAllString(lines[len(lines)-1], ""), "q: 終わる") {
		t.Errorf("キーの案内が隠れた: %q", lines[len(lines)-1])
	}
	run(m, pending)
}

// 画面は青 (34)・水色 (36) の基本色と、基本色の地 (40〜47・100〜107) を使わない: 背景が青い端末で沈む (ユーザー報告 2026-11-10)。
// 地を塗るのは配色 (style.go) の 24bit の色だけ。24bit・256 色の指定 (38;2;r;g;b など) の中の数は色の番号として読まない。
func TestScreenAvoidsBasicBlueAndBasicBackgrounds(t *testing.T) {
	m := datedModel(t, func() []board.Topic { // 購読の画面のキー (上の行の右端) も描くため、下で SetSubscriptions する
		tp := topicAt("PR 1234 の「確認」", day0)
		tp.Points = []board.Point{{Section: board.SectionConclusion, Text: "tanaka が「直す」と言った", Refs: []string{tp.Units[0].ID}}}
		return []board.Topic{tp, topicAt("昨日", day0.AddDate(0, 0, -1))}
	})
	m.chans[0].page.Base = "https://ws.slack.com/"
	m.SetSubscriptions(&Subscriptions{})
	press(m, "u")
	press(m, "r") // 読み直しを始めた中立の知らせ (結果は渡さない) も色を検査する
	for range 20 {
		m.Update(tickMsg{})
	}
	screen := strings.Join(m.Lines(), "\n")
	bgs := 0
	for _, sgr := range sgrRe.FindAllString(screen, -1) {
		ps := strings.Split(strings.Trim(sgr, "\x1b[m"), ";")
		for i := 0; i < len(ps); i++ {
			switch p := ps[i]; p {
			case "38", "48":
				if p == "48" {
					bgs++
				}
				if i+1 < len(ps) && ps[i+1] == "2" {
					i += 4
				} else {
					// 256 色は灰色の段 (232〜255。知らせの箱の落ち影 layout.ShadowNearBlack) だけを許す。番号で書いた青を通さない
					if n, err := strconv.Atoi(ps[min(i+2, len(ps)-1)]); i+2 >= len(ps) || ps[i+1] != "5" || err != nil || n < 232 {
						t.Errorf("24bit でも灰色の 256 色でもない色の指定 %q が出た", sgr)
					}
					i += 2
				}
			case "34", "36", "94", "96", "40", "41", "42", "43", "44", "45", "46", "47", "100", "101", "102", "103", "104", "105", "106", "107":
				t.Errorf("使わない色 %q が出た", sgr)
			}
		}
	}
	if bgs == 0 {
		t.Fatal("地の色が 1 つも無い (検査が空振りしている)")
	}
}

// 文脈として出した期間前の発言 (New でない) は、印が無くても未読に数えない。
func TestOldContextIsNotUnread(t *testing.T) {
	tp := topicAt("続いているスレッド", day0)
	tp.Units[0].Msgs[0].New = false
	if topicUnread(tp, map[string]string{}, nil) {
		t.Error("期間前の発言だけの話題が未読になった")
	}
}

// 購読の画面で足したチャンネルでも、選んだ話題を既読にできる (足したチャンネルの印を読み込む)。
func TestAddedChannelTracksReads(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	m := subsModel(l, &saved, &opened)
	loads := map[string]int{}
	m.SetReads(&Reads{
		Load: func(ch string) (map[string]string, error) { loads[ch]++; return map[string]string{}, nil },
		Save: func(string, map[string]string) error { return nil },
	})
	run(m, m.Init())
	run(m, press(m, "s"))
	for _, k := range []string{"x", "j", "x", "j", "x", "enter"} { // dev と ops を外し qa を付ける
		run(m, press(m, k))
	}
	if ids(m) != "C3" || loads["C3"] != 1 {
		t.Fatalf("足したチャンネルの印を読んでいない: タブ %s 読んだ回数 %v", ids(m), loads)
	}
	press(m, "l")
	if m.unreadCount(0) != 0 {
		t.Errorf("足したチャンネルで l で開いた話題が既読にならない")
	}
}

// o は Slack で開けたときだけ既読にする (リンクを組めない・開けないときは読めていない)。
func TestOpenMarksReadOnlyWhenOpened(t *testing.T) {
	var openErr error
	load := func(context.Context, subs.Channel) (render.Page, error) {
		return render.Page{Channel: "C1", ChannelName: "dev", Base: "https://ws.slack.com/", Since: day0, Until: day0, Lookback: day0,
			Topics: []board.Topic{topicAt("a", day0)}}, nil
	}
	m := quiet(New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}}, load, func(string) error { return openErr }, func() time.Time { return day0 }))
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	run(m, m.Init())
	openErr = errors.New("open が無い")
	press(m, "o")
	if m.unreadCount(0) != 1 {
		t.Fatal("開けなかった話題を既読にした")
	}
	openErr = nil
	press(m, "o")
	if m.unreadCount(0) != 0 {
		t.Error("開けた話題が既読にならない")
	}
}

// 読み直しは既読にしない: 本文の欄を開いたまま読み直して返信が付いたら、その話題は未読 (●) に戻る (欄は先頭へ戻り、返信は欄の外にありうる)。
func TestReloadWhileReadingShowsNewReplyUnread(t *testing.T) {
	reply := false
	m := datedModel(t, func() []board.Topic {
		tp := topicAt("a", day0.Add(-time.Hour))
		if reply {
			tp.Units[0].Msgs = append(tp.Units[0].Msgs, board.Msg{Ts: tsAt(day0), Name: "suzuki", Text: "返信", New: true})
		}
		return []board.Topic{tp}
	})
	press(m, "l")
	m.Lines() // キーの合間に描く (タブの未読の数を覚えた状態で読み直す)
	reply = true
	run(m, press(m, "r"))
	if m.unreadCount(0) != 1 {
		t.Error("読み直しで付いた返信を既読にした")
	}
}

// 印は進めるだけ: 後の返信まで読んだ印は、返信を含まない古い板 (前回のキャッシュ) の上で選び直しても戻さない。
func TestReadMarkNeverMovesBack(t *testing.T) {
	m := datedModel(t, func() []board.Topic { return []board.Topic{topicAt("a", day0), topicAt("b", day0.Add(-time.Hour))} })
	id := m.topics()[1].Units[0].ID
	later := tsAt(day0.Add(time.Hour))
	m.chans[0].read[id] = later
	press(m, "j")
	if got := m.chans[0].read[id]; got != later {
		t.Errorf("印が戻った: %s (want %s)", got, later)
	}
}

// 小さい端末 (一覧を描かない画面) でタブを替えた直後でも、読んであるチャンネルへ戻った直後でも、l で話題を開ける (カーソルを見出しに残さない)。
func TestTinyTerminalKeysAfterTabSwitch(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	run(m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 6})
	run(m, press(m, "tab")) // C2 を読む (読み終えたときにカーソルを話題へ収める)
	run(m, press(m, "tab")) // 読んである C1 へ戻る (読み直さないので、カーソルは一覧の先頭 = 見出しから始まる)
	press(m, "l")
	if !m.body {
		t.Error("タブを替えた直後の l で本文を開けない")
	}
}

// キーの案内は「キー: 説明」で略さない語で書き、幅 100 の端末では一覧の全部の項目が入る (本文の欄では Tab だけが落ちる)。
// 狭い端末では項目を落としても抜ける手段 (一覧では q: 終わる、本文の欄では h/q: 話題の一覧へ戻る) は残す。
func TestHelpIsUnabbreviatedAndFits(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	m := subsModel(l, &saved, &opened)
	common := []string{"o: Slack で開く", "u: 既読と未読の切り替え", "r: 今のチャンネルを読み直す", "R: 全チャンネルを読み直す"}
	plain := func(ls []string) []string {
		for i := range ls {
			ls[i] = sgrRe.ReplaceAllString(ls[i], "")
		}
		return ls
	}
	for _, tc := range []struct {
		body bool
		want []string
		exit string // 抜ける手段の項目
		line int    // exit が出る行
	}{
		{false, append([]string{"j/k: 話題を選ぶ", "l/Enter: 本文を読む", "Tab: 次のチャンネル", "q: 終わる"}, common...), "q: 終わる", 1},
		{true, append([]string{"j/k: 本文を送る", "J/K: 隣の話題へ", "h/q: 話題の一覧へ戻る"}, common...), "h/q: 話題の一覧へ戻る", 0},
	} {
		m.body = tc.body
		m.w = 100
		help := strings.Join(plain(m.helpLines()), "\n")
		for _, want := range tc.want {
			if !strings.Contains(help, want) || strings.Contains(help, "…") {
				t.Errorf("body=%v: 幅 100 の案内に %q が無い・切れた:\n%s", tc.body, want, help)
			}
		}
		if tc.body && strings.Contains(help, "q: 終わる") {
			t.Errorf("本文の欄の q は一覧へ戻るのに、案内が「q: 終わる」: %q", help)
		}
		for _, w := range []int{40, 30} { // 30: r (27 桁) と q は並べて入らない幅。抜ける手段を先に採る
			m.w = w
			for _, l := range plain(m.helpLines()) {
				if termwidth.Of(l) > w {
					t.Errorf("body=%v: 幅 %d で案内がはみ出した: %q", tc.body, w, l)
				}
			}
			if l := plain(m.helpLines())[tc.line]; !strings.Contains(l, tc.exit) {
				t.Errorf("body=%v: 幅 %d で %q が落ちた: %q", tc.body, w, tc.exit, l)
			}
		}
	}
}

// 本文の欄を開いたまま読み直しても、u で未読に戻した話題は未読のまま (読み直しは印のある単位を進めるだけ)。
func TestReloadKeepsTopicMarkedUnread(t *testing.T) {
	m := datedModel(t, func() []board.Topic { return []board.Topic{topicAt("a", day0)} })
	press(m, "l")
	press(m, "u")
	run(m, press(m, "r"))
	if m.unreadCount(0) != 1 {
		t.Error("u で未読に戻した話題が、本文を開いたままの読み直しで既読に戻った")
	}
}

// 本文で読んでいた話題が読み直しで消えたら、本文の欄を閉じる。先頭の話題に黙って替えて既読にしない。
func TestReloadDroppingTopicClosesBody(t *testing.T) {
	gone := false
	m := datedModel(t, func() []board.Topic {
		ts := []board.Topic{topicAt("a", day0), topicAt("b", day0.Add(-time.Hour))}
		if gone {
			return ts[:1]
		}
		return ts
	})
	press(m, "j")
	press(m, "l") // b を読む
	gone = true
	run(m, press(m, "r"))
	if m.body {
		t.Error("読んでいた話題が消えたのに本文の欄が開いたまま")
	}
	if m.unreadCount(0) != 1 {
		t.Error("選んでいない先頭の話題を既読にした")
	}
}

// 未読にできない話題 (他の人の期間内の発言が無い) で u を押しても、印は消さない (後で付いた返信の判定が変わらない)。
func TestToggleUnreadableKeepsMarks(t *testing.T) {
	m := datedModel(t, func() []board.Topic {
		tp := topicAt("a", day0)
		tp.Units[0].Msgs[0].Mine = true
		return []board.Topic{tp}
	})
	id := m.topics()[0].Units[0].ID
	m.chans[0].read[id] = id
	press(m, "u")
	if _, ok := m.chans[0].read[id]; !ok || !strings.Contains(m.LastToast(), "未読にできない") {
		t.Errorf("未読にできない u で印を消した: %v %q", m.chans[0].read, m.LastToast())
	}
}

// 読み直しは始めたとき (中立の知らせ) と終わったとき (成功 ✓ / 失敗 ✗) に知らせる。追い越された古い結果は知らせない。
func TestReloadStartAndFinishToasts(t *testing.T) {
	l := &loader{calls: map[string]int{}, err: map[string]error{}}
	m := newModel(l)
	l.err["C1"] = errors.New("slack が応答しない")
	run(m, m.Init())
	if got := m.LastToast(); got != "#dev を読めなかった: slack が応答しない" {
		t.Fatalf("最初の読み込みの失敗の知らせ = %q", got)
	}
	delete(l.err, "C1")
	m.chans[0].err = nil
	cmd := m.Init()
	if got := m.LastToast(); got != "#dev を読んでいます…" || !m.toast.Info() {
		t.Fatalf("最初の読み込みを始めた知らせ = %q info=%v", got, m.toast.Info())
	}
	run(m, cmd)
	if got := m.LastToast(); got != "#dev を読んだ (未読の話題 1 件)" || !m.toast.OK() {
		t.Fatalf("読み終えた知らせ = %q", got)
	}
	cmd = press(m, "r")
	if got := m.LastToast(); got != "#dev を読み直しています…" {
		t.Fatalf("読み直しを始めた知らせ = %q", got)
	}
	run(m, cmd)
	if got := m.LastToast(); got != "#dev を読み直した (未読の話題 1 件)" {
		t.Fatalf("読み直した知らせ = %q", got)
	}
	l.err["C1"] = errors.New("claude が落ちた")
	run(m, press(m, "r"))
	if got := m.LastToast(); got != "#dev を読み直せなかった: claude が落ちた" || m.toast.OK() {
		t.Fatalf("読み直せなかった知らせ = %q ok=%v", got, m.toast.OK())
	}
	delete(l.err, "C1")
	old := m.reload(0)
	run(m, m.reload(0))
	before := len(m.toast.Entries())
	m.Update(old())
	if got := len(m.toast.Entries()); got != before {
		t.Errorf("追い越された古い結果を知らせた (%d 枚 → %d 枚)", before, got)
	}
}

// 本文の欄の折り返しの覚えは、読み直した板・幅・選んだ話題が変わったら作り直す。色は写しに付ける (2 回描いても同じ画面)。
func TestBodyCacheFollowsPageWidthAndTopic(t *testing.T) {
	text := "最初の本文で「確認」した"
	m := datedModel(t, func() []board.Topic {
		a := topicAt("a", day0)
		a.Units[0].Msgs[0].Text = text
		return []board.Topic{a, topicAt("b", day0.Add(-time.Hour))}
	})
	screen := func() string { // 本文を見る (重なる知らせは消してから)
		m.toast.Clear()
		return sgrRe.ReplaceAllString(strings.Join(m.Lines(), "\n"), "")
	}
	first := strings.Join(m.Lines(), "\n")
	if again := strings.Join(m.Lines(), "\n"); again != first {
		t.Fatal("2 回目の描画が 1 回目と違う (覚えた行に色を重ねた)")
	}
	text = "読み直した本文"
	run(m, press(m, "r"))
	if !strings.Contains(screen(), "読み直した本文") {
		t.Fatalf("読み直した板の本文が出ない:\n%s", screen())
	}
	press(m, "j")
	if s := screen(); strings.Contains(s, "読み直した本文") {
		t.Fatalf("話題を替えても前の話題の本文が出ている:\n%s", s)
	}
	press(m, "k")
	text = strings.Repeat("長い本文を折り返す。", 8)
	run(m, press(m, "r"))
	m.Lines()
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	lines, _ := m.bodyLines()
	for _, l := range lines {
		if termwidth.Of(l) > m.bodyWidth() {
			t.Fatalf("幅を縮めた後も前の幅の折り返しのまま (枠で切れる): %q", l)
		}
	}
}

// 操作中の欄は、カードの枠の色を水色から流し、選んだ行を明るい地で塗る。もう一方は枠を沈んだ色で描き、選んだ行の地も沈める。l / h で入れ替わる。
func TestActivePaneIsEmphasized(t *testing.T) {
	m := datedModel(t, func() []board.Topic { return []board.Topic{topicAt("a", day0)} })
	on, off := " "+cTeal.fg()+"▂", " "+cMuted.fg()+"▂" // 上辺の最初の桁 (枠の色の流れの始まり)
	check := func(when string, leftActive bool) {
		t.Helper()
		l, r := m.leftLines(), m.rightLines()
		active, idle := l[0], r[0]
		if !leftActive {
			active, idle = r[0], l[0]
		}
		if !strings.HasPrefix(active, on) || !strings.HasPrefix(idle, off) {
			t.Errorf("%s: 操作中の欄の枠が水色から流れていない: 左 %q 右 %q", when, l[0], r[0])
		}
		activeSide, idleSide := l[1], r[1] // 左の縁 ▐ (中身の 1 行目)
		if !leftActive {
			activeSide, idleSide = r[1], l[1]
		}
		if !strings.Contains(activeSide, cTeal.fg()+"▐") || !strings.Contains(idleSide, cMuted.fg()+"▐") {
			t.Errorf("%s: 左の縁の色が操作中かどうかと合わない: 左 %q 右 %q", when, l[1], r[1])
		}
		sel := cSelIdle.bg()
		if leftActive {
			sel = cSel.bg()
		}
		if !strings.Contains(strings.Join(l, "\n"), sel+pink("▌")) {
			t.Errorf("%s: 選んだ行の地が %q でない", when, sel)
		}
	}
	check("開いた直後", true)
	press(m, "l")
	check("l の後", false)
	press(m, "h")
	check("h の後", true)
}
