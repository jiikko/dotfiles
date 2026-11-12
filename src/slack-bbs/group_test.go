package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"slack-bbs/subs"
)

// グループはメンバーのチャンネルごとに読み、単位を合わせて 1 回で分類させる (チャンネルをまたいだ話題にしてよい)。
// 分類の入力には単位のチャンネルの名前を添え、本文には単位のチャンネルの名前とそのチャンネルのリンクを出す。
// 読めないチャンネルがあっても、他のチャンネルの板は出して注記で知らせる。
func TestReadGroup(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	at := ts(since + 100) // 2 つのチャンネルで同じ ts (ts はチャンネルの中でしか一意でない)
	sl := &fakeSlack{histByCh: map[string]string{
		"C1": fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"リリースします"}]`, at),
		"C2": fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"リリースの件、了解"}]`, at),
	}, failHist: map[string]bool{"C3": true}, search: map[int]string{
		// 同じグループの ops の発言が dev の発言のリンクを貼っている: 単独のチャンネルで別の単位から貼られたものと同じく言及に出す
		1: "[" + found("C2", "ops", ts(since+200), "U1", "これ "+link("C1", at)) + "]",
	}}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[{"title":"リリース","state":"info","theme":"release","urgent":false,
		"members":["C1/%s","C2/%s"],"conclusion":{"text":"","refs":[]},"background":[],"next":[],"labels":[]}]}`, at, at))}
	d := newDeps(t, sl, cl)
	path, _ := d.subsPath()
	g := subs.Channel{ID: "grp0123abcd", Name: "times", Members: []subs.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}, {ID: "C3", Name: "gone"}}}
	if err := writeSubs(t, path, []subs.Channel{g}); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-md"}, &out, &errb, d); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if cl.calls != 1 || !strings.Contains(cl.prompt, `"channel":"dev"`) || !strings.Contains(cl.prompt, `"channel":"ops"`) {
		t.Fatalf("1 回でまとめて、チャンネルを添えて分類させていない (calls=%d):\n%s", cl.calls, cl.prompt)
	}
	md := out.String()
	for _, want := range []string{"# 📂times", ": リリース (2 件", "#dev", "#ops", "archives/C2/p", "読めなかったチャンネル: #gone"} {
		if !strings.Contains(md, want) {
			t.Errorf("出力に %q が無い:\n%s", want, md)
		}
	}
	if strings.Count(md, "へのリンク") != 1 {
		t.Errorf("グループの別のチャンネルからの言及が 1 件出ない (言及の単位の ID を付け直していない):\n%s", md)
	}
	if strings.Contains(md, "未分類") {
		t.Errorf("グループの単位を分類に当てられなかった:\n%s", md)
	}
}

// メンバーの無いグループも、見出しと stderr・-dry-run の板の名前はグループの印 (📂) で出す (板の名前をメンバーの数で決めない)。
func TestEmptyGroupIsLabeledAsGroup(t *testing.T) {
	for _, args := range [][]string{{"-md"}, {"-dry-run"}} {
		d := newDeps(t, &fakeSlack{}, &fakeClaude{})
		path, _ := d.subsPath()
		if err := writeSubs(t, path, []subs.Channel{{ID: "grp0123abcd", Name: "times"}}); err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		if rc := run(context.Background(), args, &out, &errb, d); rc != 0 {
			t.Fatalf("%v: rc=%d %s", args, rc, errb.String())
		}
		if !strings.Contains(out.String(), "📂times") || strings.Contains(out.String(), "#times") {
			t.Errorf("%v: メンバーの無いグループをチャンネルの形 (#) で出した:\n%s", args, out.String())
		}
	}
}
