package lanmgr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/setup"
)

// 段 4 の初期設定 (issue 747)。一覧の行で Enter を押すと、その相手の手順の画面になる。手順は観測した状態から毎回組み直す
// (setup.Plan。途中で止まっても、観測し直して続きから流せる)。x で次の手順を確認の板 (実際に打つコマンドを見せる) の後に実行する。
// 端末の要る手順 (sudo・最初の ssh のパスワード) は端末を明け渡す (Exec)。相手を固定 IP にした後は、新しい IP で入り直して確認の印を
// 置くまでが 1 組 (置かなければ相手の上で元に戻る = デッドマンスイッチ)。

// setupState は初期設定の画面の状態 (開いている相手 1 台分)。
type setupState struct {
	host  string     // 相手の台帳の鍵
	dest  setup.Dest // 観測・鍵・固定 IP の ssh の宛先 (Wi-Fi で入れるなら Wi-Fi)
	obs   setup.PeerNet
	seen  bool // 一度は観測を読み終えた
	ch    <-chan setup.PeerNet
	stop  context.CancelFunc
	obsAt time.Time // 今の観測を始めた時刻 (未確認の固定 IP を解いてよいかの判定)
	label string    // 画面の題 (別名か Bonjour の名前)
	// 進捗のモーダル (progress.go): after は戻った手順の名前 (その結果を確かめている間に出す)、queuedX は読み込み中に押した x
	// (読み終えたら次の手順の板を出す)、waitSince はモーダルの経過の起点
	after     string
	queuedX   bool
	waitSince time.Time
}

// conf は開いている相手の確認待ち (無ければ nil)。確認待ちは画面ではなく Model が相手ごとに持つ: 画面に持っていたころは、
// 閉じて開き直すと消え、Plan が「済み」と見て未確認の IP を台帳に書けた (敵対レビュー)。
// 受容した限界: プロセスの中にしか持たないので、確認待ちの間 (約 185 秒) にプロセスを起こし直すと消え、同じ形で未確認の IP を
// 台帳に書ける。glogx と単体の mac-lan-manager を同時に使うと、割り当てた番号 (claimed) を互いに知らない。塞ぐなら、相手の状態
// ディレクトリにある未確認の印を観測で読む (setup.StaticIP の ConfirmPath)。
func (m *Model) conf() *pendingConfirm {
	if m.su == nil {
		return nil
	}
	return m.pending[m.su.host]
}

// expireConfirms は期限を過ぎた確認待ちを片付ける (相手の上では元の設定に戻っている)。Advance と観測の取り込みで呼ぶ。
func (m *Model) expireConfirms() {
	if m.su != nil && m.su.ch == nil {
		if dl, ok := m.unconf[m.su.host]; ok && m.now.After(dl) && !m.su.obsAt.After(dl) {
			m.observe() // 相手の上の戻しが終わる頃に読み直す (その観測で未確認の印が解ける)
		}
	}
	for host, c := range m.pending {
		if se := m.setupExec; se != nil && se.step == stepConfirm && se.host == host {
			continue // 確認のプロセスから戻るのを待つ (端末を渡している間の tick が先に届き、間際の確認を期限切れにした。敵対レビュー)
		}
		if m.now.After(c.deadline) {
			delete(m.pending, host)
			delete(m.claimed, host)
			m.note("確認の期限が過ぎた: "+host+" は元の設定に戻る (読み直す)", false)
			if m.su != nil && m.su.host == host { // 窓の間の観測 (新しい IP) のまま次の手順を組まない
				m.su.seen = false
				m.observe()
			}
		}
	}
}

// pendingConfirm は相手を固定 IP にした後の確認待ち。期限を過ぎると相手の上で元に戻っている。
type pendingConfirm struct {
	static   setup.StaticIP
	deadline time.Time
}

// unconfMargin は未確認の印の期限に足す余白 (相手の上の戻しが終わるまで。待ちの 180 周の検査の時間とずれの分)。
const unconfMargin = 60 * time.Second

