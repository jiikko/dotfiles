package board

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Response は LLM に返させる分類 (llm.Schema と同じ形)。信じずに Assemble で検査する。
type Response struct {
	Topics []RespTopic `json:"topics"`
}

// RespTopic は LLM が返した話題 1 つ。要約は欄 (結論・経緯・次の動き) に分けて返させる。
type RespTopic struct {
	Title      string      `json:"title"`
	State      string      `json:"state"`
	Theme      string      `json:"theme"`      // Themes のどれか
	Urgent     bool        `json:"urgent"`     // 今すぐ手を打たないとまずい話か (障害・お客さんへの影響・セキュリティ・今日が期限)
	Members    []string    `json:"members"`    // 単位の ID
	Conclusion RespPoint   `json:"conclusion"` // 1 項目 (空なら無し)
	Background []RespPoint `json:"background"`
	Next       []RespPoint `json:"next"`
	Labels     []RespLabel `json:"labels"`
}

// 要約の欄。並びは表示の順。
const (
	SectionConclusion = "conclusion" // 結論 (今どうなっているか・何が決まったか)
	SectionBackground = "background" // 経緯
	SectionNext       = "next"       // 次の動き (誰が何をするか・答えの出ていない問い)
)

// Sections は要約の欄を表示の順に並べたもの。
var Sections = []string{SectionConclusion, SectionBackground, SectionNext}

// sectioned は欄の名前を付けた項目。
type sectioned struct {
	section string
	RespPoint
}

// points は要約の項目を欄の順に並べる。本文の無い結論は「無し」として出さない (根拠があっても「捨てたもの」に数えない)。
func (rt RespTopic) points() []sectioned {
	var out []sectioned
	if c := rt.Conclusion; c.Text != "" {
		out = append(out, sectioned{SectionConclusion, c})
	}
	for _, p := range rt.Background {
		out = append(out, sectioned{SectionBackground, p})
	}
	for _, p := range rt.Next {
		out = append(out, sectioned{SectionNext, p})
	}
	return out
}

// RespPoint は要約の項目 1 つと根拠の発言の ts。
type RespPoint struct {
	Text string   `json:"text"`
	Refs []string `json:"refs"`
}

// RespLabel は LLM が付けたラベル (要対応 / 必読) と理由・根拠。
type RespLabel struct {
	Kind   string   `json:"kind"`
	Reason string   `json:"reason"`
	Refs   []string `json:"refs"`
}

// 話題の状態。
const (
	StateDecided = "decided" // 決まった
	StateOpen    = "open"    // 議論中・未回答の質問がある
	StateInfo    = "info"    // 共有のみ
)

// 話題のテーマ。並びは LLM への指示に出す順。
const (
	ThemeIncident   = "incident"   // 障害・不具合
	ThemeRelease    = "release"    // リリース・デプロイ
	ThemeQuestion   = "question"   // 質問・相談
	ThemeDiscussion = "discussion" // 議論・設計
	ThemeAnnounce   = "announce"   // 告知
	ThemeSchedule   = "schedule"   // 予定・会議
	ThemeOps        = "ops"        // 作業・運用
	ThemeMetrics    = "metrics"    // 数字・分析
	ThemeMoney      = "money"      // お金・契約
	ThemePeople     = "people"     // 人・体制
	ThemeChat       = "chat"       // 雑談
	ThemeOther      = "other"      // その他
)

// Themes はテーマの一覧 (LLM の応答の形と、知らない値の検査に使う)。
var Themes = []string{ThemeIncident, ThemeRelease, ThemeQuestion, ThemeDiscussion, ThemeAnnounce, ThemeSchedule,
	ThemeOps, ThemeMetrics, ThemeMoney, ThemePeople, ThemeChat, ThemeOther}

// ラベルの種類。
const (
	KindAction   = "action"    // 要対応
	KindMustRead = "must_read" // 必読
)

// ラベルの出所。機械で確かめたもの (mention / reply) と LLM の判断 (llm) を見分けて出す。
const (
	SourceMention = "mention" // 自分への名指し
	SourceReply   = "reply"   // 自分が書いたスレッドへの、期間内の他人の返信
	SourceLLM     = "llm"
)

// Point は検査を通った要約の項目。Section はどの欄か (Sections のどれか)。
type Point struct {
	Section string   `json:"section"`
	Text    string   `json:"text"`
	Refs    []string `json:"refs"`
}

// Label は検査を通ったラベル。
type Label struct {
	Kind   string   `json:"kind"`
	Source string   `json:"source"`
	Reason string   `json:"reason"`
	Refs   []string `json:"refs"`
}

// Topic は板の話題 1 つ。Unclassified は LLM が落とした単位を集めた「未分類」。
type Topic struct {
	Title        string  `json:"title"`
	State        string  `json:"state"`
	Theme        string  `json:"theme"`            // Themes のどれか (未分類は other)
	Urgent       bool    `json:"urgent,omitempty"` // LLM の判断で緊急
	Unclassified bool    `json:"unclassified,omitempty"`
	Labels       []Label `json:"labels"`
	Points       []Point `json:"points"`
	Units        []Unit  `json:"units"`
}

