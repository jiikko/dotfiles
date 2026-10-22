package lanmgr

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"mac-lan-manager/netinfo"
	"mac-lan-manager/speedtest"
	"mac-lan-manager/sshdest"
)

// speedRun は測っている途中の 1 回。同時に 1 本だけ (2 本流すと互いの帯域を食って値が意味を失う)。
type speedRun struct {
	host   string
	name   string // 別名 (無ければ host。進捗のモーダルの見出し)
	ip     string
	link   netinfo.Link
	since  time.Time
	shown  bool // 進捗のモーダルを出している。esc で閉じても測定は止めない (結果は toast で出す。閉じたら t で開き直せる)
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
	Dest    sshdest.Dest
	Link    netinfo.Link
	Refusal string
}

// speedTarget は r を測るときの宛先と経路 (測れなければ Refusal)。測るのは管理対象の相手だけ (未管理は初期設定の前で鍵で入れないことが
// 多い。ユーザー決定 2026-10-18)。
func (m *Model) speedTarget(r row) SpeedTarget {
	// この Mac のネットワークを読めていない間はここで断らない: 呼び出し側が先に断る (画面の t は needsNet、CLI は SpeedTargets)
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
	// 宛先は繋ぐ IP (内部 IP を優先 = 有線を測る)。有線の名前は引いた先が同じ内部 IP なので、IP で測る (sshdest.Dest は IPv4 だけを通す)
	t.Dest = sshdest.Dest{User: user, IP: r.connectIP(), HostKeyAlias: r.host}
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
	if err := m.selfNetErr(); err != nil {
		return nil, err
	}
	var out []SpeedTarget
	for _, r := range m.rows() {
		if !r.self && r.managed {
			out = append(out, m.speedTarget(r))
		}
	}
	return out, nil
}

// selfNetErr はこの Mac のネットワークを読めていなければその理由 (CLI の speedtest / ping が相手を決める前に断る)。
func (m *Model) selfNetErr() error {
	if m.selfOK {
		return nil
	}
	if m.selfErr != nil {
		return fmt.Errorf("この Mac のネットワークを読めない: %w", m.selfErr)
	}
	return errors.New("この Mac のネットワークを読み終えていない")
}

// speedPath は測る経路の候補 1 つ (相手の IP と、それが見えたこの Mac の I/F の線)。
type speedPath struct {
	ip   string
	link netinfo.Link
	what string // 画面の名前 ("内部" / "メイン" / I/F の名前)
}

func (p speedPath) label() string {
	s := p.ip
	if l := p.link.Label(); l != "" {
		s += " (" + l + ")"
	}
	return s
}

// speedAsk は経路を選んで測る確認の板 (ovSpeed)。
type speedAsk struct {
	host, name string
	user       string
	paths      []speedPath
	cur        int
}

// speedPaths は行の Mac を測れる経路 (内部 IP → メイン IP → その他の IP の順。同じ IP は 1 つ)。網が分かれていて届かない IP
// (片方だけ固定して 169.254 と帯の番号に分かれた組) は外す: 内部 IP は rows が決めた lanAway、その他の IP はそれが見えた I/F に
// この Mac が持つ IP と lanReachable で比べる (この Mac の内部 IP 1 つと比べると、別の有線の I/F で届く IP まで外れた)。
// メイン IP とルーターのある I/F・VM・VPN の IP はルーター越しなので比べない (比べると、この Mac が 169.254 のときに Wi-Fi の IP まで外れた)。
func (m *Model) speedPaths(r row) []speedPath {
	var ps []speedPath
	add := func(ip string, l netinfo.Link, what string) {
		if ip == "" {
			return
		}
		if i := slices.IndexFunc(ps, func(p speedPath) bool { return p.ip == ip }); i >= 0 {
			if ps[i].link == netinfo.LinkUnknown { // 同じ IP の線が後の方で分かるなら、分かる方を採る (台帳の内部 IP がメインとして見えた)
				ps[i].link = l
			}
			return
		}
		ps = append(ps, speedPath{ip: ip, link: l, what: what})
	}
	if !r.lanAway {
		add(r.lanIP, r.lanLink, "内部")
	}
	add(r.mainIP, r.mainLink, "メイン")
	for _, x := range r.others {
		switch x.Kind {
		// 内部の網に居うる (Unknown は登録はあるが読めない有線の I/F を含む)。ルーターの無い Wi-Fi も、ルーターが無いので同じ網の相手にしか届かない
		case netinfo.KindWired, netinfo.KindWirelessNoRouter, netinfo.KindLinkLocal, netinfo.KindUnknown:
			if !lanReachable(m.selfIPOn(x.Device), x.Addr, m.pf.Band) {
				continue
			}
		case netinfo.KindInternet, netinfo.KindVirtual, netinfo.KindVPN:
		}
		add(x.Addr, m.self.Link(x.Device), x.Device)
	}
	return ps
}