// ledgerBlocked は host を台帳に内部 IP 付きで書いてはいけない理由 (空なら書いてよい)。台帳に書く経路 (初期設定の台帳の手順・一覧の space) は
// どれもこれを通す (一覧の space が未確認の印を見ず、未確認の IP を台帳に書けた。敵対レビュー)。
// ip は書こうとしている内部 IP。
func (m *Model) ledgerBlocked(host, ip string) string {
	if _, ok := m.pending[host]; ok {
		return "固定 IP にした後の確認を待っている (確認してから)"
	}
	if _, ok := m.unconf[host]; ok {
		return "固定 IP にした後、まだ確認していない (確認するか、相手が元に戻る頃に初期設定の画面で r で読み直す。相手を読めるようになるまで解けない)"
	}
	if ip != "" && m.tried[host][ip] {
		return "内部 IP " + ip + " は固定しようとして確認できなかった番号 (相手は元に戻っている。一覧で r を押して読み直す)"
	}
	return ""
}

// markUnconf は host を ip に固定しようとした印を立てる (未確認の期限と、確認するまで台帳に書かない番号)。
func (m *Model) markUnconf(host, ip string) {
	m.unconf[host] = m.o.Now().Add(setup.PeerDelay + setup.PeerWindow + unconfMargin)
	if m.tried[host] == nil {
		m.tried[host] = map[string]bool{}
	}
	m.tried[host][ip] = true
}

// awaitingUnconf は、開いている相手に未確認の印があり、その期限の後の観測をまだしていないか (Busy に含め、tick で期限の頃に読み直す。
// 含めないと確認待ちが消えた後に tick が止まり、読み直しが次のキーまで遅れた。敵対レビュー)。
func (m *Model) awaitingUnconf() bool {
	if m.su == nil {
		return false
	}
	dl, ok := m.unconf[m.su.host]
	return ok && !m.su.obsAt.After(dl)
}

// unconfOverdue は、開いている相手の未確認の印の期限が過ぎ、その後に読み直した観測でも解けていないか (画面に r を促す)。
func (m *Model) unconfOverdue() bool {
	if m.su == nil || m.su.ch != nil {
		return false
	}
	dl, ok := m.unconf[m.su.host]
	return ok && m.su.obsAt.After(dl)
}

// stepConfirm は相手を固定 IP にした後の「新しい IP で入り直して確認する」手順 (Plan の手順とは別に、確認待ちの間だけ出す)。
const stepConfirm setup.StepID = "confirm"

// openSetup は r の初期設定の画面を開く。
func (m *Model) openSetup(r row) {
	if r.self {
		m.note("初期設定は相手の行で Enter (この Mac の固定 IP は、その 1 つ目の手順)", false)
		return
	}
	user, _ := m.userFor(r)
	ip := r.mainIP
	if ip == "" {
		ip = r.lanIP
		if c, ok := m.confirmed[r.host]; ok {
			ip = c // 有線だけの相手は、期限内に確認した新しい IP で入る (行の 169.254 は古い。閉じて開き直すと古い宛先に戻った。敵対レビュー)
		}
	}
	d := setup.Dest{User: user, IP: ip, HostKeyAlias: r.host}
	if err := d.Validate(); err != nil {
		m.note("この Mac には入れない: "+err.Error(), false)
		return
	}
	m.closeSetup()
	m.su = &setupState{host: r.host, dest: d, label: or(r.alias, or(r.instance, r.host))}
	m.sc = scSetup
	m.observe()
}

// closeSetup は初期設定の画面を閉じる (裏の観測も止める)。
func (m *Model) closeSetup() {
	if m.su != nil && m.su.stop != nil {
		m.su.stop()
	}
	m.su = nil
	if m.sc == scSetup {
		m.sc = scList
	}
}