// Has は話題に kind のラベルがあるか。
func (t Topic) Has(kind string) bool {
	return slices.ContainsFunc(t.Labels, func(l Label) bool { return l.Kind == kind })
}

// Last は話題の最後の発言の ts。
func (t Topic) Last() string {
	last := ""
	for _, u := range t.Units {
		if l := u.Last(); TsBefore(last, l) {
			last = l
		}
	}
	return last
}

// Dropped は検査で捨てた LLM の出力の数 (利用者に warn で知らせる)。
type Dropped struct {
	UnknownMembers   int `json:"unknown_members"`   // 入力に無い単位の ID
	DuplicateMembers int `json:"duplicate_members"` // 2 つ目以降の話題に入れられた単位
	Unclassified     int `json:"unclassified"`      // どの話題にも入らなかった単位
	EmptyTopics      int `json:"empty_topics"`      // 単位が 1 つも残らなかった話題
	BadState         int `json:"bad_state"`         // 知らない状態 (議論中として扱う)
	BadTheme         int `json:"bad_theme"`         // 知らないテーマ (その他として扱う)
	Points           int `json:"points"`            // 話題の中に根拠が無い要点
	Labels           int `json:"labels"`            // 話題の中に根拠が無い・知らない種類のラベル
}

// Any は何か捨てたか。
func (d Dropped) Any() bool { return d != Dropped{} }

// String は 0 でない数だけを並べる (warn に出す)。
func (d Dropped) String() string {
	var parts []string
	for _, f := range []struct {
		n    int
		what string
	}{
		{d.UnknownMembers, "入力に無い単位"}, {d.DuplicateMembers, "2 つ目の話題にも入れた単位"},
		{d.Unclassified, "どの話題にも入らず未分類へ回した単位"}, {d.EmptyTopics, "単位が残らなかった話題"},
		{d.BadState, "知らない状態 (議論中にした)"}, {d.BadTheme, "知らないテーマ (その他にした)"}, {d.Points, "根拠の無い要点"}, {d.Labels, "根拠の無い・知らないラベル"},
	} {
		if f.n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", f.what, f.n))
		}
	}
	return strings.Join(parts, "・")
}

// Assemble は単位と LLM の分類から板の話題を組む。
//
// 守ること (どれも LLM の出力に依らず成り立つ):
//   - 全部の単位がちょうど 1 つの話題に入る。入力に無い ID は捨て、2 回目以降に出た ID は捨て、どこにも入らなかった単位は「未分類」へ
//   - 要点とラベルの根拠は、その話題に入った単位の発言の ts に限る。根拠が 1 つも残らない要点・ラベルは捨てる
//   - 機械で確かめたラベル (名指し・自分のスレッドへの返信) は LLM に依らず付ける
//
// 並びは 緊急 → 要対応 → 必読 → その他 (それぞれ最後の発言が新しい順)、未分類は最後。
func Assemble(units []Unit, resp Response) ([]Topic, Dropped) {
	byID := make(map[string]Unit, len(units))
	for _, u := range units {
		byID[u.ID] = u
	}
	placed := map[string]bool{}
	var d Dropped
	var topics []Topic
	for _, rt := range resp.Topics {
		t := Topic{Title: rt.Title, State: rt.State, Theme: rt.Theme, Urgent: rt.Urgent}
		for _, id := range rt.Members {
			u, ok := byID[id]
			switch {
			case !ok:
				d.UnknownMembers++
			case placed[id]:
				d.DuplicateMembers++
			default:
				placed[id] = true
				t.Units = append(t.Units, u)
			}
		}
		if len(t.Units) == 0 {
			d.EmptyTopics++
			continue
		}
		if t.State != StateDecided && t.State != StateOpen && t.State != StateInfo {
			d.BadState++
			t.State = StateOpen
		}
		if !slices.Contains(Themes, t.Theme) {
			d.BadTheme++
			t.Theme = ThemeOther
		}
		own := tsSet(t.Units)
		for _, p := range rt.points() {
			if refs := keep(p.Refs, own); len(refs) > 0 && p.Text != "" {
				t.Points = append(t.Points, Point{Section: p.section, Text: p.Text, Refs: refs})
				continue
			}
			d.Points++
		}
		for _, l := range rt.Labels {
			refs := keep(l.Refs, own)
			if (l.Kind != KindAction && l.Kind != KindMustRead) || len(refs) == 0 {
				d.Labels++
				continue
			}
			t.Labels = append(t.Labels, Label{Kind: l.Kind, Source: SourceLLM, Reason: l.Reason, Refs: refs})
		}
		topics = append(topics, t)
	}
	var rest []Unit
	for _, u := range units {
		if !placed[u.ID] {
			rest = append(rest, u)
		}
	}
	d.Unclassified = len(rest)
	for i := range topics {
		topics[i].Labels = append(mechanicalLabels(topics[i].Units), topics[i].Labels...)
		sortUnits(topics[i].Units)
	}
	sort.SliceStable(topics, func(i, j int) bool {
		if a, b := rank(topics[i]), rank(topics[j]); a != b {
			return a < b
		}
		return TsBefore(topics[j].Last(), topics[i].Last())
	})
	if len(rest) > 0 {
		u := Topic{Title: "未分類", State: StateInfo, Theme: ThemeOther, Unclassified: true, Units: rest}
		u.Labels = mechanicalLabels(rest)
		sortUnits(u.Units)
		topics = append(topics, u)
	}
	return topics, d
}

