package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"slack-bbs/subs"
)

// -dry-run は、読み直したスレッドのうち前回の板と返信の数・最後の返信が同じものを数える。返信の数は、前回の板が期間で省いた
// 返信 (board.OldContext を超えた期間前の返信) も含めて比べる。返信が増えたスレッドは同じと数えない。同じと数えたうち、
// 両方に在る返信の本文が編集されたものは別に数える。前回の板が無い板は、その数を添える。
func TestDryRunCountsThreadsUnchangedSinceCachedBoard(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	old := since - 3600 // 期間より前に始まり、期間前の返信が OldContext を超えるスレッド
	fresh := since + 10
	oldThread := func(lastOld string) string {
		s := fmt.Sprintf(`{"ts":%q,"thread_ts":%q,"user":"U1","text":"古い親"}`, ts(old), ts(old))
		for i := int64(1); i <= 7; i++ {
			text := "前の返信"
			if i == 7 {
				text = lastOld
			}
			s += fmt.Sprintf(`,{"ts":%q,"thread_ts":%q,"user":"U1","text":%q}`, ts(old+i), ts(old), text)
		}
		return "[" + s + fmt.Sprintf(`,{"ts":%q,"thread_ts":%q,"user":"U1","text":"新しい返信"}]`, ts(since+50), ts(old))
	}
	history := func(freshReplies int, freshLatest int64) string {
		return fmt.Sprintf(`[
			{"ts":%q,"thread_ts":%q,"user":"U1","text":"新しい親","reply_count":%d,"latest_reply":%q},
			{"ts":%q,"thread_ts":%q,"user":"U1","text":"古い親","reply_count":8,"latest_reply":%q}]`,
			ts(fresh), ts(fresh), freshReplies, ts(freshLatest), ts(old), ts(old), ts(since+50))
	}
	freshThread := func(n int) string {
		s := fmt.Sprintf(`{"ts":%q,"thread_ts":%q,"user":"U1","text":"新しい親"}`, ts(fresh), ts(fresh))
		for i := 1; i <= n; i++ {
			s += fmt.Sprintf(`,{"ts":%q,"thread_ts":%q,"user":"U1","text":"返信"}`, ts(fresh+int64(i)), ts(fresh))
		}
		return "[" + s + "]"
	}
	sl := &fakeSlack{
		history: history(1, fresh+1),
		threads: map[string]string{ts(old): oldThread("前の返信"), ts(fresh): freshThread(1)},
	}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[
		{"title":"話題","state":"info","theme":"other","members":[%q,%q],"background":[],"labels":[]}]}`, ts(old), ts(fresh)))}
	d := newDeps(t, sl, cl)
	dir := filepath.Join(t.TempDir(), "cache")
	d.cacheDir = func() (string, error) { return dir, nil }

	dry := func() string {
		t.Helper()
		var out, errb bytes.Buffer
		if rc := run(context.Background(), []string{"-dry-run", "C1"}, &out, &errb, d); rc != 0 {
			t.Fatalf("rc=%d %s", rc, errb.String())
		}
		return out.String()
	}
	if out := dry(); !strings.Contains(out, "スレッド 2 本") || !strings.Contains(out, "同じ 0 本 (うち本文が編集された 0 本) / 前回の板に無い 2 本 / 親が history に無い 0 本 (前回の板が無い板 1)") {
		t.Errorf("前回の板が無いとき:\n%s", out)
	}

	readMD(t, d, "C1") // 前回の板 (Raw) を書く
	if out := dry(); !strings.Contains(out, "同じ 2 本 (うち本文が編集された 0 本) / 前回の板に無い 0 本") || strings.Contains(out, "前回の板が無い板") {
		t.Errorf("何も変わっていないとき:\n%s", out)
	}

	// 期間前の返信 (最後の返信ではない) の本文だけを編集した: 返信の数と最後の返信は変わらない
	sl.threads[ts(old)] = oldThread("直した返信")
	if out := dry(); !strings.Contains(out, "同じ 2 本 (うち本文が編集された 1 本)") {
		t.Errorf("返信の本文を編集したとき:\n%s", out)
	}

	sl.history, sl.threads[ts(fresh)] = history(2, fresh+2), freshThread(2)
	if out := dry(); !strings.Contains(out, "同じ 1 本 (うち本文が編集された 1 本) / 前回の板に無い 0 本") {
		t.Errorf("片方のスレッドに返信が増えたとき:\n%s", out)
	}

	// 返信が 1 つ消えて 1 つ付いた: 数は前回と同じでも、最後の返信が違えば同じと数えない
	sl.history = history(1, fresh+5)
	sl.threads[ts(fresh)] = fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"新しい親"},{"ts":%q,"thread_ts":%q,"user":"U1","text":"別の返信"}]`,
		ts(fresh), ts(fresh), ts(fresh+5), ts(fresh))
	if out := dry(); !strings.Contains(out, "同じ 1 本 (うち本文が編集された 1 本) / 前回の板に無い 0 本") {
		t.Errorf("返信の数が同じで最後の返信が違うとき:\n%s", out)
	}
}

// グループの板では、単位の ID が "<チャンネル>/<ts>" になる。前回の板の単位は、メンバーのチャンネルと親の ts で突き合わせる
// (2 つのメンバーで同じ ts のスレッドも別のものとして数える)。
func TestDryRunComparesGroupThreadsByMemberChannel(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	at := ts(since + 10) // 2 つのチャンネルで同じ ts
	parent := fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"親","reply_count":1,"latest_reply":%q}]`, at, at, ts(since+20))
	sl := &fakeSlack{
		histByCh: map[string]string{"C1": parent, "C2": parent},
		threads: map[string]string{at: fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"親"},{"ts":%q,"thread_ts":%q,"user":"U1","text":"返信"}]`,
			at, at, ts(since+20), at)},
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
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-dry-run"}, &out, &errb, d); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "スレッド 2 本") || !strings.Contains(out.String(), "同じ 2 本 (うち本文が編集された 0 本) / 前回の板に無い 0 本") {
		t.Errorf("グループのスレッドを前回の板と突き合わせていない:\n%s", out.String())
	}
}
