// Package render は板 (話題の列) を掲示板風の Markdown にする。
//
// 原文は書き換えない (並べ方・印・リンクを足すだけ)。Slack の本文・名前・LLM の見出しは外から来た文字列なので、
// 端末の制御文字を termsafe で落としてから出す。
package render

import (
	"fmt"
	"io"
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
	fmt.Fprintf(&sb, "# #%s — %s 〜 %s (発言 %d / 話題 %d)\n\n", clean(p.ChannelName), stamp(p.Since), stamp(p.Until), msgs, countTopics(p.Topics))
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

// Mark は話題の印 (要対応 / 必読 / 状態の順に、当てはまるものを並べる)。
func Mark(t board.Topic) string {
	var marks []string
	if t.Has(board.KindAction) {
		marks = append(marks, "🔴 要対応")
	}
	if t.Has(board.KindMustRead) {
		marks = append(marks, "📌 必読")
	}
	if !t.Unclassified {
		marks = append(marks, stateMark[t.State])
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
func (p Page) Body(t board.Topic, md bool) []string {
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
		out = append(out, p.unit(u, md)...)
	}
	return out
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

func (p Page) unit(u board.Unit, links bool) []string {
	var out []string
	link := ""
	if links {
		if l := p.link(u.ID, u); l != "" {
			link = " " + l
		}
	}
	if u.Continuing {
		out = append(out, fmt.Sprintf("🔁 続いているスレッド (始まり %s)%s", stamp(tsTime(u.ID)), link))
	} else if u.Thread {
		out = append(out, "💬 スレッド"+link)
	}
	for i, m := range u.Msgs {
		if i == 1 && u.Omitted > 0 {
			out = append(out, fmt.Sprintf("    … 期間前の返信 %d 件を省略", u.Omitted))
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

// URL は ts の発言の permalink (Base が分からなければ空)。
func (p Page) URL(ts string, u board.Unit) string {
	if p.Base == "" {
		return ""
	}
	url := fmt.Sprintf("%sarchives/%s/p%s", p.Base, p.Channel, strings.ReplaceAll(ts, ".", ""))
	if u.Thread && ts != u.ID {
		url += fmt.Sprintf("?thread_ts=%s&cid=%s", u.ID, p.Channel)
	}
	return clean(url)
}

func clean(s string) string { return termsafe.PlainLine(s) }

func stamp(t time.Time) string { return t.Format("01/02 15:04") }

func tsTime(ts string) time.Time { return board.TsTime(ts) }