// selfIPOn はこの Mac が dev に持つ IPv4 (無ければ "" = lanReachable は届くと見る)。
func (m *Model) selfIPOn(dev string) string {
	for _, x := range m.self.IPs {
		if x.Device == dev {
			return x.Addr
		}
	}
	return ""
}

// askSpeed は t (通信速度を測る) の板を出す。帯域を使い切るので必ず板を通し、y だけで実行する (Enter を打ち続けても流れない。
// Y は一覧では ssh のコピーで指が慣れている)。測れるかは CLI と同じ speedTarget で決め、経路は j/k で選ぶ。既定は最初の有線の経路
// (ユーザー指示 2026-10-18: 既定は有線)。
func (m *Model) askSpeed(r row) {
	switch {
	case m.o.SpeedTest == nil:
		m.note("この画面では通信速度を測れない", false)
		return
	case m.speedRun != nil: // 2 本目は流さず、閉じた進捗のモーダルを開き直す (どの行で押しても、今測っている相手のモーダル)
		m.speedRun.shown = true
		return
	}
	t := m.speedTarget(r)
	if t.Refusal != "" {
		m.note(t.Refusal, false)
		return
	}
	paths := m.speedPaths(r)
	if len(paths) == 0 {
		m.note("測る IP が分からない", false)
		return
	}
	cur := max(slices.IndexFunc(paths, func(p speedPath) bool { return p.link == netinfo.LinkWired }), 0)
	m.ov, m.speedAsk = ovSpeed, &speedAsk{host: t.Host, name: t.Name, user: t.Dest.User, paths: paths, cur: cur}
}

// currentPaths は host の行の今の経路 (行が無ければ nil)。
func (m *Model) currentPaths(host string) []speedPath {
	for _, r := range m.rows() {
		if r.host == host && !r.self {
			return m.speedPaths(r)
		}
	}
	return nil
}

// keySpeed は経路を選ぶ板のキー。j/k で選び、y で測る。Enter は何もしない (板は閉じない)。他のキーは取り消し (ガイド §4)。
func (m *Model) keySpeed(key string) Action {
	a := m.speedAsk
	switch key {
	case "j", "down":
		a.cur = min(a.cur+1, len(a.paths)-1)
		return None
	case "k", "up":
		a.cur = max(a.cur-1, 0)
		return None
	case "enter":
		return None
	}
	m.ov, m.speedAsk = ovNone, nil
	if key != "y" {
		return None
	}
	if m.speedRun != nil { // 板を出している間に別の測定が始まることは無いが、y の時点で確かめる (同時に 2 本流さない)
		m.note("測定中 ("+m.speedRun.host+")。終わってから", false)
		return None
	}
	p := a.paths[a.cur]
	// 板を出している間に一覧を読み直して経路が変わっていたら測らない (板の候補は開いたときの写し)
	now := m.currentPaths(a.host)
	i := slices.IndexFunc(now, func(q speedPath) bool { return q.ip == p.ip })
	if i < 0 {
		m.note(p.ip+" はもう見えていない (t で開き直す)", false)
		return None
	}
	p = now[i] // 線は今の値を使う (板を出している間に分かった線の札を結果に付ける)
	// 宛先は IP で測る (有線の名前は引いた先が同じ内部 IP。sshdest.Dest は IPv4 だけを通す)
	d := sshdest.Dest{User: a.user, IP: p.ip, HostKeyAlias: a.host}
	if err := d.Validate(); err != nil {
		m.note("測れない: "+err.Error(), false)
		return None
	}
	m.startSpeed(a.host, a.name, d, p.link) // 始まりを toast だけで知らせない (数秒で消え、10 秒余りの測定の間に何も見えなくなる。進捗のモーダルを出す)
	return None
}

