package card

import (
	"strings"
	"testing"
	"time"
)

// 今の待ちは、誰の番か (Turn) を読んで、その番で何を待っているかを言う。人の番は理由を添える。
func TestWaitingOn(t *testing.T) {
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	prev := Card{ID: "C-001", State: Running}
	cases := []struct {
		name string
		c    Card
		r    Roles
		want string
	}{
		{"依頼は PM が分ける", Card{State: Requested}, Roles{}, "PM が依頼を分ける"},
		{"PM を起こさないなら人が分ける", Card{State: Requested}, Roles{PMOff: true}, "人の番: 依頼を分ける"},
		{"順番の前が終わっていない", Card{State: Planned, After: []string{"C-001"}}, Roles{}, "C-001 の後"},
		{"再開を待つ", Card{State: Planned, Session: "s", Resume: "答え"}, Roles{}, "PG の空き (同じ session の再開"},
		// 順番が止めるのは初めての起動だけ (dispatcher と同じ card.HeldBy)。一度起動したカードの再開は前のカードを待たない
		{"起動済みの再開は順番で止めない", Card{State: Planned, Session: "s", Resume: "答え", After: []string{"C-001"}}, Roles{}, "PG の空き (同じ session の再開"},
		{"起動を確かめている", Card{State: Planned, Launching: "再開", After: []string{"C-001"}}, Roles{}, "dispatcher が PG の再開を確かめている"},
		{"停滞の印を添える", Card{State: Running, Stalled: true}, Roles{}, "PG の turn の途中 (watchdog が停滞と判定した)"},
		{"起動を待つ", Card{State: Planned}, Roles{}, "PG の空き"},
		{"PG の turn の途中", Card{State: Running}, Roles{}, "PG の turn の途中"},
		{"テストの係の実行", Card{State: Running, Run: "make test", Exec: Exec{Command: "make test", Since: now}}, Roles{}, "テストの係の実行"},
		{"テストの係の順番", Card{State: Running, Run: "make test", Wait: Wait{Kind: WaitResource, Resource: "テスト", Position: 2}}, Roles{}, "テストの係に頼んだコマンドの順番: テスト の順番待ち (2 番目)"},
		{"テストの係がまだ始めない", Card{State: Running, Run: "make test"}, Roles{}, "テストの係が始めるのを待っている"},
		{"利用枠", Card{State: Running, Wait: Wait{Kind: WaitQuota}}, Roles{}, "利用枠の回復"},
		{"質問は PM", Card{State: Waiting, Wait: Wait{Kind: WaitQuestion}}, Roles{}, "PM が質問に答えるか人に回す"},
		{"権限の確認は人", Card{State: Waiting, Wait: Wait{Kind: WaitPermission}}, Roles{}, "人の番: 権限の確認に答える"},
		{"落ちて止めた PG は人", Card{State: Waiting, Wait: Wait{Kind: WaitCrashed}}, Roles{}, "人の番: 落ち続けたので止めた PG"},
		{"PM を起こさないなら質問は人", Card{State: Waiting, Wait: Wait{Kind: WaitQuestion}}, Roles{PMOff: true}, "人の番: 質問に答える (PM を起こさない設定)"},
		{"レビューは取り込みの係", Card{State: Review}, Roles{}, "取り込みの係のレビュー"},
		{"取り込みの係を起こさないならレビューは人", Card{State: Review}, Roles{IntegratorOff: true}, "人の番: レビュー (取り込みの係を起こさない設定)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.WaitingOn([]Card{prev}, tc.r); !strings.HasPrefix(got, tc.want) {
				t.Fatalf("got %q, want prefix %q", got, tc.want)
			}
		})
	}
	for _, c := range []Card{{State: Done}, {State: Review, Archived: true}} {
		if got := c.WaitingOn(nil, Roles{}); got != "" {
			t.Fatalf("終えたカードに待ちを出した: %q", got)
		}
	}
}

