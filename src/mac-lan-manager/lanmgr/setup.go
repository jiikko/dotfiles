package lanmgr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	conf  *pendingConfirm // 相手を固定 IP にした後、確認を待っている
	label string          // 画面の題 (別名か Bonjour の名前)
}

// pendingConfirm は相手を固定 IP にした後の確認待ち。期限を過ぎると相手の上で元に戻っている。
type pendingConfirm struct {
	static   setup.StaticIP
	deadline time.Time
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
	su.ch, su.stop = ch, cancel
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
		su.obs, su.seen, su.ch, su.stop = p, true, nil, nil
	default:
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
func (m *Model) selfWired() (netinfo.Service, bool) {
	if !m.pf.LanByBand {
		for _, s := range m.self.Enabled() {
			if s.Device == m.pf.LanIF && m.self.WiredIF(s.Device) {
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
	if c := m.su.conf; c != nil {
		left := int(c.deadline.Sub(m.now).Seconds())
		st := setup.Step{ID: stepConfirm, Title: fmt.Sprintf("新しい IP (%s) で入り直して確認する (残り %d 秒)", c.static.IP, max(left, 0))}
		if left <= 0 {
			st.Blocked = "期限が過ぎた (相手は元の設定に戻っている。r で読み直す)"
		}
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
	used := make([]string, 0, len(m.hosts)+len(m.self.IPs)+2*len(rows))
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
			"$ " + shellLine(argv),
		}, exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID}, ""
	case setup.StepKey:
		argv, err := setup.KeyArgv(su.dest)
		if err != nil {
			return nil, err.Error()
		}
		return &ask{title: "相手に公開鍵を置く", body: []string{
			"鍵 (~/.ssh/id_ed25519) が無ければ作り、" + su.dest.User + "@" + su.dest.IP + " に置く。相手のパスワードを聞く",
			"$ ssh-copy-id -i ~/.ssh/id_ed25519.pub " + su.dest.User + "@" + su.dest.IP,
		}, exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID}, ""
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
		}, exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID, static: &s}, ""
	case stepConfirm:
		c := su.conf
		nd := su.dest
		nd.IP = c.static.IP
		argv, err := setup.ConfirmArgv(nd, c.static)
		if err != nil {
			return nil, err.Error()
		}
		return &ask{title: "新しい IP で入り直して確認する", body: []string{
			nd.User + "@" + nd.IP + " に入り直して確認の印を置く (相手の sudo のパスワードを聞く)",
		}, exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID}, ""
	case setup.StepLedger:
		if m.o.Ledger == nil {
			return nil, "台帳が無い"
		}
		h := ledger.Host{Host: su.host, User: su.dest.User, LAN: su.obs.IP, Alias: r.alias}
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
	switch key {
	case "q", "esc", "h", "left":
		m.closeSetup()
	case "r":
		m.observe()
		m.note("読み直している", true)
	case "x":
		if m.su.ch != nil {
			m.note("相手を読んでいる (少し待ってから)", false)
			return None
		}
		st, ok := nextStep(m.setupSteps())
		if !ok {
			m.note("全部済んでいる", true)
			return None
		}
		if st.Blocked != "" {
			m.note(st.Blocked, false)
			return None
		}
		a, why := m.setupAction(st)
		if a == nil {
			m.note(why, false)
			return None
		}
		m.ask, m.ov = a, ovConfirm
	case "enter":
		m.note("x で実行する", false) // Enter は開閉 (ガイド §3)。手順の実行は x
	}
	return None
}

// setupExecDone は初期設定の手順のプロセスから戻ったとき (ExecDone から)。成功したら次の状態へ進め、相手を読み直す。
func (m *Model) setupExecDone(step setup.StepID, static *setup.StaticIP, failed bool) {
	su := m.su
	if su == nil {
		return
	}
	switch {
	case failed:
	case step == setup.StepPeerStatic && static != nil:
		su.conf = &pendingConfirm{static: *static, deadline: m.o.Now().Add(setup.PeerDelay + setup.PeerWindow)}
	case step == stepConfirm:
		su.conf = nil
	}
	m.observe()
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
