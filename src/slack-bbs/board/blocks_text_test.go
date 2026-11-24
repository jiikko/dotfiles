package board

import (
	"encoding/json"
	"testing"

	"slack-bbs/source"
)

// blocks_text は名指しだけを表示名にし、本文の山括弧・実体参照は書き換えない (mrkdwn ではない)。
func TestPlainBlocks(t *testing.T) {
	names := map[string]string{"U1": "tanaka"}
	got := PlainBlocks("<@U1> と <@U9> へ。<#C1> と <!subteam^S1>。Array<String> を返す。a &gt; b", names)
	want := "@tanaka と @U9 へ。#C1 と @group。Array<String> を返す。a &gt; b"
	if got != want {
		t.Errorf("PlainBlocks = %q\nwant         %q", got, want)
	}
}

// bot の発言は blocks_text (slack-cli が blocks から組み立てた本文) を本文にし、名指しも blocks_text から見る。
// slack-cli の -json の欄の名前で読めることも確かめる (欄の名前を取り違えると、黙って text の要約に戻る)。
func TestBuildUsesBlocksTextOfBotMessage(t *testing.T) {
	var hist []source.Message
	in := `[
	  {"ts":"1100.000000","bot_id":"B1","username":"ai-bot","text":"要約だけ","blocks_text":"## 見出し\n<@ME> Array<String> を返す"},
	  {"ts":"1200.000000","user":"U2","text":"<@U1> 人の発言"}
	]`
	if err := json.Unmarshal([]byte(in), &hist); err != nil {
		t.Fatal(err)
	}
	units, err := Build(Input{History: hist, Since: since, Me: "ME", Names: map[string]string{"U1": "tanaka", "ME": "me"}})
	if err != nil {
		t.Fatal(err)
	}
	byTs := map[string]Msg{}
	for _, u := range units {
		for _, m := range u.Msgs {
			byTs[m.Ts] = m
		}
	}
	bot := byTs["1100.000000"]
	if bot.Text != "## 見出し\n@me Array<String> を返す" {
		t.Errorf("bot の本文が blocks_text でない: %q", bot.Text)
	}
	if !bot.ToMe {
		t.Errorf("blocks_text の名指しを拾っていない: %+v", bot)
	}
	if human := byTs["1200.000000"]; human.Text != "@tanaka 人の発言" || human.ToMe {
		t.Errorf("人の発言: %+v", human)
	}
}