// 頼んだ場所は PG の worktree からの相対で書く (repo 全体の make test と src/pro-con の make test を見分ける)。
func TestRunDir(t *testing.T) {
	c := Card{ID: "C-011"}
	for cwd, want := range map[string]string{
		"/r/.claude/worktrees/pc-c-011/src/pro-con": "src/pro-con",
		"/r/.claude/worktrees/pc-c-011":             "worktree の直下",
		"/r/.claude/worktrees/pc-c-011/":            "worktree の直下",
		"/elsewhere":                                "/elsewhere",
	} {
		if got := RunDir(c, cwd); got != want {
			t.Fatalf("RunDir(%q) = %q, want %q", cwd, got, want)
		}
	}
}

// 進捗の行: commit (本数・subject・残りの本数)・未 commit・issue の済み / 残り・テストの最後の結果・取り込みの衝突。
func TestProgressLines(t *testing.T) {
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	dur := func(d time.Duration) string { return d.String() }
	c := Card{ID: "C-011",
		Progress: &Progress{Base: "origin/master", Ahead: 7, Commits: []Commit{{"a1", "一"}, {"b2", "二"}}, Dirty: 3, LastCommit: now.Add(-time.Minute),
			Issues: []IssueProgress{{Ref: "dotfiles#469", Done: 1, Total: 3, Left: []string{"実装", "テスト"}}, {Ref: "dotfiles#042", Last: "半分"}, {Ref: "dotfiles#001"}}},
		LastRun: &RunRecord{Command: "make test", Cwd: "/r/.claude/worktrees/pc-c-011/src/pro-con", RC: 0, Took: 17 * time.Second, At: now.Add(-2 * time.Minute),
			Tail: []string{"ok  pro-con"}},
		Conflicts: []string{"C-011 の commit 済みの分が origin/master と衝突する (f.go)"}, ConflictsAt: now,
	}
	got := strings.Join(c.ProgressLines(now, dur), "\n")
	for _, want := range []string{
		"commit: origin/master より 7 本先 (最後の commit 1m0s前)", "  a1 一", "  b2 二", "  ほか 5 本", "未 commit の変更: 3 ファイル",
		"issue dotfiles#469 の進捗: 済み 1 / 残り 2", "  [ ] 実装", "  [ ] テスト",
		"issue dotfiles#042 の進捗の最後の記録: 半分", "issue dotfiles#001 の本文に進捗の記録はまだ無い",
		"テスト: 最後の結果 rc=0 (所要 17s・2m0s前) src/pro-con で `make test`", "  ok  pro-con",
		"取り込み: C-011 の commit 済みの分が origin/master と衝突する (f.go)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q が無い:\n%s", want, got)
		}
	}
	c.Conflicts = nil
	if got := strings.Join(c.ProgressLines(now, dur), "\n"); !strings.Contains(got, "取り込み: 衝突は見えていない") {
		t.Fatalf("見張りが見て衝突が無いことを言わない:\n%s", got)
	}
	if got := (Card{}).ProgressLines(now, dur); got != nil {
		t.Fatalf("何も無いカードに行を出した: %q", got)
	}
	c.Conflicts, c.ConflictsAt = []string{"衝突する"}, now.Add(-ConflictsStale-time.Second) // 見張りが止まって残った衝突
	if got := strings.Join(c.ProgressLines(now, dur), "\n"); !strings.Contains(got, "取り込み: 衝突する (見張りが 5m1s前に見たまま。見張りが止まっている?)") {
		t.Fatalf("古い衝突をそう言わない:\n%s", got)
	}
	c.LastRun = &RunRecord{Command: "make test", RC: -1, At: now, Err: "PG の作業ディレクトリが記録に無いので実行できない"} // 実行せずに返した
	if got := strings.Join(c.ProgressLines(now, dur), "\n"); !strings.Contains(got, "(頼んだ場所が記録に無い) で `make test`") || !strings.Contains(got, "  PG の作業ディレクトリが記録に無い") {
		t.Fatalf("実行できなかった理由・場所の無いことを言わない:\n%s", got)
	}
	c.ProgressAt = now.Add(-ProgressStale - time.Second)
	if h := ProgressHead(c, now, dur); !strings.Contains(h, "集めたまま") {
		t.Fatalf("古い進捗をそう言わない: %q", h)
	}
}
