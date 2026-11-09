package main

import (
	"bytes"

	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"slack-bbs/job"
	"slack-bbs/llm"
	"slack-bbs/render"
	"slack-bbs/subs"
	"slack-bbs/view"
)

var now = time.Unix(2_000_000, 0)

// fakeSlack は slack-cli の代わり。呼ばれた引数を記録し、サブコマンドごとに JSON を返す。
type fakeSlack struct {
	calls   [][]string
	history string
	threads map[string]string
	warn    string // history の stderr (slack-cli が途中で切れたときの警告を模す)
	failTh  bool   // thread を全部失敗させる (消えたスレッド・認証切れを模す)
}

func (f *fakeSlack) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	out, err := f.run(args)
	if args[0] == "history" {
		return out, []byte(f.warn), err
	}
	return out, nil, err
}

func (f *fakeSlack) run(args []string) ([]byte, error) {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "whoami":
		return []byte(`{"user_id":"ME","url":"https://ws.slack.com/"}`), nil
	case "users":
		return []byte(`[{"id":"U1","name":"tanaka"},{"id":"ME","name":"me"}]`), nil
	case "channels":
		return []byte(`[{"id":"C1","name":"dev","is_member":true}]`), nil
	case "history":
		return []byte(f.history), nil
	case "thread":
		if f.failTh {
			return nil, errors.New("slack thread: exit status 1: thread_not_found")
		}
		if s, ok := f.threads[args[len(args)-1]]; ok {
			return []byte(s), nil
		}
		return []byte(`[]`), nil
	}
	return nil, fmt.Errorf("想定外の呼び出し %v", args)
}

// fakeClaude は claude -p の代わり。受け取った入力を記録し、決まった分類を返す。
type fakeClaude struct {
	calls  int
	prompt string
	args   []string
	out    string
}

func (f *fakeClaude) Run(_ context.Context, args []string, stdin []byte) ([]byte, error) {
	f.calls++
	f.args, f.prompt = args, string(stdin)
	return []byte(f.out), nil
}

func claudeOut(structured string) string {
	return `{"type":"result","subtype":"success","is_error":false,"result":"","total_cost_usd":0.01,"usage":{"input_tokens":5,"output_tokens":7},"structured_output":` + structured + `}`
}

func ts(sec int64) string { return fmt.Sprintf("%d.000000", sec) }

// keyOf は prepare の出力から show に渡す鍵を取る。
func keyOf(t *testing.T, out *bytes.Buffer) string {
	t.Helper()
	var pr prepared
	if err := json.Unmarshal(out.Bytes(), &pr); err != nil || pr.Key == "" {
		t.Fatalf("prepare の出力に鍵が無い: %v %s", err, out.String())
	}
	return pr.Key
}

func newDeps(t *testing.T, sl *fakeSlack, cl *fakeClaude) deps {
	path := filepath.Join(t.TempDir(), "subscriptions.json")
	return deps{
		slack: sl, claude: cl, now: func() time.Time { return now },
		subsPath: func() (string, error) { return path, nil },
		pick:     func(*subs.Picker) error { return nil },
		tty:      func() bool { return false },
	}
}

