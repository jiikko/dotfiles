package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"slack-bbs/view"
)

// cacheFixture は発言 2 件のチャンネルと、2 件を別々の話題に分ける claude と、使い捨てのキャッシュの置き場を用意する。
func cacheFixture(t *testing.T) (*fakeSlack, *fakeClaude, deps, string) {
	t.Helper()
	since := now.Add(-48 * time.Hour).Unix()
	a, b := ts(since+100), ts(since+200)
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"リリース"},{"ts":%q,"user":"U1","text":"障害"}]`, b, a)}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[
		{"title":"話題A","state":"info","members":[%q],"points":[{"text":"a","refs":[%q]}],"labels":[]},
		{"title":"話題B","state":"decided","members":[%q],"points":[],"labels":[]}]}`, a, a, b))}
	d := newDeps(t, sl, cl)
	dir := filepath.Join(t.TempDir(), "cache")
	d.cacheDir = func() (string, error) { return dir, nil }
	return sl, cl, d, dir
}

func readMD(t *testing.T, d deps, args ...string) (string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	if rc := run(context.Background(), append([]string{"-md"}, args...), &out, &errb, d); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	return out.String(), errb.String()
}

// 取り直した発言がすべて前回に分類済みなら claude を呼ばずに前回の分類で組む。新しい発言・編集された発言・別のモデル・-no-cache
// では分類し直す。期間が進んで古い発言が外れただけなら使い回し、外れたものを「捨てたもの」に数えない。
func TestReadReusesCachedSummary(t *testing.T) {
	sl, cl, d, dir := cacheFixture(t)
	since := now.Add(-48 * time.Hour).Unix()

	readMD(t, d, "C1")
	if cl.calls != 1 {
		t.Fatalf("1 回目: claude %d 回", cl.calls)
	}
	for _, name := range []string{"C1.raw.json", "C1.summary.json"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v (want 0600)", name, fi, err)
		}
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("置き場: %v %v (want 0700)", fi, err)
	}

	md, errs := readMD(t, d, "C1")
	if cl.calls != 1 {
		t.Errorf("新しい発言が無いのに claude を呼んだ (%d 回)", cl.calls)
	}
	if !strings.Contains(md, "話題A") || !strings.Contains(md, "話題B") || !strings.Contains(errs, "新しい発言が無いので") {
		t.Errorf("前回の分類で組んでいない:\n%s\n%s", md, errs)
	}

	// 期間が進んで「リリース」(since+100) が外れた: 残りは分類済みなので使い回す
	d.now = func() time.Time { return now.Add(150 * time.Second) }
	md, errs = readMD(t, d, "C1")
	if cl.calls != 1 || strings.Contains(md, "話題A") || !strings.Contains(md, "話題B") || strings.Contains(errs, "捨てたもの") {
		t.Errorf("古い発言が外れただけで分類し直した、または外れたものを捨てたものに数えた (claude %d 回):\n%s\n%s", cl.calls, md, errs)
	}
	d.now = func() time.Time { return now }

	for _, c := range []struct {
		name    string
		history string
		args    []string
	}{
		{"-no-cache", "", []string{"-no-cache"}}, // 前回の分類がそのまま使える状態で試す (後のケースは保存したモデルや発言が変わる)
		{"新しい発言", fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"新しい"},{"ts":%q,"user":"U1","text":"リリース"},{"ts":%q,"user":"U1","text":"障害"}]`,
			ts(since+300), ts(since+200), ts(since+100)), nil},
		{"編集された発言", fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"リリース (延期)"},{"ts":%q,"user":"U1","text":"障害"}]`,
			ts(since+200), ts(since+100)), nil},
		{"別のモデル", "", []string{"-model", "opus"}},
	} {
		if c.history != "" {
			sl.history = c.history
		}
		before := cl.calls
		readMD(t, d, append(c.args, "C1")...)
		if cl.calls != before+1 {
			t.Errorf("%s: 分類し直していない (claude %d → %d 回)", c.name, before, cl.calls)
		}
	}
}