// observe は相手を裏で読み直す (前の観測は捨てる)。
func (m *Model) observe() {
	su := m.su
	if su == nil || m.o.Observe == nil {
		return
	}
	if su.stop != nil {
		su.stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	ch := make(chan setup.PeerNet, 1)
	su.ch, su.stop, su.obsAt = ch, cancel, m.o.Now()
	obs, d := m.o.Observe, su.dest
	go func() {
		defer cancel()
		ch <- obs(ctx, d)
	}()
}

// takeObserve は読み終えた観測を取り込む (Advance とキーの度に呼ぶ)。
func (m *Model) takeObserve() {
	su := m.su
	if su == nil || su.ch == nil {
		return
	}
	select {
	case p := <-su.ch:
		m.applyObserve(p)
	default:
	}
	m.expireConfirms()
}

// applyObserve は読み終えた観測を取り込む。
func (m *Model) applyObserve(p setup.PeerNet) {
	su := m.su
	su.obs, su.seen, su.ch, su.stop = p, true, nil, nil
	if p.KeyErr != nil { // 画面には状態の行で出す。理由の全文 (ssh の stderr) は一覧に残す (行は幅で切れる)
		m.record("相手を読めない ("+su.host+"): "+p.KeyErr.Error(), false)
	}
	// 未確認の固定 IP は、期限の後に始めた観測で解く (相手は元に戻っているか、確認されて新しい IP のまま: どちらも観測が正しい)
	// 読めなかった観測 (届かない・netinfo の失敗) では解かない: 次の観測で読み直す (解くと、後で固定が見えても試した番号が外れなかった。敵対レビュー)
	if dl, ok := m.unconf[su.host]; ok && su.obsAt.After(dl) && p.NetKnown {
		delete(m.unconf, su.host)
		// 期限の後にまだ試した番号の手動の固定に見えるなら、相手の上で確定している (Script は確認が無ければ必ず戻す。
		// 戻せないのは電源断・kill -9 だけで、そのときも相手はその番号にいる)
		if p.Method == "manual" {
			delete(m.tried[su.host], p.IP)
		}
	}
}

// setupObserved は Plan に渡す観測 (この Mac・相手の行・裏で読んだ相手)。
func (m *Model) setupObserved() (setup.Observed, row) {
	su := m.su
	var r row
	for _, x := range m.rows() {
		if x.host == su.host && !x.self {
			r = x
			break
		}
	}
	o := setup.Observed{Band: m.pf.Band}
	if s, ok := m.selfWired(); ok {
		o.SelfService, o.SelfIP, o.SelfMethod = s.Name, s.IP, s.Method
	}
	o.Peer = setup.PeerObs{
		Host: su.host, WifiIP: r.mainIP, LanIP: r.lanIP, SSHOpen: r.ssh || r.managed,
		KeyOK: su.obs.KeyOK, NetKnown: su.obs.NetKnown, Service: su.obs.Service, IP: su.obs.IP, Method: su.obs.Method,
		Managed: r.managed, LedgerLAN: r.ledgerLAN,
	}
	return o, r
}

// selfWired はこの Mac の内部の網のサービス (I/F で決める設定ならその I/F、帯で決めるならルーターの無い有線の先頭)。
// 受容した限界: 帯で決める設定では、DHCP を一時的に失ってルーターが無く見えるインターネット用の有線も候補に入る。
// 確認の板にサービス名を出しているので、選ぶ前に人が見る。確実にするなら I/F で決める設定にする
func (m *Model) selfWired() (netinfo.Service, bool) {
	if !m.pf.LanByBand {
		for _, s := range m.self.Enabled() {
			if s.Device == m.pf.LanIF && !s.Wireless() && m.self.WiredIF(s.Device) { // ネットに繋がっていない有線だけ (Offline と同じ)
				return s, true
			}
		}
		return netinfo.Service{}, false
	}
	if off := m.self.Offline(); len(off) > 0 {
		return off[0], true
	}
	return netinfo.Service{}, false
}

// setupSteps は画面に並べる手順 (確認待ちなら、相手の固定 IP の後に確認を差し込む)。
func (m *Model) setupSteps() []setup.Step {
	o, _ := m.setupObserved()
	steps := setup.Plan(o)
	if c := m.conf(); c != nil {
		left := int(c.deadline.Sub(m.now).Seconds())
		st := setup.Step{ID: stepConfirm, Title: fmt.Sprintf("新しい IP (%s) で入り直して確認する (残り %d 秒)", c.static.IP, max(left, 0))}
		out := make([]setup.Step, 0, len(steps)+1)
		for _, s := range steps {
			out = append(out, s)
			if s.ID == setup.StepPeerStatic {
				out = append(out, st)
			}
		}
		return out
	}
	return steps
}

// nextStep は x で実行する手順 (済んでいない最初の手順)。
func nextStep(steps []setup.Step) (setup.Step, bool) {
	for _, s := range steps {
		if s.ID == stepConfirm {
			return s, true // 確認待ちは、相手の固定 IP が済んで見えても先にやる
		}
	}
	for _, s := range steps {
		if !s.Done {
			return s, true
		}
	}
	return setup.Step{}, false
}

// usedIPs は帯の番号を決めるときに避ける IP (台帳の内部 IP・この Mac・見えている相手)。電源の切れた Mac は見えないので台帳も入れる。
func (m *Model) usedIPs() []string {
	rows := m.rows()
	used := make([]string, 0, len(m.hosts)+len(m.self.IPs)+2*len(rows)+len(m.claimed))
	for _, ip := range m.claimed { // この起動で割り当てた番号 (台帳に載る前の相手の新しい IP は、台帳にも Bonjour の行にもまだ無い)
		used = append(used, ip)
	}
	for _, h := range m.hosts {
		used = append(used, h.LAN)
	}
	for _, x := range m.self.IPs {
		used = append(used, x.Addr)
	}
	for _, r := range rows {
		used = append(used, r.lanIP, r.mainIP)
	}
	return used
}

// newToken は確認の印の名前 (実行ごとに変える)。
func newToken() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "t" + hex.EncodeToString(b), nil
}