// 通しの経路: 古い親に期間内の返信が付いたスレッドを取り直し、分類させ、話題として出す。
// LLM が落とした単位は未分類に出る。slack-cli のフラグは引数より前に置く。
func TestReadEndToEnd(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	parent := since - 3600
	sl := &fakeSlack{
		history: fmt.Sprintf(`[
			{"ts":%q,"user":"U1","text":"今日のリリース"},
			{"ts":%q,"user":"U1","text":"雑談"},
			{"ts":%q,"thread_ts":%q,"user":"ME","text":"CI が遅い","reply_count":1,"latest_reply":%q}
		]`, ts(since+200), ts(since+100), ts(parent), ts(parent), ts(since+50)),
		threads: map[string]string{ts(parent): fmt.Sprintf(`[
			{"ts":%q,"thread_ts":%q,"user":"ME","text":"CI が遅い"},
			{"ts":%q,"thread_ts":%q,"user":"U1","text":"キャッシュを見ます"}]`, ts(parent), ts(parent), ts(since+50), ts(parent))},
	}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[
		{"title":"CI","state":"open","members":[%q],"points":[{"text":"キャッシュを調べる","refs":[%q]}],"labels":[]},
		{"title":"リリース","state":"decided","members":[%q],"points":[],"labels":[]}]}`, ts(parent), ts(since+50), ts(since+200)))}
	var out, errb bytes.Buffer
	rc := run(context.Background(), []string{"C1"}, &out, &errb, newDeps(t, sl, cl))
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errb.String())
	}
	for _, c := range sl.calls {
		if c[0] == "history" || c[0] == "thread" {
			if c[len(c)-1] == "-json" || strings.HasPrefix(c[len(c)-1], "-") {
				t.Errorf("フラグが引数の後ろにある: %v", c)
			}
		}
	}
	md := out.String()
	for _, want := range []string{
		"🔴 要対応 🟡 議論中: CI", // 自分のスレッドへの期間内の返信
		"要対応 (あなたのスレッドへの返信)",
		"🔁 続いているスレッド",
		"(前) ", // 期間前の親は文脈として印を付ける
		"🟢 決定: リリース",
		"## 未分類", // LLM が落とした「雑談」
		"雑談",
		"?thread_ts=" + ts(parent) + "&cid=C1", // 返信の permalink
	} {
		if !strings.Contains(md, want) {
			t.Errorf("出力に %q が無い:\n%s", want, md)
		}
	}
	if !strings.Contains(errb.String(), "LLM の分類から捨てたもの") {
		t.Errorf("落ちた単位を warn していない: %s", errb.String())
	}
	if !strings.Contains(cl.prompt, "キャッシュを見ます") || strings.Contains(cl.prompt, "<@") {
		t.Errorf("claude への入力: %s", cl.prompt)
	}
}

// -dry-run は claude を呼ばない。
func TestDryRunDoesNotCallClaude(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"x"}]`, ts(since+1))}
	cl := &fakeClaude{}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-dry-run", "C1"}, &out, &errb, newDeps(t, sl, cl)); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if cl.calls != 0 {
		t.Errorf("-dry-run で claude を %d 回呼んだ", cl.calls)
	}
	if !strings.Contains(out.String(), "送る発言 1") {
		t.Errorf("出力: %s", out.String())
	}
}

// 上限を超えたら claude へ送らずに止める。
func TestMaxStopsBeforeSending(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"a"},{"ts":%q,"user":"U1","text":"b"}]`, ts(since+2), ts(since+1))}
	cl := &fakeClaude{}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-max", "1", "C1"}, &out, &errb, newDeps(t, sl, cl)); rc != 1 {
		t.Fatalf("rc=%d (want 1) %s", rc, errb.String())
	}
	if cl.calls != 0 {
		t.Errorf("上限を超えたのに claude を呼んだ")
	}
}

// 古い slack-cli (親に reply_count / latest_reply を出さない版) の疑いは、黙らずに板の注記に出す
// (止めはしない: 返信が全部消された親しか無いチャンネルは、新しい版でも同じ形になる)。
func TestOldSlackCLIIsNoted(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"thread_ts":%q,"user":"U1","text":"親"}]`, ts(since-10), ts(since-10))}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"C1"}, &out, &errb, newDeps(t, sl, &fakeClaude{})); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "slack-cli が古い") {
		t.Errorf("注記が出ていない:\n%s", out.String())
	}
}

// 購読が無く引数も無ければ、選び方を案内して止める。
func TestNoSubscriptions(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := run(context.Background(), nil, &out, &errb, newDeps(t, &fakeSlack{}, &fakeClaude{})); rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(errb.String(), "slack-bbs sub") {
		t.Errorf("stderr: %s", errb.String())
	}
}

// 購読を選んで保存し、引数なしの実行がそのチャンネルを読む。
func TestSubThenRead(t *testing.T) {
	sl := &fakeSlack{history: `[]`}
	d := newDeps(t, sl, &fakeClaude{})
	d.pick = func(p *subs.Picker) error {
		p.Key("x")
		p.Key("enter")
		return nil
	}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"sub"}, &out, &errb, d); rc != 0 {
		t.Fatalf("sub rc=%d %s", rc, errb.String())
	}
	out.Reset()
	if rc := run(context.Background(), []string{"-json"}, &out, &errb, d); rc != 0 {
		t.Fatalf("read rc=%d %s", rc, errb.String())
	}
	var pages []map[string]any
	if err := json.Unmarshal(out.Bytes(), &pages); err != nil || len(pages) != 1 || pages[0]["channel"] != "C1" {
		t.Errorf("購読したチャンネルを読んでいない: %v %s", err, out.String())
	}
}

