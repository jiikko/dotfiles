package lanmgr

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"

	"mac-lan-manager/reach"
	"mac-lan-manager/setup"
)

// 疎通の確認 (p)。管理対象の相手の名前 (Bonjour の <host>.local と有線の名前) ごとに、引けた IP へ ping と ssh の口 (22 番) を
// 確かめる。名前が引けることと届くことは別 (有線の名前の広告は、その IP を持つ I/F が無くなった後も残る。wiredCheck は引けるかだけを見る)。
// 帯域を使わず数秒で終わるので確認の板は通さない (t との違い)。相手ごとに同時に 1 本 (同じ相手を重ねて確かめても結果は変わらない)。
// 一覧を開いた (Reload) ときと、結果の無い・確かめた名前が今と違う管理対象の行には自分で確かめる (autoReach。一覧の「疎通」の列。
// ユーザー指示 2026-10-20: 開いたときに確かめてアイコンで出す。名前を引くだけだと、広告だけが残った有線の名前を ✓ と出すので ping まで見る)。

// reachRun は確かめている途中の 1 回。
type reachRun struct {
	names  []string // 確かめている名前 (reachResult.names と比べて、行の名前が変わったら確かめ直す)
	lanIP  string   // 確かめ始めたときの内部 IP (reachResult.lanIP)
	tell   bool     // 終わったら知らせを出す (p で頼んだ回)
	ch     <-chan []reach.Name
	cancel context.CancelFunc
}

// reachResult は相手ごとに覚えた最後の結果 (時刻を一緒に出し、古い値を今の値と読ませない)。lanIP は確かめたときの内部 IP
// (有線の名前が違う IP に引けたかを、確かめた時点の値で比べる)。
type reachResult struct {
	names []reach.Name
	asked []string // 確かめた名前 (autoReach が今の名前と比べる。names は reach の戻りで、確かめた名前と同じ順・同じ数とは限らない)
	lanIP string
	at    time.Time
}

// ReachTarget は疎通を確かめる相手 1 台。画面の p と CLI の ping が同じ規則 (reachTarget) で決める。Refusal が空でなければ確かめない。
// ssh のユーザー名・鍵は要らない (speedTarget と違い、ssh の口が開くかまでしか見ない)。
type ReachTarget struct {
	Host    string   // 台帳の鍵
	Name    string   // 別名 (無ければ Host)
	Names   []string // 確かめる名前 (Bonjour の名前、内部 IP が固定なら有線の名前)
	LanIP   string   // 内部 IP (有線の名前がこれと違う IP に引けたら知らせる)
	Refusal string
}

func (m *Model) reachTarget(r row) ReachTarget {
	t := ReachTarget{Host: r.host, Name: or(r.alias, r.host), LanIP: r.lanIP}
	switch {
	case r.self:
		t.Refusal = "この Mac 自身は確かめない"
		return t
	case !r.managed:
		t.Refusal = "疎通は管理対象の Mac だけ確かめる (台帳に登録してから)"
		return t
	}
	t.Names = []string{r.host}
	if m.lanFixed(r) { // 有線の名前を持つのは内部 IP が固定の行だけ (wiredNames と同じ条件)
		if name, err := setup.WiredName(r.host); err == nil {
			t.Names = append(t.Names, name)
		}
	}
	return t
}

// ReachTargets は管理対象の相手を一覧の順に返す (CLI の ping)。この Mac のネットワークを読めていなければ nil と理由
// (内部 IP が固定かを決められず、有線の名前を確かめる相手を決められない)。
func (m *Model) ReachTargets() ([]ReachTarget, error) {
	if err := m.selfNetErr(); err != nil {
		return nil, err
	}
	var out []ReachTarget
	for _, r := range m.rows() {
		if !r.self && r.managed {
			out = append(out, m.reachTarget(r))
		}
	}
	return out, nil
}

