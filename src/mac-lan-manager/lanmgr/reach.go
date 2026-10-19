package lanmgr

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"

	"mac-lan-manager/reach"
	"mac-lan-manager/setup"
)

// 疎通の確認 (p)。管理対象の相手の名前 (Bonjour の <host>.local と有線の名前) ごとに、引けた IP へ ping と ssh の口 (22 番) を
// 確かめる。名前が引けることと届くことは別 (有線の名前の広告は、その IP を持つ I/F が無くなった後も残る。wiredCheck は引けるかだけを見る)。
// 帯域を使わず数秒で終わるので確認の板は通さない (t との違い)。同時に 1 本 (同じ相手を重ねて確かめても結果は変わらない)。

// reachRun は確かめている途中の 1 回。
type reachRun struct {
	host   string
	lanIP  string // 確かめ始めたときの内部 IP (reachResult.lanIP)
	ch     <-chan []reach.Name
	cancel context.CancelFunc
}

// reachResult は相手ごとに覚えた最後の結果 (時刻を一緒に出し、古い値を今の値と読ませない)。lanIP は確かめたときの内部 IP
// (有線の名前が違う IP に引けたかを、確かめた時点の値で比べる)。
type reachResult struct {
	names []reach.Name
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
	case m.reachRun != nil:
		m.note("疎通を確かめている途中 ("+m.reachRun.host+")。終わってから", false)
		return
	}
	t := m.reachTarget(r)
	if t.Refusal != "" {
		m.note(t.Refusal, false)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []reach.Name, 1) // 取り消した後に届いた結果は誰も読まない (送り手を止めない)
	m.reachRun = &reachRun{host: t.Host, lanIP: t.LanIP, ch: ch, cancel: cancel}
	m.logf("reach start %s names=%s", t.Host, strings.Join(t.Names, ","))
	run := m.o.Reach
	go func() { ch <- run(ctx, t.Names) }()
	m.note(t.Name+" の疎通を確かめています ("+strings.Join(t.Names, " / ")+")", true)
}

// takeReach は確かめた結果を取り込む。
func (m *Model) takeReach() {
	rr := m.reachRun
	if rr == nil {
		return
	}
	select {
	case names := <-rr.ch:
		rr.cancel()
		m.reachRun = nil
		if m.reach == nil {
			m.reach = map[string]reachResult{}
		}
		m.reach[rr.host] = reachResult{names: names, lanIP: rr.lanIP, at: m.now}
		var bad []string
		for _, n := range names {
			m.logf("reach done %s %s", rr.host, ReachSummary(n))
			if !n.Reachable() {
				bad = append(bad, n.Name)
			}
		}
		if len(bad) > 0 {
			m.note(rr.host+": 届かない名前がある ("+strings.Join(bad, " / ")+")", false)
			return
		}
		m.note(rr.host+": 全部の名前に届く", true)
	default:
	}
}

// stopReach は確認を取り消す (glogx の板を閉じたとき・CLI の終わり)。
func (m *Model) stopReach() {
	if m.reachRun == nil {
		return
	}
	m.logf("reach cancel %s", m.reachRun.host)
	m.reachRun.cancel()
	m.reachRun = nil
}

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
	if rr := m.reachRun; rr != nil && rr.host == r.host {
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
