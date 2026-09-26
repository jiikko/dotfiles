package dispatcher

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// progressGit は本物の git を一時ディレクトリで走らせる (進捗は本物の git の出力の形に依る)。
func progressGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func saveCards(t *testing.T, dir string, cards []card.Card) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(dir, func(st *store.State) error {
		st.Cards, st.NextID = cards, len(cards)+1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 進捗は PG の worktree の git (origin/master より先の commit・未 commit の変更) と、worktree の issue の本文の「進捗」節から集める。
// worktree がまだ無いカードは repo の本文だけ読む。依頼・完了・片付けたカードと、設定に無い repo のカードは集めない。
func TestGatherProgress(t *testing.T) {
	root := t.TempDir()
	repo, dir := filepath.Join(root, "repo"), filepath.Join(root, "state")
	progressGit(t, root, "init", "-q", "-b", "master", repo)
	writeFile(t, filepath.Join(repo, "issues", "epic", "415", "469-progress.md"), "# 469\n\n## 進捗\n\n- [ ] 見本\n")
	writeFile(t, filepath.Join(repo, "issues", "042-old.md"), "# 42\n\n## 進捗\n\n- 2026-09-01 起票\n- 2026-09-02 半分\n")
	progressGit(t, repo, "add", ".")
	progressGit(t, repo, "commit", "-qm", "base")
	progressGit(t, repo, "update-ref", "refs/remotes/origin/master", "HEAD")
	wt := filepath.Join(repo, ".claude", "worktrees", "pc-c-001")
	progressGit(t, repo, "worktree", "add", "-q", "-b", "worktree-pc-c-001", wt, "origin/master")
	writeFile(t, filepath.Join(wt, "issues", "epic", "415", "469-progress.md"), "# 469\n\n## 進捗\n\n- [x] 見本\n- [ ] 実装\n")
	progressGit(t, wt, "commit", "-qam", "見本を決めた")
	writeFile(t, filepath.Join(wt, "a.go"), "package a\n")
	writeFile(t, filepath.Join(wt, "issues", "042-old.md"), "書きかけ\n") // 未 commit の変更 2 つ (未追跡と変更)

	ref := func(n int) []card.IssueRef { return []card.IssueRef{{Repo: "r", Number: n}} }
	saveCards(t, dir, []card.Card{
		// 別の repo (x) の issue は読まない
		{ID: "C-001", Repo: "r", State: card.Running, Issues: append(ref(469), card.IssueRef{Repo: "x", Number: 469})},
		{ID: "C-002", Repo: "r", State: card.Planned, Issues: ref(42)}, // worktree がまだ無い
		{ID: "C-003", Repo: "r", State: card.Done, Issues: ref(469)},
		{ID: "C-004", Repo: "r", State: card.Requested, Issues: ref(469)},
		{ID: "C-005", Repo: "other", State: card.Running, Issues: ref(469)},
		{ID: "C-006", Repo: "r", State: card.Review, Issues: ref(999)}, // 本文が無い
	})
	d := &Dispatcher{Dir: dir, Repos: map[string]string{"r": repo}, ProgressGit: ExecProgressGit{}}
	now := time.Now()
	p, err := d.gatherProgress(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	c1 := p.Cards["C-001"]
	if c1.Base != "origin/master" || c1.Ahead != 1 || len(c1.Commits) != 1 || c1.Commits[0].Subject != "見本を決めた" || c1.Dirty != 2 || c1.LastCommit.IsZero() {
		t.Fatalf("worktree の git を読まない: %+v", c1)
	}
	if len(c1.Issues) != 1 || c1.Issues[0].Done != 1 || c1.Issues[0].Total != 2 || len(c1.Issues[0].Left) != 1 || c1.Issues[0].Left[0] != "実装" {
		t.Fatalf("worktree の issue の進捗 (PG が書き足した分) を読まない: %+v", c1.Issues)
	}
	c2 := p.Cards["C-002"]
	if c2.Base != "" || len(c2.Issues) != 1 || c2.Issues[0].Last != "2026-09-02 半分" || c2.Issues[0].Ref != "r#042" {
		t.Fatalf("worktree の無いカードは repo の本文を読むはず: %+v", c2)
	}
	if c6 := p.Cards["C-006"]; !strings.Contains(c6.Err, "r#999") {
		t.Fatalf("本文が無い issue を知らせない: %+v", c6)
	}
	for _, id := range []string{"C-003", "C-004", "C-005"} {
		if _, ok := p.Cards[id]; ok {
			t.Fatalf("%s の進捗を集めた (完了・依頼・設定に無い repo は集めない)", id)
		}
	}
}

// 「進捗」節は同じか浅い見出しで終わる (深い見出しは節の中)。コードの囲みの中の見出し・チェックボックスは数えない。
// チェックボックスがあれば最後の項目は出さない。
func TestParseProgress(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		done, total int
		left        []string
		last        string
	}{
		{name: "checkbox", body: "## 概要\n- [ ] 概要の項目\n## 進捗\n- [x] 一\n### 決定\n- [ ] 二\n  - [X] 子\n```\n- [ ] 囲み\n## 囲みの見出し\n```\n- [ ] 三\n## 関連\n- [ ] 関連の項目\n",
			done: 2, total: 4, left: []string{"二", "三"}},
		{name: "bullets", body: "# t\n## 進捗（2026-09-09）\n- 起票\n  続きの行\n- 段階 1\n  - 子の項目\n## 次\n- 別の節\n", last: "段階 1"},
		{name: "none", body: "# t\n## 概要\n- [ ] 進捗の節ではない\n"},
		// 題 (H1) に「進捗」があっても節にしない (issue 469 自身の題がこの形)
		{name: "title", body: "# 469: 作業の進捗を確かめられる\n## 概要\n- [ ] 概要の項目\n## 関連\n- 関連の項目\n## 進捗\n"},
		// 節の中の深い見出しが「進捗」を含んでも節を縮めない
		{name: "nested", body: "## 進捗\n### 進捗メモ\n- [ ] a\n### 次\n- [ ] b\n## 関連\n- [ ] c\n", total: 2, left: []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ip, err := parseProgress(card.IssueProgress{}, bufio.NewScanner(strings.NewReader(tc.body)))
			if err != nil {
				t.Fatal(err)
			}
			if ip.Done != tc.done || ip.Total != tc.total || strings.Join(ip.Left, "|") != strings.Join(tc.left, "|") || ip.Last != tc.last {
				t.Fatalf("got %+v", ip)
			}
		})
	}
}

// 集め直すのは progressEvery ごと、前の回が終わってから。ProgressGit が無ければ集めない。
func TestCollectProgressInterval(t *testing.T) {
	dir := t.TempDir()
	saveCards(t, dir, nil)
	d := &Dispatcher{Dir: dir}
	d.collectProgress(context.Background(), t0)
	if !d.progressAt.IsZero() {
		t.Fatal("ProgressGit が無いのに集めた")
	}
	d.ProgressGit = ExecProgressGit{}
	d.collectProgress(context.Background(), t0)
	for d.progressBusy.Load() {
		time.Sleep(time.Millisecond)
	}
	p, errs := store.LoadDerived(dir)
	if len(errs) > 0 || !p.Progress.At.Equal(t0) {
		t.Fatalf("集めた進捗を書かない: %+v %v", p.Progress, errs)
	}
	d.collectProgress(context.Background(), t0.Add(progressEvery-time.Second))
	if !d.progressAt.Equal(t0) {
		t.Fatal("間隔の前に集め直した")
	}
}
