package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
)

func searchSpy() *spy {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	return &spy{snap: backend.Snapshot{Now: now, Limit: 2, DispatcherTick: now, Cards: []card.Card{
		{ID: "C-001", State: card.Planned, Since: now, Title: "glogx の diff で日本語", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 931}}},
		{ID: "C-003", State: card.Planned, Since: now, Title: "tmux の通知が重なる"},
		{ID: "C-008", State: card.Done, Since: now, Title: "起動時間の短縮", Request: "GLOGX を速く"},
		{ID: "C-010", State: card.Done, Since: now, Title: "av1ify の失敗"},
	}}}
}

// flatLaneIDs はレーンの並び (絞り込み後) を左の列から繋げたもの。
func flatLaneIDs(m *Model) []string {
	var out []string
	for _, cs := range m.lanes() {
		for _, c := range cs {
			out = append(out, c.ID)
		}
	}
	return out
}

// / で打つたびにレーンを絞る (題名・原文・issue。大文字小文字を区別しない)。打っている間はボードのキーが欄に取られ、ボードは暗くしない。
func TestSearchFiltersLanesWhileTyping(t *testing.T) {
	be := searchSpy()
	m := New(be, nil)
	press(m, "/")
	typeText(m, "glogx")
	if got := flatLaneIDs(m); !slices.Equal(got, []string{"C-001", "C-008"}) {
		t.Fatalf("glogx で絞ったレーン: %v", got)
	}
	if m.mode != modeBoard || m.dimWhileTyping([]string{"x"})[0] != "x" {
		t.Fatal("検索を打っている間にボードを暗くした (絞った結果を見ながら打つ)")
	}
	typeText(m, " nq") // n (新しい依頼) と q (1 段戻る) も検索語の文字
	if m.search.query() != "glogx nq" || m.mode != modeBoard || !m.search.typing {
		t.Fatalf("打っている間のキーがボードの操作になった: query=%q mode=%v", m.search.query(), m.mode)
	}
	if got := flatLaneIDs(m); len(got) != 0 {
		t.Fatalf("語は AND: %v", got)
	}
	press(m, "esc")
	if m.search.active || len(flatLaneIDs(m)) != 4 {
		t.Fatalf("esc で絞り込みをやめて全部に戻る: active=%v %v", m.search.active, flatLaneIDs(m))
	}
}

// enter で確定したら絞り込みは残り、ボードのキーが絞った結果に効く。件数の行の頭に印、レーンの見出しに (一致/全部)。
// 件数の行の列ごとの枚数と x の対象 (visible) は絞る前のまま。
func TestSearchConfirmKeepsFilterAndMarks(t *testing.T) {
	m := New(searchSpy(), nil)
	m.width, m.height = 160, 30
	press(m, "/")
	typeText(m, "#931")
	press(m, "enter")
	if !m.search.active || m.search.typing || m.selected != "C-001" {
		t.Fatalf("確定後: active=%v typing=%v selected=%q", m.search.active, m.search.typing, m.selected)
	}
	screen := ansi.Strip(m.render())
	for _, want := range []string{"/ #931 1/4 枚", "着手待ち (1/2)", "完了 (0/2)", "着手待ち 2", "完了 2", "esc 絞り込みをやめる"} {
		if !strings.Contains(screen, want) {
			t.Errorf("画面に %q が無い:\n%s", want, screen)
		}
	}
	if m.doneInTab() != 2 {
		t.Fatalf("x の対象を絞り込みで減らした: %d", m.doneInTab())
	}
	press(m, "enter") // ボードのキー = 詳細を開く
	if !m.showDetail {
		t.Fatal("確定後の enter がボードに効かない")
	}
	press(m, "esc", "esc") // 1 つ目で詳細を閉じ、2 つ目で絞り込みをやめる (1 段ずつ戻る)
	if m.showDetail || m.search.active {
		t.Fatalf("esc で 1 段ずつ戻らない: detail=%v search=%v", m.showDetail, m.search.active)
	}
}

// 選んでいたカードが絞り込みで隠れたら、選択は見えているカードへ移る (隠れたカードに操作が効かないように)。
func TestSearchMovesSelectionOffHiddenCard(t *testing.T) {
	m := New(searchSpy(), nil)
	press(m, "j") // 着手待ちの 2 枚目 (C-003)
	if m.selected != "C-003" {
		t.Fatalf("前提: %q", m.selected)
	}
	press(m, "/")
	typeText(m, "glogx")
	if !slices.Contains(flatLaneIDs(m), m.selected) {
		t.Fatalf("隠れたカード %q を選んだまま: %v", m.selected, flatLaneIDs(m))
	}
	press(m, "enter")
	typeText(m, "j") // 確定後の j はボードの移動 (欄には入らない)
	if m.search.query() != "glogx" {
		t.Fatalf("確定後のキーが検索語に入った: %q", m.search.query())
	}
}

// 絞っている間は K / J (見えている隣と入れ替わらない) と x (隠れた完了も片付く) を断り、backend に頼まない。
func TestSearchRefusesMoveAndClear(t *testing.T) {
	be := searchSpy()
	m := New(be, nil)
	press(m, "/")
	typeText(m, "の")
	press(m, "enter", "J", "K", "x")
	if len(be.applied) != 0 || m.mode != modeBoard {
		t.Fatalf("絞っている間に K / J / x が通った: %#v mode=%v", be.applied, m.mode)
	}
	press(m, "q", "J") // q は板が無ければ絞り込みを 1 段戻る。やめた後は J が効く
	if m.search.active || len(be.applied) != 1 {
		t.Fatalf("q で絞り込みをやめて J が通るはず: active=%v %#v", m.search.active, be.applied)
	}
}

// 空のまま enter は絞り込みごとやめる (印だけが残ると嘘になる)。ペーストは打っている間だけ検索語に入る。
func TestSearchEmptyEnterAndPaste(t *testing.T) {
	m := New(searchSpy(), nil)
	press(m, "/", "enter")
	if m.search.active {
		t.Fatal("空で確定したのに絞り込みが残った")
	}
	press(m, "/")
	m.Update(tea.PasteMsg{Content: "tmux\n"})
	if got := flatLaneIDs(m); m.search.query() != "tmux " || !slices.Equal(got, []string{"C-003"}) {
		t.Fatalf("ペーストが検索語に入らない: %q %v", m.search.query(), got)
	}
}

// 一致した語の色は SGR の外の文字にだけ付く (語が色の番号 38;5;… に一致して SGR を壊さない)。
func TestMarkHitsSkipsSGR(t *testing.T) {
	s := sgrBold + "C-038 " + fg(38) + "Glogx" + sgrReset
	got := markHits(s, []string{"38", "glogx"}, sgrBold)
	if ansi.Strip(got) != "C-038 Glogx" {
		t.Fatalf("文字が変わった: %q", ansi.Strip(got))
	}
	if !strings.Contains(got, fg(38)) || !strings.Contains(got, sgrHit+"38"+sgrFgReset+sgrBold) || !strings.Contains(got, sgrHit+"Glogx") {
		t.Fatalf("SGR の外の語だけを浮かせるはず: %q", got)
	}
}
