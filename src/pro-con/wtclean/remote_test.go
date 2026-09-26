package wtclean

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pro-con/card"
)

// 🚨 本物の git push --delete を呼ぶ。remote は sandbox の下の git init --bare で、allowRemote は testing.Testing の間
// origin が sandbox の外なら実行前に拒否する (本物の remote に触らない)。

type remoteFixture struct {
	*fixture
	bare string
}

// newRemoteFixture は sandbox の下に偽の remote (bare) と、それを origin にした repo を作る (master を push 済み)。
func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	parent, err := os.MkdirTemp(sandbox, "remote")
	if err != nil {
		t.Fatal(err)
	}
	parent, _ = filepath.EvalSymlinks(parent)
	bare := filepath.Join(parent, "origin.git")
	git(t, parent, "init", "-q", "--bare", "-b", "master", bare)
	f := newFixture(t, parent)
	git(t, f.repo, "remote", "add", "origin", bare)
	git(t, f.repo, "config", "fetch.prune", "false") // 開発機の ~/.gitconfig の fetch.prune に頼らない (消えた remote は Fetch の --prune で拾う)
	git(t, f.repo, "push", "-q", "origin", "master")
	return &remoteFixture{fixture: f, bare: bare}
}

// branch は master から branch を切って file の commit を 1 本足し、origin へ push する (ローカルのブランチは残さない)。
func (f *remoteFixture) branch(branch, file string) string {
	git(f.t, f.repo, "checkout", "-q", "-b", branch, "master")
	sha := f.commit(f.repo, file)
	git(f.t, f.repo, "push", "-q", "origin", branch)
	git(f.t, f.repo, "checkout", "-q", "master")
	git(f.t, f.repo, "branch", "-q", "-D", branch)
	return sha
}

// landRemote は commit を master に入れて origin へ push する (pick なら cherry-pick で。祖先にならない)。
func (f *remoteFixture) landRemote(sha string, pick bool) {
	f.land(sha, pick)
	git(f.t, f.repo, "push", "-q", "origin", "master")
}

func (f *remoteFixture) done(id string) {
	f.in.Cards[id] = card.Card{ID: id, Repo: "r", State: card.Done}
}

func (f *remoteFixture) onRemote(branch string) bool {
	return git(f.t, f.bare, "for-each-ref", "refs/heads/"+branch) != ""
}

func (f *remoteFixture) scanRemote() map[string]RemoteVerdict {
	f.t.Helper()
	vs, err := ScanRemote(context.Background(), f.in)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]RemoteVerdict{}
	for _, v := range vs {
		out[v.Branch] = v
	}
	return out
}

