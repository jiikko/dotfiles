package board

import (
	"regexp"
	"strings"
)

// Slack の本文の山括弧の記法 (<@U123>・<#C123|name>・<!here>・<https://…|label>)。
var angle = regexp.MustCompile(`<([^<>]+)>`)

// slack-cli の blocks_text に残る記法 (人・チャンネル・グループの名指し)。blocks_text は mrkdwn ではなく、
// 本文の文字をそのまま持つので、angle で拾うとコードの「Array<String>」のような山括弧まで崩す。
var blocksToken = regexp.MustCompile(`<(?:@[A-Z0-9]+|#[A-Z0-9]+|!subteam\^[A-Z0-9]+)>`)

var entities = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

// Plain は Slack の mrkdwn の山括弧の記法を人が読める形にほどく (メンションは表示名、リンクは「ラベル (URL)」)。
// 太字・斜体などの記号はそのまま残す (原文を書き換えないため。読む人には意味が通じる)。
func Plain(text string, names map[string]string) string {
	out := angle.ReplaceAllStringFunc(text, plainToken(names))
	return entities.Replace(out)
}

// PlainBlocks は slack-cli の blocks_text (bot の発言の blocks を組み立てた本文) の名指しを表示名にする。
// リンクは slack-cli が「ラベル (URL)」に組み立て済みで、&lt; などの実体参照も無いので、名指しの記法の他は触らない。
func PlainBlocks(text string, names map[string]string) string {
	return blocksToken.ReplaceAllStringFunc(text, plainToken(names))
}

// plainToken は、山括弧の記法 1 つ (<…>) を人が読める形にする関数を返す。
func plainToken(names map[string]string) func(tok string) string {
	return func(tok string) string { return plainOne(tok, names) }
}

func plainOne(tok string, names map[string]string) string {
	body := tok[1 : len(tok)-1]
	target, label, hasLabel := strings.Cut(body, "|")
	switch {
	case strings.HasPrefix(target, "@"):
		if n := names[target[1:]]; n != "" {
			return "@" + n
		}
		if hasLabel {
			return "@" + label
		}
		return "@" + target[1:]
	case strings.HasPrefix(target, "#"):
		if hasLabel && label != "" {
			return "#" + label
		}
		return "#" + target[1:]
	case strings.HasPrefix(target, "!subteam^"):
		if hasLabel {
			return label
		}
		return "@group"
	case strings.HasPrefix(target, "!"):
		if hasLabel {
			return label
		}
		return "@" + target[1:]
	case hasLabel && label != target:
		return label + " (" + target + ")"
	default:
		return target
	}
}

// Mentions は本文 (mrkdwn をほどく前) が user ID の人を名指ししているか。
func Mentions(text, userID string) bool {
	return userID != "" && (strings.Contains(text, "<@"+userID+">") || strings.Contains(text, "<@"+userID+"|"))
}
