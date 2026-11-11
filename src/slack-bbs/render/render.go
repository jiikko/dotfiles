// Package render は板 (話題の列) を掲示板風の Markdown にする。
//
// 原文は書き換えない (並べ方・印・リンクを足すだけ)。Slack の本文・名前・LLM の見出しは外から来た文字列なので、
// 端末の制御文字を termsafe で落としてから出す。
package render

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/board"
)

// Page は 1 チャンネルの板。
type Page struct {
	Channel     string        `json:"channel"`      // チャンネル ID
	ChannelName string        `json:"channel_name"` // 表示名 (分からなければ ID)
	Since       time.Time     `json:"since"`
	Until       time.Time     `json:"until"`
	Lookback    time.Time     `json:"lookback"` // これより前に始まったスレッドは見ていない
	Topics      []board.Topic `json:"topics"`
	Base        string        `json:"-"` // permalink の根 (https://<workspace>.slack.com/)
	Notes       []string      `json:"notes,omitempty"`
	// ClassifiedAt は話題に分けた時刻。新しい発言が無くて前回の分類を使い回したときは、取得より前になる (ゼロは分類していない)
	ClassifiedAt time.Time `json:"classified_at"`
	// Mentions はこの板の発言への言及 (別のチャンネル・別の話題で、発言のリンクを貼った発言)。分類 (LLM) には渡さない
	Mentions []Mention `json:"mentions,omitempty"`
	// Members はグループの板のチャンネル ID → 名前 (単位の Ch を名前で出す)。単独のチャンネルの板では空
	Members map[string]string `json:"members,omitempty"`
}

// Mention は板の発言へのリンクを貼った発言 1 件。Unit は言及された発言の単位 (話題への振り分けと既読の印に使う)。
type Mention struct {
	Unit        string `json:"unit"`
	Target      string `json:"target"`       // 言及された発言の ts (スレッドだけを指すリンクならスレッドの親の ts)
	TargetName  string `json:"target_name"`  // 言及された発言の書き手
	TargetMine  bool   `json:"target_mine"`  // 言及された発言が自分のものか
	Ts          string `json:"ts"`           // 言及した発言の ts
	Name        string `json:"name"`         // 言及した人
	Mine        bool   `json:"mine"`         // 言及したのが自分か (自分の言及では未読にしない)
	ChannelName string `json:"channel_name"` // 言及した発言のチャンネル
	Text        string `json:"text"`         // mrkdwn をほどいた本文
	URL         string `json:"url"`          // 言及した発言の permalink
}

// MentionsOf は話題 t の発言への言及 (古い順)。
func (p Page) MentionsOf(t board.Topic) []Mention {
	var out []Mention
	for _, m := range p.Mentions {
		if slices.ContainsFunc(t.Units, func(u board.Unit) bool { return u.ID == m.Unit }) {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b Mention) int {
		switch {
		case board.TsBefore(a.Ts, b.Ts):
			return -1
		case board.TsBefore(b.Ts, a.Ts):
			return 1
		}
		return 0
	})
	return out
}

var stateMark = map[string]string{
	board.StateDecided: "🟢 決定",
	board.StateOpen:    "🟡 議論中",
	board.StateInfo:    "⚪ 共有",
}

