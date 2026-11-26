package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"slack-bbs/cache"
	"slack-bbs/source"
	"slack-bbs/subs"
)

func threadCalls(sl *fakeSlack) int {
	n := 0
	for _, c := range sl.calls {
		if c[0] == "thread" {
			n++
		}
	}
	return n
}

// reuseFixture はスレッド 1 本 (期間より前に始まり、期間内に返信が付いた) のチャンネルと、使い捨てのキャッシュの置き場を用意する。
// set(n, last) は親の返信の数を n、スレッドの最後の返信の本文を last にする。
func reuseFixture(t *testing.T) (sl *fakeSlack, d deps, dir string, set func(n int, last string)) {
	t.Helper()
	since := now.Add(-48 * time.Hour).Unix()
	parent := since - 3600
	sl = &fakeSlack{threads: map[string]string{}}
	set = func(n int, last string) {
		latest := ts(since + int64(n))
		sl.history = fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"CI が遅い","reply_count":%d,"latest_reply":%q}]`,
			ts(parent), ts(parent), n, latest)
		msgs := fmt.Sprintf(`{"ts":%q,"thread_ts":%q,"user":"U1","text":"CI が遅い"}`, ts(parent), ts(parent))
		for i := 1; i <= n; i++ {
			text := "返信"
			if i == n {
				text = last
			}
			msgs += fmt.Sprintf(`,{"ts":%q,"thread_ts":%q,"user":"U1","text":%q}`, ts(since+int64(i)), ts(parent), text)
		}
		sl.threads[ts(parent)] = "[" + msgs + "]"
	}
	set(1, "キャッシュを見ます")
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[{"title":"CI","state":"info","theme":"other","members":[%q],"background":[],"labels":[]}]}`, ts(parent)))}
	d = newDeps(t, sl, cl)
	dir = filepath.Join(t.TempDir(), "cache")
	d.cacheDir = func() (string, error) { return dir, nil }
	return sl, d, dir, set
}

// 読み直しは、返信の数と最後の返信が前回と同じスレッドを slack-cli で読み直さず、前回の返信で板を組む。どちらかが変わったら読み直す。
// -no-cache は前回のスレッドを使わない (読んだものは残す)。-dry-run は使わず、残しもしない (全部を読んで前回の板と比べるのが役目)。
func TestReadReusesUnchangedThreads(t *testing.T) {
	sl, d, dir, set := reuseFixture(t)
	md := func(args ...string) string {
		t.Helper()
		out, _ := readMD(t, d, args...)
		return out
	}
	md("C1")
	if n := threadCalls(sl); n != 1 {
		t.Fatalf("1 回目: スレッドを %d 回読んだ (want 1)", n)
	}
	for i := 2; i <= 3; i++ { // 使い回したスレッドも残す (残さないと、3 回目は読み直す)
		if out := md("C1"); threadCalls(sl) != 1 || !strings.Contains(out, "キャッシュを見ます") {
			t.Errorf("%d 回目: 変わっていないスレッドを読み直した・前回の返信で板を組んでいない (スレッド %d 回):\n%s", i, threadCalls(sl), out)
		}
	}

	set(2, "直りました")
	if out := md("C1"); threadCalls(sl) != 2 || !strings.Contains(out, "直りました") {
		t.Errorf("返信が付いたスレッドを読み直していない (スレッド %d 回):\n%s", threadCalls(sl), out)
	}

	md("-no-cache", "C1")
	if n := threadCalls(sl); n != 3 {
		t.Errorf("-no-cache で前回のスレッドを使った (スレッド %d 回, want 3)", n)
	}

	p := filepath.Join(dir, "C1.threads.json")
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	set(3, "まだ遅い")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-dry-run", "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if n := threadCalls(sl); n != 4 {
		t.Errorf("-dry-run が前回のスレッドを使った (スレッド %d 回, want 4)", n)
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Errorf("-dry-run が読んだスレッドを残した")
	}
}