// claude の引数: 設定・hooks・CLAUDE.md を読まず、セッションを残さず、道具を持たない。
func TestClaudeArgsIsolate(t *testing.T) {
	args := strings.Join(llm.Args("sonnet"), " ")
	for _, want := range []string{"--no-session-persistence", "--setting-sources  ", "--tools ", "--strict-mcp-config", "--json-schema"} {
		if !strings.Contains(args+" ", want) {
			t.Errorf("claude の引数に %q が無い: %s", want, args)
		}
	}
}

func TestParseSince(t *testing.T) {
	got, _, err := parseSince("2d", now)
	if err != nil || !got.Equal(now.Add(-48*time.Hour)) {
		t.Errorf("2d → %v %v", got, err)
	}
	if _, _, err := parseSince("0d", now); err == nil {
		t.Error("0d を受けた")
	}
	if _, _, err := parseSince("abc", now); err == nil {
		t.Error("abc を受けた")
	}
}

// 2 段の経路: prepare が依頼を書き、外で分類して応答を書き、show が板を出して自分のファイルだけを消す。
func TestPrepareThenShow(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"<@ME> レビューお願い"},{"ts":%q,"user":"U1","text":"雑談"}]`, ts(since+2), ts(since+1))}
	d := newDeps(t, sl, &fakeClaude{})
	dir := filepath.Join(t.TempDir(), "job")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("prepare rc=%d %s", rc, errb.String())
	}
	key := keyOf(t, &out)
	var pr prepared
	if err := json.Unmarshal(out.Bytes(), &pr); err != nil || len(pr.Channels) != 1 {
		t.Fatalf("prepare の出力: %v %s", err, out.String())
	}
	c := pr.Channels[0]
	prompt, err := os.ReadFile(c.Prompt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@me レビューお願い", c.Response, `"enum":["decided","open","info"]`} {
		if !strings.Contains(string(prompt), want) {
			t.Errorf("分類する側への文面に %q が無い", want)
		}
	}
	resp := fmt.Sprintf(`{"topics":[{"title":"レビュー","state":"open","members":[%q],"points":[],"labels":[]}]}`, ts(since+2))
	if err := os.WriteFile(c.Response, []byte(resp), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if rc := run(context.Background(), []string{"show", "-key", key, dir}, &out, &errb, d); rc != 0 {
		t.Fatalf("show rc=%d %s", rc, errb.String())
	}
	for _, want := range []string{"🔴 要対応 🟡 議論中: レビュー", "要対応 (名指し)", "## 未分類", "雑談"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("show の出力に %q が無い:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("show の後に依頼のディレクトリが残った: %v", err)
	}
}

// 応答が無い・形が違うチャンネルがあれば、出せたものだけ出して rc=1 にし、ディレクトリを消さない。
func TestShowKeepsDirWhenResponseMissingOrMalformed(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	for name, resp := range map[string]string{"無い": "", "知らない欄": `{"topics":[],"extra":1}`, "JSON でない": "```json\n{}\n```"} {
		t.Run(name, func(t *testing.T) {
			sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"x"}]`, ts(since+1))}
			d := newDeps(t, sl, &fakeClaude{})
			dir := filepath.Join(t.TempDir(), "job")
			var out, errb bytes.Buffer
			if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
				t.Fatalf("prepare rc=%d %s", rc, errb.String())
			}
			key := keyOf(t, &out)
			if resp != "" {
				if err := os.WriteFile(filepath.Join(dir, "C1.response.json"), []byte(resp), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if rc := run(context.Background(), []string{"show", "-key", key, dir}, &out, &errb, d); rc != 1 {
				t.Fatalf("rc=%d (want 1)", rc)
			}
			if _, err := os.Stat(filepath.Join(dir, "C1.request.json")); err != nil {
				t.Errorf("出せなかったのに依頼を消した: %v", err)
			}
		})
	}
}

