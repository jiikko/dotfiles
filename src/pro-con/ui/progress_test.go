package ui

import (
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// 詳細に、今の待ちと進捗 (dispatcher が集めた commit・未 commit・issue の進捗節、テストの係の最後の結果) を出す。
// 指示の見出しは中身どおり「PG への指示」(issue 469)。画面は集めたものを読むだけで、git もファイルも読まない。
func TestDrawerShowsProgress(t *testing.T) {
	be := newSpy()
	now := be.snap.Now
	be.snap.Cards = []card.Card{{ID: "C-011", State: card.Running, Since: now, Prompt: "469 を実装して",
		Progress: &card.Progress{Worktree: "/r/.claude/worktrees/pc-c-011", Branch: "worktree-pc-c-011", Base: "origin/master", Ahead: 1, Commits: []card.Commit{{Hash: "a1b2c3", Subject: "見本を決めた"}}, Dirty: 2,
			Issues: []card.IssueProgress{{Ref: "dotfiles#469", Done: 1, Total: 2, Left: []string{"実装"}}}},
		ProgressAt: now,
		LastRun:    &card.RunRecord{Command: "make test", Cwd: "/r/.claude/worktrees/pc-c-011/src/pro-con", Took: 17 * time.Second, At: now.Add(-time.Minute)},
	}}
	m := New(be, nil)
	m.width, m.drawerCard = 200, "C-011"
	body := strings.Join(m.drawerBody(), "\n")
	for _, want := range []string{"PG への指示: 469 を実装して", "今の待ち: PG の turn の途中", "worktree", "  /r/.claude/worktrees/pc-c-011", "ブランチ worktree-pc-c-011", "進捗", "commit: origin/master より 1 本先",
		sgrYellow + "a1b2c3" + sgrReset + " 見本を決めた", "未 commit の変更: 2 ファイル", "issue dotfiles#469 の進捗: 済み 1 / 残り 1", "[ ] 実装",
		"rc=0 (所要 17秒", "src/pro-con で `make test`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("詳細に %q が無い:\n%s", want, body)
		}
	}
	if strings.Contains(body, "PM に渡した指示") {
		t.Fatalf("指示の見出しが PM 宛てのまま:\n%s", body)
	}
}

// 人の番の待ちは目立たせる (黄)。PG の番の待ちは地の色。
func TestDrawerWaitingOnHighlightsHumansTurn(t *testing.T) {
	be := newSpy()
	be.snap.Cards = []card.Card{
		{ID: "C-001", State: card.Waiting, Since: be.snap.Now, Wait: card.Wait{Kind: card.WaitPermission, Question: "Bash を許す?"}},
		{ID: "C-002", State: card.Running, Since: be.snap.Now},
	}
	m := New(be, nil)
	m.width = 200
	line := func(id string) string {
		m.drawerCard = id
		for _, l := range m.drawerBody() {
			if strings.Contains(l, "今の待ち: ") {
				return l
			}
		}
		t.Fatalf("%s に今の待ちが無い", id)
		return ""
	}
	if l := line("C-001"); !strings.HasPrefix(l, sgrYellow) || !strings.Contains(l, "人の番: 権限の確認に答える") {
		t.Fatalf("人の番の待ちを目立たせない: %q", l)
	}
	if l := line("C-002"); strings.HasPrefix(l, sgrYellow) {
		t.Fatalf("PG の番の待ちを人の番と同じ色で出した: %q", l)
	}
}
