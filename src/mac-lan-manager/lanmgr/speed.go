package lanmgr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"mac-lan-manager/netinfo"
	"mac-lan-manager/setup"
	"mac-lan-manager/speedtest"
)

// speedRun は測っている途中の 1 回。同時に 1 本だけ (2 本流すと互いの帯域を食って値が意味を失う)。
type speedRun struct {
	host   string
	ip     string
	link   netinfo.Link
	ch     <-chan speedtest.Result
	cancel context.CancelFunc
}

// speedResult は相手ごとに覚えた最後の結果 (画面を閉じても残す。測った時刻と経路を一緒に出し、古い値を今の速さと読ませない)。
type speedResult struct {
	speedtest.Result
	at   time.Time
	ip   string
	link netinfo.Link
}

// SpeedTarget は通信速度を測る相手 1 台。画面の t と CLI の speedtest が同じ規則 (speedTarget) で決める。Refusal が空でなければ測らない。
type SpeedTarget struct {
	Host    string // 台帳の鍵
	Name    string // 別名 (無ければ Host)
	Dest    setup.Dest
	Link    netinfo.Link
	Refusal string
}

// speedTarget は r を測るときの宛先と経路 (測れなければ Refusal)。測るのは管理対象の相手だけ (未管理は初期設定の前で鍵で入れないことが
// 多い。ユーザー決定 2026-10-18)。
func (m *Model) speedTarget(r row) SpeedTarget {
	t := SpeedTarget{Host: r.host, Name: or(r.alias, r.host)}
	switch {
	case r.self:
		t.Refusal = "この Mac 自身は測らない"
		return t
	case !r.managed:
		t.Refusal = "通信速度は管理対象の Mac だけ測る (台帳に登録してから)"
		return t
	}
	if why := m.sshRefusal(r); why != "" {
		t.Refusal = why
		return t
	}
	user, _ := m.userFor(r)
	// 宛先は繋ぐ IP (内部 IP を優先 = 有線を測る)。有線の名前は引いた先が同じ内部 IP なので、IP で測る (setup.Dest は IPv4 だけを通す)
	t.Dest = setup.Dest{User: user, IP: r.connectIP(), HostKeyAlias: r.host}
	if err := t.Dest.Validate(); err != nil {
		t.Refusal = "測れない: " + err.Error()
		return t
	}
	t.Link = m.connectLink(r)
	return t
}

// SpeedTargets は管理対象の相手を一覧の順に返す (CLI の speedtest)。この Mac のネットワークを読めていなければ nil と理由
// (宛先の分類ができず、Wi-Fi の IP を有線として測りうる)。
func (m *Model) SpeedTargets() ([]SpeedTarget, error) {
	if !m.selfOK {
		if m.selfErr != nil {
			return nil, fmt.Errorf("この Mac のネットワークを読めない: %w", m.selfErr)
		}
		return nil, errors.New("この Mac のネットワークを読み終えていない")
	}
	var out []SpeedTarget
	for _, r := range m.rows() {
		if !r.self && r.managed {
			out = append(out, m.speedTarget(r))
		}
	}
	return out, nil
}

// askSpeed は t (通信速度を測る) の確認の板を出す。帯域を使い切るので必ず板を通し、Enter では実行しない (行を選んで Enter を
// 打ち続けても流れない)。
func (m *Model) askSpeed(r row) {
	switch {
	case m.o.SpeedTest == nil:
		m.note("この画面では通信速度を測れない", false)
		return
	case m.speedRun != nil:
		m.note("測定中 ("+m.speedRun.host+")。終わってから", false)
		return
	}
	t := m.speedTarget(r)
	if t.Refusal != "" {
		m.note(t.Refusal, false)
		return
	}
	d, link := t.Dest, t.Link
	via := d.IP
	if l := link.Label(); l != "" {
		via += " (" + l + ")"
	}
	secs := int(speedtest.Duration / time.Second)
	m.ov, m.ask = ovConfirm, &ask{
		title: "通信速度を測る",
		body: []string{
			or(r.alias, r.host) + " との通信速度を測ります",
			"宛先: " + via,
			fmt.Sprintf("上り・下りを %d 秒ずつ、ssh 越しで回線をほぼ使い切ります (その間、同じ回線の他の通信は遅くなります)", secs),
			"値は ssh の暗号化を通した速さです (1 Gbps を超える回線は暗号化の速さで頭打ちになりえます)",
		},
		strict: true,
		yes: func() Notice {
			m.startSpeed(r.host, d, link)
			return Notice{fmt.Sprintf("%s の通信速度を測っています (約 %d 秒)", or(r.alias, r.host), 2*secs), true}
		},
	}
}