// 読む画面は、起動したらキャッシュの前回の板をすぐ出し (claude を待たない)、裏で読み直す。-no-cache なら前回の板を出さない。
func TestBrowsePreloadsCachedBoard(t *testing.T) {
	_, cl, d, _ := cacheFixture(t)
	readMD(t, d, "C1")
	calls := cl.calls
	d.tty = func() bool { return true }
	for _, c := range []struct {
		args    []string
		preload bool
	}{{nil, true}, {[]string{"-no-cache"}, false}, {[]string{"-model", "opus"}, false}} {
		var before bool
		var titles string
		d.browse = func(m *view.Model) error {
			p, ok := m.Page(0)
			before = ok
			for _, tp := range p.Topics {
				titles += tp.Title + " "
			}
			m.Update(m.Init()())
			return nil
		}
		var out, errb bytes.Buffer
		if rc := run(context.Background(), append(c.args, "C1"), &out, &errb, d); rc != 0 {
			t.Fatalf("rc=%d %s", rc, errb.String())
		}
		if before != c.preload || (c.preload && !strings.Contains(titles, "話題A")) {
			t.Errorf("%v: 起動時の板 %v (want %v) 話題: %q", c.args, before, c.preload, titles)
		}
	}
	if cl.calls != calls+2 { // 使い回せる 1 回目は呼ばず、-no-cache と別のモデルの起動で呼ぶ
		t.Errorf("claude %d 回 (want %d)", cl.calls-calls, 2)
	}
}

// 前回の板は slack-cli を待たずに出す: whoami が失敗しても画面を開いて前回の板を出し、失敗は読み直しの誤りとして出す。
func TestBrowseShowsCacheBeforeSlack(t *testing.T) {
	sl, _, d, _ := cacheFixture(t)
	readMD(t, d, "C1")
	sl.failWho = true
	d.tty = func() bool { return true }
	opened := false
	var page bool
	var lines string
	d.browse = func(m *view.Model) error {
		opened = true
		_, page = m.Page(0)
		m.Update(m.Init()())
		lines = strings.Join(m.Lines(), "\n")
		return nil
	}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if !opened || !page || !strings.Contains(lines, "読み直しに失敗 (") || !strings.Contains(lines, "のキャッシュを出している)") || !strings.Contains(lines, "話題A") {
		t.Errorf("slack-cli の失敗で前回の板を出せなかった: opened=%v page=%v\n%s", opened, page, lines)
	}
}

// 新しい発言を分類できなかった (claude の失敗・分類の前に閉じた) 後の起動でも、前回の板を出す (取得の結果だけを新しくしない)。
func TestFailedClassifyKeepsPreviousBoard(t *testing.T) {
	sl, cl, d, _ := cacheFixture(t)
	readMD(t, d, "C1")
	since := now.Add(-48 * time.Hour).Unix()
	sl.history = fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"新しい"},{"ts":%q,"user":"U1","text":"リリース"},{"ts":%q,"user":"U1","text":"障害"}]`,
		ts(since+300), ts(since+200), ts(since+100))
	good := cl.out
	cl.out = "壊れた応答"
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-md", "C1"}, &out, &errb, d); rc == 0 {
		t.Fatalf("分類の失敗で rc=0")
	}
	cl.out = good
	d.tty = func() bool { return true }
	var page bool
	d.browse = func(m *view.Model) error {
		_, page = m.Page(0)
		return nil
	}
	if rc := run(context.Background(), []string{"C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if !page {
		t.Errorf("分類に失敗した後の起動で前回の板を出さない")
	}
}

// LLM のラベルは読み手から見た判断なので、別の読み手 (別のアカウント) では使い回さずに分類し直す。
func TestOtherReaderReclassifies(t *testing.T) {
	sl, cl, d, _ := cacheFixture(t)
	readMD(t, d, "C1")
	sl.me = "U9"
	readMD(t, d, "C1")
	if cl.calls != 2 {
		t.Errorf("別の読み手で前回の分類を使い回した (claude %d 回)", cl.calls)
	}
}