// 返信の数が同じでも最後の返信が違えば (1 つ消えて 1 つ付いた) 読み直す。最後の返信が同じでも返信の数が違えば (途中の返信が消えた)
// 読み直す。前回の返信が上限で切れていたら、変わっていなくても読み直す。
func TestReadRereadsThreadsWhenLatestDiffersOrTruncated(t *testing.T) {
	sl, d, dir, _ := reuseFixture(t)
	readMD(t, d, "C1")
	since := now.Add(-48 * time.Hour).Unix()
	parent := ts(since - 3600)
	// 返信が 1 つ消えて 1 つ付いた: 数は 1 のまま、最後の返信が since+9 になる (スレッドの中身も同じ形にする)
	sl.history = strings.Replace(sl.history, fmt.Sprintf(`"latest_reply":%q`, ts(since+1)), fmt.Sprintf(`"latest_reply":%q`, ts(since+9)), 1)
	sl.threads[parent] = fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"CI が遅い"},{"ts":%q,"thread_ts":%q,"user":"U1","text":"付け直した"}]`,
		parent, parent, ts(since+9), parent)
	readMD(t, d, "C1")
	if n := threadCalls(sl); n != 2 {
		t.Errorf("最後の返信が変わったスレッドを読み直していない (スレッド %d 回, want 2)", n)
	}
	// 途中に返信が 1 つ増えた: 最後の返信は同じまま、数だけ 2 になる
	sl.history = strings.Replace(sl.history, `"reply_count":1`, `"reply_count":2`, 1)
	sl.threads[parent] = fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"CI が遅い"},{"ts":%q,"thread_ts":%q,"user":"U1","text":"途中"},{"ts":%q,"thread_ts":%q,"user":"U1","text":"付け直した"}]`,
		parent, parent, ts(since+5), parent, ts(since+9), parent)
	readMD(t, d, "C1")
	if n := threadCalls(sl); n != 3 {
		t.Errorf("返信の数が変わったスレッドを読み直していない (スレッド %d 回, want 3)", n)
	}

	st := cache.Store{Dir: dir}
	th, ok, err := st.LoadThreads("C1")
	if err != nil || !ok || len(th.Items) != 1 {
		t.Fatalf("読んだスレッドを残していない: ok=%v err=%v %+v", ok, err, th)
	}
	full := make([]source.Message, source.ThreadLimit)
	for i := range full {
		full[i] = th.Items[0].Msgs[0]
	}
	th.Items[0].Msgs = full
	if err := st.SaveThreads("C1", th); err != nil {
		t.Fatal(err)
	}
	readMD(t, d, "C1")
	if n := threadCalls(sl); n != 4 {
		t.Errorf("上限で切れていた前回の返信を使った (スレッド %d 回, want 4)", n)
	}
}

// 読む画面の読み直しは、全部を取り直す (R) と -no-cache のときだけ前回のスレッドを使わない。どちらでも読んだものは残す。
func TestBrowseThreadsReuseOnlyWhenNotFull(t *testing.T) {
	st := cache.Store{Dir: t.TempDir()}
	for _, c := range []struct {
		full, noCache, read bool
	}{
		{false, false, true},
		{true, false, false},
		{false, true, false},
	} {
		got := browseThreads(options{noCache: c.noCache}, st, c.full)
		if got.read != c.read || !got.write {
			t.Errorf("full=%v -no-cache=%v: read=%v write=%v (want read=%v write=true)", c.full, c.noCache, got.read, got.write, c.read)
		}
	}
}