// speedBox は経路を選ぶ板。
func (m *Model) speedBox() string {
	a := m.speedAsk
	body := append(make([]string, 0, 1+len(a.paths)+3), a.name+" との通信速度を測ります。経路:")
	for i, p := range a.paths {
		mark, st := "  ", sText
		if i == a.cur {
			mark, st = "▶ ", sAccent
		}
		body = append(body, mark+st.Render(p.label())+sDim.Render("  "+p.what))
	}
	secs := int(speedtest.Duration / time.Second)
	body = append(body, "",
		fmt.Sprintf("上り・下りを %d 秒ずつ、ssh 越しで回線をほぼ使い切ります (その間、同じ回線の他の通信は遅くなります)", secs),
		"値は ssh の暗号化を通した速さです (1 Gbps を超える回線は暗号化の速さで頭打ちになりえます)")
	return modal("通信速度を測る", body, hintLine(speedBoxHints), m.w)
}

// speedShown は測定の進捗のモーダルを出すか (測っている途中で、esc で閉じておらず、他の板が開いていない)。
func (m *Model) speedShown() bool { return m.speedRun != nil && m.speedRun.shown && m.ov == ovNone }

// keySpeedProgress は進捗のモーダルのキー。esc / q で閉じる (測定は裏で続く)。他のキーは捨てる (裏の一覧に届かせない)。
func (m *Model) keySpeedProgress(key string) Action {
	if key == "esc" || key == "q" {
		m.speedRun.shown = false
		return None
	}
	m.note("測定中 (esc で閉じる。測定は裏で続く)", false)
	return None
}

// speedProgressBox は測っている間のモーダル (回る印・相手と経路・経過秒)。
func (m *Model) speedProgressBox() string {
	sr := m.speedRun
	el := max(int(m.now.Sub(sr.since)/time.Second), 0)
	// 所要の見込みを「約 10 秒」と出さない: 流す前の ssh の接続と鍵の確認は相手次第で、出すと経過が見込みを超えて表示される
	body := []string{
		sAccent.Render(m.spin()) + " " + sText.Render(sr.name) + "  " + sDim.Render(speedPath{ip: sr.ip, link: sr.link}.label()),
		sDim.Render(fmt.Sprintf("  経過 %d 秒  (ssh で繋いでから上り・下りを %d 秒ずつ)", el, int(speedtest.Duration/time.Second))),
	}
	return modal("通信速度を測っています", body, hintLine(speedProgressHints), m.w)
}

func (m *Model) startSpeed(host, name string, d sshdest.Dest, link netinfo.Link) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan speedtest.Result, 1) // 取り消した後に届いた結果は誰も読まない (送り手を止めない)
	m.speedRun = &speedRun{host: host, name: name, ip: d.IP, link: link, since: m.o.Now(), shown: true, ch: ch, cancel: cancel}
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
			m.speed = map[string]map[string]speedResult{}
		}
		if m.speed[sr.host] == nil {
			m.speed[sr.host] = map[string]speedResult{}
		}
		m.speed[sr.host][sr.ip] = speedResult{Result: res, at: m.now, ip: sr.ip, link: sr.link}
		path := speedPath{ip: sr.ip, link: sr.link}.label()
		if res.Err != nil {
			m.logf("speed done %s ip=%s err=%v", sr.host, sr.ip, res.Err)
			m.note(sr.host+" ("+path+") の通信速度を測れなかった: "+res.Err.Error(), false)
			return
		}
		m.logf("speed done %s ip=%s up=%.0f down=%.0f", sr.host, sr.ip, res.UpBps, res.DownBps)
		m.note(fmt.Sprintf("%s (%s): ↑ %s  ↓ %s", sr.host, path, speedtest.Format(res.UpBps), speedtest.Format(res.DownBps)), true)
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

