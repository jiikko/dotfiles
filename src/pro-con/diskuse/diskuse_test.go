package diskuse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", n)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func group(t *testing.T, u Usage, name string) Group {
	t.Helper()
	for _, g := range u.Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("置き場 %s が無い: %+v", name, u.Groups)
	return Group{}
}

// 数えるのは pro-con が作った物だけ: 記録の worktree と、その隣の pc-* (記録から落ちた物)。repo の本体と、pc- でない worktree と、
// 記録に無い session の transcript は数えない。
func TestMeasureCountsOnlyProConThings(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(repo, ".claude", "worktrees")
	write(t, filepath.Join(repo, "big-main-file"), 100_000)   // repo の本体
	write(t, filepath.Join(wt, "pc-c-001", "a"), 8192)        // 記録にある (完了)
	write(t, filepath.Join(wt, "pc-c-002", "sub", "b"), 4096) // 記録にある (cwd は下のディレクトリ)
	write(t, filepath.Join(wt, "pc-c-003", "c"), 4096)        // 記録から落ちた pc-*
	write(t, filepath.Join(wt, "someone-else", "d"), 100_000) // pro-con の外の worktree
	projects := filepath.Join(root, "projects")
	write(t, filepath.Join(projects, "p1", "s1.jsonl"), 4096)        // 記録の session の transcript
	write(t, filepath.Join(projects, "other", "zzz.jsonl"), 100_000) // pro-con の外の session
	state := filepath.Join(root, "state")
	write(t, filepath.Join(state, "live", "runs", "C-001.log"), 16384)
	write(t, filepath.Join(state, "live", "cards.json"), 10)

	u := Measure(Input{
		StateDir: state,
		Projects: projects,
		Sessions: []Session{
			{SessionID: "s1", CardID: "C-001", Cwd: filepath.Join(wt, "pc-c-001")},
			{SessionID: "s2", CardID: "C-002", Cwd: filepath.Join(wt, "pc-c-002", "sub")},
			{SessionID: "s9", CardID: "C-009", Cwd: filepath.Join(wt, "pc-c-009")}, // 消えた worktree は数えない
			{SessionID: "s8", CardID: "X", Cwd: repo},                              // worktree でない cwd
		},
		Done: map[string]bool{"C-001": true},
	})
	if len(u.Warnings) != 0 {
		t.Fatalf("警告: %v", u.Warnings)
	}
	w := group(t, u, GroupWorktrees)
	var names []string
	for _, it := range w.Items {
		names = append(names, it.Name)
		if it.Name == "pc-c-001" && (it.Card != "C-001" || !it.Done) {
			t.Fatalf("完了したカードの worktree に印が無い: %+v", it)
		}
		if it.Name == "pc-c-002" && it.Card != "C-002" {
			t.Fatalf("下のディレクトリで動いた session の worktree にカードが付かない: %+v", it)
		}
	}
	if strings.Join(names, ",") != "pc-c-001,pc-c-002,pc-c-003" || w.Count != 3 {
		t.Fatalf("worktree の内訳 = %v (count %d)", names, w.Count)
	}
	if w.Bytes >= 100_000 {
		t.Fatalf("repo の本体か外の worktree を数えた: %d", w.Bytes)
	}
	tr := group(t, u, GroupTranscripts)
	if tr.Count != 1 || tr.Items[0].Name != "p1" || tr.Items[0].Card != "C-001" {
		t.Fatalf("transcript の内訳 = %+v", tr.Items)
	}
	st := group(t, u, GroupState)
	if st.Count != 2 || st.Items[0].Name != "live/runs/" {
		t.Fatalf("状態の置き場の内訳 = %+v", st.Items)
	}
	if u.Total != w.Bytes+tr.Bytes+st.Bytes {
		t.Fatalf("合計 %d が置き場の和と合わない", u.Total)
	}
	for i := 1; i < len(u.Groups); i++ {
		if u.Groups[i-1].Bytes < u.Groups[i].Bytes {
			t.Fatalf("置き場が大きい順でない: %+v", u.Groups)
		}
	}
}

func TestHuman(t *testing.T) {
	for b, want := range map[int64]string{512: "512B", 236 * 1024: "236K", 5_452_595: "5.2M", 1_610_612_736: "1.5G"} {
		if got := Human(b); got != want {
			t.Errorf("Human(%d) = %s, want %s", b, got, want)
		}
	}
}

// 記録の cwd が整っていない (./ や .. や // を含む) と、整える前の添字で切って範囲の外になり、画面ごと落ちていた (敵対的レビュー 2026-09-26)。
func TestMeasureUncleanCwd(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "repo", ".claude", "worktrees", "pc-a", "f"), 10)
	for _, cwd := range []string{root + "/repo/./.claude/worktrees/pc-a", root + "/x/../repo/.claude/worktrees/pc-a", root + "/repo//.claude/worktrees/pc-a"} {
		u := Measure(Input{Sessions: []Session{{CardID: "C-1", Cwd: cwd}}})
		if g := group(t, u, GroupWorktrees); g.Count != 1 || g.Items[0].Card != "C-1" {
			t.Fatalf("%s: worktree の内訳 = %+v", cwd, g.Items)
		}
	}
}