// グループの板では、前回のスレッドをメンバーのチャンネルと親の ts で引く (2 つのメンバーで同じ ts のスレッドを取り違えない)。
func TestGroupReusesThreadsByMemberChannel(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	at := ts(since + 10)
	hist := func(latest int64) string {
		return fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"親","reply_count":1,"latest_reply":%q}]`, at, at, ts(latest))
	}
	thread := func(latest int64, reply string) map[string]string {
		return map[string]string{at: fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"親"},{"ts":%q,"thread_ts":%q,"user":"U1","text":%q}]`,
			at, at, ts(latest), at, reply)}
	}
	sl := &fakeSlack{
		histByCh:    map[string]string{"C1": hist(since + 20), "C2": hist(since + 20)},
		threadsByCh: map[string]map[string]string{"C1": thread(since+20, "dev の返信"), "C2": thread(since+20, "ops の返信")},
	}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[{"title":"話題","state":"info","theme":"other","members":["C1/%s","C2/%s"],"background":[],"labels":[]}]}`, at, at))}
	d := newDeps(t, sl, cl)
	dir := filepath.Join(t.TempDir(), "cache")
	d.cacheDir = func() (string, error) { return dir, nil }
	path, _ := d.subsPath()
	g := subs.Channel{ID: "grp0123abcd", Name: "times", Members: []subs.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}}}
	if err := writeSubs(t, path, []subs.Channel{g}); err != nil {
		t.Fatal(err)
	}
	readMD(t, d)
	if out, _ := readMD(t, d); threadCalls(sl) != 2 || !strings.Contains(out, "dev の返信") || !strings.Contains(out, "ops の返信") {
		t.Errorf("グループの変わっていないスレッドを読み直した・取り違えた (スレッド %d 回, want 2):\n%s", threadCalls(sl), out)
	}
	sl.histByCh["C2"], sl.threadsByCh["C2"] = hist(since+30), thread(since+30, "ops の新しい返信")
	if out, _ := readMD(t, d); threadCalls(sl) != 3 || !strings.Contains(out, "dev の返信") || !strings.Contains(out, "ops の新しい返信") {
		t.Errorf("変わった C2 だけを読み直していない (スレッド %d 回, want 3):\n%s", threadCalls(sl), out)
	}
}

// 使い回すスレッドでも、親の編集は今回の history の親で拾う (返信の数と最後の返信は変わらないが、新しい本文は history に届いている)。
func TestReusedThreadTakesParentFromHistory(t *testing.T) {
	sl, d, _, _ := reuseFixture(t)
	readMD(t, d, "C1")
	sl.history = strings.Replace(sl.history, `"text":"CI が遅い"`, `"text":"CI が直った"`, 1)
	if out, _ := readMD(t, d, "C1"); threadCalls(sl) != 1 || !strings.Contains(out, "CI が直った") {
		t.Errorf("使い回したスレッドの親を history の新しい本文にしていない (スレッド %d 回):\n%s", threadCalls(sl), out)
	}
}

// 「チャンネルにも投稿」の返信の編集も、history に届いた新しい本文で拾う。
func TestReusedThreadTakesBroadcastReplyFromHistory(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	parent, reply := ts(since-3600), ts(since+5)
	hist := func(text string) string {
		return fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":%q},{"ts":%q,"thread_ts":%q,"user":"U1","text":"親","reply_count":1,"latest_reply":%q}]`,
			reply, parent, text, parent, parent, reply)
	}
	sl := &fakeSlack{history: hist("投稿 v1"), threads: map[string]string{parent: fmt.Sprintf(
		`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"親"},{"ts":%q,"thread_ts":%q,"user":"U1","text":"投稿 v1"}]`, parent, parent, reply, parent)}}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[{"title":"話題","state":"info","theme":"other","members":[%q],"background":[],"labels":[]}]}`, parent))}
	d := newDeps(t, sl, cl)
	dir := filepath.Join(t.TempDir(), "cache")
	d.cacheDir = func() (string, error) { return dir, nil }
	readMD(t, d, "C1")
	sl.history = hist("投稿 v2")
	if out, _ := readMD(t, d, "C1"); threadCalls(sl) != 1 || !strings.Contains(out, "投稿 v2") || strings.Contains(out, "投稿 v1") {
		t.Errorf("使い回したスレッドの「チャンネルにも投稿」の返信を history の新しい本文にしていない (スレッド %d 回):\n%s", threadCalls(sl), out)
	}
}

// 読んだスレッドが history の親の値と合わない (history の後に返信が動いた・途中で止まった) か、slack-cli が警告を出したら残さない。
// 残すと、次の読み直しで欠けた・消えた返信を使い回し続ける。
func TestInconsistentOrWarnedThreadIsNotKept(t *testing.T) {
	sl, d, _, _ := reuseFixture(t)
	parent := ts(now.Add(-48*time.Hour).Unix() - 3600)
	good := sl.threads[parent]
	sl.threads[parent] = fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"CI が遅い"}]`, parent, parent) // 返信が欠けた
	readMD(t, d, "C1")
	sl.threads[parent] = good
	readMD(t, d, "C1")
	if n := threadCalls(sl); n != 2 {
		t.Errorf("親の値と合わないスレッドを残して使い回した (スレッド %d 回, want 2)", n)
	}
	// 件数は合うが最後の返信が違う (history を読んだ後に返信が 1 つ消えて 1 つ付いた)
	sl.threads[parent] = fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"CI が遅い"},{"ts":%q,"thread_ts":%q,"user":"U1","text":"別の返信"}]`,
		parent, parent, ts(now.Add(-48*time.Hour).Unix()+7), parent)
	readMD(t, d, "-no-cache", "C1")
	sl.threads[parent] = good
	readMD(t, d, "C1")
	if n := threadCalls(sl); n != 4 {
		t.Errorf("最後の返信が親の値と合わないスレッドを残して使い回した (スレッド %d 回, want 4)", n)
	}

	sl.thWarn = "警告: 取得が途中で切れた\n  結果は不完全です。\n"
	readMD(t, d, "-no-cache", "C1") // 警告つきで読み直し、残っていたものを置き換える
	sl.thWarn = ""
	readMD(t, d, "C1")
	if n := threadCalls(sl); n != 6 {
		t.Errorf("警告の付いたスレッドを残して使い回した (スレッド %d 回, want 6)", n)
	}
}

// 読む画面の読み直しは、full (R) のときだけ前回のスレッドを使わない。
func TestBrowseLoaderRereadsAllOnlyWhenFull(t *testing.T) {
	sl, d, dir, _ := reuseFixture(t)
	cli := source.Client{R: sl}
	identify := func(ctx context.Context) (source.Me, source.Users, error) {
		me, err := cli.Whoami(ctx)
		if err != nil {
			return source.Me{}, source.Users{}, err
		}
		us, err := cli.Users(ctx)
		return me, us, err
	}
	o, err := parseFlags(nil, now, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	load := browseLoader(o, d, cache.Store{Dir: dir}, cli, identify, func() (func(), bool) { return func() {}, true })
	ch := subs.Channel{ID: "C1", Name: "dev"}
	for i, c := range []struct {
		full bool
		want int
	}{{false, 1}, {false, 1}, {true, 2}, {false, 2}} {
		if _, err := load(context.Background(), ch, c.full); err != nil {
			t.Fatal(err)
		}
		if n := threadCalls(sl); n != c.want {
			t.Errorf("%d 回目 (full=%v): スレッド %d 回, want %d", i+1, c.full, n, c.want)
		}
	}
}
