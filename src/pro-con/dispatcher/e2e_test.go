package dispatcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pro-con/card"
	"pro-con/store"
)

// 偽の PG の台本: 起動で質問 / 回答で再開したらテストの係に頼む / テストの係の結果で再開したらレビューへ。
// 再開は別の session id の session を立て、前の session は一覧から消える (本物の claude の形)。
func TestE2EFakePGScript(t *testing.T) {
	e := E2E{Root: t.TempDir()}
	l := e.Launcher()
	id1, err := l.Start(context.Background(), e.RepoDir(), "pc-c-001", "")
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(e.RepoDir(), ".claude", "worktrees", "pc-c-001")
	if st, err := os.Stat(wt); err != nil || !st.IsDir() {
		t.Fatalf("worktree を作らない: %v", err)
	}
	id2, err := l.Resume(context.Background(), id1, "", wt, "", "続けてください")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Resume(context.Background(), id2, "", wt, "", "テストの係の結果: `echo e2e-ok` rc=0"); err != nil {
		t.Fatal(err)
	}
	ss, _ := e.List(context.Background())
	if len(ss) != 1 || ss[0].ID == id1 || ss[0].ID == id2 || ss[0].Cwd != wt || ss[0].Kind != "background" {
		t.Fatalf("再開で前の session を消して新しい session を立てていない: %+v", ss)
	}
	var kinds []string
	names, _ := filepath.Glob(filepath.Join(e.StateDir(), store.InboxDir, "*.json"))
	for _, n := range names {
		data, _ := os.ReadFile(n)
		for _, k := range []string{`"ask"`, `"run"`, `"review"`} {
			if strings.Contains(string(data), `"kind": `+k) || strings.Contains(string(data), `"kind":`+k) {
				kinds = append(kinds, strings.Trim(k, `"`))
			}
		}
		if strings.Contains(string(data), `"run"`) && !strings.Contains(string(data), wt) {
			t.Fatalf("テストの係への頼みに worktree の cwd が無い: %s", data)
		}
	}
	if strings.Join(kinds, ",") != "ask,run,review" {
		t.Fatalf("台本どおりに箱へ置いていない: %v", kinds)
	}
}

// 偽の PM は依頼の列のカードを着手待ちにする (e2e の repo の issue で)。
func TestE2EFakePM(t *testing.T) {
	e := E2E{Root: t.TempDir()}
	if _, err := store.Submit(e.StateDir(), store.Request{Kind: "add", Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(e.StateDir(), t0, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.FakePM(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(e.StateDir(), t0, nil); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load(e.StateDir())
	if c := st.Cards[0]; c.State != card.Planned || c.Repo != E2ERepo {
		t.Fatalf("偽の PM が着手待ちにしない / repo が付かない: %v %q", c.State, c.Repo)
	}
}