// 期間内の発言が無いチャンネルは応答が無くても出す。
func TestShowEmptyChannelNeedsNoResponse(t *testing.T) {
	d := newDeps(t, &fakeSlack{history: `[]`}, &fakeClaude{})
	dir := filepath.Join(t.TempDir(), "job")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("prepare rc=%d %s", rc, errb.String())
	}
	key := keyOf(t, &out)
	if !strings.Contains(out.String(), `"prompt": ""`) {
		t.Errorf("発言の無いチャンネルに分類の依頼を出した: %s", out.String())
	}
	out.Reset()
	if rc := run(context.Background(), []string{"show", "-key", key, dir}, &out, &errb, d); rc != 0 {
		t.Fatalf("show rc=%d %s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "期間内の発言はありません") {
		t.Errorf("出力: %s", out.String())
	}
}

// show が消すのは prepare と分類する側が書いたファイルだけ。他のファイルがあればディレクトリごと残す。
func TestShowCleanLeavesForeignFiles(t *testing.T) {
	d := newDeps(t, &fakeSlack{history: `[]`}, &fakeClaude{})
	dir := filepath.Join(t.TempDir(), "job")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("prepare rc=%d", rc)
	}
	key := keyOf(t, &out)
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc := run(context.Background(), []string{"show", "-key", key, dir}, &out, &errb, d); rc != 0 {
		t.Fatalf("出し切ったのに rc=%d: %s", rc, errb.String())
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("他のファイルを消した: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "C1.request.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("自分の依頼が残った: %v", err)
	}
}

// 一覧のチャンネル ID は、ファイル名として外へ出る形 (パスの区切り) を受けない。
func TestShowRejectsPathLikeChannel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(fmt.Sprintf(`{"version":%d,"channels":["../x"]}`, job.Version)), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"show", "-key", "00", dir}, &out, &errb, deps{}); rc != 1 {
		t.Fatalf("rc=%d (want 1)", rc)
	}
	if !strings.Contains(errb.String(), "ファイル名に使えない") {
		t.Errorf("stderr: %s", errb.String())
	}
}

// 利用者が渡した既存のディレクトリは、空になっても消さない (prepare が作ったディレクトリだけを消す)。
func TestShowKeepsUserProvidedDir(t *testing.T) {
	d := newDeps(t, &fakeSlack{history: `[]`}, &fakeClaude{})
	dir := t.TempDir()
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("prepare rc=%d %s", rc, errb.String())
	}
	key := keyOf(t, &out)
	if rc := run(context.Background(), []string{"show", "-key", key, dir}, &out, &errb, d); rc != 0 {
		t.Fatalf("show rc=%d %s", rc, errb.String())
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("利用者のディレクトリを消した: %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("自分のファイルが残った: %v", ents)
	}
}

// 前の依頼が残ったディレクトリへの prepare は断る (古い応答が新しい単位に当たる・前のチャンネルの本文が残り続ける)。
func TestPrepareRefusesLeftoverJob(t *testing.T) {
	d := newDeps(t, &fakeSlack{history: `[]`}, &fakeClaude{})
	dir := filepath.Join(t.TempDir(), "job")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("prepare rc=%d", rc)
	}
	errb.Reset()
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 1 {
		t.Fatalf("前の依頼の上に prepare した (rc=%d)", rc)
	}
	if !strings.Contains(errb.String(), "前の依頼が残っている") {
		t.Errorf("stderr: %s", errb.String())
	}
}

// 依頼のファイルが prepare の後に書き換えられていたら (分類する側が本文の指示に従った等)、組まずに止める。
func TestShowRejectsTamperedRequest(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"x"},{"ts":%q,"user":"U1","text":"y"}]`, ts(since+2), ts(since+1))}
	d := newDeps(t, sl, &fakeClaude{})
	dir := filepath.Join(t.TempDir(), "job")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("prepare rc=%d", rc)
	}
	key := keyOf(t, &out)
	req := filepath.Join(dir, "C1.request.json")
	b, err := os.ReadFile(req)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	r["units"] = r["units"].([]any)[:1] // 単位を 1 つ消す
	b, _ = json.Marshal(r)
	if err := os.WriteFile(req, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "C1.response.json"), []byte(`{"topics":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	errb.Reset()
	if rc := run(context.Background(), []string{"show", "-key", key, dir}, &out, &errb, d); rc != 1 {
		t.Fatalf("書き換えた依頼を受けた (rc=%d)", rc)
	}
	if !strings.Contains(errb.String(), "変わっている") {
		t.Errorf("stderr: %s", errb.String())
	}
}

