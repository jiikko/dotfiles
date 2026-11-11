package subs

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"slack-bbs/source"
)

var opsAll = []source.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "dev-ops"}, {ID: "C3", Name: "times-koji"}, {ID: "C4", Name: "2024"}}

// チャンネルは ID・#名前・名前で引ける。名前を先に引く (数字だけの名前を ID と取り違えない)。名前は完全一致で、無ければ近い名前を
// 添えて断る。一覧がある間は、一覧に無い ID も断る (打ち間違えた ID を購読に入れない)。一覧が空なら ID の形はそのまま受ける。
func TestResolve(t *testing.T) {
	for arg, want := range map[string]string{"C1": "dev", "#dev": "dev", "dev-ops": "dev-ops", "2024": "2024", "#2024": "2024"} {
		c, err := Resolve(opsAll, arg)
		if err != nil || c.Name != want {
			t.Errorf("Resolve(%q) = %+v, %v; want 名前 %s", arg, c, err, want)
		}
	}
	if _, err := Resolve(opsAll, "#de"); err == nil || !strings.Contains(err.Error(), "#dev #dev-ops") {
		t.Errorf("無い名前: %v (近い名前を添える)", err)
	}
	if _, err := Resolve(opsAll, "xyz"); !errors.Is(err, ErrNoChannel) {
		t.Errorf("無い名前: %v", err)
	}
	if _, err := Resolve(opsAll, "C0TYPO123"); !errors.Is(err, ErrNoChannel) {
		t.Errorf("一覧に無い ID を受けた: %v", err)
	}
	if c, err := Resolve(nil, "C0123"); err != nil || c.ID != "C0123" {
		t.Errorf("一覧が無いときの ID: %+v, %v", c, err)
	}
}

// 購読の付け外しとグループの操作は、新しい一覧を返して渡した一覧を書き換えない。並びは チャンネル → グループ のまま、
// 同じものは二重に入れない。保存した一覧は Load で読める (画面の保存と同じ検査を通る)。
func TestSubscriptionOps(t *testing.T) {
	cur := []Channel{{ID: "C1", Name: "dev"}, {ID: "grp00000001", Name: "g", Members: []Channel{{ID: "C1", Name: "dev"}}}}
	orig := slices.Clone(cur)
	got, n := AddChannels(cur, Channel{ID: "C2", Name: "dev-ops"}, Channel{ID: "C1", Name: "dev"})
	if n != 1 || len(got) != 3 || got[1].ID != "C2" || !got[2].IsGroup() {
		t.Fatalf("AddChannels = %+v, %d", got, n)
	}
	got, n = RemoveChannels(got, "C1")
	if n != 1 || got[0].ID != "C2" || len(got[1].Members) != 1 {
		t.Fatalf("RemoveChannels = %+v, %d (グループのメンバーは変えない)", got, n)
	}
	got, err := CreateGroup(got, "times", Channel{ID: "C3", Name: "times-koji"}, Channel{ID: "C3", Name: "times-koji"})
	if err != nil || len(got) != 3 || got[2].Name != "times" || len(got[2].Members) != 1 || !got[2].IsGroup() {
		t.Fatalf("CreateGroup = %+v, %v", got, err)
	}
	if _, err := CreateGroup(got, "times"); err == nil {
		t.Error("同じ名前のグループを作れた")
	}
	got, n, err = GroupAdd(got, "g", Channel{ID: "C2", Name: "dev-ops"}, Channel{ID: "C1", Name: "dev"})
	if err != nil || n != 1 || len(got[1].Members) != 2 {
		t.Fatalf("GroupAdd = %+v, %d, %v", got, n, err)
	}
	got, n, err = GroupRemove(got, "g", "C1")
	if err != nil || n != 1 || got[1].Members[0].ID != "C2" {
		t.Fatalf("GroupRemove = %+v, %d, %v", got, n, err)
	}
	if _, _, err := GroupAdd(got, "nope"); err == nil || !strings.Contains(err.Error(), "あるのは: g times") {
		t.Errorf("無いグループ: %v", err)
	}
	got, err = RenameGroup(got, "g", "team")
	if err != nil || got[1].Name != "team" || got[1].ID != "grp00000001" {
		t.Fatalf("RenameGroup = %+v, %v (ID は変えない)", got, err)
	}
	got, err = DeleteGroup(got, "times")
	if err != nil || len(got) != 2 {
		t.Fatalf("DeleteGroup = %+v, %v", got, err)
	}
	if _, _, err := GroupRemove(cur, "g", "C1"); err != nil || cur[1].Members[0].ID != "C1" { // 渡した一覧のグループから直接外す
		t.Errorf("GroupRemove が渡した一覧のメンバーの配列を書き換えた: %+v, %v", cur[1].Members, err)
	}
	if !slices.EqualFunc(cur, orig, func(a, b Channel) bool { return a.ID == b.ID && len(a.Members) == len(b.Members) }) || cur[1].Members[0].ID != "C1" {
		t.Errorf("渡した一覧を書き換えた: %+v", cur)
	}
	path := filepath.Join(t.TempDir(), "subscriptions.json")
	if err := Save(path, got); err != nil {
		t.Fatal(err)
	}
	if back, err := Load(path); err != nil || len(back) != 2 || back[1].Name != "team" {
		t.Errorf("保存した一覧を読めない: %+v, %v", back, err)
	}
}

// 保存の前にも読み込みと同じ形の検査をかける (次の起動で読めないファイルを書かない)。
func TestSaveRejectsWhatLoadRejects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subscriptions.json")
	for _, chs := range [][]Channel{{{ID: "bad/x", Name: "x"}}, {{ID: "grp00000001", Name: "g", Members: []Channel{{ID: "C1"}, {ID: "C1"}}}}} {
		if err := Save(path, chs); err == nil {
			t.Errorf("読めない形を保存した: %+v", chs)
		}
	}
}
