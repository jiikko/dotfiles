package lanmgr

import "fmt"

// bgJob は裏の仕事 1 つ。busy の間は Busy (tick を回す) で、見出しに label を出す。
type bgJob struct {
	busy  func(m *Model) bool
	label func(m *Model) string
}

// bgJobs は表の順が見出しの優先順 (同時に走っていれば上を出す)。裏の仕事を足すときはここに 1 行足す: Busy と見出しを別々に
// 書いていたころは、速度の測定と疎通の確かめが見出しを書き漏らし、走っている間「有線の名前を確かめている」と出た。
// 取り込み (Advance の take*) と止め方 (Close の stop*) は順序に意味があるので表にしない (取り込みの順を変えると、同じ tick の中で
// どの結果を見て次を決めるかが変わる)。
var bgJobs = []bgJob{
	{func(m *Model) bool { return m.scan != nil && m.scan.Busy(m.now) },
		func(m *Model) string { return fmt.Sprintf("Bonjour で探しています (%d 台)", len(m.peers)) }},
	{func(m *Model) bool { return m.su != nil && m.su.ch != nil }, fixed("相手に接続して確認中")},
	{func(m *Model) bool { return m.netCh != nil }, fixed(netLoadingLabel)},
	{func(m *Model) bool { return len(m.pending) > 0 || m.awaitingUnconf() }, fixed("固定 IP の確認待ち")},
	{func(m *Model) bool { return len(m.bg) > 0 }, fixed("有線の名前の広告を止めている")},
	{func(m *Model) bool { return m.speedRun != nil }, fixed("通信速度を測っている")},
	{func(m *Model) bool { return len(m.reachRuns) > 0 }, fixed("疎通を確かめている")},
	{func(m *Model) bool { return m.wiredWait > 0 }, fixed("有線の名前を確かめている")},
}

const netLoadingLabel = "この Mac のネットワーク設定を確認中"

// busyJob は走っている裏の仕事のうち、見出しに出す 1 つ (無ければ nil)。
func (m *Model) busyJob() *bgJob {
	for i := range bgJobs {
		if bgJobs[i].busy(m) {
			return &bgJobs[i]
		}
	}
	return nil
}

// busyLabel は見出しに出す待ちの文 (走っていなければ "")。この Mac のネットワークを一度も読み終えていない間は、それを先に出す
// (読む前は一覧の分類が仮なので、Bonjour の走査より先に知らせる)。
func (m *Model) busyLabel() string {
	j := m.busyJob()
	switch {
	case j == nil:
		return ""
	case !m.netReady:
		return netLoadingLabel
	}
	return j.label(m)
}