// startReach は p。カーソルの行の相手を裏で確かめる。
func (m *Model) startReach(r row) {
	switch {
	case m.o.Reach == nil:
		m.note("この画面では疎通を確かめられない", false)
		return
	case m.reachRuns[r.host] != nil: // 一覧を開いたときに自分で確かめている回なら、終わったら知らせる回にする (2 本目は起こさない)
		m.reachRuns[r.host].tell = true
		m.note("疎通を確かめている途中 ("+r.host+")。終わったら知らせる", true)
		return
	}
	t := m.reachTarget(r)
	if t.Refusal != "" {
		m.note(t.Refusal, false)
		return
	}
	m.runReach(t, true)
	m.note(t.Name+" の疎通を確かめています ("+strings.Join(t.Names, " / ")+")", true)
}

// runReach は t を裏で確かめる。tell は終わったときに知らせを出すか (p で頼んだ回だけ。開いたときに自分で確かめる回は列に出すだけ)。
func (m *Model) runReach(t ReachTarget, tell bool) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []reach.Name, 1) // 取り消した後に届いた結果は誰も読まない (送り手を止めない)
	if m.reachRuns == nil {
		m.reachRuns = map[string]*reachRun{}
	}
	m.reachRuns[t.Host] = &reachRun{names: t.Names, lanIP: t.LanIP, tell: tell, ch: ch, cancel: cancel}
	m.logf("reach start %s names=%s", t.Host, strings.Join(t.Names, ","))
	run, names := m.o.Reach, t.Names
	go func() { ch <- run(ctx, names) }()
}

// autoReach は、結果が無いか、確かめた名前が今の名前と違う管理対象の行を裏で確かめる (一覧の「疎通」の列。Advance から呼ぶ)。
// この Mac のネットワークを読むまでは待つ (内部 IP が固定かが決まらず、有線の名前を確かめる行を決められない)。
// 失敗した結果も結果として残すので、届かない相手を確かめ続けることはない (確かめ直すのは r / 一覧を開き直したとき・p)。
func (m *Model) autoReach() {
	if m.o.Reach == nil || !m.selfOK {
		return
	}
	for _, r := range m.rows() {
		if r.self || !r.managed || m.reachRuns[r.host] != nil {
			continue
		}
		t := m.reachTarget(r)
		if res, ok := m.reach[r.host]; ok && slices.Equal(res.asked, t.Names) {
			continue
		}
		m.runReach(t, false)
	}
}

// takeReach は確かめ終えた回の結果を取り込む。
func (m *Model) takeReach() {
	for host, rr := range m.reachRuns {
		var names []reach.Name
		select {
		case names = <-rr.ch:
		default:
			continue
		}
		rr.cancel()
		delete(m.reachRuns, host)
		if m.reach == nil {
			m.reach = map[string]reachResult{}
		}
		m.reach[host] = reachResult{names: names, asked: rr.names, lanIP: rr.lanIP, at: m.now}
		var bad []string
		for _, n := range names {
			m.logf("reach done %s %s", host, ReachSummary(n))
			if !n.Reachable() {
				bad = append(bad, n.Name)
			}
		}
		switch {
		case !rr.tell:
		case len(bad) > 0:
			m.note(host+": 届かない名前がある ("+strings.Join(bad, " / ")+")", false)
		default:
			m.note(host+": 全部の名前に届く", true)
		}
	}
}

// stopReach は確かめている途中の回を全部取り消す (glogx の板を閉じたとき・CLI の終わり)。
func (m *Model) stopReach() {
	for host, rr := range m.reachRuns {
		m.logf("reach cancel %s", host)
		rr.cancel()
	}
	m.reachRuns = nil
}

// forgetReach は確かめ終えた結果を捨てる (Reload。開き直した一覧で、前に開いたときの ✓ を今の値と読ませない。autoReach が確かめ直す)。
// 確かめている途中の回は残す (取り消すと、開き直すたびに ping が 2 本重なる)。
func (m *Model) forgetReach() { m.reach = nil }

