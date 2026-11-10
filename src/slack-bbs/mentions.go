package main

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jiikko/dotfiles/src/termsafe"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/source"
	"slack-bbs/subs"
)

// mentionPages は言及を探す search のページの上限 (1 ページ source.SearchCount 件)。リンクを貼るボットが多いチャンネルでは
// 1 日で数ページになる (実測 2026-11-11: 2 日で 5 ページを超えたチャンネルがある)。届いたら板の注記に出す。
const mentionPages = 10

// permalink は本文の中の発言のリンク (https://<workspace>.slack.com/archives/<チャンネル>/p<ts の . を除いた数字>[?thread_ts=…])。
// 数字の末尾 6 桁がマイクロ秒で、その前が秒 (秒の桁は決め打たない)。
var permalink = regexp.MustCompile(`/archives/([A-Z0-9]+)/p(\d+)(\d{6})(\?[^\s|>]*)?`)

var threadParam = regexp.MustCompile(`thread_ts=(\d+\.\d{6})`)

// findMentions は、チャンネル ch の板の発言 (units) へのリンクを貼った発言を Slack の検索で探す。
// 検索は「archives/<チャンネル ID>」で候補をまとめて引き (話題ごとに検索しない)、本文のリンクが指す ts を単位の発言と突き合わせて、
// 板の発言を指すものだけを残す。同じ単位の中の発言が自分の単位を指すもの (スレッドの中で上の発言を貼った等) は言及に数えない。
// 期間の始まり sinceTs より前の言及は付けない (付けると、ページの境しだいで付いたり付かなかったりする)。sinceTs より前の発言まで
// 読んだか、空のページが返ったら止める (search.messages は新しい順)。
//
// ボットの投稿 (us.IsBot) は言及に数えない。リアクションの付いた発言を別のチャンネルへ流す Reacji Channeler のように、
// リンクを機械的に貼るボットが多く、人が引用・感想を書いた言及が埋もれる。人が書いた本文をボットが代わりに投稿するもの
// (ワークフローのフォーム・人の名前で投稿する連携) も一緒に落ちる。それを拾いたくなったら、ボットごとに残す集合を持つ。
// 捨てたボットの投稿も mentionPages のページ数には数える (ページの境を変えると、期間の始まりまで読んだかの判定が崩れる)。
//
// 見えないもの: 「転送」で共有した発言は本文に元の URL が無い (attachments にあり、slack-cli の search は出さない)。
//
// 見えるのは自分のアカウントで検索できる発言だけ (公開チャンネルと、自分が入っている非公開チャンネル・DM)。
func findMentions(ctx context.Context, cli source.Client, ch subs.Channel, sinceTs, sinceDay string, units []board.Unit, me string, us source.Users) (ms []render.Mention, truncated bool, err error) {
	type target struct {
		unit string
		msg  board.Msg
		ok   bool // msg が在る (スレッドの親の ts だけで当たったときは偽)
	}
	byTs := map[string]target{}
	for _, u := range units {
		for _, m := range u.Msgs {
			byTs[m.Ts] = target{unit: u.ID, msg: m, ok: true}
		}
		if _, ok := byTs[u.ID]; !ok {
			byTs[u.ID] = target{unit: u.ID}
		}
	}
	unitOf := func(ts string) string {
		if t, ok := byTs[ts]; ok {
			return t.unit
		}
		return ""
	}
	seen := map[string]bool{}
	query := fmt.Sprintf("archives/%s after:%s", ch.ID, sinceDay)
	for page := 1; ; page++ {
		if page > mentionPages {
			return ms, true, nil
		}
		fs, err := cli.Search(ctx, query, page)
		if err != nil {
			return ms, false, err
		}
		if len(fs) == 0 {
			return ms, false, nil
		}
		oldest := fs[0].Ts
		for _, f := range fs {
			if board.TsBefore(f.Ts, oldest) {
				oldest = f.Ts
			}
			if board.TsBefore(f.Ts, sinceTs) || us.IsBot(f) {
				continue
			}
			for _, l := range permalink.FindAllStringSubmatch(f.Text, -1) {
				if l[1] != ch.ID {
					continue
				}
				ts := l[2] + "." + l[3]
				t, ok := byTs[ts]
				if !ok {
					if th := threadParam.FindStringSubmatch(l[4]); th != nil {
						t, ok = byTs[th[1]] // 板に無い返信でも、そのスレッドが板にあれば話題は決まる
					}
				}
				if !ok || f.Channel.ID == ch.ID && unitOf(f.Ts) == t.unit {
					continue
				}
				key := f.Channel.ID + "/" + f.Ts + "/" + t.unit
				if seen[key] {
					continue
				}
				seen[key] = true
				name := us.Names[f.User]
				if name == "" {
					name = f.Username
				}
				m := render.Mention{
					Unit: t.unit, Target: ts, Ts: f.Ts, Name: name, Mine: f.User == me,
					ChannelName: f.Channel.Name, Text: board.Plain(f.Text, us.Names), URL: f.Permalink,
				}
				if t.ok {
					m.TargetName, m.TargetMine = t.msg.Name, t.msg.Mine
				} else {
					m.Target, m.TargetName = t.unit, "スレッド"
				}
				ms = append(ms, m)
			}
		}
		if board.TsBefore(oldest, sinceTs) {
			return ms, false, nil
		}
	}
}

// mentionNote は言及の検索に失敗した・打ち切ったときの板の注記 (無ければ空)。
func mentionNote(truncated bool, err error) string {
	switch {
	case err != nil:
		return "この板の発言への言及を検索できなかった (言及の節は出ない): " + termsafe.ClipRunes(termsafe.PlainLine(err.Error()), 200)
	case truncated:
		return fmt.Sprintf("言及の候補が多く、新しい %d 件までしか見ていない (それより前の言及は出ない)", mentionPages*source.SearchCount)
	}
	return ""
}
