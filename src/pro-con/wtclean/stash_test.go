package wtclean

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// issue 552: disposable_tmp の repo では、worktree の直下の tmp/ の無視されたファイルだけなら消してよい側にする。
// tmp/ の外の無視されたファイル・入れ子の tmp/・追跡していない tmp/ のファイルは今までどおり残す。
func TestJudgeDisposableTmp(t *testing.T) {
	f := newFixture(t, sandbox)
	f.in.DisposableTmp = map[string]bool{"r": true}
	write(t, filepath.Join(f.repo, ".git", "info", "exclude"), ".env\n")
	only := f.add("C-001")
	write(t, filepath.Join(only, "tmp", "mut-456.log"), "x\n")
	write(t, filepath.Join(only, "tmp", "493", "a.ans"), "x\n")
	env := f.add("C-002")
	write(t, filepath.Join(env, "tmp", "a.log"), "x\n")
	write(t, filepath.Join(env, ".env"), "SECRET=1\n")
	nested := f.add("C-003") // .gitignore の tmp/ はどの深さにも効くが、使い捨てとみなすのは直下の tmp/ だけ
	write(t, filepath.Join(nested, "src", "tmp", "data.json"), "x\n")
	unlanded := f.add("C-004")
	f.commit(unlanded, "only-here.txt")
	write(t, filepath.Join(unlanded, "tmp", "a.log"), "x\n")

	got := f.scan()
	if v := got["pc-c-001"]; v.Action != RemoveAll || !strings.Contains(v.Why, "tmp/ の無視されたファイル 2 件は wtclean-tmp/") ||
		!slices.Equal(v.Tmp, []string{"tmp/493/a.ans", "tmp/mut-456.log"}) {
		t.Errorf("tmp/ だけの pc-c-001 = %s / %q / %q", v.Action, v.Why, v.Tmp)
	}
	for name, why := range map[string]string{"pc-c-002": "(.env)", "pc-c-003": "(src/tmp/data.json)"} {
		if v := got[name]; v.Action != Keep || !strings.Contains(v.Why, why) || len(v.Tmp) > 0 {
			t.Errorf("%s = %s / %q / %q, want 消さない / %q", name, v.Action, v.Why, v.Tmp, why)
		}
	}
	if v := got["pc-c-004"]; v.Action != Keep || !v.Ask || !strings.Contains(v.Why, "無い commit") || !strings.Contains(v.Why, "1 件は wtclean-tmp/") {
		t.Errorf("取り込んでいない pc-c-004 = %s / ask=%v / %q", v.Action, v.Ask, v.Why)
	}

	f.in.DisposableTmp = nil // 設定に無い repo の tmp/ は今までどおり残す
	if v := f.scan()["pc-c-001"]; v.Action != Keep || !strings.Contains(v.Why, "無視されたファイルがある (tmp/493/a.ans") {
		t.Errorf("disposable でない repo の pc-c-001 = %s / %q", v.Action, v.Why)
	}
}

// 消すときは tmp/ を状態の置き場の wtclean-tmp/<repo>/<名前>/ へ退避してから消し、結果に退避した先を出す。
// 同じ名前の退避が既にあれば <名前>.2 へ置く (前の退避を上書きしない)。
func TestCleanStashesTmp(t *testing.T) {
	f := newFixture(t, sandbox)
	f.in.DisposableTmp = map[string]bool{"r": true}
	wt := f.add("C-001")
	write(t, filepath.Join(wt, "tmp", "493", "a.ans"), "sample\n")
	state := filepath.Join(filepath.Dir(f.repo), "state-"+filepath.Base(f.repo))
	old := filepath.Join(state, TmpStashDir, "r", "pc-c-001", "tmp", "keep.txt")
	write(t, old, "earlier\n")

	res := f.clean(f.scan(), Options{StateDir: state})
	dest := filepath.Join(state, TmpStashDir, "r", "pc-c-001.2")
	if r := res["pc-c-001"]; r.Outcome != Removed || !strings.Contains(r.Detail, "tmp/ の 1 件を "+dest+" へ退避した (30 日後") {
		t.Errorf("= %s (%s)", r.Outcome, r.Detail)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "tmp", "493", "a.ans")); err != nil || string(b) != "sample\n" {
		t.Errorf("退避に中身が無い: %q %v", b, err)
	}
	if b, err := os.ReadFile(old); err != nil || string(b) != "earlier\n" {
		t.Errorf("前の退避を壊した: %q %v", b, err)
	}
	if exists(wt) || f.hasBranch(BranchName("pc-c-001")) {
		t.Error("worktree かブランチが残っている")
	}
}

// 状態の置き場が分からなければ退避できないので消さない (tmp/ を黙って失わない)。
func TestCleanKeepsTmpWithoutStateDir(t *testing.T) {
	f := newFixture(t, sandbox)
	f.in.DisposableTmp = map[string]bool{"r": true}
	wt := f.add("C-001")
	write(t, filepath.Join(wt, "tmp", "a.log"), "x\n")
	if r := f.clean(f.scan(), Options{})["pc-c-001"]; r.Outcome != Failed || !strings.Contains(r.Detail, "退避できない") ||
		!exists(filepath.Join(wt, "tmp", "a.log")) {
		t.Errorf("= %s (%s)", r.Outcome, r.Detail)
	}
}

