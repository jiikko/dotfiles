package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// found は search の -json の 1 件 (channel はオブジェクト)。
func found(ch, chName, ts, user, text string) string {
	return fmt.Sprintf(`{"ts":%q,"user":%q,"text":%q,"permalink":"https://ws.slack.com/archives/%s/p%s","channel":{"id":%q,"name":%q}}`,
		ts, user, text, ch, strings.ReplaceAll(ts, ".", ""), ch, chName)
}

func link(ch, ts string) string {
	return "<https://ws.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "") + ">"
}

// 板の発言へのリンクを貼った発言を、話題の「この話題への言及」に出す。自分の発言が引用されたものは「あなたの発言」と書く。
// 板に無い発言へのリンク・同じ単位の中で自分の単位を指すリンクは言及にしない。スレッドだけを指すリンク (板に無い返信) でも
// スレッドが板にあれば、その話題に付ける。期間の始まりより前の候補まで読んだら、次のページを頼まない。
// ボットの投稿 (user の無いもの・is_bot のユーザー・Slackbot) は言及にしない。
func TestMentionsAreCollectedPerTopic(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	mine, other, parent := ts(since+100), ts(since+200), ts(since+300)
	sl := &fakeSlack{
		history: fmt.Sprintf(`[
			{"ts":%q,"user":"ME","text":"設計はこうしたい"},
			{"ts":%q,"user":"U1","text":"今日のリリース"},
			{"ts":%q,"thread_ts":%q,"user":"U1","text":"障害の件","reply_count":1,"latest_reply":%q}
		]`, mine, other, parent, parent, ts(since+310)),
		threads: map[string]string{parent: fmt.Sprintf(`[
			{"ts":%q,"thread_ts":%q,"user":"U1","text":"障害の件"},
			{"ts":%q,"thread_ts":%q,"user":"U1","text":"上を見て %s"}]`, parent, parent, ts(since+310), parent, link("C1", parent))},
		search: map[int]string{
			1: "[" + strings.Join([]string{
				found("C9", "random", ts(since+900), "U1", "これどう思う? "+link("C1", mine)),
				found("C9", "random", ts(since+800), "U1", "関係ない "+link("C1", ts(since-99999))),
				found("C9", "random", ts(since+850), "U1", "別のチャンネルの同じ ts "+link("C7", mine)),
				found("C5", "kudos", ts(since+860), "", "リアクションで流れた "+link("C1", mine)),
				found("C5", "kudos", ts(since+870), "B1", "ボットが要約した "+link("C1", mine)),
				found("C5", "kudos", ts(since+880), "USLACKBOT", "リマインダー "+link("C1", mine)),
				found("C1", "dev", ts(since+310), "U1", "上を見て "+link("C1", parent)),
				found("C8", "ops", ts(since+700), "ME", "共有 <https://ws.slack.com/archives/C1/p"+strings.ReplaceAll(ts(since+305), ".", "")+"?thread_ts="+parent+"&cid=C1>"),
			}, ",") + "]",
			2: "[" + found("C9", "random", ts(since+1), "U1", "境の上 "+link("C1", mine)) + "," + found("C9", "random", ts(since-10), "U1", "期間の前の引用 "+link("C1", mine)) + "]",
			3: "[" + found("C9", "random", ts(since-20), "U1", "読まないはず "+link("C1", mine)) + "]",
		},
	}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[
		{"title":"設計","state":"open","members":[%q],"labels":[]},
		{"title":"リリース","state":"decided","members":[%q],"labels":[]},
		{"title":"障害","state":"open","members":[%q],"labels":[]}]}`, mine, other, parent))}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"C1"}, &out, &errb, newDeps(t, sl, cl)); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errb.String())
	}
	md := out.String()
	section := func(title string) string { // 見出しが title の話題の節 (板は話題を状態の順に並べ替える)
		for _, s := range strings.Split(md, "\n## ") {
			if head, _, _ := strings.Cut(s, "\n"); strings.Contains(head, ": "+title+" (") {
				return s
			}
		}
		t.Fatalf("話題 %q が無い:\n%s", title, md)
		return ""
	}
	design, release, outage := section("設計"), section("リリース"), section("障害")
	if !strings.Contains(design, "この話題への言及 (2 件)") || !strings.Contains(design, "[#random] これどう思う?") ||
		!strings.Contains(design, "あなたの発言") || !strings.Contains(design, "](https://ws.slack.com/archives/C9/p") {
		t.Errorf("自分の発言への別のチャンネルの言及が、その話題に出ない:\n%s", design)
	}
	if strings.Contains(release, "言及") {
		t.Errorf("言及されていない話題に言及の節がある:\n%s", release)
	}
	if !strings.Contains(outage, "この話題への言及 (1 件)") || !strings.Contains(outage, "[#ops] 共有") || strings.Contains(outage, "[#dev]") {
		t.Errorf("スレッドへの言及 (板に無い返信へのリンク) が出ない・同じスレッドの中のリンクを言及にした:\n%s", outage)
	}
	if strings.Contains(md, "関係ない") || strings.Contains(md, "読まないはず") || strings.Contains(md, "別のチャンネルの同じ ts") || strings.Contains(md, "期間の前の引用") || strings.Contains(md, "kudos") {
		t.Errorf("板に無い発言・別のチャンネルの発言への言及、期間より前まで読んだ後の候補、ボットの投稿を出した:\n%s", md)
	}
	pages := 0
	for _, c := range sl.calls {
		if c[0] == "search" {
			pages++
			if q, want := c[len(c)-1], "archives/C1 after:"+now.Add(-48*time.Hour).AddDate(0, 0, -2).Format("2006-01-02"); q != want {
				t.Errorf("search の語: %q", q)
			}
		}
	}
	if pages != 2 {
		t.Errorf("search を %d ページ読んだ, want 2 (2 ページ目で期間の始まりより前に届く)", pages)
	}
	if strings.Contains(cl.prompt, "これどう思う") {
		t.Error("言及を分類 (claude) に送った")
	}
}

// 言及の検索に失敗しても板は出し、注記で知らせる。
func TestMentionSearchFailureIsNoted(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"x"}]`, ts(since+1)), failSe: true}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[{"title":"x","state":"open","members":[%q],"labels":[]}]}`, ts(since+1)))}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"C1"}, &out, &errb, newDeps(t, sl, cl)); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "言及を検索できなかった") || !strings.Contains(out.String(), "ratelimited") {
		t.Errorf("検索の失敗が板の注記に無い:\n%s", out.String())
	}
}

// 候補が上限のページまで続いたら打ち切り、注記で知らせる。
func TestMentionSearchStopsAtPageLimit(t *testing.T) {
	since := now.Add(-48 * time.Hour).Unix()
	sl := &fakeSlack{history: fmt.Sprintf(`[{"ts":%q,"user":"U1","text":"x"}]`, ts(since+1)), search: map[int]string{}}
	for p := 1; p <= mentionPages+1; p++ {
		sl.search[p] = "[" + found("C9", "random", ts(since+5000-int64(p)), "U1", "x") + "]"
	}
	cl := &fakeClaude{out: claudeOut(fmt.Sprintf(`{"topics":[{"title":"x","state":"open","members":[%q],"labels":[]}]}`, ts(since+1)))}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"C1"}, &out, &errb, newDeps(t, sl, cl)); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errb.String())
	}
	pages := 0
	for _, c := range sl.calls {
		if c[0] == "search" {
			pages++
		}
	}
	if pages != mentionPages || !strings.Contains(out.String(), "言及の候補が多く") {
		t.Errorf("search %d ページ (want %d) / 注記: %s", pages, mentionPages, out.String())
	}
}