func rank(t Topic) int {
	switch {
	case t.Urgent:
		return -1
	case t.Has(KindAction):
		return 0
	case t.Has(KindMustRead):
		return 1
	}
	return 2
}

func sortUnits(us []Unit) {
	sort.SliceStable(us, func(i, j int) bool { return TsBefore(us[i].Msgs[0].Ts, us[j].Msgs[0].Ts) })
}

func tsSet(us []Unit) map[string]bool {
	s := map[string]bool{}
	for _, u := range us {
		for _, m := range u.Msgs {
			s[m.Ts] = true
		}
	}
	return s
}

func keep(refs []string, own map[string]bool) []string {
	var out []string
	for _, r := range refs {
		if own[r] && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// mechanicalLabels は LLM に依らず確かめられる「要対応」: 期間内に自分を名指しした他人の発言と、
// 自分が 1 度でも書いたスレッドに期間内に付いた他人の返信。どちらも、同じ単位で自分がその後に書いていれば済んだものとして外す。
func mechanicalLabels(us []Unit) []Label {
	var mention, reply []string
	for _, u := range us {
		mine := u.MineOmitted // 省いた返信はどれも期間前なので、期間内の返信より前に書いたものとして数える
		for _, m := range u.Msgs {
			if m.Mine {
				mine = true
				inUnit := func(ts string) bool { return tsIn(u, ts) } // 自分が返した後なら済んでいる
				reply = slices.DeleteFunc(reply, inUnit)
				mention = slices.DeleteFunc(mention, inUnit)
				continue
			}
			if !m.New {
				continue
			}
			if m.ToMe {
				mention = append(mention, m.Ts)
			} else if u.Thread && mine {
				reply = append(reply, m.Ts)
			}
		}
	}
	var out []Label
	if len(mention) > 0 {
		out = append(out, Label{Kind: KindAction, Source: SourceMention, Reason: fmt.Sprintf("あなたへの名指しが %d 件", len(mention)), Refs: mention})
	}
	if len(reply) > 0 {
		out = append(out, Label{Kind: KindAction, Source: SourceReply, Reason: fmt.Sprintf("あなたが書いたスレッドに返信が %d 件", len(reply)), Refs: reply})
	}
	return out
}

func tsIn(u Unit, ts string) bool {
	return slices.ContainsFunc(u.Msgs, func(m Msg) bool { return m.Ts == ts })
}

// Prune は前回の分類 resp を今の単位 units に当てられる形に絞る: 今は無い単位の ID と根拠の ts を外し、根拠の無くなった要点・ラベルと
// 単位の無くなった話題を外す。期間が進んで古い発言が外れただけのときに前回の分類を使い回すためで、外れたものを Assemble の
// 「捨てたもの」(LLM の誤り) に数えない。入力に無い ID を LLM が作った誤りも、ここで外れて数えられなくなるので、
// LLM を呼んだ直後の応答には当てない (キャッシュから読み戻した応答にだけ当てる)。
func Prune(resp Response, units []Unit) Response {
	ids := map[string]bool{}
	for _, u := range units {
		ids[u.ID] = true
	}
	own := tsSet(units)
	var out Response
	for _, rt := range resp.Topics {
		t := RespTopic{Title: rt.Title, State: rt.State, Theme: rt.Theme, Urgent: rt.Urgent}
		for _, id := range rt.Members {
			if ids[id] {
				t.Members = append(t.Members, id)
			}
		}
		if len(t.Members) == 0 {
			continue
		}
		prune := func(p RespPoint) (RespPoint, bool) {
			refs := keep(p.Refs, own)
			return RespPoint{Text: p.Text, Refs: refs}, len(refs) > 0
		}
		if c, ok := prune(rt.Conclusion); ok {
			t.Conclusion = c
		}
		for _, p := range rt.Background {
			if p, ok := prune(p); ok {
				t.Background = append(t.Background, p)
			}
		}
		for _, p := range rt.Next {
			if p, ok := prune(p); ok {
				t.Next = append(t.Next, p)
			}
		}
		for _, l := range rt.Labels {
			if refs := keep(l.Refs, own); len(refs) > 0 {
				t.Labels = append(t.Labels, RespLabel{Kind: l.Kind, Reason: l.Reason, Refs: refs})
			}
		}
		out.Topics = append(out.Topics, t)
	}
	return out
}