// 応答の JSON の後ろに続きがあれば受けない。
func TestShowRejectsTrailingGarbage(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	d := newDeps(t, &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"x"}]`, ts(since+1))}, &fakeClaude{})
	dir := filepath.Join(t.TempDir(), "job")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("prepare rc=%d", rc)
	}
	key := keyOf(t, &out)
	if err := os.WriteFile(filepath.Join(dir, "C1.response.json"), []byte(`{"topics":[]} 説明: 分類しました`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc := run(context.Background(), []string{"show", "-key", key, dir}, &out, &errb, d); rc != 1 {
		t.Fatalf("後ろに続きのある応答を受けた (rc=%d)", rc)
	}
}

// 依頼を 1 つも書けなかった prepare は、自分で作った一時ディレクトリを残さない。
func TestPrepareLeavesNothingWhenNoChannel(t *testing.T) {
	sl := &fakeSlack{history: `not json`}
	d := newDeps(t, sl, &fakeClaude{})
	dir := filepath.Join(t.TempDir(), "job")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("空のディレクトリが残った: %v", err)
	}
}

// slack-cli が stderr に出した警告 (途中で切れた等。rc=0 のまま) は、板の注記に出す。
func TestSlackCLIWarningBecomesNote(t *testing.T) {
	sl := &fakeSlack{history: `[]`, warn: "slack: 取得が途中で切れた\n"}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"C1"}, &out, &errb, newDeps(t, sl, &fakeClaude{})); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "slack-cli の警告 (history): slack: 取得が途中で切れた") {
		t.Errorf("警告が板に出ていない:\n%s", out.String())
	}
}

// 古い slack-cli の判定は「どの親もスレッドの動きを持たない」とき。返信が全部消された親が 1 つ混ざっても止めない。
func TestOldSlackCLIDetectionToleratesDeletedReplies(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[
		{"ts":%q,"thread_ts":%q,"user":"U1","text":"返信が全部消えた親"},
		{"ts":%q,"thread_ts":%q,"user":"U1","text":"親","reply_count":1,"latest_reply":%q}]`,
		ts(since+5), ts(since+5), ts(since+1), ts(since+1), ts(since+2))}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-dry-run", "C1"}, &out, &errb, newDeps(t, sl, &fakeClaude{})); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
}

// 依頼を書き換えたうえでハッシュと一覧も作り直しても (分類役はディレクトリを書ける)、鍵の無い書き換えは見破る。
// created_dir を真に書き換えて、利用者のディレクトリを消させることもできない。
func TestShowRejectsForgedManifest(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	d := newDeps(t, &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"x"}]`, ts(since+1))}, &fakeClaude{})
	dir := t.TempDir() // 利用者が渡した既存のディレクトリ
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"prepare", "-out", dir, "C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("prepare rc=%d", rc)
	}
	key := keyOf(t, &out)
	mp := filepath.Join(dir, "manifest.json")
	b, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["created_dir"] = true
	b, _ = json.Marshal(m)
	if err := os.WriteFile(mp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "C1.response.json"), []byte(`{"topics":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	errb.Reset()
	if rc := run(context.Background(), []string{"show", "-key", key, dir}, &out, &errb, d); rc != 1 {
		t.Fatalf("書き換えた一覧を受けた (rc=%d)", rc)
	}
	if !strings.Contains(errb.String(), "署名が鍵と合わない") {
		t.Errorf("stderr: %s", errb.String())
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("利用者のディレクトリが消えた: %v", err)
	}
	// 鍵が無い show も断る
	errb.Reset()
	if rc := run(context.Background(), []string{"show", dir}, &out, &errb, d); rc != 1 {
		t.Fatalf("鍵の無い show を受けた (rc=%d)", rc)
	}
}

