package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

var t0 = time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)

// rig は本物の git の repo (origin/master を手で置く) と、PG の worktree (<repo>/.claude/worktrees/pc-<id>) を持つ。
type rig struct {
	t     *testing.T
	dir   string // 状態の置き場
	repo  string
	cards []card.Card
	sent  []store.Request
	fail  bool // Submit を失敗させる
	m     *Monitor
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	r := &rig{t: t, dir: filepath.Join(root, "state"), repo: filepath.Join(root, "repo")}
	r.git(root, "init", "-q", "-b", "master", r.repo)
	r.write("", "f.txt", "1\n2\n3\n")
	r.write("", "g.txt", "a\n")
	r.git(r.repo, "add", ".")
	r.git(r.repo, "commit", "-qm", "base")
	r.git(r.repo, "update-ref", "refs/remotes/origin/master", "HEAD")
	r.m = &Monitor{Dir: r.dir, Repos: map[string]string{"r": r.repo}, Now: func() time.Time { return t0 },
		Submit: func(q store.Request) (string, error) {
			if r.fail {
				return "", errors.New("箱が満杯")
			}
			r.sent = append(r.sent, q)
			return "id", nil
		}}
	return r
}

func (r *rig) git(dir string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// wt は id のカードの PG の worktree (無ければ作る)。
func (r *rig) wt(id string) string {
	wt := filepath.Join(r.repo, ".claude", "worktrees", "pc-"+strings.ToLower(id))
	if _, err := os.Stat(wt); err != nil {
		r.git(r.repo, "worktree", "add", "-q", "-b", "worktree-pc-"+strings.ToLower(id), wt, "origin/master")
	}
	return wt
}

func (r *rig) write(id, name, body string) {
	r.t.Helper()
	dir := r.repo
	if id != "" {
		dir = r.wt(id)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// commit は id の PG の worktree (id が空なら master) で name を body にして commit する。
func (r *rig) commit(id, name, body string) {
	r.t.Helper()
	r.write(id, name, body)
	dir := r.repo
	if id != "" {
		dir = r.wt(id)
	}
	r.git(dir, "add", name)
	r.git(dir, "commit", "-qm", name)
}

// master は master に commit して origin/master を進める (取り込みの係の push の代わり)。
func (r *rig) master(name, body string) {
	r.t.Helper()
	r.commit("", name, body)
	r.git(r.repo, "update-ref", "refs/remotes/origin/master", "HEAD")
}

func (r *rig) card(id string, s card.State) {
	r.t.Helper()
	r.cards = append(r.cards, card.Card{ID: id, Title: id, Repo: "r", State: s, Since: t0})
	r.save()
}

func (r *rig) save() {
	r.t.Helper()
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	b, err := json.Marshal(store.State{NextID: len(r.cards) + 1, Cards: r.cards})
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, store.StateFile), b, 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// check は 1 回見て、置いた知らせを返す。
func (r *rig) check() []store.Request {
	r.t.Helper()
	r.sent = nil
	if _, err := r.m.Check(context.Background()); err != nil {
		r.t.Fatal(err)
	}
	for _, q := range r.sent {
		if q.Kind != store.KindMonitor {
			r.t.Fatalf("見張りの知らせでない依頼を置いた: %+v", q)
		}
	}
	return r.sent
}

func notes(qs []store.Request) string {
	var out []string
	for _, q := range qs {
		out = append(out, q.CardID+": "+q.Note)
	}
	return strings.Join(out, "\n")
}

// commit 済みの分が master と衝突したら 1 度だけ知らせ、衝突したファイルを書く。直したら「見えなくなった」を 1 度知らせる。
func TestConflictWithMasterIsToldOnceAndWhenGone(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.commit("C-001", "f.txt", "1\nPG\n3\n")
	if got := r.check(); len(got) != 0 {
		t.Fatalf("master が動いていないのに衝突を知らせた:\n%s", notes(got))
	}
	r.master("f.txt", "1\nMASTER\n3\n")
	got := r.check()
	if len(got) != 1 || got[0].CardID != "C-001" || !strings.Contains(got[0].Note, "origin/master と衝突する (f.txt)") {
		t.Fatalf("master との衝突を知らせない:\n%s", notes(got))
	}
	if got := r.check(); len(got) != 0 {
		t.Fatalf("変わっていないのに知らせ直した:\n%s", notes(got))
	}
	r.git(r.wt("C-001"), "merge", "-q", "-X", "theirs", "origin/master", "-m", "取り込み") // PG が master を取り込んで直した
	got = r.check()
	if len(got) != 1 || !strings.Contains(got[0].Note, "C-001 と origin/master の衝突は見えなくなった") {
		t.Fatalf("衝突が消えたのを知らせない:\n%s", notes(got))
	}
}

// commit していない変更・commit の無い worktree・取り込み済みのカードは見ない。依頼・完了・片付けたカードの worktree も見ない。
func TestOnlyCommittedWorkOfWatchedCardsIsChecked(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.write("C-001", "f.txt", "1\nPG\n3\n") // commit していない
	for _, id := range []string{"C-002", "C-003", "C-004"} {
		r.commit(id, "f.txt", "1\n"+id+"\n3\n")
	}
	r.card("C-002", card.Done)
	r.card("C-003", card.Requested)
	r.card("C-004", card.Review)
	r.cards[len(r.cards)-1].Archived = true
	r.save()
	r.master("f.txt", "1\nMASTER\n3\n")
	if got := r.check(); len(got) != 0 {
		t.Fatalf("見ないはずのカードの衝突を知らせた:\n%s", notes(got))
	}
}

// PG どうしの衝突は、どちらも master と衝突しないときに知らせる (master と衝突するカードの組は二重に知らせず、見ていないだけなので「消えた」にもしない)。
func TestConflictBetweenPGs(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.card("C-002", card.Review)
	r.commit("C-001", "g.txt", "one\n")
	r.commit("C-002", "g.txt", "two\n")
	got := r.check()
	if len(got) != 1 || !strings.Contains(got[0].Note, "C-001 と C-002 の commit 済みの分どうしが衝突する (g.txt)") {
		t.Fatalf("PG どうしの衝突を知らせない:\n%s", notes(got))
	}
	r.master("g.txt", "master\n") // 両方とも master と衝突する: 組の知らせは消え、master との衝突を 2 つ知らせる
	got = r.check()
	s := notes(got)
	if len(got) != 2 || !strings.Contains(s, "C-001 の commit 済みの分が origin/master と衝突する") || !strings.Contains(s, "C-002 の commit 済みの分が origin/master と衝突する") {
		t.Fatalf("master との衝突を知らせない / 見ていないだけの組の衝突を「消えた」にした:\n%s", s)
	}
}

// テストの順番が長い (3 本以上か、先頭が 30 分以上) ときに 1 度、戻ったら 1 度知らせる。待ち時間が伸びるだけでは知らせ直さない。
// 数えるのは dispatcher が列に並べたカード (Wait がテストの係の順番) だけ。
func TestLongTestQueue(t *testing.T) {
	r := newRig(t)
	now := t0
	r.m.Now = func() time.Time { return now }
	queued := func(id string, pos int, at time.Time, res string) {
		r.cards = append(r.cards, card.Card{ID: id, Title: id, State: card.Running, Since: t0, Run: "make test", RunAt: at,
			Wait: card.Wait{Kind: card.WaitResource, Resource: res, Position: pos}})
		r.save()
	}
	queued("C-001", 1, t0, store.RunResource)
	queued("C-002", 2, t0.Add(time.Minute), store.RunResource)
	queued("C-009", 0, t0, "repo の lock (pro-con の外)") // テストの係の列の外で待っている 1 本は数えない
	if got := r.check(); len(got) != 0 {
		t.Fatalf("2 本・待ち 0 分で長いと知らせた:\n%s", notes(got))
	}
	now = t0.Add(31 * time.Minute)
	got := r.check()
	if len(got) != 1 || !strings.Contains(got[0].Note, "2 本が待っている。先頭の C-001 は頼んでから 31m0s") {
		t.Fatalf("先頭が 30 分待っているのに知らせない:\n%s", notes(got))
	}
	now = t0.Add(40 * time.Minute)
	if got := r.check(); len(got) != 0 {
		t.Fatalf("待ち時間が伸びただけで知らせ直した:\n%s", notes(got))
	}
	r.cards = r.cards[1:2] // 先頭が実行に入り、列は C-002 だけ (頼んでから 39 分) → まだ長い
	r.cards[0].RunAt = now.Add(-time.Minute)
	r.save()
	got = r.check()
	if len(got) != 1 || got[0].Note != "テストの順番の長さが戻った" {
		t.Fatalf("戻ったのを知らせない:\n%s", notes(got))
	}
	for i := 1; i <= 3; i++ {
		queued("C-01"+string(rune('0'+i)), i+1, now, store.RunResource)
	}
	if got := r.check(); len(got) != 1 || !strings.Contains(got[0].Note, "4 本が待っている") {
		t.Fatalf("3 本以上で知らせない:\n%s", notes(got))
	}
}

// 受付の箱に置けなかった知らせは、次の回に置き直す (置けた分は二重に置かない)。
func TestUnsentNotesAreRetried(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.commit("C-001", "f.txt", "1\nPG\n3\n")
	r.master("f.txt", "1\nMASTER\n3\n")
	r.fail = true
	if _, err := r.m.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "受付の箱に置けない") {
		t.Fatalf("置けなかったことを返さない: %v", err)
	}
	r.fail = false
	if got := r.check(); len(got) != 1 {
		t.Fatalf("置けなかった知らせを置き直さない:\n%s", notes(got))
	}
	if got := r.check(); len(got) != 0 {
		t.Fatalf("置いた知らせを二重に置いた:\n%s", notes(got))
	}
}

// 取り込む先が読めない repo は、前に知らせた衝突を「見えなくなった」にしない (失敗は返す)。
func TestUnreadableRepoKeepsFindings(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.commit("C-001", "f.txt", "1\nPG\n3\n")
	r.master("f.txt", "1\nMASTER\n3\n")
	if got := r.check(); len(got) != 1 {
		t.Fatalf("衝突を知らせない:\n%s", notes(got))
	}
	r.git(r.repo, "update-ref", "-d", "refs/remotes/origin/master")
	r.sent = nil
	if _, err := r.m.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "取り込む先") {
		t.Fatalf("取り込む先が無いことを返さない: %v", err)
	}
	if len(r.sent) != 0 {
		t.Fatalf("見られなかった repo の衝突を消えたとした:\n%s", notes(r.sent))
	}
}

// merge-tree が失敗した組は飛ばして 1 度だけ Log に出し、同じ組では git を呼び直さない。
func TestMergeFailureIsLoggedOnce(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.commit("C-001", "f.txt", "1\nPG\n3\n")
	fg := &failingGit{Git: ExecGit{}}
	r.m.Git = fg
	var logs []string
	r.m.Log = func(s string) { logs = append(logs, s) }
	for range 2 {
		if got := r.check(); len(got) != 0 {
			t.Fatalf("見られなかった組を衝突として知らせた:\n%s", notes(got))
		}
	}
	if fg.calls != 1 || len(logs) != 1 || !strings.Contains(logs[0], "C-001 と origin/master の衝突を見られない") {
		t.Fatalf("失敗した組を 1 度だけ出さない / 呼び直した: calls=%d logs=%v", fg.calls, logs)
	}
}

type failingGit struct {
	Git
	calls int
}

func (g *failingGit) MergeTree(context.Context, string, string, string) (bool, []string, error) {
	g.calls++
	return false, nil, errors.New("git merge-tree が rc=128")
}

// 組のどちらか片方だけが master と衝突するときも、組の衝突は知らせない (その片方が master を取り込めば組の結果も変わる)。
func TestPairSkippedWhenEitherConflictsWithMaster(t *testing.T) {
	for _, side := range []string{"C-001", "C-002"} {
		r := newRig(t)
		r.card("C-001", card.Running)
		r.card("C-002", card.Running)
		r.commit("C-001", "g.txt", "one\n")
		r.commit("C-002", "g.txt", "two\n")
		r.commit(side, "f.txt", "1\n"+side+"\n3\n")
		r.master("f.txt", "1\nMASTER\n3\n")
		got := r.check()
		if len(got) != 1 || got[0].CardID != side || !strings.Contains(got[0].Note, "origin/master と衝突する (f.txt)") {
			t.Fatalf("%s だけが master と衝突するのに、組の衝突も知らせた / master との衝突を知らせない:\n%s", side, notes(got))
		}
	}
}

// merge-tree が失敗した組・止める途中の Check は、前に知らせた衝突を「消えた」にしない (失敗・取り消しを「衝突なし」と取り違えない)。
func TestFailedOrCanceledCheckKeepsFindings(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.commit("C-001", "f.txt", "1\nPG\n3\n")
	r.master("f.txt", "1\nMASTER\n3\n")
	if got := r.check(); len(got) != 1 {
		t.Fatalf("衝突を知らせない:\n%s", notes(got))
	}
	r.master("g.txt", "moved\n") // 取り込む先が動いて組が変わる → 覚えた結果を使わず git を呼ぶ
	r.m.Git = &failingGit{Git: ExecGit{}}
	if got := r.check(); len(got) != 0 {
		t.Fatalf("merge-tree の失敗で「消えた」を置いた:\n%s", notes(got))
	}
	r.m.Git = nil
	r.master("g.txt", "moved again\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.sent = nil
	if _, err := r.m.Check(ctx); err == nil || len(r.sent) != 0 {
		t.Fatalf("取り消された Check が知らせを置いた / 取り消しを返さない: err=%v\n%s", err, notes(r.sent))
	}
	if got := r.check(); len(got) != 0 {
		t.Fatalf("残っている衝突を知らせ直した / 消えたとした:\n%s", notes(got))
	}
}

// 組の鍵はカードの ID の順 (レーンの並べ替えで cards.json の順が変わっても、同じ組を「消えた」→「衝突する」と知らせ直さない)。
func TestPairKeyIgnoresCardOrder(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.card("C-002", card.Running)
	r.commit("C-001", "g.txt", "one\n")
	r.commit("C-002", "g.txt", "two\n")
	if got := r.check(); len(got) != 1 {
		t.Fatalf("組の衝突を知らせない:\n%s", notes(got))
	}
	r.cards[0], r.cards[1] = r.cards[1], r.cards[0]
	r.save()
	if got := r.check(); len(got) != 0 {
		t.Fatalf("並べ替えただけで知らせ直した:\n%s", notes(got))
	}
}

// 今見えている衝突を ConflictsFile に写す (カードの詳細が読む。issue 469)。PG どうしの組は両方のカードに載せ、
// 見られなかった回は前の結果を残し、直したら消す。
func TestConflictsFileMirrorsCurrentFindings(t *testing.T) {
	r := newRig(t)
	r.card("C-001", card.Running)
	r.card("C-002", card.Review)
	r.commit("C-001", "g.txt", "one\n")
	r.commit("C-002", "g.txt", "two\n")
	r.check()
	load := func() store.Conflicts {
		t.Helper()
		if _, err := os.Stat(filepath.Join(r.dir, store.ConflictsFile)); err != nil {
			t.Fatal(err)
		}
		d, errs := store.LoadDerived(r.dir)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		return d.Conflicts
	}
	c := load()
	for _, id := range []string{"C-001", "C-002"} {
		if len(c.Cards[id]) != 1 || !strings.Contains(c.Cards[id][0], "C-001 と C-002 の commit 済みの分どうしが衝突する") {
			t.Fatalf("%s に組の衝突が載らない: %+v", id, c.Cards)
		}
	}
	base := r.git(r.repo, "rev-parse", "refs/remotes/origin/master")
	r.git(r.repo, "update-ref", "-d", "refs/remotes/origin/master") // 見られない回: 前の結果を残す
	_, _ = r.m.Check(context.Background())
	if c := load(); len(c.Cards["C-001"]) != 1 || len(c.Cards["C-002"]) != 1 {
		t.Fatalf("見られなかった回に衝突を消した: %+v", c.Cards)
	}
	// 見張りを起こし直した直後 (知らせた物をメモリに持たない) に見られない回も、前に書いた結果を残す
	r.m = &Monitor{Dir: r.dir, Repos: r.m.Repos, Now: r.m.Now, Submit: r.m.Submit}
	_, _ = r.m.Check(context.Background())
	if c := load(); len(c.Cards["C-001"]) != 1 || len(c.Cards["C-002"]) != 1 {
		t.Fatalf("起こし直した直後の見られなかった回に衝突を消した: %+v", c.Cards)
	}
	r.git(r.repo, "update-ref", "refs/remotes/origin/master", base)
	r.commit("C-002", "g.txt", "one\n") // 直した
	r.check()
	if c := load(); len(c.Cards) != 0 || c.At.IsZero() {
		t.Fatalf("直した衝突が残る / 見た時刻が無い: %+v", c)
	}
}
