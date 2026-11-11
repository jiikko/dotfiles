// Package board は会話の発言を「単位」(スレッド 1 本、またはスレッド外の発言 1 件) にまとめ直し、LLM の分類を検査して
// 話題ごとの板 (Board) に組む。
//
// LLM に任せるのは「どの単位がどの話題か」と見出し・要点・ラベルだけで、本文は原文を並べる。LLM の応答は信じずに
// 検査する: 全部の単位がちょうど 1 つの話題に入る (落ちた単位は「未分類」へ、無い ID は捨てる)、要点とラベルの根拠は
// その話題の中の発言に限る。
package board

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"slack-bbs/source"
)

// Msg は表示と LLM の入力に使う発言 1 件。
type Msg struct {
	Ts   string `json:"ts"`
	User string `json:"user"`  // user ID (ボットは bot ID か username)
	Name string `json:"name"`  // 表示名
	Text string `json:"text"`  // mrkdwn をほどいた本文
	New  bool   `json:"new"`   // 期間内の発言か (false は文脈として出す期間前の発言)
	Mine bool   `json:"mine"`  // 自分の発言か
	ToMe bool   `json:"to_me"` // 自分を名指ししているか (mrkdwn をほどく前の本文で判定する)
}

// Unit は分類の単位。スレッドは既に 1 つの話題なので割らない。
type Unit struct {
	ID string `json:"id"` // スレッドは親の ts、スレッド外の発言はその ts。グループの単位は "<チャンネル ID>/<ts>" (InChannel)
	// Ch はグループ (複数のチャンネルをまとめた購読) の単位のチャンネル ID。単独のチャンネルの単位では空 (板のチャンネル)
	Ch         string `json:"ch,omitempty"`
	Thread     bool   `json:"thread"`
	Continuing bool   `json:"continuing"` // 期間より前に始まり、期間内に返信が付いたスレッド
	Omitted    int    `json:"omitted"`    // 文脈として出さなかった期間前の返信の数
	Truncated  bool   `json:"truncated"`  // スレッドの返信が上限で切れている
	// MineOmitted は、出さなかった期間前の返信に自分の発言があったか (「自分が書いたスレッド」の判定を、省いた文脈で落とさない)
	MineOmitted bool `json:"mine_omitted"`
	// Unreadable はスレッドを読めなかった (消えた・取得の失敗) こと。親と「チャンネルにも投稿」の返信だけを出す
	Unreadable bool  `json:"unreadable,omitempty"`
	Msgs       []Msg `json:"messages"`
}

// InChannel はチャンネル ch の単位を、グループの板に入れる形にする: ID を "<ch>/<ts>" にしてチャンネルを持たせる
// (ts はチャンネルの中でしか一意でない。単独のチャンネルの単位の ID は ts のまま変えない: 既読の印とキャッシュの鍵が変わる)。
func (u Unit) InChannel(ch string) Unit {
	u.ID, u.Ch = ch+"/"+u.ID, ch
	return u
}

// ParentTs は単位の始まりの ts (スレッドは親の ts)。ID からチャンネルの部分を除いたもの。
func (u Unit) ParentTs() string {
	if u.Ch != "" {
		if ts, ok := strings.CutPrefix(u.ID, u.Ch+"/"); ok {
			return ts
		}
	}
	return u.ID
}

// Last は単位の最後の発言の ts。
func (u Unit) Last() string { return u.Msgs[len(u.Msgs)-1].Ts }

// OldContext は「続いている」スレッドで、期間前の返信を文脈として何件まで出すか (親は別に必ず出す)。
// 全部出すと古い長いスレッドで LLM に送る量が膨らみ、出さないと新しい返信が何への返事か分からない。
const OldContext = 5

// Input はまとめ直しの材料。
type Input struct {
	History []source.Message            // チャンネルの history (遡る期間の分)
	Threads map[string][]source.Message // 親の ts → スレッド (親を含む古い順)
	Since   string                      // 期間の始まりの ts
	Me      string                      // 自分の user ID
	Names   map[string]string           // user ID → 表示名
}

// NeedThreads はスレッドを取り直す親の ts。親か最後の返信が期間内のスレッドと、期間内に「チャンネルにも投稿」された返信の
// スレッド (親が遡る範囲より前で history に無くても、チャンネルの流れに見えている発言を落とさない)。
// 返信の無い親や、動きが期間より前で止まったスレッドは取らない。
func NeedThreads(history []source.Message, since string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range history {
		var ts string
		switch {
		case m.IsThreadParent() && m.ReplyCount > 0 && (!TsBefore(m.Ts, since) || !TsBefore(m.LatestReply, since)):
			ts = m.Ts
		case m.IsBroadcastReply() && !TsBefore(m.Ts, since):
			ts = m.ThreadTs
		}
		if ts != "" && !seen[ts] {
			seen[ts] = true
			out = append(out, ts)
		}
	}
	return out
}

// Build は材料を単位にまとめ直す。単位は最初の発言の古い順。同じ ts の発言は 1 つの単位にしか入らない。
func Build(in Input) ([]Unit, error) {
	need := NeedThreads(in.History, in.Since)
	needSet := map[string]bool{}
	for _, ts := range need {
		needSet[ts] = true
	}
	parents := map[string]source.Message{}
	broadcast := map[string][]source.Message{}
	for _, m := range in.History {
		switch {
		case m.IsThreadParent():
			parents[m.Ts] = m
		case m.IsBroadcastReply():
			broadcast[m.ThreadTs] = append(broadcast[m.ThreadTs], m)
		}
	}
	var units []Unit
	for _, ts := range need {
		units = append(units, in.thread(ts, parents, broadcast[ts]))
	}
	for _, m := range in.History {
		if m.IsBroadcastReply() || needSet[m.Ts] || TsBefore(m.Ts, in.Since) {
			continue // 返信はスレッドの側に出る。期間内の「チャンネルにも投稿」の返信は NeedThreads がスレッドを取り直している
		}
		units = append(units, Unit{ID: m.Ts, Msgs: []Msg{in.msg(m)}})
	}
	sort.SliceStable(units, func(i, j int) bool { return TsBefore(units[i].Msgs[0].Ts, units[j].Msgs[0].Ts) })
	if err := ValidateUnits(units); err != nil {
		return nil, err
	}
	return units, nil
}