// setupAction は次の手順の確認の板 (y で起こすプロセスか、台帳への書き込み) を組む。組めなければ理由を返す。
func (m *Model) setupAction(st setup.Step) (*ask, string) {
	su := m.su
	o, r := m.setupObserved()
	switch st.ID {
	case setup.StepSelfStatic:
		ip, err := setup.SuggestIP(o.Band, m.usedIPs())
		if err != nil {
			return nil, err.Error()
		}
		argv, err := setup.SelfStaticArgv(o.SelfService, ip)
		if err != nil {
			return nil, err.Error()
		}
		return &ask{title: "この Mac の有線を固定 IP にする", body: []string{
			o.SelfService + " を " + ip + " / 255.255.255.0 (ルーター無し) にする。sudo のパスワードを聞く",
			"🚨 この Mac の変更は自動では戻らない。ルーター無しの固定になるかは未確認 (issue 789)。戻すなら sudo networksetup -setdhcp " + setup.Quote(o.SelfService),
			"$ " + shellLine(argv),
		}, exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID}, ""
	case setup.StepKey:
		argv, err := setup.KeyArgv(su.dest)
		if err != nil {
			return nil, err.Error()
		}
		body := []string{"鍵 (~/.ssh/id_ed25519) が無ければ作り、" + su.dest.User + "@" + su.dest.IP + " に置く。相手のパスワードを聞く"}
		if su.obs.HostKeyUnknown {
			body = append(body, "初めての相手: ssh が相手の指紋を出して続けるかを聞く (相手で ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub と見比べてから yes)")
		}
		for _, l := range strings.Split(argv[2], "\n") {
			body = append(body, "$ "+l)
		}
		return &ask{title: "相手に公開鍵を置く", body: body, exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID}, ""
	case setup.StepPeerStatic:
		used := append(m.usedIPs(), o.SelfIP)
		ip, err := setup.SuggestIP(o.Band, used)
		if err != nil {
			return nil, err.Error()
		}
		tok, err := newToken()
		if err != nil {
			return nil, "確認の印の名前を作れない: " + err.Error()
		}
		s, err := setup.PeerStatic(su.obs, ip, tok)
		if err != nil {
			return nil, err.Error()
		}
		argv, err := setup.PeerStaticArgv(su.dest, s)
		if err != nil {
			return nil, err.Error()
		}
		return &ask{title: "相手の有線を固定 IP にする", body: []string{
			su.obs.Service + " を " + ip + " / 255.255.255.0 (ルーター無し) にする。相手の sudo のパスワードを聞く",
			fmt.Sprintf("変えた後 %d 秒以内に新しい IP で入り直して確認しないと、相手の上で元の設定 (%s) に戻る", int(setup.PeerWindow.Seconds()), s.Restore.Method),
			"$ " + shellLine(argv),
		}, exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID, static: &s}, ""
	case stepConfirm:
		c := m.conf()
		nd := su.dest
		nd.IP = c.static.IP
		argv, err := setup.ConfirmArgv(nd, c.static)
		if err != nil {
			return nil, err.Error()
		}
		return &ask{title: "新しい IP で入り直して確認する", body: []string{
			nd.User + "@" + nd.IP + " に入り直して確認の印を置く (相手の sudo のパスワードを聞く)",
			"$ " + shellLine(argv),
		}, exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID}, ""
	case setup.StepLedger:
		if why := m.ledgerBlocked(su.host, su.obs.IP); why != "" {
			return nil, why
		}
		if m.o.Ledger == nil {
			return nil, "台帳が無い"
		}
		h := ledger.Host{Host: su.host, User: su.dest.User, LAN: su.obs.IP, Alias: r.alias}
		for _, x := range m.hosts {
			if x.Host != su.host && x.LAN == h.LAN {
				return nil, "内部 IP " + h.LAN + " は台帳の " + x.Host + " と同じ (番号を振り直す)"
			}
		}
		return &ask{title: "台帳に内部 IP 付きで載せる", body: []string{
			su.host + " を管理対象にする: user " + h.User + " / 内部 IP " + h.LAN,
		}, yes: func() Notice {
			if err := m.o.Ledger.Upsert(h); err != nil {
				return Notice{"登録できない: " + err.Error(), false}
			}
			m.reloadLedger()
			return Notice{"台帳に載せた: " + su.host, true}
		}}, ""
	}
	return nil, "この手順は実行できない"
}