func (m *Model) startSpeed(host string, d setup.Dest, link netinfo.Link) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan speedtest.Result, 1) // 取り消した後に届いた結果は誰も読まない (送り手を止めない)
	m.speedRun = &speedRun{host: host, ip: d.IP, link: link, ch: ch, cancel: cancel}
	m.logf("speed start %s ip=%s link=%s", host, d.IP, or(link.Label(), "?"))
	run, now := m.o.SpeedTest, m.o.Now
	go func() { ch <- run(ctx, d, now) }()
}

// takeSpeed は測定の結果を取り込む。
func (m *Model) takeSpeed() {
	sr := m.speedRun
	if sr == nil {
		return
	}
	select {
	case res := <-sr.ch:
		sr.cancel()
		m.speedRun = nil
		if m.speed == nil {
			m.speed = map[string]speedResult{}
		}
		m.speed[sr.host] = speedResult{Result: res, at: m.now, ip: sr.ip, link: sr.link}
		if res.Err != nil {
			m.logf("speed done %s err=%v", sr.host, res.Err)
			m.note(sr.host+" の通信速度を測れなかった: "+res.Err.Error(), false)
			return
		}
		m.logf("speed done %s up=%.0f down=%.0f", sr.host, res.UpBps, res.DownBps)
		m.note(fmt.Sprintf("%s: ↑ %s  ↓ %s", sr.host, speedtest.Format(res.UpBps), speedtest.Format(res.DownBps)), true)
	default:
	}
}

// stopSpeed は測定を取り消す (glogx の板を閉じたとき。ssh を止めれば相手の wc / head も書き先・読み元を失って終わる)。
func (m *Model) stopSpeed() {
	if m.speedRun == nil {
		return
	}
	m.logf("speed cancel %s", m.speedRun.host)
	m.speedRun.cancel()
	m.speedRun = nil
}

// errTail は失敗の文の最後の行を無害化し、width 桁を超えるなら先頭を落とす。ssh の stderr は先頭に相手の Banner、末尾に原因
// ("Connection refused" など) が来るので、先頭から残すと原因が落ちた。全文は知らせとログに残る。
func errTail(err error, width int) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	s := termsafe.PlainLine(lines[len(lines)-1])
	cut := len(lines) > 1
	for termwidth.Of(s) > width-2 {
		_, n := utf8.DecodeRuneInString(s)
		s, cut = s[n:], true
	}
	if cut {
		s = "… " + s
	}
	return s
}

// speedLine は詳細カードの通信速度の行 (管理対象の相手だけ。測っていなければ t の案内)。
func (m *Model) speedLine(r row, label func(string) string) []string {
	if r.self || !r.managed {
		return nil
	}
	if sr := m.speedRun; sr != nil && sr.host == r.host {
		return []string{label("通信速度") + sDim.Render(fmt.Sprintf("測定中… (上り・下り 各 %d 秒)", int(speedtest.Duration/time.Second)))}
	}
	res, ok := m.speed[r.host]
	if !ok {
		return []string{label("通信速度") + sDim.Render("未測定  ") + keycap("t", "測る")}
	}
	when := sDim.Render("  " + res.at.Format("15:04") + " に測定 (" + res.ip + ")")
	if res.Err != nil {
		// 時刻と宛先は次の行に置く (同じ行だとカードの幅を超えて折り返した)
		return []string{label("通信速度") + sWarn.Render(errTail(res.Err, 60)), label("") + when}
	}
	lines := []string{label("通信速度") + sText.Render(fmt.Sprintf("↑ %s  ↓ %s", speedtest.Format(res.UpBps), speedtest.Format(res.DownBps))) +
		tagAfter(res.link) + when}
	if res.link == netinfo.LinkWired && speedtest.Capped100M(res.Result) {
		lines = append(lines, lipgloss.NewStyle().Width(12).Render("")+sWarn.Render("🚨 100 Mbps 付近で頭打ち (途中に 100M の機器・ケーブル?)"))
	}
	return lines
}
