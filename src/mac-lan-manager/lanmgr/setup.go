package lanmgr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"

	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/setup"
)

// 段 4 の初期設定。一覧の行で Enter を押すと、その相手の手順の画面になる。手順は観測した状態から毎回組み直す
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
// 閉じて開き直すと消え、Plan が「済み」と見て未確認の IP を台帳に書けた。
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
			m.note("確認の期限が過ぎた: "+host+" は元の設定に戻る (r でリロード)", false)
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
// どれもこれを通す (一覧の space が未確認の印を見ず、未確認の IP を台帳に書けた)。
// ip は書こうとしている内部 IP。
func (m *Model) ledgerBlocked(host, ip string) string {
	if _, ok := m.pending[host]; ok {
		return "固定 IP にした後の確認を待っている (確認してから)"
	}
	if _, ok := m.unconf[host]; ok {
		return "固定 IP にした後、まだ確認していない (確認するか、相手が元に戻る頃に初期設定の画面で r でリロード。相手に接続できるようになるまで解けない)"
	}
	if ip != "" && m.tried[host][ip] {
		return "内部 IP " + ip + " は固定しようとして確認できなかった番号 (相手は元に戻っている。一覧で r でリロード)"
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
// 含めないと確認待ちが消えた後に tick が止まり、読み直しが次のキーまで遅れた)。
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
		m.askSelfStatic()
		return
	}
	d, err := m.destFor(r)
	if err != nil {
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
	if m.ov == ovConfirm && m.ask != nil && m.ask.exec != nil { // 初期設定の手順の板も閉じる (画面の外に残すと、相手の無い手順を起こせた)
		m.ov, m.ask = ovNone, nil
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
	m.logf("observe start %s (%s@%s)", su.host, su.dest.User, su.dest.IP)
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
	m.logf("observe done %s key_ok=%v host_key_unknown=%v err=%v net=%v service=%q ip=%s method=%s (%.1f 秒)", su.host, p.KeyOK, p.HostKeyUnknown,
		p.KeyErr, p.NetKnown, p.Service, p.IP, p.Method, m.o.Now().Sub(su.obsAt).Seconds())
	if p.KeyErr != nil { // 画面には状態の行で出す。理由の全文 (ssh の stderr) は一覧に残す (行は幅で切れる)
		m.record("接続できない ("+su.host+"): "+p.KeyErr.Error(), false)
	}
	// 未確認の固定 IP は、期限の後に始めた観測で解く (相手は元に戻っているか、確認されて新しい IP のまま: どちらも観測が正しい)
	// 読めなかった観測 (届かない・netinfo の失敗) では解かない: 次の観測で読み直す (解くと、後で固定が見えても試した番号が外れなかった)
	if dl, ok := m.unconf[su.host]; ok && su.obsAt.After(dl) && p.NetKnown {
		delete(m.unconf, su.host)
		// 期限の後にまだ試した番号の手動の固定に見えるなら、相手の上で確定している (Script は確認が無ければ必ず戻す。
		// 戻せないのは電源断・kill -9 だけで、そのときも相手はその番号にいる)
		if netinfo.FixedIP(p.Method) {
			delete(m.tried[su.host], p.IP)
		}
	}
}

// destFor は行の Mac に ssh で入る宛先 (Wi-Fi で入れるなら Wi-Fi、有線だけなら有線)。
func (m *Model) destFor(r row) (setup.Dest, error) {
	user, _ := m.userFor(r)
	ip := r.mainIP
	if ip == "" {
		ip = r.lanIP
		if c, ok := m.confirmed[r.host]; ok {
			ip = c // 有線だけの相手は、期限内に確認した新しい IP で入る (行の 169.254 は古い。閉じて開き直すと古い宛先に戻った。敵対レビュー)
		}
	}
	d := setup.Dest{User: user, IP: ip, HostKeyAlias: r.host}
	return d, d.Validate()
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
	o := m.selfObserved()
	o.Peer = setup.PeerObs{
		Host: su.host, WifiIP: r.mainIP, LanIP: r.lanIP, SSHOpen: r.ssh || r.managed,
		KeyOK: su.obs.KeyOK, NetKnown: su.obs.NetKnown, Service: su.obs.Service, IP: su.obs.IP, Method: su.obs.Method,
		Managed: r.managed, LedgerLAN: r.ledgerLAN, AdvertKnown: su.obs.AdvertKnown, Advert: su.obs.Advert,
	}
	return o, r
}

// selfObserved は Plan に渡す観測のうち、この Mac の分 (相手の初期設定と、この Mac だけの固定 (askSelfStatic) が共有する)。
func (m *Model) selfObserved() setup.Observed {
	o := setup.Observed{Band: m.pf.Band}
	if s, ok := m.selfWired(); ok {
		o.SelfService, o.SelfIP, o.SelfMethod, o.SelfMask = s.Name, s.IP, s.Method, s.Mask
	}
	if s, ok := m.self.Main(); ok {
		o.SelfMainIP, o.SelfMainMask = s.IP, s.Mask
	}
	o.SelfHost, o.SelfAdvertKnown, o.SelfAdvert = m.host, m.selfAdvertKnown, m.selfAdvert
	return o
}

// selfStaticRefusal は、この Mac の有線を固定する板を出せない理由 ("" なら出せる。詳細の案内と askSelfStatic が同じ述語を見る)。
func (m *Model) selfStaticRefusal() string {
	// 読んでいる途中・読み損ねた後は、古い網 (固定の前の DHCP / 169.254) で板を組まない (相手の初期設定の x と同じ守り。固定した直後に
	// もう一度 sudo の板が出る形を作らない)
	if m.netCh != nil {
		return "この Mac のネットワークを読んでいる途中 (少し待ってから)"
	}
	if m.selfErr != nil && !errors.Is(m.selfErr, netinfo.ErrIPsIncomplete) {
		return "この Mac のネットワークを読み直せなかった (r で読み直す)"
	}
	s, ok := m.selfWired()
	if !ok && !m.pf.LanByBand {
		// I/F で決める設定が前のアダプタの I/F を指したまま (アダプタを替えた後)。r では直らない
		return "設定の内部の I/F (" + termsafe.PlainLine(m.pf.LanIF) + ") が繋がっていない有線として見つからない (アダプタを替えたなら , の設定で新しい I/F を選ぶ)"
	}
	if !ok {
		return "内部の網に使う有線が見つからない (アダプタを挿してから r で読み直す)"
	}
	if setup.SelfFixed(m.pf.Band, s.Method, s.IP) {
		return "この Mac の有線 (" + termsafe.PlainLine(s.Name) + ") は固定 IP " + s.IP + " になっている"
	}
	return ""
}

// askSelfStatic は「この Mac」の詳細の enter: この Mac の有線を固定 IP にする板を出す (相手の初期設定を通らずに)。
// 手元の有線のアダプタを替えると macOS は別のネットワークのサービスとして扱い、前のサービスの固定 IP は付いてこない
// (2026-10-20 に実際に起きた: 新しいアダプタが 169.254 のままで、相手の有線の IP へ届かなかった)。そのときに相手の初期設定を開かずに直せるように。
// 中身 (サービスと IP の選び方・戻し方の案内・コマンド) は相手の初期設定の段と同じ selfStaticAsk
func (m *Model) askSelfStatic() {
	if why := m.selfStaticRefusal(); why != "" {
		m.note(why, false)
		return
	}
	a, why := m.selfStaticAsk(setup.StepSelfStatic)
	if a == nil {
		m.note(why, false)
		return
	}
	a.selfOnly = true
	a.exec.Banner = m.execBanner(a) // 端末に題と「パスワードはこの下で聞かれる」を出し、失敗したら Enter で戻るまで止める (相手の初期設定の板と同じ)
	m.ask, m.ov = a, ovConfirm
}

// selfStaticAsk はこの Mac の有線を固定 IP にする確認の板 (組めなければ理由)。
func (m *Model) selfStaticAsk(step setup.StepID) (*ask, string) {
	o := m.selfObserved()
	// 前の IP は、この Mac の有線の名前の広告に載せていた IP (この Mac は台帳に載らない)
	var prevs []string
	if o.SelfAdvertKnown {
		prevs = append(prevs, o.SelfAdvert.IP)
	}
	ip, reused, why, err := pickRefixIP(o.Band, m.usedIPsExcept(m.host), m.self.Services, o.SelfService, prevs)
	if err != nil {
		return nil, err.Error()
	}
	mask := netinfo.MaskFor(o.Band)
	argv, err := setup.SelfStaticArgv(o.SelfService, ip, mask)
	if err != nil {
		return nil, err.Error()
	}
	return &ask{title: "この Mac の有線を固定 IP にする", body: dropEmpty(
		o.SelfService+" を "+ip+" / "+mask+" (ルーター無し) にする"+reusedNote(reused)+"。sudo のパスワードを聞く",
		sharedNote(m.self.Services, o.SelfService, ip), why,
		"🚨 この Mac の変更は自動では戻らない。戻すなら "+selfRestoreLine(o),
		"$ "+shellLine(argv),
	), exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: step}, ""
}

// selfWired はこの Mac の内部の網のサービス (I/F で決める設定ならその I/F、帯で決めるならルーターの無い有線の先頭)。
// 受容した限界: 帯で決める設定では、DHCP を一時的に失ってルーターが無く見えるインターネット用の有線も候補に入る。
// 確認の板にサービス名を出しているので、選ぶ前に人が見る。確実にするなら I/F で決める設定にする
func (m *Model) selfWired() (netinfo.Service, bool) {
	if !m.pf.LanByBand {
		for _, s := range m.self.Enabled() {
			if s.Device == m.pf.LanIF && m.self.LanIF(s.Device) { // ネットに繋がっていない有線だけ (Offline と同じ)
				return s, true
			}
		}
		return netinfo.Service{}, false
	}
	off := m.self.Offline()
	for _, s := range off { // 帯に入る番号を持つ有線を先に (一覧の内部 IP (selfLan) と同じサービスを見る。食い違うと札と手順の済みがずれた。敵対レビュー)
		if netinfo.InBand(m.pf.Band, s.IP) && netinfo.FixedIP(s.Method) { // 自動の番号が帯を引いた別の I/F を固定しにいかない (帯が 169.254 の中のとき。敵対レビュー)
			return s, true
		}
	}
	if len(off) > 0 {
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
	for i, s := range steps {
		if !s.Done {
			// この Mac の名前の手順が止まっていても (この Mac の広告を読めない等)、相手の名前の手順は進められる (どちらも仕上げで互いに
			// 依らない)。止まっている手順を飛ばすのはここだけ: 鍵が止まっている間にこの Mac の固定へ進むと、人が相手の
			// 指紋を確かめる前に戻らない変更が走る
			if s.ID == setup.StepSelfName && s.Blocked != "" {
				for _, t := range steps[i+1:] {
					if t.ID == setup.StepPeerName && !t.Done && t.Blocked == "" {
						return t, true
					}
				}
			}
			return s, true
		}
	}
	return setup.Step{}, false
}

// usedIPsExcept は host の Mac を固定するときに避ける IP (他の Mac が持つ・持ちうる IP)。host の Mac に前に振った IP (台帳の内部 IP・
// この起動で振った IP・試した IP・台帳から写した行の内部 IP) は数えない (固定し直すときに前の IP をもう一度振るため。setup.PickIP)。
// host が今 Bonjour で見せている IP は数える (前のアダプタが繋がったままなら、同じ IP を新しい I/F に付けると 1 台の中で重なる。
// Bonjour はこの Mac の I/F ごとに 1 つしか持たないので、相手のサービスの観測 (pickRefixIP) でも見る)。
func (m *Model) usedIPsExcept(host string) []string {
	rows := m.rows()
	used := make([]string, 0, len(m.hosts)+len(m.self.IPs)+2*len(rows)+len(m.claimed))
	for h, ip := range m.claimed { // この起動で割り当てた番号 (台帳に載る前の相手の新しい IP は、台帳にも Bonjour の行にもまだ無い)
		if h != host {
			used = append(used, ip)
		}
	}
	for _, h := range m.hosts {
		if h.Host != host {
			used = append(used, h.LAN)
		}
	}
	// 他の相手に試した番号も避ける: 失敗で戻った固定 (相手の上では変わっていることがある) や、期限が切れて戻している最中・戻せなかった
	// (電源断) 相手は、claimed にも台帳にも行にも無いまま、その番号を持ちうる (別の相手に同じ番号を出した)。
	// host の Mac の試した番号は避けない (同じ Mac に同じ番号を出し直すのは重ならない)。この Mac を固定するときは、開いている相手の
	// 試した番号も避ける (その相手が戻せずに持っている番号を、この Mac が前の IP として取りうる)
	for h, ips := range m.tried {
		if h == host {
			continue
		}
		for ip := range ips {
			used = append(used, ip)
		}
	}
	for _, x := range m.self.IPs {
		used = append(used, x.Addr)
	}
	for _, r := range rows {
		if r.host == host {
			// 行の内部 IP は台帳から写した値のことがある (今は誰も持っていない前の IP)。Bonjour で見えている IP だけを数える
			used = append(used, m.bonjourIPs(host)...)
			continue
		}
		used = append(used, r.lanIP, r.mainIP)
		for _, x := range r.others { // 他の I/F に見えた番号も避ける (帯を 169.254 の中に置く設定では、自動で付いた番号と同じ網に居る)
			used = append(used, x.Addr)
		}
	}
	return used
}

// bonjourIPs は host の Mac が今 Bonjour で見せている IP。
func (m *Model) bonjourIPs(host string) []string {
	var out []string
	for _, p := range m.peers {
		if ledger.Key(p.Host) != host {
			continue
		}
		for _, ip := range p.Addrs {
			out = append(out, ip)
		}
	}
	return out
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
		return m.selfStaticAsk(st.ID)
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
		used := append(m.usedIPsExcept(su.host), o.SelfIP)
		// 前の IP の候補は台帳の内部 IP → 相手が有線の名前の広告に載せていた IP → この起動で振った IP (帯の外・使われているものは飛ばす)
		prevs := []string{o.Peer.LedgerLAN}
		if o.Peer.AdvertKnown {
			prevs = append(prevs, o.Peer.Advert.IP)
		}
		prevs = append(prevs, m.claimed[su.host])
		ip, reused, why, err := pickRefixIP(o.Band, used, su.obs.Services, su.obs.Service, prevs)
		if err != nil {
			return nil, err.Error()
		}
		tok, err := newToken()
		if err != nil {
			return nil, "確認の印の名前を作れない: " + err.Error()
		}
		s, err := setup.PeerStatic(su.obs, ip, netinfo.MaskFor(o.Band), tok)
		if err != nil {
			return nil, err.Error()
		}
		argv, err := setup.PeerStaticArgv(su.dest, s)
		if err != nil {
			return nil, err.Error()
		}
		return &ask{title: "相手の有線を固定 IP にする", body: dropEmpty(
			su.obs.Service+" を "+ip+" / "+s.Mask+" (ルーター無し) にする"+reusedNote(reused)+"。相手の sudo のパスワードを聞く",
			sharedNote(su.obs.Services, su.obs.Service, ip), why,
			fmt.Sprintf("変えた後 %d 秒以内に新しい IP で入り直して確認しないと、相手の上で元の設定 (%s) に戻る", int(setup.PeerWindow.Seconds()), netinfo.MethodLabel(s.Restore.Method)),
			// 実際の argv は戻しの仕掛け (数十行の sh) を 1 語に引用して持つので、全文を出すと板が画面を埋めた (ユーザー報告)。要約を出す
			"$ ssh -t "+su.dest.User+"@"+su.dest.IP+" → sudo networksetup -setmanual "+setup.Quote(s.Service)+" "+ip+" "+s.Mask+
				fmt.Sprintf(" (確認が無ければ %d 秒後に元へ戻す仕掛けを相手で走らせる)", int(setup.PeerWindow.Seconds())),
		), exec: &ExecRequest{Argv: argv, ExitIsFailure: true}, step: st.ID, static: &s}, ""
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
	case setup.StepSelfName, setup.StepPeerName:
		host, ip, who := m.host, o.SelfIP, "この Mac"
		if st.ID == setup.StepPeerName {
			host, ip, who = su.host, su.obs.IP, "相手 ("+su.dest.User+"@"+su.dest.IP+" に鍵で入る)"
		}
		name, err := setup.WiredName(host)
		if err != nil {
			return nil, err.Error()
		}
		a := setup.NameAdvert{Name: name, IP: ip}
		if err := a.Validate(); err != nil {
			return nil, err.Error()
		}
		argv := []string{"sh", "-c", a.InstallScript()}
		if st.ID == setup.StepPeerName {
			argv = su.dest.SSHArgv(false, true, "sh -c "+setup.Quote(a.InstallScript()))
		}
		return &ask{title: st.Title, body: []string{
			who + " で " + name + " → " + ip + " を Bonjour に広告し続ける (ssh / 画面共有 / smb をこの名前で有線へ繋げる)",
			"ユーザーの LaunchAgent (~/Library/LaunchAgents/" + setup.WiredNameLabel + ".plist) が dns-sd -P を起こし続ける。sudo は要らない",
			"$ dns-sd -P " + strings.TrimSuffix(name, ".local") + " … " + name + " " + ip,
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
			// 既に載っている行は内部 IP だけを書き換え、無いときだけ足す (古い一覧の行で Upsert すると、別のプロセスが変えた別名を古い値に
			// 戻した)。既にある行の user は触らない (この手順の役目は内部 IP。別のプロセスが u で変えた・空 (既定) にした値を、開いた時の値や推測の
			// $USER で書き戻した)。使えない形の user だけは直す (一覧はそれを捨てて推測で入るので、板の user と台帳が食い違った)
			set := func(x *ledger.Host) {
				x.LAN = h.LAN
				if x.User != "" && !ledger.ValidUser(x.User) {
					x.User = h.User
				}
			}
			var err error
			if r.managed { // 開いた時は載っていたのに、別のプロセスが外した: 古い一覧の値で作り直さない
				if err = m.o.Ledger.Update(h.Host, set); errors.Is(err, ledger.ErrNotFound) {
					return Notice{"台帳から外されている (一覧を開き直して登録し直す)", false}
				}
			} else {
				err = m.o.Ledger.Merge(h, set)
			}
			if err != nil {
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
	m.settleProgress(false)
	if m.progressShown() { // 進捗のモーダル: esc / q で閉じる (読み込みは裏で続く)。他のキーは捨てる (読み終える前の板を出さない)
		if key == "esc" || key == "q" {
			m.su.after, m.su.queuedX = "", false
		} else {
			// 黙って捨てると、読み込み中に押した y が効いたように見えた (ユーザー報告)
			m.note("確認中: 終わったら次の手順の確認を表示 (esc で閉じる)", false)
		}
		return None
	}
	switch key {
	case "q", "esc", "h", "left":
		m.closeSetup()
	case "r":
		if m.selfErr != nil && m.netCh == nil {
			// 前の読み直しが失敗していたら、この Mac の網も読み直す (相手だけだと「r で読み直す」が効かなかった)。毎回は読まない:
			// 読んでいる間の x は予約に回り、板がすぐ出なくなる
			m.startNetLoad()
		}
		m.observe() // 読んでいる間は状態の行に出す (toast は出さない)
	case "e":
		m.openLog()
	case "x":
		// 読み込み中 (相手の観測・この Mac の網) は予約する。この Mac の網だけを読んでいる間に通すと、読み直す前の古い網で計画を組み、
		// この Mac の固定の板がもう一度出た。確認待ちの間は観測を待たない (確認は観測を使わず、待つ分だけ窓を削った)
		if m.setupBusy() && m.conf() == nil {
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
	m.su.queuedX = false // 自分で板を開いたら予約は済み (残すと、取り消した板を次の tick で開き直した。敵対レビュー)
	{
		if !m.su.seen && m.conf() == nil {
			// 観測の口が無い (live が相手を読まない版) か、まだ一度も読めていない: 何も分からないまま本物の sudo / ssh-copy-id を起こさない
			m.note("まだ接続できていない (r でリロード)", false)
			return
		}
		if errors.Is(m.su.obs.KeyErr, setup.ErrUnknown) && m.conf() == nil {
			// 読みに行ったが届かない (Bonjour の IP へ ssh が届かない等): 届かない相手に向けて、この Mac の固定 (自動では戻らない) から
			// 始めない。鍵が無いだけの相手は KeyErr が nil なので止まらない。確認待ちの間は掛けない: 確認は新しい IP へ入るので観測とは
			// 関係が無く、有線だけの相手は古い IP の観測が届かずに確認まで断られ、期限が切れた
			m.note("相手に届かない (r でリロード): "+m.su.obs.KeyErr.Error(), false)
			return
		}
		if m.selfErr != nil && !errors.Is(m.selfErr, netinfo.ErrIPsIncomplete) && m.conf() == nil {
			// 確認待ちの間は掛けない (確認はこの Mac の網を使わない。断ると窓が削れ、期限で相手が元に戻った)
			// 前に読んだ網は残しているが、それで計画を組まない: この Mac を固定した後の読み直しが失敗すると、固定の前の網で
			// 「この Mac を固定する」の板 (sudo) がもう一度出た
			m.note("この Mac のネットワークを読み直せなかった (r で読み直す)", false)
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
		// 台帳に書けた)。印の期限は確認の期限より unconfMargin だけ後ろ: 相手の上の戻しは、手元の期限より後に終わる。
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
		// 有線でしか入れない相手は、宛先を新しい IP に替える (古い 169.254 に入りに行き続けた)
		if m.su != nil && m.su.host == host {
			if _, r := m.setupObserved(); r.mainIP == "" {
				m.su.dest.IP = c.static.IP
			}
		}
	}
	if m.su != nil && m.su.host == host {
		m.su.after, m.su.waitSince = m.stepTitle(string(step)), now // その結果を確かめている間、進捗のモーダルを出す
	}
	if step == setup.StepPeerName {
		m.startWiredLookups() // 広告した名前を一覧の宛先に使えるか引き直す (この Mac の側は網の読み直しの後に引く)
	}
	if step == setup.StepSelfStatic || step == setup.StepSelfName {
		// この Mac の網も読み直す (相手だけを読み直していたので、固定 IP にしても「未固定」のまま次の手順へ進まなかった。ユーザー報告)
		m.startNetLoad()
	}
	if m.su != nil && m.su.host == host {
		m.observe()
	}
}

// selfRestoreLine はこの Mac の固定を戻すコマンド (今の設定へ。-setdhcp に決め打ちしない: 帯を変えた後に前の帯で固定していた人を
// 元に戻さない)。今の設定が戻し方を組めない形 (マスクが読めない手動など) なら DHCP へ。
func selfRestoreLine(o setup.Observed) string {
	r := setup.Restore{Method: o.SelfMethod, IP: o.SelfIP, Mask: o.SelfMask}
	if r.Validate() != nil {
		r = setup.Restore{Method: netinfo.MethodDHCP}
	}
	return "sudo " + r.Line(o.SelfService)
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

// reusedNote は確認の板で、前と同じ IP をもう一度振るときの一言。
func reusedNote(reused bool) string {
	if reused {
		return " (前と同じ IP)"
	}
	return ""
}

// pickRefixIP は固定する IP を決める (setup.PickIP)。services は固定する Mac の全部のサービス、target は固定するサービス。前の IP を
// 同じ Mac の別のサービスが繋がったまま (Active) 持っていれば振らない (1 台の中の 2 つの I/F に同じ IP が乗る)。繋がっているかを
// 読めなかったサービスも同じに扱う (振らない側に倒す)。I/F が無い・繋がっていないサービス (抜いたアダプタ) が持っているだけなら振る
// (差し替えて使う形。両方を同時に繋ぐと重なるので sharedNote で知らせる)。note は前の IP を振らなかった理由 (無ければ "")。
func pickRefixIP(band string, used []string, services []netinfo.Service, target string, prevs []string) (ip string, reused bool, note string, err error) {
	busy := map[string]string{} // 使っている (かもしれない) IP → サービス名
	for _, sv := range services {
		if sv.Name != target && sv.IP != "" && (sv.Active || !sv.IfRead && !sv.IfGone) {
			used = append(used, sv.IP)
			busy[sv.IP] = sv.Name
		}
	}
	ip, reused, err = setup.PickIP(band, used, prevs...)
	if err == nil && !reused {
		for _, prev := range prevs {
			if name, ok := busy[prev]; ok {
				note = "前の IP (" + prev + ") は " + termsafe.PlainLine(name) + " が繋がったまま使っている (か、繋がっているかを読めなかった) ので振らない。" +
					"そのアダプタを使わないなら抜いてから r で読み直すと、前の IP に戻せる"
				break
			}
		}
	}
	return ip, reused, note, err
}

// sharedNote は、固定する IP を同じ Mac の繋がっていない別のサービスも持っているときの一言 (無ければ "")。
func sharedNote(services []netinfo.Service, target, ip string) string {
	var names []string
	for _, sv := range services {
		if sv.Name != target && sv.IP == ip {
			names = append(names, termsafe.PlainLine(sv.Name)) // サービス名は相手の networksetup の出力 (外から来た値)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "🚨 " + strings.Join(names, " / ") + " も同じ IP の設定を持つ (前のアダプタ)。両方のアダプタを同時に繋ぐと 1 台の中で IP が重なる"
}

// dropEmpty は空の行を落とした板の本文 (出すときだけ足す一言を、空行にしない)。
func dropEmpty(lines ...string) []string {
	return slices.DeleteFunc(lines, func(l string) bool { return l == "" })
}