// ReachSummary は名前 1 つの結果の 1 行 (ログ・詳細カード・CLI)。
func ReachSummary(n reach.Name) string {
	switch {
	case n.LookupErr != nil:
		return n.Name + " 引けない (" + n.LookupErr.Error() + ")"
	case len(n.Addrs) == 0:
		return n.Name + " 引けない"
	}
	parts := make([]string, 0, len(n.Addrs))
	for _, a := range n.Addrs {
		parts = append(parts, a.IP+" "+addrSummary(a))
	}
	return n.Name + " → " + strings.Join(parts, " / ")
}

// addrSummary は IP 1 つの ping と ssh の口。
func addrSummary(a reach.Addr) string {
	var ping string
	switch {
	case a.Ping.Err != nil:
		ping = "ping 測れない"
	case a.Ping.Recv > 0:
		ping = fmt.Sprintf("ping %.1fms (%d/%d)", a.Ping.AvgMs, a.Ping.Recv, a.Ping.Sent)
	default:
		ping = fmt.Sprintf("ping 届かない (%d/%d)", a.Ping.Recv, a.Ping.Sent)
	}
	ssh := "ssh 開"
	if a.SSH != nil {
		ssh = "ssh 閉"
	}
	return ping + "  " + ssh
}

// reachLines は詳細カードの疎通の行 (管理対象の相手だけ。確かめていなければ p の案内)。
func (m *Model) reachLines(r row, label func(string) string) []string {
	if r.self || !r.managed {
		return nil
	}
	head := label("疎通")
	next := func() string { h := head; head = label(""); return h } // 項目名は最初の行だけ
	if m.reachRuns[r.host] != nil {
		return []string{next() + sDim.Render("確かめている…")}
	}
	res, ok := m.reach[r.host]
	if !ok {
		return []string{next() + sDim.Render("未確認  ") + keycap("p", "確かめる")}
	}
	lines := make([]string, 0, len(res.names)+1) // 名前ごとの行と、確かめた時刻の行
	for _, n := range res.names {
		st := sOK.Render("✓ ")
		if !n.Reachable() {
			st = sWarn.Render("✗ ")
		}
		line := st + sText.Render(termsafe.PlainLine(ReachSummary(n))) // 引けない理由は外のコマンドの文
		if strings.HasSuffix(n.Name, ".wired.local") && n.Elsewhere(res.lanIP) {
			line += sWarn.Render("  内部 IP (" + res.lanIP + ") と違う")
		}
		lines = append(lines, next()+line)
	}
	// 日付も出す (glogx の板は日をまたいで開いたままになり、時刻だけだと前日の ✓ を今の値と読ませる)
	return append(lines, next()+sDim.Render(res.at.Format("1/2 15:04")+" に確認"))
}

// reachCell は一覧の「疎通」の列 (L = <host>.local / W = <host>.wired.local)。✓ 届く / ✗ 届かない・引けない・有線の名前が内部 IP と違う /
// - 有線の名前を持たない / … 確かめている途中・まだ確かめていない。管理対象の相手だけ (他の行は空)。
func (m *Model) reachCell(r row) string {
	if r.self || !r.managed {
		return ""
	}
	t := m.reachTarget(r)
	res, ok := m.reach[r.host]
	if m.reachRuns[r.host] != nil || !ok || !slices.Equal(res.asked, t.Names) {
		w := sMuted.Render("W-")
		if len(t.Names) > 1 {
			w = sMuted.Render("W…")
		}
		return sMuted.Render("L…") + " " + w
	}
	mark := func(prefix string, name string) string {
		for _, n := range res.names {
			if n.Name != name {
				continue
			}
			if !n.Reachable() || (prefix == "W" && n.Elsewhere(res.lanIP)) {
				return sWarn.Render(prefix + "✗")
			}
			return sOK.Render(prefix + "✓")
		}
		return sWarn.Render(prefix + "✗") // 確かめた名前の結果が無い (reach が返さなかった)
	}
	w := sMuted.Render("W-")
	if len(t.Names) > 1 {
		w = mark("W", t.Names[1])
	}
	return mark("L", t.Names[0]) + " " + w
}