// keySetup は初期設定の画面のキー。
func (m *Model) keySetup(key string) Action {
	m.settleProgress()
	if m.progressShown() { // 進捗のモーダル: esc / q で閉じる (読み込みは裏で続く)。他のキーは捨てる (読み終える前の板を出さない)
		if key == "esc" || key == "q" {
			m.su.after, m.su.queuedX = "", false
		}
		return None
	}
	switch key {
	case "q", "esc", "h", "left":
		m.closeSetup()
	case "r":
		m.observe() // 読んでいる間は状態の行に出す (toast は出さない)
	case "e":
		m.openLog()
	case "x":
		if m.su.ch != nil && m.conf() == nil { // 確認待ちの間は観測を待たない (確認は観測を使わず、待つ分だけ窓を削った)
			m.su.queuedX, m.su.waitSince = true, m.o.Now() // 読み終えたら次の手順の板を出す (断る toast を出していた。ユーザー報告)
			return None
		}
		m.setupX()
	case "enter":
		m.note("x で実行する", false) // Enter は開閉 (ガイド §3)。手順の実行は x
	}
	return None
}

// setupX は次の手順の確認の板を出す (x。読み込み中に押した x も、読み終えたときにここを通る)。
func (m *Model) setupX() {
	{
		if !m.su.seen && m.conf() == nil {
			// 観測の口が無い (live が相手を読まない版) か、まだ一度も読めていない: 何も分からないまま本物の sudo / ssh-copy-id を起こさない (敵対レビュー)
			m.note("相手を読めていない (この版は相手を読む口が無いか、r で読み直す)", false)
			return
		}
		if errors.Is(m.su.obs.KeyErr, setup.ErrUnknown) && m.conf() == nil {
			// 読みに行ったが届かない (Bonjour の IP へ ssh が届かない等): 届かない相手に向けて、この Mac の固定 (自動では戻らない) から
			// 始めない。鍵が無いだけの相手は KeyErr が nil なので止まらない。確認待ちの間は掛けない: 確認は新しい IP へ入るので観測とは
			// 関係が無く、有線だけの相手は古い IP の観測が届かずに確認まで断られ、期限が切れた (敵対レビュー)
			m.note("相手に届かない (r で読み直す): "+m.su.obs.KeyErr.Error(), false)
			return
		}
		st, ok := nextStep(m.setupSteps())
		if !ok {
			m.note("全部済んでいる", true)
			return
		}
		if st.Blocked != "" {
			m.note(st.Blocked, false)
			return
		}
		a, why := m.setupAction(st)
		if a == nil {
			m.note(why, false)
			return
		}
		if a.exec != nil {
			a.exec.Banner = m.execBanner(a)
		}
		m.ask, m.ov = a, ovConfirm
	}
}

