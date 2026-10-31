package filer

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// confirmSearch は / で q を打って Enter で確定する。
func confirmSearch(m *Model, q string) {
	m.HandleKey("/")
	typeText(m, q)
	m.HandleKey("enter")
}

// 確定した語は ↑ で新しい順に、↓ で戻る順に辿れ、最新を越えた ↓ で打ちかけ (空) に戻る。履歴はファイルに残り、次の Model でも辿れる
func TestSearchHistoryUpDownAndPersists(t *testing.T) {
	root := fixture(t)
	m := newAt(t, root)
	m.Resize(120, 40)
	settle(m)
	confirmSearch(m, "file")
	confirmSearch(m, "song")
	m.HandleKey("/")
	for i, step := range []struct{ key, want string }{
		{"up", "song"}, {"up", "file"}, {"up", "file"}, {"down", "song"}, {"down", ""},
	} {
		m.HandleKey(step.key)
		if got := m.search.line.String(); got != step.want {
			t.Fatalf("%d 打目の %s の後の入力 = %q, want %q", i+1, step.key, got, step.want)
		}
	}
	m.HandleKey("up")
	if m.cur.raw != "song.mp3" {
		t.Fatalf("履歴を呼び出しても一致へ飛ばない: cur=%s", m.cur.raw)
	}
	m.HandleKey("esc")

	m2 := newAt(t, root) // 同じ設定の置き場 (fixture の isolateConfig) から読み直す
	m2.Resize(120, 40)
	m2.HandleKey("/")
	m2.HandleKey("up")
	if got := m2.search.line.String(); got != "song" {
		t.Fatalf("次の Model で履歴の最新が出ない: %q (履歴 %v)", got, m2.searchHist)
	}
}

// 打ちかけの語で始まる履歴だけを辿る (vim の / と同じ)。端では止まり、最新を越えた ↓ で打ちかけの語に戻る
func TestSearchHistoryFiltersByTypedPrefix(t *testing.T) {
	m := newTest(t)
	for _, q := range []string{"one", "two", "owl"} {
		confirmSearch(m, q)
	}
	m.HandleKey("/")
	typeText(m, "o")
	for i, step := range []struct{ key, want string }{
		{"up", "owl"}, {"up", "one"}, {"up", "one"}, {"down", "owl"}, {"down", "o"},
		// カーソルを動かしただけ (語は変わらない) なら、打ちかけの語 (o) のまま辿り続ける
		{"up", "owl"}, {"left", "owl"}, {"up", "one"}, {"down", "owl"},
		// 辿った後に打ち直したら、次の ↑ はその語 (ow) で始まる履歴を新しい方から辿り直す
		{"backspace", "ow"}, {"up", "owl"},
	} {
		m.HandleKey(step.key)
		if got := m.search.line.String(); got != step.want {
			t.Fatalf("%d 打目の %s の後の入力 = %q, want %q", i+1, step.key, got, step.want)
		}
	}
}

// 同じ語は新しい方だけを残し、50 件で切る。読み直しても同じ
func TestSearchHistoryDedupesAndKeeps50(t *testing.T) {
	root := fixture(t)
	m := newAt(t, root)
	for i := range 60 {
		confirmSearch(m, fmt.Sprintf("q%02d", i))
	}
	confirmSearch(m, "q30") // 古い語を打ち直すと最新へ動く
	want := []string{}
	for i := 10; i < 60; i++ { // q30 を除いた 49 件 + 最新の q30 = 50 件
		if i != 30 {
			want = append(want, fmt.Sprintf("q%02d", i))
		}
	}
	want = append(want, "q30")
	if len(want) != searchHistKeep || !slices.Equal(m.searchHist, want) {
		t.Fatalf("メモリの履歴:\n got %v\nwant %v", m.searchHist, want)
	}
	if got := newAt(t, root).searchHist; !slices.Equal(got, want) {
		t.Fatalf("読み直した履歴:\n got %v\nwant %v", got, want)
	}
}

// 一致が無い語も残す (打ち直す手間を省く)。履歴が無いときの ↑ は何もしない (一致の移動もしない)
func TestSearchHistoryKeepsNoMatchAndUpWithoutHistoryIsNoop(t *testing.T) {
	m := newTest(t)
	m.HandleKey("/")
	typeText(m, "file")
	at := m.cur
	m.HandleKey("up")
	if m.cur != at || m.search.line.String() != "file" {
		t.Fatalf("履歴が無いのに ↑ で動いた: cur=%s line=%q", m.cur.raw, m.search.line.String())
	}
	m.HandleKey("esc")
	confirmSearch(m, "zzznomatch")
	if !slices.Equal(m.searchHist, []string{"zzznomatch"}) {
		t.Fatalf("一致の無い語が履歴に残らない: %v", m.searchHist)
	}
}

// 先頭が空白の語はメモリにも残さない (ファイルには残さないので、片方だけだと開いている間だけ辿れて次に開くと消える)
func TestSearchHistorySkipsLeadingSpaceInMemoryToo(t *testing.T) {
	m := newTest(t)
	confirmSearch(m, " file")
	if len(m.searchHist) != 0 {
		t.Fatalf("先頭が空白の語がメモリの履歴に残った: %q", m.searchHist)
	}
}

// 開いている間に同じ語を何度も打ってファイルに重なっても、次に開くと重複なしで 50 件を読み、ファイルも畳んだ形に書き直す
func TestSearchHistoryReadsPast50DuplicateLines(t *testing.T) {
	root := fixture(t)
	var b strings.Builder
	for i := range 50 {
		b.WriteString(strconv.Quote(fmt.Sprintf("q%02d", i)) + "\n")
	}
	for i := range 300 {
		b.WriteString(strconv.Quote([]string{"a", "b"}[i%2]) + "\n")
	}
	path := searchHistoryPath()
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := newAt(t, root).searchHist
	if len(got) != searchHistKeep || got[0] != "q02" || !slices.Equal(got[len(got)-2:], []string{"a", "b"}) {
		t.Fatalf("重なった語の後ろの 50 件 (q02〜q49, a, b) にならない: %d 件 %v", len(got), got)
	}
	if _, lines := readHistory(path, searchHistReadLines); lines != searchHistKeep {
		t.Fatalf("ファイルを畳んだ形に書き直していない: %d 行", lines)
	}
}
