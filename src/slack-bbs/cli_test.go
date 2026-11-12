package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"slack-bbs/cache"
	"slack-bbs/source"
	"slack-bbs/subs"
	"slack-bbs/view"
)

// cliDeps は前に取ったチャンネルの一覧 (参加 2・不参加 1) をキャッシュに置いた deps。
func cliDeps(t *testing.T) (deps, *fakeSlack, string) {
	sl := &fakeSlack{history: `[]`}
	d := newDeps(t, sl, &fakeClaude{})
	dir := t.TempDir()
	d.cacheDir = func() (string, error) { return dir, nil }
	list := []source.Channel{{ID: "C1", Name: "dev", IsMember: true, NumMembers: 42}, {ID: "C2", Name: "times-koji", IsMember: true, NumMembers: 3},
		{ID: "C3", Name: "random", NumMembers: 812}}
	if err := (cache.Store{Dir: dir}).SaveChannels(list, now); err != nil {
		t.Fatal(err)
	}
	return d, sl, dir
}

func runCLI(t *testing.T, d deps, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	rc := run(context.Background(), args, &out, &errb, d)
	return out.String(), errb.String(), rc
}

// 画面を開かずに購読を付け外しし、グループを作る・出し入れする・名前を変える・消す。チャンネルは ID・#名前・名前で指せる。
// 前に取ったチャンネルの一覧があれば Slack を呼ばない。
func TestCLISubscriptionsAndGroups(t *testing.T) {
	d, sl, _ := cliDeps(t)
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"sub", "add", "#dev", "C3"}, "購読を 2 件足した"},
		{[]string{"sub", "rm", "random"}, "購読を 1 件外した"},
		{[]string{"group", "create", "times", "times-koji", "#dev"}, "グループ「times」を作った (2 チャンネル)"},
		{[]string{"group", "rm", "times", "dev"}, "グループ「times」から 1 件外した"},
		{[]string{"group", "add", "times", "C3"}, "グループ「times」に 1 件入れた"},
		{[]string{"group", "rename", "times", "分報"}, "グループ「times」を「分報」にした"},
	}
	for _, s := range steps {
		out, errs, rc := runCLI(t, d, s.args...)
		if rc != 0 || !strings.Contains(out, s.want) {
			t.Fatalf("%v: rc=%d out=%q err=%q", s.args, rc, out, errs)
		}
	}
	path, _ := d.subsPath()
	cur, err := readSubs(t, path)
	if err != nil || len(cur) != 2 || cur[0].ID != "C1" || cur[1].Name != "分報" || len(cur[1].Members) != 2 || cur[1].Members[1].ID != "C3" {
		t.Fatalf("保存した購読 = %+v, %v", cur, err)
	}
	if out, _, rc := runCLI(t, d, "group", "list"); rc != 0 || !strings.Contains(out, "📂分報") || !strings.Contains(out, "#times-koji #random") {
		t.Errorf("group list: rc=%d %q", rc, out)
	}
	if out, _, rc := runCLI(t, d, "group", "delete", "分報"); rc != 0 || !strings.Contains(out, "消した") {
		t.Errorf("group delete: rc=%d %q", rc, out)
	}
	if len(sl.calls) != 0 {
		t.Errorf("前に取った一覧があるのに Slack を呼んだ: %v", sl.calls)
	}
	// 打ち間違い・無いグループ・購読していないものを外す は rc=1、使い方の誤りは rc=2。どれも購読を変えずに理由を出す
	for _, c := range []struct {
		args []string
		rc   int
	}{{[]string{"sub", "add", "#de"}, 1}, {[]string{"group", "add", "nope", "dev"}, 1}, {[]string{"sub", "rm", "times-koji"}, 1},
		{[]string{"group", "delete"}, 2}, {[]string{"sub", "add"}, 2}, {[]string{"group", "bogus"}, 2}} {
		before, _ := readSubs(t, path)
		_, errs, rc := runCLI(t, d, c.args...)
		after, _ := readSubs(t, path)
		if rc != c.rc || errs == "" || len(before) != len(after) {
			t.Errorf("%v: rc=%d (want %d) err=%q (購読 %d → %d)", c.args, rc, c.rc, errs, len(before), len(after))
		}
	}
	if _, errs, _ := runCLI(t, d, "sub", "add", "#de"); !strings.Contains(errs, "#dev") {
		t.Errorf("近い名前を添えていない: %q", errs)
	}
}

