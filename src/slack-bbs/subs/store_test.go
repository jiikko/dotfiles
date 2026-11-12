package subs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// グループはチャンネルと分けて書き (古い slack-bbs がグループをチャンネルとして読まない)、読むとチャンネルの後ろに並ぶ。
// 読めない形のグループの ID は断る (ID はキャッシュのファイル名に使う)。
func TestSaveLoadGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subscriptions.json")
	g := Channel{ID: NewGroupID(), Name: "times", Members: []Channel{{ID: "C1", Name: "times_a"}, {ID: "C2", Name: "times_b"}}}
	if err := saveCurrent(path, []Channel{g, {ID: "C1", Name: "times_a"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"groups"`) {
		t.Fatalf("グループを groups に書いていない:\n%s", b)
	}
	got, err := loadOnly(path)
	if err != nil || len(got) != 2 || got[0].ID != "C1" || !got[1].IsGroup() || len(got[1].Members) != 2 {
		t.Fatalf("読み戻し: %+v %v", got, err)
	}
	if got[1].Label() != "📂times" || got[0].Label() != "#times_a" {
		t.Errorf("Label: %q %q", got[1].Label(), got[0].Label())
	}
	if err := os.WriteFile(path, []byte(`{"channels":[],"groups":[{"id":"../x","name":"bad"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOnly(path); err == nil {
		t.Error("読めない形のグループの ID を受け入れた")
	}
}

// グループのメンバーが重複している・チャンネルの ID の形でない購読の一覧は断る (同じ単位の ID が 2 つできて板ごと読めなくなる)。
func TestLoadRejectsBadMembers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subscriptions.json")
	for _, body := range []string{
		`{"channels":[],"groups":[{"id":"grp0123abcd","name":"t","members":[{"id":"C1"},{"id":"C1"}]}]}`,
		`{"channels":[],"groups":[{"id":"grp0123abcd","name":"t","members":[{"id":"grp0123abcd"}]}]}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadOnly(path); err == nil {
			t.Errorf("断らなかった: %s", body)
		}
	}
}

// saveCurrent は今のファイルの中身に重ねて書く (テストの準備。production と同じ SaveIf の道を通す)。
func saveCurrent(path string, chs []Channel) error {
	base := ""
	if b, err := os.ReadFile(path); err == nil {
		base = version(b)
	}
	_, err := SaveIf(path, chs, base)
	return err
}

func loadOnly(path string) ([]Channel, error) {
	chs, _, err := LoadVersion(path)
	return chs, err
}
