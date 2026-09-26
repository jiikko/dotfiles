package eventlog

// プロセスの出来事 (設定画面のログのタブ。issue 512)。events.jsonl の出来事のうち、プロセスの一生 (起動・落ちた・止めた・起こし直した・
// 入れ替わった・取り込んだ) に当たるものを選び、役 (dispatcher / supervisor / 見張り / PM / 取り込み / PG / 画面) を付け、
// 同じ出来事の繰り返しを 1 つに畳む。出どころは pro-con log と同じ (Follower が読んだ Event)。

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// 役の名前 (pro-con ps の役と同じ語)。
const (
	RoleDispatcher = "dispatcher"
	RoleSupervisor = "supervisor"
	RoleMonitor    = "見張り"
	RolePM         = "PM"
	RoleIntegrator = "取り込み"
	RolePG         = "PG"
	RoleScreen     = "画面"
)

// カードの欄の、PG でない役 (dispatcher.PMCardID / IntegratorCardID と同じ値。eventlog は dispatcher を import できない)。
const (
	pmCard         = "PM"
	integratorCard = "INT"
)

// Role は e がプロセスの出来事なら、その役を返す (カードの状態の変化・テストの係の実行などは ok = false)。
func Role(e Event) (role string, ok bool) {
	switch e.Kind {
	case KindSupervisor:
		return RoleSupervisor, true
	case KindMonitor: // 見張りの知らせ (取り込みの衝突・テストの順番の長さ) はプロセスの出来事ではない。見張り自身の出来事の文は「見張り」で始まる
		return RoleMonitor, strings.HasPrefix(e.Reason, RoleMonitor)
	case KindScreen:
		return RoleScreen, true
	case KindDispatcher, KindUpgrade, KindScreens, KindRecover, KindError:
		return RoleDispatcher, true
	case KindRegister, KindSuspect, KindCrash, KindWatchdog:
		return cardRole(e.Card, RolePG), true
	case KindLaunch, KindStop:
		return cardRole(e.Card, RoleDispatcher), true
	}
	return "", false
}

// cardRole はカードの欄から役を決める (カードが無ければ none)。
func cardRole(cardID, none string) string {
	switch {
	case cardID == pmCard:
		return RolePM
	case cardID == integratorCard:
		return RoleIntegrator
	case cardID != "":
		return RolePG
	}
	return none
}

// FoldGap は、同じ出来事がこの間を空けずに繰り返したら 1 つに畳む長さ (これより間が空いたら別の行)。
// 実測 (2026-09-26): 同じ失敗の繰り返しは 3 秒ごと (Tick ごと)。止め直しは 10 秒ごと。🚨 長くしない: 10 分にすると、別の起動の
// 「起動時の確かめ」(数分おきの起こし直し・入れ替え) まで 1 つに畳まれ、起こし直した回数が見えなくなる
const FoldGap = 2 * time.Minute

// Folded は畳んだ出来事 (Last が最後の 1 件。N は畳んだ数、First は最初の 1 件の時刻)。
type Folded struct {
	Last  Event
	Role  string
	N     int
	First time.Time
}

// Fold は出来事を時刻順に並べ (ファイルの中では受付の箱を経た出来事が前後しうる)、role が ok を返すものだけを残し、同じ出来事
// (種類・カード・数字を除いた文が同じ) の FoldGap 以内の繰り返しを 1 つに畳む。畳んだ行は最後の 1 件の位置に並ぶ。
func Fold(evs []Event, role func(Event) (string, bool)) []Folded {
	sorted := make([]Event, len(evs))
	copy(sorted, evs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	type group struct {
		f   Folded
		pos int // 最後の 1 件の位置
	}
	var groups []*group
	open := map[string]*group{}
	for i, e := range sorted {
		r, ok := role(e)
		if !ok {
			continue
		}
		k := foldKey(e)
		if g := open[k]; g != nil && e.At.Sub(g.f.Last.At) <= FoldGap {
			g.f.Last, g.f.N, g.pos = e, g.f.N+1, i
			continue
		}
		g := &group{f: Folded{Last: e, Role: r, N: 1, First: e.At}, pos: i}
		groups = append(groups, g)
		open[k] = g
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].pos < groups[j].pos })
	out := make([]Folded, len(groups))
	for i, g := range groups {
		out[i] = g.f
	}
	return out
}

// digits は文の中の数字 (pid・回数・時刻)。カード ID (C-012) の数字は残す (別のカードの出来事を 1 つにしない)。
var digits = regexp.MustCompile(`C-[0-9]+|[0-9]+`)

func foldKey(e Event) string {
	masked := digits.ReplaceAllStringFunc(e.Reason, func(s string) string {
		if strings.HasPrefix(s, "C-") {
			return s
		}
		return "#"
	})
	return e.Kind + "\x00" + e.Card + "\x00" + masked
}

// AnyRole は Role と同じ役を返し、プロセスの出来事でないもの (カードの状態の変化・見張りの知らせ) にも役を付ける
// (ログのタブの「カードの出来事も出す」)。見張りの知らせは見張り、ほかはカードの欄から (カードが無ければ dispatcher)。
func AnyRole(e Event) (string, bool) {
	if r, ok := Role(e); ok || r != "" {
		return r, true
	}
	return cardRole(e.Card, RoleDispatcher), true
}