// setupExecDone は初期設定の手順のプロセスから戻ったとき (ExecDone から)。成功したら次の状態へ進め、相手を読み直す。
func (m *Model) setupExecDone(host string, step setup.StepID, static *setup.StaticIP, failed bool) {
	if host == "" {
		return
	}
	now := m.o.Now()
	if step == setup.StepPeerStatic && static != nil {
		// 未確認の印は起こした時点で立ててある。戻った時刻から数え直す (失敗で戻っても相手の上では変わっていることがある)。
		// 確認するか、相手の上の戻しが終わった後に読み直すまで、台帳には書かない (観測だけを信じると、元に戻る相手の新しい IP を
		// 台帳に書けた。敵対レビュー)。印の期限は確認の期限より unconfMargin だけ後ろ: 相手の上の戻しは、手元の期限より後に終わる。
		// 相手の lock を読んで解く形にしないのは、lock が root だけが読める状態ディレクトリにあり、ssh で読めないため
		m.markUnconf(host, static.IP)
	}
	switch {
	case failed:
	case step == setup.StepPeerStatic && static != nil:
		m.pending[host] = &pendingConfirm{static: *static, deadline: now.Add(setup.PeerDelay + setup.PeerWindow)}
		m.claimed[host] = static.IP
	case step == stepConfirm:
		c := m.pending[host]
		delete(m.pending, host)
		if c == nil {
			break
		}
		// 確認の ssh の 0 は「相手の待ちが印を受け取った」(ConfirmCommand は受け取られるまで待つ)。手元の期限を過ぎていても確定している
		delete(m.unconf, host)
		delete(m.tried[host], c.static.IP)
		m.confirmed[host] = c.static.IP
		// 有線でしか入れない相手は、宛先を新しい IP に替える (古い 169.254 に入りに行き続けた。敵対レビュー)
		if m.su != nil && m.su.host == host {
			if _, r := m.setupObserved(); r.mainIP == "" {
				m.su.dest.IP = c.static.IP
			}
		}
	}
	if m.su != nil && m.su.host == host {
		m.su.after, m.su.waitSince = m.stepTitle(string(step)), now // その結果を確かめている間、進捗のモーダルを出す
	}
	if step == setup.StepSelfStatic {
		// この Mac の網も読み直す (相手だけを読み直していたので、固定 IP にしても「未固定」のまま次の手順へ進まなかった。ユーザー報告)
		m.startNetLoad()
	}
	if m.su != nil && m.su.host == host {
		m.observe()
	}
}

// shellLine は argv を確認の板に出す 1 行にする (語ごとに sh の引用)。
func shellLine(argv []string) string {
	words := make([]string, 0, len(argv))
	for _, a := range argv {
		if a != "" && !strings.ContainsAny(a, " \t\n'\"$`\\;&|<>()*?[]{}~#") {
			words = append(words, a)
		} else {
			words = append(words, setup.Quote(a))
		}
	}
	return strings.Join(words, " ")
}