// Markdown は板を Markdown で w へ書く。
func Markdown(w io.Writer, p Page) error {
	var sb strings.Builder
	msgs := 0
	for _, t := range p.Topics {
		for _, u := range t.Units {
			for _, m := range u.Msgs {
				if m.New {
					msgs++
				}
			}
		}
	}
	mark := "#"
	if len(p.Members) > 0 {
		mark = "📂" // グループの板
	}
	fmt.Fprintf(&sb, "# %s%s — %s 〜 %s (発言 %d / 話題 %d)\n\n", mark, clean(p.ChannelName), stamp(p.Since), stamp(p.Until), msgs, countTopics(p.Topics))
	fmt.Fprintf(&sb, "%s より前に始まったスレッドは見ていない。\n", p.Lookback.Format("01/02"))
	for _, n := range p.Notes {
		fmt.Fprintf(&sb, "🚨 %s\n", clean(n))
	}
	if len(p.Topics) == 0 {
		sb.WriteString("\n期間内の発言はありません。\n")
	}
	for _, t := range p.Topics {
		sb.WriteString("\n")
		p.topic(&sb, t)
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

func countTopics(ts []board.Topic) int {
	n := 0
	for _, t := range ts {
		if !t.Unclassified {
			n++
		}
	}
	return n
}

func (p Page) topic(sb *strings.Builder, t board.Topic) {
	fmt.Fprintf(sb, "## %s\n", Head(t))
	for _, l := range p.Body(t, true) {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
}

// themeMark はテーマの印と名前。どの印も表示幅が 2 で変わらないもの (異体字セレクタで幅が揺れる記号を使わない)。
var themeMark = map[string][2]string{
	board.ThemeIncident:   {"🐛", "障害"},
	board.ThemeRelease:    {"🚀", "リリース"},
	board.ThemeQuestion:   {"❓", "質問・相談"},
	board.ThemeDiscussion: {"💬", "議論・設計"},
	board.ThemeAnnounce:   {"📣", "告知"},
	board.ThemeSchedule:   {"📅", "予定・会議"},
	board.ThemeOps:        {"🔧", "作業・運用"},
	board.ThemeMetrics:    {"📈", "数字・分析"},
	board.ThemeMoney:      {"💰", "お金・契約"},
	board.ThemePeople:     {"👥", "人・体制"},
	board.ThemeChat:       {"☕", "雑談"},
	board.ThemeOther:      {"📝", "その他"},
}

// ThemeIcon は話題のテーマの印 (知らないテーマはその他の印)。
func ThemeIcon(t board.Topic) string {
	if m, ok := themeMark[t.Theme]; ok {
		return m[0]
	}
	return themeMark[board.ThemeOther][0]
}

// UrgentMark は緊急の話題の印 (LLM の判断)。
const UrgentMark = "🚨"

// Mark は話題の印 (緊急 / 要対応 / 必読 / 状態 / テーマの順に、当てはまるものを並べる)。
func Mark(t board.Topic) string {
	var marks []string
	if t.Urgent {
		marks = append(marks, UrgentMark+" 緊急")
	}
	if t.Has(board.KindAction) {
		marks = append(marks, "🔴 要対応")
	}
	if t.Has(board.KindMustRead) {
		marks = append(marks, "📌 必読")
	}
	if !t.Unclassified {
		marks = append(marks, stateMark[t.State])
		if m, ok := themeMark[t.Theme]; ok && t.Theme != board.ThemeOther { // 「その他」は見出しの雑音になるので出さない
			marks = append(marks, m[0]+" "+m[1])
		}
	}
	return strings.Join(marks, " ")
}

// Count は話題の発言の数 (文脈の発言を含む)。
func Count(t board.Topic) int {
	n := 0
	for _, u := range t.Units {
		n += len(u.Msgs)
	}
	return n
}

// Head は話題の見出しの行 (印・見出し・件数・最終の時刻)。
func Head(t board.Topic) string {
	head := Mark(t)
	if head != "" {
		head += ": "
	}
	return fmt.Sprintf("%s%s (%d 件, 最終 %s)", head, clean(t.Title), Count(t), stamp(tsTime(t.Last())))
}

// HeadingMark は読む画面で欄の見出し (結論・経緯・次の動き・原文) の頭に置く印。読む画面はこれで見出しの行を見分けて色を付ける。
const HeadingMark = "▍"

var sectionName = map[string]string{
	board.SectionConclusion: "結論",
	board.SectionBackground: "経緯",
	board.SectionNext:       "次の動き",
}

// Body は話題の中身の行 (ラベル・要約の欄・原文)。md が真なら Markdown の形 (見出しは太字、根拠の発言へのリンクを添える)、
// 偽なら読む画面の形 (見出しは HeadingMark で始める)。欄の間には空行を入れる。
// 行は折り返さない (画面の幅で折り返すのは読む側)。外から来た文字列は無害化してある。
func (p Page) Body(t board.Topic, md bool) []string { return p.BodySince(t, md, nil) }

// NewMark は本文で、前に読んだ所より後の発言の前に入れる区切り。
const NewMark = "    ── ここから新着 ──"

// BodySince は Body に、前に読んだ所の区切りを入れたもの。since は単位の ID → 前に読んだときの単位の最後の発言の ts
// (無い単位・空の印の単位は、全部が初めてなので区切らない)。区切りは印より後の期間内の発言の前に 1 つだけ入れる。
func (p Page) BodySince(t board.Topic, md bool, since map[string]string) []string {
	var out []string
	ref := func(refs []string) string {
		if !md {
			return ""
		}
		if l := p.links(refs, t); l != "" {
			return " " + l
		}
		return ""
	}
	heading := func(s string) string {
		if md {
			return "**" + s + "**"
		}
		return HeadingMark + s
	}
	for _, l := range t.Labels {
		out = append(out, fmt.Sprintf("- %s: %s%s", labelName(l), clean(l.Reason), ref(l.Refs)))
	}
	if len(t.Labels) > 0 {
		out = append(out, "")
	}
	for _, sec := range board.Sections {
		first := true
		for _, pt := range t.Points {
			if pt.Section != sec {
				continue
			}
			// 頭の空白を落とす: LLM の文が空白で始まると字下げが原文の発言の行 (4 桁) と同じになり、読む画面が発言の頭として色を付ける
			pt.Text = strings.TrimSpace(pt.Text)
			if first {
				out, first = append(out, heading(sectionName[sec])), false
			}
			switch {
			case md:
				out = append(out, fmt.Sprintf("- %s%s", clean(pt.Text), ref(pt.Refs)))
			case sec == board.SectionConclusion:
				out = append(out, "  "+clean(pt.Text))
			default:
				out = append(out, "  • "+clean(pt.Text))
			}
		}
		if !first {
			out = append(out, "")
		}
	}
	out = append(out, heading("原文"))
	for i, u := range t.Units {
		if md || i > 0 { // Markdown は見出しの直後に字下げの行を続けると段落の続きと読まれるので、空行で区切る
			out = append(out, "")
		}
		out = append(out, p.unit(u, md, since[u.ID])...)
	}
	if ms := p.MentionsOf(t); len(ms) > 0 {
		out = append(out, "", heading(fmt.Sprintf("この話題への言及 (%d 件)", len(ms))))
		for _, m := range ms {
			if md {
				out = append(out, "")
			}
			out = append(out, mentionLines(m, md)...)
		}
	}
	return out
}

// mentionLines は言及 1 件の行: 発言と同じ「時刻 名前: 本文」の形 (読む画面が発言の頭として色を付ける) に、チャンネルと
// 何を引用したか (自分の発言なら「あなたの発言」) を添える。
func mentionLines(m Mention, md bool) []string {
	lines := strings.Split(m.Text, "\n")
	out := []string{fmt.Sprintf("    %s %s: [#%s] %s", tsTime(m.Ts).Format("01/02 15:04"), clean(m.Name), clean(m.ChannelName), clean(lines[0]))}
	for _, l := range lines[1:] {
		out = append(out, "      "+clean(l))
	}
	whose := clean(m.TargetName) + " の発言"
	if m.TargetMine {
		whose = "あなたの発言"
	}
	cite := fmt.Sprintf("      ↳ %s (%s) へのリンク", whose, tsTime(m.Target).Format("01/02 15:04"))
	if md && m.URL != "" {
		cite += " [↗](" + clean(m.URL) + ")"
	}
	return append(out, cite)
}

func labelName(l board.Label) string {
	switch {
	case l.Kind == board.KindAction && l.Source == board.SourceMention:
		return "要対応 (名指し)"
	case l.Kind == board.KindAction && l.Source == board.SourceReply:
		return "要対応 (あなたのスレッドへの返信)"
	case l.Kind == board.KindAction:
		return "要対応? (推定)"
	}
	return "必読 (推定)"
}

func (p Page) unit(u board.Unit, links bool, since string) []string {
	var out []string
	link := ""
	if links {
		if l := p.link(u.ParentTs(), u); l != "" {
			link = " " + l
		}
	}
	where := ""
	if u.Ch != "" {
		where = "#" + clean(p.memberName(u.Ch)) + " " // グループの板: どのチャンネルの発言か
	}
	switch {
	case u.Continuing:
		out = append(out, fmt.Sprintf("%s🔁 続いているスレッド (始まり %s)%s", where, stamp(tsTime(u.ParentTs())), link))
	case u.Thread:
		out = append(out, where+"💬 スレッド"+link)
	case where != "":
		out = append(out, strings.TrimSpace(where)+link)
	}
	marked := since == ""
	for i, m := range u.Msgs {
		if i == 1 && u.Omitted > 0 {
			out = append(out, fmt.Sprintf("    … 期間前の返信 %d 件を省略", u.Omitted))
		}
		if !marked && m.New && board.TsBefore(since, m.Ts) {
			out, marked = append(out, NewMark), true
		}
		prefix := "    "
		if !m.New {
			prefix = "    (前) "
		}
		lines := strings.Split(m.Text, "\n")
		out = append(out, fmt.Sprintf("%s%s %s: %s", prefix, tsTime(m.Ts).Format("01/02 15:04"), clean(m.Name), clean(lines[0])))
		// 続きの行は時刻の頭から 2 桁下げる。prefix は全角の「前」を含むので、バイト数ではなく表示幅で数える
		for _, l := range lines[1:] {
			out = append(out, strings.Repeat(" ", termwidth.Of(prefix)+2)+clean(l))
		}
	}
	if u.Truncated {
		out = append(out, "    🚨 返信が多く、途中で切れている")
	}
	if u.Unreadable {
		out = append(out, "    🚨 スレッドを読めなかった (親とチャンネルにも投稿された返信だけを出している)")
	}
	return out
}

// links は根拠の発言へのリンク。話題の中のどの単位の発言かを引いて permalink を組む。
func (p Page) links(refs []string, t board.Topic) string {
	var out []string
	for _, r := range refs {
		for _, u := range t.Units {
			if !hasMsg(u, r) {
				continue
			}
			if l := p.link(r, u); l != "" {
				out = append(out, l)
			}
			break
		}
	}
	return strings.Join(out, " ")
}

func hasMsg(u board.Unit, ts string) bool {
	for _, m := range u.Msgs {
		if m.Ts == ts {
			return true
		}
	}
	return false
}

// link は ts の発言への Markdown のリンク。history / thread の発言には permalink が無いので組み立てる
// (https://<workspace>.slack.com/archives/<channel>/p<ts から . を除いた値>、スレッドの返信は thread_ts を足す)。
func (p Page) link(ts string, u board.Unit) string {
	if url := p.URL(ts, u); url != "" {
		return "[↗](" + url + ")"
	}
	return ""
}

// URL は ts の発言の permalink (Base が分からなければ空)。グループの板では単位のチャンネル (u.Ch) で組む。
func (p Page) URL(ts string, u board.Unit) string {
	if p.Base == "" {
		return ""
	}
	ch := p.Channel
	if u.Ch != "" {
		ch = u.Ch
	}
	url := fmt.Sprintf("%sarchives/%s/p%s", p.Base, ch, strings.ReplaceAll(ts, ".", ""))
	if parent := u.ParentTs(); u.Thread && ts != parent {
		url += fmt.Sprintf("?thread_ts=%s&cid=%s", parent, ch)
	}
	return clean(url)
}

// memberName はグループの板のチャンネル ch の名前 (分からなければ ID)。
func (p Page) memberName(ch string) string {
	if n := p.Members[ch]; n != "" {
		return n
	}
	return ch
}

func clean(s string) string { return termsafe.PlainLine(s) }

func stamp(t time.Time) string { return t.Format("01/02 15:04") }

func tsTime(ts string) time.Time { return board.TsTime(ts) }