// 退避の後に worktree を消せなければ (git が断る・その間に無視されたファイルができた)、退避したファイルを元へ戻し、空の退避は消す。
func TestRemoveTreeRestoresTmpOnFailure(t *testing.T) {
	for name, late := range map[string]string{
		"git が断る (追跡していないファイル)": "late.txt",
		"退避の間に tmp/ にファイルができた":  "tmp/late.log",
		"退避の間に .env ができた":       ".env",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, sandbox)
			write(t, filepath.Join(f.repo, ".git", "info", "exclude"), ".env\n")
			f.in.DisposableTmp = map[string]bool{"r": true}
			wt := f.add("C-001")
			write(t, filepath.Join(wt, "tmp", "a.log"), "x\n")
			v := f.scan()["pc-c-001"]
			write(t, filepath.Join(wt, late), "x\n") // 判定の後にできた
			state := filepath.Join(filepath.Dir(f.repo), "state-"+filepath.Base(f.repo))
			if dest, err := removeTree(context.Background(), v, state); err == nil {
				t.Fatalf("消した (退避 %s)", dest)
			}
			if !exists(wt) || !exists(filepath.Join(wt, "tmp", "a.log")) || !exists(filepath.Join(wt, late)) {
				t.Error("worktree か tmp/ のファイルが元に無い")
			}
			if exists(filepath.Join(state, TmpStashDir, "r", "pc-c-001")) {
				t.Error("空の退避が残っている")
			}
		})
	}
}

// 戻すとき、退避の間に元の場所へ同じ名前でできたファイルは上書きしない (退避した方を残して失敗を返す。どちらも失わない)。
func TestStashUndoKeepsNewerFile(t *testing.T) {
	f := newFixture(t, sandbox)
	f.in.DisposableTmp = map[string]bool{"r": true}
	wt := f.add("C-001")
	write(t, filepath.Join(wt, "tmp", "a.log"), "old\n")
	state := filepath.Join(filepath.Dir(f.repo), "state-"+filepath.Base(f.repo))
	dest, undo, err := stashTmp(f.scan()["pc-c-001"], state)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, "tmp", "a.log"), "new\n")
	if err := undo(); err == nil {
		t.Error("上書きせずに戻せないのに失敗を返さない")
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "tmp", "a.log")); string(b) != "new\n" {
		t.Errorf("新しいファイルを上書きした: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "tmp", "a.log")); string(b) != "old\n" {
		t.Errorf("退避した方を失った: %q", b)
	}
}

// 人が「消す」と決めた (Discard) ものも、tmp/ を退避してから消し、結果に退避した先を出す (553 の画面に出る)。
func TestDiscardStashesTmp(t *testing.T) {
	f := newFixture(t, sandbox)
	f.in.DisposableTmp = map[string]bool{"r": true}
	wt := f.add("C-001")
	f.commit(wt, "only-here.txt")
	write(t, filepath.Join(wt, "tmp", "a.log"), "x\n")
	state := filepath.Join(filepath.Dir(f.repo), "state-"+filepath.Base(f.repo))
	r := Discard(context.Background(), f.scan()["pc-c-001"], Options{StateDir: state, Fresh: func(context.Context) (Inputs, error) { return f.in, nil }})
	dest := filepath.Join(state, TmpStashDir, "r", "pc-c-001")
	if r.Outcome != Removed || !strings.Contains(r.Detail, dest) || !exists(filepath.Join(dest, "tmp", "a.log")) {
		t.Errorf("= %s (%s)", r.Outcome, r.Detail)
	}
}

// 30 日を過ぎた退避だけを消し、空になった repo のディレクトリも消す。
func TestPruneTmpStash(t *testing.T) {
	state, err := os.MkdirTemp(sandbox, "state")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, c := range []struct {
		path string
		age  time.Duration
	}{
		{"a/pc-c-001", TmpKeep + time.Hour},
		{"a/pc-c-002", TmpKeep - time.Hour},
		{"b/pc-c-003", TmpKeep + time.Hour},
	} {
		p := filepath.Join(state, TmpStashDir, c.path)
		write(t, filepath.Join(p, "tmp", "x.log"), "x\n")
		if err := os.Chtimes(p, now.Add(-c.age), now.Add(-c.age)); err != nil {
			t.Fatal(err)
		}
	}
	gone, err := PruneTmpStash(state, now)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(state, TmpStashDir)
	if want := []string{filepath.Join(root, "a", "pc-c-001"), filepath.Join(root, "b", "pc-c-003")}; !slices.Equal(gone, want) {
		t.Errorf("消した = %q, want %q", gone, want)
	}
	if !exists(filepath.Join(root, "a", "pc-c-002", "tmp", "x.log")) || exists(filepath.Join(root, "a", "pc-c-001")) || exists(filepath.Join(root, "b")) {
		t.Error("30 日に満たない退避を消したか、過ぎたもの・空の repo のディレクトリが残っている")
	}
	if _, err := PruneTmpStash(os.TempDir(), now); err == nil {
		t.Error("テストの二進で sandbox の外を消そうとして拒否しなかった")
	}
}