// 日付で渡した -since は読み直しでもずらさない。期間で渡したら押した時点までずらす。
func TestBrowseReloadWindow(t *testing.T) {
	for _, c := range []struct {
		since     string
		wantShift bool
	}{{"2d", true}, {time.Unix(1_900_000, 0).Format("2006-01-02"), false}} {
		t.Run(c.since, func(t *testing.T) {
			clock := now
			sl := &fakeSlack{history: `[]`}
			d := newDeps(t, sl, &fakeClaude{})
			d.now = func() time.Time { return clock }
			d.tty = func() bool { return true }
			var got []render.Page
			d.browse = func(m *view.Model) error {
				m.Update(m.Init()()) // 起動の読み込み
				p, _ := m.Page(0)
				got = append(got, p)
				clock = now.Add(time.Hour)
				_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
				m.Update(cmd())
				p, _ = m.Page(0)
				got = append(got, p)
				return nil
			}
			var out, errb bytes.Buffer
			if rc := run(context.Background(), []string{"-since", c.since, "C1"}, &out, &errb, d); rc != 0 {
				t.Fatalf("rc=%d %s", rc, errb.String())
			}
			if len(got) != 2 {
				t.Fatalf("読み込み %d 回", len(got))
			}
			shifted := got[1].Since.Sub(got[0].Since)
			if c.wantShift && shifted != time.Hour {
				t.Errorf("期間で渡したのに since が %v ずれた (want 1h)", shifted)
			}
			if !c.wantShift && shifted != 0 {
				t.Errorf("日付で渡した since が %v ずれた", shifted)
			}
			if !got[1].Until.Equal(now.Add(time.Hour)) {
				t.Errorf("until = %v (押した時点までを読む)", got[1].Until)
			}
		})
	}
}

// 読む画面では、分類から捨てたものを stderr でなく板の注記に出す (画面の中では stderr が見えない)。
func TestBrowseNotesDropped(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"x"}]`, ts(since+1))}
	cl := &fakeClaude{out: claudeOut(`{"topics":[]}`)}
	d := newDeps(t, sl, cl)
	d.tty = func() bool { return true }
	var page render.Page
	d.browse = func(m *view.Model) error {
		m.Update(m.Init()())
		page, _ = m.Page(0)
		return nil
	}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"C1"}, &out, &errb, d); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if len(page.Notes) == 0 || !strings.Contains(page.Notes[len(page.Notes)-1], "未分類") {
		t.Errorf("注記: %v", page.Notes)
	}
	if errb.Len() != 0 {
		t.Errorf("読む画面で stderr に書いた: %s", errb.String())
	}
}

// スレッドの取得に失敗しても、チャンネルごとは止めない。そのスレッドは「読めなかった」印で出し、失敗は注記に (数本まで) 出す。
func TestThreadFailureDoesNotStopChannel(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	var hist []string
	for i := range 5 {
		p := ts(since + int64(10*i+1))
		hist = append(hist, fmt.Sprintf(`{"ts":%q,"thread_ts":%q,"user":"U1","text":"親%d","reply_count":1,"latest_reply":%q}`, p, p, i, ts(since+int64(10*i+2))))
	}
	sl := &fakeSlack{history: "[" + strings.Join(hist, ",") + "]", failTh: true}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-md", "-dry-run", "C1"}, &out, &errb, newDeps(t, sl, &fakeClaude{})); rc != 0 {
		t.Fatalf("スレッドの失敗でチャンネルごと止まった rc=%d %s", rc, errb.String())
	}
	cl := &fakeClaude{out: claudeOut(`{"topics":[]}`)}
	out.Reset()
	if rc := run(context.Background(), []string{"-md", "C1"}, &out, &errb, newDeps(t, sl, cl)); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	md := out.String()
	if n := strings.Count(md, "を読めなかった: "); n != maxThreadErrNotes {
		t.Errorf("1 本ずつの注記 %d 本 (want %d)", n, maxThreadErrNotes)
	}
	if !strings.Contains(md, "ほか 2 本のスレッドも読めなかった") || strings.Count(md, "スレッドを読めなかった (親と") != 5 {
		t.Errorf("出力:\n%s", md)
	}
}

// -h は使い方を出して 0、知らないフラグは誤りと案内を出して 2 (flag 自身の読めない一覧は出さない)。
func TestFlagErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"-h"}, &out, &errb, newDeps(t, &fakeSlack{}, &fakeClaude{})); rc != 0 || !strings.Contains(out.String(), "使い方:") {
		t.Errorf("-h: rc=%d %s", rc, out.String())
	}
	errb.Reset()
	if rc := run(context.Background(), []string{"show", "-nope", "x"}, &out, &errb, newDeps(t, &fakeSlack{}, &fakeClaude{})); rc != 2 ||
		!strings.Contains(errb.String(), "slack-bbs -h") || strings.Contains(errb.String(), "Usage of") {
		t.Errorf("知らないフラグ: rc=%d %s", rc, errb.String())
	}
}
