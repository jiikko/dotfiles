package board

import (
	"regexp"
	"strings"
)

// Slack の本文の山括弧の記法 (<@U123>・<#C123|name>・<!here>・<https://…|label>)。
var angle = regexp.MustCompile(`<([^<>]+)>`)

var entities = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

// Plain は Slack の mrkdwn の山括弧の記法を人が読める形にほどく (メンションは表示名、リンクは「ラベル (URL)」)。
// 太字・斜体などの記号はそのまま残す (原文を書き換えないため。読む人には意味が通じる)。
func Plain(text string, names map[string]string) string {
	out := angle.ReplaceAllStringFunc(text, func(tok string) string {
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
	})
	return entities.Replace(out)
}

// Mentions は本文 (mrkdwn をほどく前) が user ID の人を名指ししているか。
func Mentions(text, userID string) bool {
	return userID != "" && (strings.Contains(text, "<@"+userID+">") || strings.Contains(text, "<@"+userID+"|"))
}