func (f *remoteFixture) cleanRemote(vs map[string]RemoteVerdict) map[string]RemoteResult {
	f.t.Helper()
	var list []RemoteVerdict
	for _, v := range vs {
		list = append(list, v)
	}
	out := map[string]RemoteResult{}
	opt := Options{Fresh: func(context.Context) (Inputs, error) { return f.in, nil }}
	if err := CleanRemote(context.Background(), list, opt, func(r RemoteResult) { out[r.Verdict.Branch] = r }); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func TestRemoteJudgeAndClean(t *testing.T) {
	f := newRemoteFixture(t)
	f.landRemote(f.branch("worktree-pc-c-001", "anc.txt"), false)
	f.done("C-001")
	f.landRemote(f.branch("worktree-pc-c-002", "picked.txt"), true)
	f.done("C-002")
	f.branch("worktree-pc-c-003", "only-here.txt")
	f.done("C-003")
	f.landRemote(f.branch("worktree-pc-c-004", "review.txt"), false)
	f.in.Cards["C-004"] = card.Card{ID: "C-004", Repo: "r", State: card.Review}
	f.landRemote(f.branch("worktree-pc-c-005", "unknown.txt"), false)
	f.landRemote(f.branch("worktree-pc-c-006", "stop.txt"), false)
	f.in.Cards["C-006"] = card.Card{ID: "C-006", Repo: "r", State: card.Done, StopAfterClose: true}
	f.landRemote(f.branch("worktree-pc-c-007", "other-repo.txt"), false)
	f.in.Cards["C-007"] = card.Card{ID: "C-007", Repo: "other", State: card.Done}
	f.landRemote(f.branch("worktree-pc-c-001-r1", "retry.txt"), false) // 作り直した版は元のカード (C-001) で見る
	f.landRemote(f.branch("worktree-pc-pm-20260927", "pm.txt"), false)
	f.landRemote(f.branch("topic", "topic.txt"), false) // PG のブランチでないものは並べもしない

	got := f.scanRemote()
	want := map[string]struct {
		remove bool
		card   string
		why    string
	}{
		"worktree-pc-c-001":       {true, "C-001", "祖先"},
		"worktree-pc-c-002":       {true, "C-002", "git cherry の 1 本が全部 -"},
		"worktree-pc-c-003":       {false, "C-003", "origin/master に無い commit が 1 本"},
		"worktree-pc-c-004":       {false, "C-004", "カードが完了していない"},
		"worktree-pc-c-005":       {false, "", "記録に無いカード"},
		"worktree-pc-c-006":       {false, "C-006", "止め終えていない"},
		"worktree-pc-c-007":       {false, "C-007", "カードの repo (other)"},
		"worktree-pc-c-001-r1":    {true, "C-001", "祖先"},
		"worktree-pc-pm-20260927": {false, "", "PM・取り込みの係"},
	}
	if len(got) != len(want) {
		t.Errorf("判定した本数 = %d、want %d: %v", len(got), len(want), got)
	}
	for b, w := range want {
		v, ok := got[b]
		if !ok {
			t.Errorf("%s を判定していない", b)
			continue
		}
		if v.Remove != w.remove || v.CardID != w.card || !strings.Contains(v.Why, w.why) {
			t.Errorf("%s = remove %v card %q (%s)、want remove %v card %q (%s を含む)", b, v.Remove, v.CardID, v.Why, w.remove, w.card, w.why)
		}
	}

	res := f.cleanRemote(got)
	for b, w := range want {
		switch r, ok := res[b]; {
		case w.remove && (!ok || r.Outcome != Removed || f.onRemote(b)):
			t.Errorf("%s を消していない: %+v", b, r)
		case !w.remove && (ok || !f.onRemote(b)):
			t.Errorf("%s を消しに行った: %+v", b, r)
		}
	}
	if !f.onRemote("topic") {
		t.Error("PG のブランチでない topic を消した")
	}
	if git(t, f.repo, "for-each-ref", "refs/remotes/origin/worktree-pc-c-001") != "" {
		t.Error("消したブランチの追跡 (origin/worktree-pc-c-001) が残っている")
	}
	// 取り込む先の祖先でない先端 (cherry-pick で入った C-002) だけを refs/pro-con/removed/origin/ に残す
	kept := git(t, f.repo, "for-each-ref", "--format=%(refname)", "refs/pro-con/removed/origin/")
	if want := "refs/pro-con/removed/origin/worktree-pc-c-002/" + got["worktree-pc-c-002"].Head; kept != want {
		t.Errorf("残した先端 = %q、want %q", kept, want)
	}
}

// 一覧の後に remote で動いた・消えたブランチは、消す直前の fetch --prune と判定し直しで拾う。
func TestRemoteCleanRejudgesAfterFetch(t *testing.T) {
	f := newRemoteFixture(t)
	f.landRemote(f.branch("worktree-pc-c-001", "one.txt"), false)
	f.done("C-001")
	f.landRemote(f.branch("worktree-pc-c-002", "two.txt"), false)
	f.done("C-002")
	got := f.scanRemote()
	if !got["worktree-pc-c-001"].Remove || !got["worktree-pc-c-002"].Remove {
		t.Fatalf("前提: 両方消してよいはず: %+v", got)
	}
	// 一覧の後に、別の clone から C-001 のブランチへ master に無い commit を push し、C-002 のブランチを消す
	other := filepath.Join(filepath.Dir(f.bare), "other")
	git(t, filepath.Dir(f.bare), "clone", "-q", f.bare, other)
	git(t, other, "checkout", "-q", "worktree-pc-c-001")
	write(t, filepath.Join(other, "late.txt"), "late\n")
	git(t, other, "add", "late.txt")
	git(t, other, "commit", "-qm", "late")
	git(t, other, "push", "-q", "origin", "worktree-pc-c-001", ":worktree-pc-c-002")

	res := f.cleanRemote(got)
	if r := res["worktree-pc-c-001"]; r.Outcome != Skipped || !strings.Contains(r.Detail, "無い commit が 1 本") || !f.onRemote("worktree-pc-c-001") {
		t.Errorf("動いたブランチ = %+v (remote にある: %v)", r, f.onRemote("worktree-pc-c-001"))
	}
	if r := res["worktree-pc-c-002"]; r.Outcome != Skipped || !strings.Contains(r.Detail, "remote にもう無い") {
		t.Errorf("消えたブランチ = %+v", r)
	}
}

// 判定した先端 (lease) から remote が動いていれば、git push が断って消さない (判定し直しと push の間の窓)。
func TestDeleteRemoteHonorsLease(t *testing.T) {
	f := newRemoteFixture(t)
	f.landRemote(f.branch("worktree-pc-c-001", "one.txt"), false)
	f.done("C-001")
	v := f.scanRemote()["worktree-pc-c-001"]
	stale := v
	stale.Head = git(t, f.repo, "rev-parse", "master~1")
	if err := deleteRemote(context.Background(), stale); err == nil || !f.onRemote("worktree-pc-c-001") {
		t.Fatalf("lease と違う先端で消した: err = %v", err)
	}
	if err := deleteRemote(context.Background(), v); err != nil || f.onRemote("worktree-pc-c-001") {
		t.Fatalf("lease と同じ先端で消せない: err = %v", err)
	}
}

// push が成功しても remote にブランチが残っていれば (remote の hook が作り直した等)、消したと報告しない。
func TestDeleteRemoteVerifiesGone(t *testing.T) {
	f := newRemoteFixture(t)
	f.landRemote(f.branch("worktree-pc-c-001", "one.txt"), false)
	f.done("C-001")
	v := f.scanRemote()["worktree-pc-c-001"]
	hook := filepath.Join(f.bare, "hooks", "post-receive")
	write(t, hook, "#!/bin/sh\ngit update-ref refs/heads/worktree-pc-c-001 "+v.Head+"\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := deleteRemote(context.Background(), v); err == nil || !strings.Contains(err.Error(), "残っている") {
		t.Fatalf("remote に残ったのに err = %v", err)
	}
}

// pushurl が取得先と違えば、消えたかは送り先で確かめる (ls-remote origin は取得先を読み、消したのに「残っている」と読む)。
func TestDeleteRemoteVerifiesOnPushURL(t *testing.T) {
	f := newRemoteFixture(t)
	f.landRemote(f.branch("worktree-pc-c-001", "one.txt"), false)
	f.done("C-001")
	mirror := filepath.Join(filepath.Dir(f.bare), "mirror.git")
	git(t, f.repo, "clone", "-q", "--bare", f.bare, mirror)
	git(t, f.repo, "remote", "set-url", "--push", "origin", mirror)
	v := f.scanRemote()["worktree-pc-c-001"]
	if err := deleteRemote(context.Background(), v); err != nil {
		t.Fatalf("送り先から消えたのに err = %v", err)
	}
	if git(t, mirror, "for-each-ref", "refs/heads/worktree-pc-c-001") != "" {
		t.Error("送り先に残っている")
	}
}

// origin の無い repo (手元だけ) は remote のブランチが無いだけで、失敗にしない。
func TestScanRemoteSkipsRepoWithoutOrigin(t *testing.T) {
	f := newFixture(t, sandbox)
	vs, err := ScanRemote(context.Background(), f.in)
	if err != nil || len(vs) != 0 {
		t.Errorf("origin の無い repo = %v, %v", vs, err)
	}
}

// テストの二進では、origin (取得先・送り先のどちらか) が sandbox の外なら、読む fetch の前にも消す前にも拒否する。
func TestRemoteCleanRefusesOutsideSandbox(t *testing.T) {
	for _, push := range []bool{false, true} {
		f := newRemoteFixture(t)
		f.landRemote(f.branch("worktree-pc-c-001", "one.txt"), false)
		f.done("C-001")
		listed := f.scanRemote()
		outside := filepath.Join(t.TempDir(), "outside.git")
		git(t, f.repo, "clone", "-q", "--bare", f.bare, outside)
		if push {
			git(t, f.repo, "remote", "set-url", "--push", "origin", outside)
		} else {
			git(t, f.repo, "remote", "set-url", "origin", outside)
		}
		if _, err := ScanRemote(context.Background(), f.in); err == nil || !strings.Contains(err.Error(), "置き場") {
			t.Errorf("push=%v: sandbox の外の remote を読んだ: %v", push, err)
		}
		res := f.cleanRemote(listed)
		if r := res["worktree-pc-c-001"]; r.Outcome != Failed || !strings.Contains(r.Detail, "置き場") {
			t.Errorf("push=%v: sandbox の外の remote = %+v", push, r)
		}
		if git(t, outside, "for-each-ref", "refs/heads/worktree-pc-c-001") == "" {
			t.Errorf("push=%v: sandbox の外の remote のブランチを消した", push)
		}
	}
}