// channels は参加しているチャンネルを、購読しているか・人数・最終投稿 (前に取った値) と出す。-all で参加していないものも、
// -json で同じ中身を JSON で出す。
func TestCLIChannels(t *testing.T) {
	d, _, dir := cliDeps(t)
	if _, _, rc := runCLI(t, d, "sub", "add", "dev"); rc != 0 {
		t.Fatal(rc)
	}
	if err := (cache.Store{Dir: dir}).SaveLastPosts(map[string]subs.LastPost{"C1": {Ts: ts(now.Unix()), At: now}}); err != nil {
		t.Fatal(err)
	}
	out, _, rc := runCLI(t, d, "channels")
	if rc != 0 || !strings.Contains(out, "#dev") || strings.Contains(out, "#random") || !strings.Contains(out, now.Format("2006-01-02")) {
		t.Fatalf("channels: rc=%d\n%s", rc, out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "#dev") && !strings.HasPrefix(l, "x") {
			t.Errorf("購読している dev に印が無い: %q", l)
		}
	}
	out, _, _ = runCLI(t, d, "channels", "-all", "-json")
	var rows []struct {
		ID         string `json:"id"`
		Subscribed bool   `json:"subscribed"`
		Member     bool   `json:"member"`
		Members    int    `json:"members"`
		LastPost   string `json:"last_post"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 3 || !rows[0].Subscribed || rows[2].Member || rows[2].Members != 812 || rows[0].LastPost == "" {
		t.Errorf("channels -all -json = %+v, %v\n%s", rows, err, out)
	}
	if _, errs, rc := runCLI(t, d, "channels", "-x"); rc != 2 || errs == "" {
		t.Errorf("知らないオプション: rc=%d %q", rc, errs)
	}
}

// 購読の画面を開いている間に CLI で購読を変えたら、画面の保存は上書きせずに断る (CLI の変更を知らせずに消さない)。
// 何も変わらない操作はファイルを書かない。
func TestCLIAndPickerDoNotClobber(t *testing.T) {
	d, _, _ := cliDeps(t)
	path, _ := d.subsPath()
	var pickErr error
	d.pick = func(p *subs.Picker, _ ...tea.Cmd) error {
		if _, _, rc := runCLI(t, d, "sub", "add", "times-koji"); rc != 0 { // 画面を開いている間に別のペインで足した
			t.Fatal(rc)
		}
		p.Key("x")
		p.Key("enter")
		return nil
	}
	_, errs, rc := runCLI(t, d, "sub")
	pickErr = errors.New(errs)
	cur, _ := readSubs(t, path)
	if rc != 1 || !strings.Contains(pickErr.Error(), "書き換えられた") || len(cur) != 1 || cur[0].Name != "times-koji" {
		t.Errorf("画面の保存が CLI の変更を上書きした: rc=%d %q 購読=%+v", rc, errs, cur)
	}
	before, _ := os.Stat(path)
	if out, _, rc := runCLI(t, d, "sub", "add", "times-koji"); rc != 0 || !strings.Contains(out, "変えていない") {
		t.Errorf("もう購読しているものを足した: rc=%d %q", rc, out)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Error("何も変わらないのにファイルを書いた")
	}
}

// 前に取った一覧に無い名前は、1 回だけ一覧を取り直して引き直す (新しく参加したチャンネル)。同じ名前のグループが 2 つあると、
// 名前で指したら断り、ID なら指せる。
func TestCLIRefreshesStaleListAndAmbiguousGroups(t *testing.T) {
	d, sl, _ := cliDeps(t) // 偽の slack-cli の channels は #dev だけでなく、取り直すと新しい #newchan を返すようにする
	sl.channels = `[{"id":"C9","name":"newchan","is_member":true}]`
	if out, errs, rc := runCLI(t, d, "sub", "add", "newchan"); rc != 0 || !strings.Contains(out, "1 件足した") {
		t.Fatalf("古い一覧を取り直していない: rc=%d %q %q", rc, out, errs)
	}
	path, _ := d.subsPath()
	cur, _ := readSubs(t, path)
	g1 := subs.Channel{ID: "grp00000001", Name: "times", Members: []subs.Channel{{ID: "C1", Name: "dev"}}}
	g2 := subs.Channel{ID: "grp00000002", Name: "times"}
	if err := writeSubs(t, path, append(cur, g1, g2)); err != nil {
		t.Fatal(err)
	}
	if _, errs, rc := runCLI(t, d, "group", "delete", "times"); rc != 1 || !strings.Contains(errs, "2 つ以上") {
		t.Errorf("同じ名前のグループを名前で消した: rc=%d %q", rc, errs)
	}
	if _, _, rc := runCLI(t, d, "group", "delete", "grp00000002"); rc != 0 {
		t.Errorf("ID で指したグループを消せない: rc=%d", rc)
	}
}

// 読む画面を開いている間に CLI で購読を変えたら、読む画面の d (購読を外す) は上書きせずに断る。
func TestCLIAndUnsubscribeDoNotClobber(t *testing.T) {
	d, _, _ := cliDeps(t)
	d.tty = func() bool { return true }
	if _, _, rc := runCLI(t, d, "sub", "add", "dev"); rc != 0 {
		t.Fatal(rc)
	}
	path, _ := d.subsPath()
	var toast string
	d.browse = func(m *view.Model) error {
		if _, _, rc := runCLI(t, d, "sub", "add", "times-koji"); rc != 0 { // 画面を開いている間に別のペインで足した
			t.Fatal(rc)
		}
		m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
		m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		toast = m.LastToast()
		return nil
	}
	if _, errs, rc := runCLI(t, d); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errs)
	}
	cur, _ := readSubs(t, path)
	if len(cur) != 2 || !strings.Contains(toast, "外していない") {
		t.Errorf("d が CLI の変更を上書きした: 購読=%+v 知らせ=%q", cur, toast)
	}
}

// 一覧を取るのは 1 回だけ (1 回約 1 分): キャッシュが無いまま無い名前を足しても 2 回取らない。外す操作は Slack を呼ばない。
func TestCLIFetchesChannelListAtMostOnce(t *testing.T) {
	sl := &fakeSlack{history: `[]`}
	d := newDeps(t, sl, &fakeClaude{})
	dir := t.TempDir()
	d.cacheDir = func() (string, error) { return dir, nil }
	if _, _, rc := runCLI(t, d, "sub", "add", "typo-name"); rc != 1 {
		t.Fatalf("無い名前を足せた: rc=%d", rc)
	}
	n := 0
	for _, c := range sl.calls {
		if c[0] == "channels" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("一覧を %d 回取った (1 回まで)", n)
	}
	sl.calls = nil
	if _, _, rc := runCLI(t, d, "sub", "rm", "typo1", "typo2"); rc != 1 || len(sl.calls) != 0 {
		t.Errorf("外す操作で Slack を呼んだ: rc=%d calls=%v", rc, sl.calls)
	}
}

// アーカイブ済みのチャンネルは、購読にもグループにも新しく入れない (購読の画面と同じ。入れると読み直しのたびに「読めなかった」の注記が出続ける)。
// 外すのは購読の画面と同じく受ける。
func TestCLIRefusesToAddArchivedChannels(t *testing.T) {
	d, sl, dir := cliDeps(t)
	list := []source.Channel{{ID: "C1", Name: "dev", IsMember: true}, {ID: "C4", Name: "old-proj", IsArchived: true}}
	if err := (cache.Store{Dir: dir}).SaveChannels(list, now); err != nil {
		t.Fatal(err)
	}
	if _, _, rc := runCLI(t, d, "group", "create", "g", "dev"); rc != 0 {
		t.Fatalf("group create: rc=%d", rc)
	}
	path, _ := d.subsPath()
	before, _ := os.ReadFile(path)
	for _, args := range [][]string{{"sub", "add", "#old-proj"}, {"sub", "add", "C4"}, {"group", "add", "g", "old-proj"}, {"group", "create", "h", "old-proj"}} {
		_, errs, rc := runCLI(t, d, args...)
		if rc != 1 || !strings.Contains(errs, "アーカイブ") {
			t.Errorf("%v: rc=%d err=%q (アーカイブ済みだと断るはず)", args, rc, errs)
		}
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Errorf("断った操作が購読を書き換えた:\n%s", after)
	}
	if len(sl.calls) != 0 {
		t.Errorf("前に取った一覧で引けるのに Slack を呼んだ: %v", sl.calls)
	}
}

// writeSubs は今の購読に重ねて書く (テストの準備。production と同じ照合の道を通す)。
func writeSubs(t *testing.T, path string, chs []subs.Channel) error {
	t.Helper()
	_, base, err := subs.LoadVersion(path)
	if err != nil {
		return err
	}
	_, err = subs.SaveIf(path, chs, base)
	return err
}

func readSubs(t *testing.T, path string) ([]subs.Channel, error) {
	t.Helper()
	chs, _, err := subs.LoadVersion(path)
	return chs, err
}