func boolOrder(b bool) int {
	if b {
		return 1
	}
	return 0
}

// linkOrder は結果を並べる順 (有線 → 無線 → 分からない)。
func linkOrder(l netinfo.Link) int {
	switch l {
	case netinfo.LinkWired:
		return 0
	case netinfo.LinkWireless:
		return 1
	case netinfo.LinkUnknown:
	}
	return 2
}

// speedLine は詳細カードの通信速度の行 (管理対象の相手だけ。測っていなければ t の案内)。
func (m *Model) speedLine(r row, label func(string) string) []string {
	if r.self || !r.managed {
		return nil
	}
	lines := make([]string, 0, 2*len(m.speed[r.host])+1) // 経路ごとに結果と頭打ち (か時刻) の 2 行、測定中の 1 行
	next := firstLabel(label, "通信速度")
	if sr := m.speedRun; sr != nil && sr.host == r.host {
		lines = append(lines, next()+sDim.Render(fmt.Sprintf("測定中… %s (上り・下り 各 %d 秒)", speedPath{ip: sr.ip, link: sr.link}.label(), int(speedtest.Duration/time.Second))))
	}
	// 経路ごとの結果を有線から並べる (有線と無線の差を見比べる)。測っている IP の前の結果は重ねない。今の経路に無い IP
	// (169.254 が付け直された・Bonjour が一時的に見失った) の結果は消さずに「今は見えていない」と付ける (隠すと「未測定」と出て、
	// 走査のたびに出たり消えたりした。古い IP の行を今の速さと読ませない)
	paths := m.speedPaths(r)
	results := slices.DeleteFunc(slices.Collect(maps.Values(m.speed[r.host])), func(res speedResult) bool {
		return m.speedRun != nil && m.speedRun.host == r.host && m.speedRun.ip == res.ip
	})
	seen := func(ip string) bool { return slices.ContainsFunc(paths, func(p speedPath) bool { return p.ip == ip }) }
	slices.SortFunc(results, func(a, b speedResult) int {
		if c := cmp.Compare(boolOrder(!seen(a.ip)), boolOrder(!seen(b.ip))); c != 0 { // 今見えている経路を上に
			return c
		}
		if c := cmp.Compare(linkOrder(a.link), linkOrder(b.link)); c != 0 {
			return c
		}
		return strings.Compare(a.ip, b.ip)
	})
	for _, res := range results {
		when := sDim.Render("  " + res.at.Format("15:04") + " に測定 (" + res.ip + ")")
		if !seen(res.ip) {
			when += sMuted.Render("  今は見えていない")
		}
		if res.Err != nil {
			// 時刻と宛先は次の行に置く (同じ行だとカードの幅を超えて折り返した)
			lines = append(lines, next()+sWarn.Render(errTail(res.Err, 60)), next()+when)
			continue
		}
		lines = append(lines, next()+sText.Render(fmt.Sprintf("↑ %s  ↓ %s", speedtest.Format(res.UpBps), speedtest.Format(res.DownBps)))+tagAfter(res.link)+when)
		if res.link == netinfo.LinkWired && seen(res.ip) && speedtest.Capped100M(res.Result) {
			lines = append(lines, next()+sWarn.Render("🚨 100 Mbps 付近で頭打ち (途中に 100M の機器・ケーブル?)"))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, next()+sDim.Render("未測定  ")+keycap("t", "測る"))
	}
	return lines
}
