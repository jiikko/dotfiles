package main

// pro-con help — pro-con を外から動かす人と Claude 向けの話題ごとの説明 (issue 510)。
// 本文は help/<話題>.md が正本 (README は実装側の事情だけを持ち、使い方はここを指す)。
// 役・レーン・人の番・流れの説明は画面の ? の表と同じ正本 (ui.RoleMeanings / card.State.Meaning / ui.HumansTurnMeaning /
// card.MainFlow・card.FlowDetours・card.AfterDone) から差し込む。

import (
	"embed"
	"fmt"
	"io"
	"strings"

	"pro-con/card"
	"pro-con/ui"
)

//go:embed help/*.md
var helpFS embed.FS

// helpTopic は話題 1 つ。names の先頭が正式な名前で、残りは別名。
type helpTopic struct {
	names   []string
	summary string
}

var helpTopics = []helpTopic{
	{[]string{"terms", "用語"}, "役 (PM・PG・取り込みの係・見張り・dispatcher…)・受付の箱・レーン・人の番"},
	{[]string{"flow", "流れ"}, "カードがどの順にレーンを渡り、誰が何で動かすか (寄り道・完了の後も)"},
	{[]string{"usage", "使い方"}, "依頼の出し方・質問への答え方・画面の開き方 (--join / --view)・削除と片付け"},
	{[]string{"debug", "デバッグ"}, "状態の置き場の中身・ps / log / card show・止まったとき・クラッシュの後・ライブアップグレード"},
}

// runHelp は pro-con help の本体。引数なしは話題の一覧、知らない話題は一覧を stderr に出して rc=2。
func runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = fmt.Fprint(stdout, helpIndex())
		return 0
	}
	if len(args) == 1 {
		if t, ok := findHelpTopic(args[0]); ok {
			_, _ = fmt.Fprint(stdout, helpText(t))
			return 0
		}
	}
	_, _ = fmt.Fprintf(stderr, "pro-con help: 知らない話題 %q\n%s", strings.Join(args, " "), helpIndex())
	return 2
}

func findHelpTopic(name string) (helpTopic, bool) {
	for _, t := range helpTopics {
		for _, n := range t.names {
			if strings.EqualFold(n, name) {
				return t, true
			}
		}
	}
	return helpTopic{}, false
}

func helpIndex() string {
	var b strings.Builder
	b.WriteString("usage: pro-con help <話題>\n")
	for _, t := range helpTopics {
		fmt.Fprintf(&b, "  %-8s %s\n", t.names[0], t.summary)
	}
	b.WriteString("コマンドごとの引数は各コマンドの usage (pro-con card を引数なしで・pro-con log --help など)\n")
	return b.String()
}

// helpText は話題の本文。目印の行 ({{…}}) は正本から作った説明に置き換える。
func helpText(t helpTopic) string {
	src, err := helpFS.ReadFile("help/" + t.names[0] + ".md")
	if err != nil {
		panic(err) // 話題の表と embed の組はテストが固定する
	}
	var roles, lanes strings.Builder
	for _, r := range ui.RoleMeanings() {
		fmt.Fprintf(&roles, "- **%s**: %s\n", r[0], r[1])
	}
	for i, s := range card.Columns {
		fmt.Fprintf(&lanes, "%d. **%s**: %s\n", i+1, s.Label(), s.Meaning())
	}
	var after strings.Builder
	for _, a := range card.AfterDone {
		fmt.Fprintf(&after, "- %s\n", a)
	}
	return strings.NewReplacer(
		"{{役}}\n", roles.String(),
		"{{レーン}}\n", lanes.String(),
		"{{人の番}}\n", ui.HumansTurnMeaning+"。\n",
		"{{通常の流れ}}\n", flowMarkdown(card.MainFlow),
		"{{寄り道}}\n", flowMarkdown(card.FlowDetours),
		"{{完了の後}}\n", after.String(),
	).Replace(string(src))
}

// flowMarkdown は移り変わりを 1 行ずつの箇条にする (画面の ? の流れのタブと同じ順・同じ文)。
func flowMarkdown(steps []card.FlowStep) string {
	var b strings.Builder
	for _, st := range steps {
		fmt.Fprintf(&b, "- **%s** — %s: %s\n", st.Heading(), st.Who, st.How)
	}
	return b.String()
}
