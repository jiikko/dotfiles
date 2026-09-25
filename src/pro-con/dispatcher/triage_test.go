package dispatcher

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 要約役には、同じコマンドの最近の結果を渡す: 実際に走って rc が出たものだけを、同じコマンドに絞って、このカードの前回と分かる形で (issue 475)。
func TestRunFailureGetsRecentResultsOfSameCommand(t *testing.T) {
	r, fr, _ := runRig(t, 2)
	var got []SummaryInput
	r.d.Summarize = func(_ context.Context, in SummaryInput) (string, error) {
		got = append(got, in)
		return "判定: 判定できない", nil
	}
	run := func(id, cmd string, rc int, at time.Time) {
		t.Helper()
		askRun(t, r.dir, id, cmd, at)
		r.tick(t)
		fr.release <- rc
		waitDone(t, r)
	}
	run("C-001", "make test", 2, t0)
	run("C-002", "go vet ./...", 1, t0.Add(time.Second)) // 別のコマンドは材料に入れない
	run("C-002", "make test", 0, t0.Add(2*time.Second))
	run("C-001", "make test", 2, t0.Add(3*time.Second))
	if len(got) != 3 {
		t.Fatalf("失敗した 3 本を要約させない: %d", len(got))
	}
	if got[0].Recent != "" {
		t.Fatalf("前が無いのに最近の結果がある: %q", got[0].Recent)
	}
	last := got[2].Recent
	if strings.Count(last, "\n") != 2 || !strings.Contains(last, "C-002: rc=0") || !strings.Contains(last, "C-001 (このカードの前回): rc=2") || strings.Contains(last, "rc=1") {
		t.Fatalf("同じコマンドの最近の結果が違う:\n%s", last)
	}
	if strings.Index(last, "C-002") > strings.Index(last, "C-001") {
		t.Fatalf("新しい順でない:\n%s", last)
	}
	if !strings.Contains(r.l.resumes[len(r.l.resumes)-1], "一次判定。見込みで、証拠ではない") {
		t.Fatalf("要約が見込みだと PG に伝えない: %s", r.l.resumes[len(r.l.resumes)-1])
	}
}

// rc が出ていない実行 (実行できなかった・途中で止めた) と、rc がコマンドのものか分からない実行は「最近の結果」に入れない。
func TestRememberSkipsRunsWithoutCommandRC(t *testing.T) {
	d := &Dispatcher{}
	job := &runJob{cardID: "C-001", command: "make test"}
	d.remember(job, runResult{rc: -1, err: errors.New("時間切れか中断")}, t0)
	d.remember(job, runResult{rc: 122, err: errors.New("lockman の rc かもしれない")}, t0)
	if len(d.recent) != 0 {
		t.Fatalf("rc の無い実行を覚えた: %+v", d.recent)
	}
	d.remember(job, runResult{rc: 1}, t0)
	if got := d.recentRuns("C-002", "", "make test"); !strings.Contains(got, "C-001: rc=1") {
		t.Fatalf("走った実行を覚えない: %q", got)
	}
	if got := d.recentRuns("C-002", "other", "make test"); got != "" {
		t.Fatalf("別の repo の同じコマンドの結果を混ぜた: %q", got)
	}
}

// 利用枠が 95% 以上 (新しく起動・再開しない) のときに始めた実行は、失敗しても要約させない (ログの末尾だけ渡す)。
func TestRunFailureNotSummarizedWhenQuotaStops(t *testing.T) {
	r, fr, summarized := runRig(t, 1)
	r.d.Usage = func(context.Context) (Usage, error) { return Usage{Week: 96}, nil }
	r.tick(t) // 枠を読む (テストの係の後に読むので、頼みより先に 1 回回す)
	askRun(t, r.dir, "C-001", "make test", t0)
	r.tick(t)
	fr.release <- 2
	waitDone(t, r)
	if len(*summarized) != 0 {
		t.Fatalf("枠が 95%% 以上なのに要約させた: %v", *summarized)
	}
	if c := states(t, r.dir)["C-001"]; !strings.Contains(c.Resume, "FAIL: TestFoo") || strings.Contains(c.Resume, "要約") { // 枠で再開は待つので、渡す結果の文を見る
		t.Fatalf("要約しないときにログの末尾だけを渡さない: %q", c.Resume)
	}
}

// 変更の材料は、取り込む先との分かれ目から作業ツリーまで (commit 前の変更と未追跡のファイルも入る)。取り込む先が無ければ空。
func TestChangeStatIncludesUncommittedWork(t *testing.T) {
	root := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	repo := filepath.Join(root, "r")
	git(root, "init", "-q", "-b", "master", repo)
	write(filepath.Join(repo, "a.go"), "package a\n")
	git(repo, "add", ".")
	git(repo, "commit", "-qm", "base")
	if got := changeStat(context.Background(), repo); got != "" {
		t.Fatalf("取り込む先が無いのに材料がある: %q", got)
	}
	git(repo, "update-ref", "refs/remotes/origin/master", "HEAD")
	write(filepath.Join(repo, "b.go"), "package a\n")
	git(repo, "add", "b.go")
	git(repo, "commit", "-qm", "b")
	write(filepath.Join(repo, "a.go"), "package a\n\nfunc F() {}\n") // commit 前
	write(filepath.Join(repo, "new.txt"), "x\n")                     // 未追跡
	got := changeStat(context.Background(), repo)
	if !strings.Contains(got, "a.go") || !strings.Contains(got, "b.go") || !strings.Contains(got, "未追跡のファイル: 1 個") {
		t.Fatalf("commit 済み・commit 前・未追跡の変更が材料に入らない:\n%s", got)
	}
}

// prompt には 3 つの材料と、判定の 4 つの選択肢を入れる (材料が無ければ「なし」)。
func TestSummaryPromptCarriesMaterials(t *testing.T) {
	p := summaryPrompt(SummaryInput{Tail: "FAIL: TestFoo", Diff: " a.go | 2 +-", Recent: "- 09-26 10:00 C-002: rc=2\n"})
	for _, want := range []string{"FAIL: TestFoo", "a.go | 2 +-", "C-002: rc=2", "判定: この変更のせい", "判定: 負荷・環境で落ちた見込み", "判定: 前から落ちる見込み", "判定: 判定できない"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt に %q が無い:\n%s", want, p)
		}
	}
	if p := summaryPrompt(SummaryInput{Tail: "x"}); strings.Count(p, "(なし)") != 2 {
		t.Fatalf("材料が無いことを書かない:\n%s", p)
	}
}