// ValidateUnits は単位の列の形を確かめる: 単位の ID が空でなく重複せず、どの単位にも発言があり、同じチャンネルの同じ ts の発言が 2 回出ない
// (グループの板では、別のチャンネルの同じ ts は別の発言)。
// Build の結果のほか、2 段の実行で外から読み戻した単位にも当てる (読み戻したファイルは書き換えられうる)。
func ValidateUnits(units []Unit) error {
	ids := map[string]bool{}
	seen := map[string]string{}
	for _, u := range units {
		if u.ID == "" || ids[u.ID] {
			return fmt.Errorf("単位の ID %q が空か重複している", u.ID)
		}
		ids[u.ID] = true
		if len(u.Msgs) == 0 {
			return fmt.Errorf("単位 %s に発言が無い", u.ID)
		}
		for _, m := range u.Msgs {
			key := u.Ch + "/" + m.Ts
			if prev, ok := seen[key]; ok {
				return fmt.Errorf("発言 %s が単位 %s と %s の両方に入った", m.Ts, prev, u.ID)
			}
			seen[key] = u.ID
		}
	}
	return nil
}

// thread はスレッド 1 本の単位。parents は history にあった親 (遡る範囲より前の親は無い)、broadcast は history にあった
// 「チャンネルにも投稿」の返信で、スレッドを読めなかったときに代わりに出す (読めたスレッドの発言を落とさない)。
func (in Input) thread(parentTs string, parents map[string]source.Message, broadcast []source.Message) Unit {
	u := Unit{ID: parentTs, Thread: true, Continuing: TsBefore(parentTs, in.Since)}
	parent, known := parents[parentTs]
	msgs := in.Threads[parentTs]
	if len(msgs) == 0 {
		u.Unreadable = true
		if known {
			msgs = append(msgs, parent)
		}
		msgs = append(msgs, broadcast...)
	}
	if len(msgs) >= source.ThreadLimit {
		u.Truncated = true
	}
	var old []source.Message
	omit := func(ms []source.Message) {
		u.Omitted += len(ms)
		for _, o := range ms {
			if in.Me != "" && o.User == in.Me {
				u.MineOmitted = true
			}
		}
	}
	for _, m := range msgs {
		switch {
		case m.Ts == parentTs:
			u.Msgs = append(u.Msgs, in.msg(m))
		case TsBefore(m.Ts, in.Since):
			old = append(old, m)
		default:
			cut := max(len(old)-OldContext, 0)
			omit(old[:cut])
			for _, o := range old[cut:] {
				u.Msgs = append(u.Msgs, in.msg(o))
			}
			old = nil
			u.Msgs = append(u.Msgs, in.msg(m))
		}
	}
	omit(old) // 期間内の返信が後ろに無い (上限で切れた等) ときの期間前の返信は出さない
	if len(u.Msgs) == 0 && !known {
		// 親が history に無く、読めた返信が全部期間前で省いた (上限で切れた等) とき: 期間内に見えていた返信だけは出す
		for _, b := range broadcast {
			u.Msgs = append(u.Msgs, in.msg(b))
		}
		u.Unreadable = true
	}
	if known && (len(u.Msgs) == 0 || u.Msgs[0].Ts != parentTs) {
		// スレッドの応答に親が無いことは無いはずだが、無ければ history の親を先頭に置く (見出しの文脈を失わない)
		u.Msgs = append([]Msg{in.msg(parent)}, u.Msgs...)
	}
	return u
}

func (in Input) msg(m source.Message) Msg {
	id := m.User
	if id == "" {
		id = m.BotID
	}
	name := in.Names[m.User]
	switch {
	case name != "":
	case m.Username != "":
		name = m.Username
	case id != "":
		name = id
	default:
		name = "(不明)"
	}
	text := Plain(m.Text, in.Names)
	if strings.TrimSpace(text) == "" {
		text = "(添付のみ)"
	}
	return Msg{
		Ts: m.Ts, User: id, Name: name, Text: text,
		New:  !TsBefore(m.Ts, in.Since),
		Mine: in.Me != "" && m.User == in.Me,
		ToMe: Mentions(m.Text, in.Me),
	}
}

// TsBefore は Slack の ts a が b より前か。ts は "秒.マイクロ秒" で、float にすると桁が足りないので整数の組で比べる。
// 読めない ts は空の ts と同じく最も古いものとして扱う (欄の無い latest_reply は「動きが無い」)。
func TsBefore(a, b string) bool {
	as, au := parseTs(a)
	bs, bu := parseTs(b)
	if as != bs {
		return as < bs
	}
	return au < bu
}

func parseTs(ts string) (sec, usec int64) {
	s, u, _ := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return -1, 0
	}
	if u != "" {
		u = (u + "000000")[:6]
		usec, _ = strconv.ParseInt(u, 10, 64)
	}
	return sec, usec
}

// TsTime は ts の時刻 (秒まで)。読めない ts は Unix 時刻 0。
func TsTime(ts string) time.Time {
	sec, _ := parseTs(ts)
	return time.Unix(max(sec, 0), 0)
}

// TsOf は Unix 秒を Slack の ts の形にする。
func TsOf(unix int64) string { return strconv.FormatInt(unix, 10) + ".000000" }
