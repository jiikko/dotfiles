package setup

import "mac-lan-manager/netinfo"

// Observed は初期設定の手順を組む材料 (観測した今の状態)。観測できなかったものは空のまま渡す (推測で埋めない)。
type Observed struct {
	Band string // 内部の網の帯 ("192.168.100.0/24"。設定の帯)

	// この Mac の有線 (内部の網に使う I/F のサービス)。空 = ルーターの無い有線のサービスが無い。SelfMask は戻し方の案内に使う
	SelfService, SelfIP, SelfMethod, SelfMask string
	// SelfNoWire はこの Mac に内部の網に使える有線が 1 本も無い (無線だけで操作している) と読めたこと。このときこの Mac の固定と
	// 名前の広告は任意 (飛ばす) にし、相手の手順は Wi-Fi 越しに進める (ユーザー指示 2026-10-24)。有線が繋がっているのに設定の I/F と
	// 違う・網を読めていないときは立てない (飛ばすと、アダプタを挿し替えた Mac が固定を忘れたまま相手を固定する)
	SelfNoWire bool
	// SelfMainIP / SelfMainMask はこの Mac のメイン (ルーターのある網。Wi-Fi) の IP とマスク。帯がこの網と重なると、その網の宛先が有線へ流れる
	SelfMainIP, SelfMainMask string
	// SelfHost はこの Mac の Bonjour のホスト名 (有線の名前の元)。SelfAdvert はこの Mac の有線の名前の広告の状態 (読めたときだけ SelfAdvertKnown)
	SelfHost        string
	SelfAdvertKnown bool
	SelfAdvert      NameState

	Peer PeerObs
}

// PeerObs は相手の観測。
type PeerObs struct {
	Host      string // 台帳の鍵 (.local のホスト名)
	WifiIP    string // Wi-Fi (メイン) 側で見えた IP。入れるならこちらから変える (変えても ssh が切れない)
	LanIP     string // 有線側で見えた IP
	SSHOpen   bool   // ssh を広告している (リモートログインがオン)
	KeyOK     bool   // 鍵で入れた (BatchMode の ssh が通った)
	NetKnown  bool   // 相手のネットワーク (サービス・方式) を読めた
	Service   string // 相手の有線のサービス名 (ルーターの無いもの)
	IP        string // そのサービスの今の IP
	Method    string // netinfo.MethodDHCP など
	Managed   bool   // 台帳に載っている
	LedgerLAN string // 台帳の内部 IP
	// AdvertKnown / Advert は相手の有線の名前の広告の状態 (観測で読めたときだけ)
	AdvertKnown bool
	Advert      NameState
}

// StepID は手順の名前 (画面と台帳の進み具合のキー)。
type StepID string

// 手順の ID。並びは Plan が返す順 (鍵 → この Mac → 相手 → 台帳 → この Mac の名前 → 相手の名前)。前の手順が済まないと後ろは Blocked。
const (
	StepSelfStatic StepID = "self-static" // この Mac の有線を帯の固定 IP にする (先にしないと、相手を変えた後に有線で届かない)
	StepKey        StepID = "key"         // 相手に自分の公開鍵を置く
	StepPeerStatic StepID = "peer-static" // 相手の有線を帯の固定 IP にする (デッドマンスイッチ付き)
	StepLedger     StepID = "ledger"      // 台帳に内部 IP 付きで載せる
	StepSelfName   StepID = "self-name"   // この Mac の有線の名前 (<ホスト名>.wired.local) を広告する (wiredname.go)
	StepPeerName   StepID = "peer-name"   // 相手の有線の名前を広告する
)

// Step は手順の 1 つ。Done なら済んでいる。Blocked が空でなければ、その理由で今は進めない。Skipped が空でなければ、その理由で
// 任意 (やらずに後ろへ進む)。
type Step struct {
	ID      StepID
	Title   string
	Done    bool
	Blocked string
	Skipped string
}

// Settled は済んだか飛ばす手順か (後ろの手順を止めない)。
func (s Step) Settled() bool { return s.Done || s.Skipped != "" }

// noWireSkip はこの Mac が無線だけのときに飛ばす手順の理由。
const noWireSkip = "この Mac に内部の網の有線が無い (無線だけ)。相手の手順は Wi-Fi で進める"

// SelfFixed はこの Mac の有線 (method / ip) が帯の固定か。画面 (lanmgr の selfLan) と初期設定 (Plan の self) が同じ答えを出すための
// 1 つの述語 (別々に組むと、札は固定なのに初期設定は固定し直そうとした。整合の監査)。手動で帯の中なら固定。方式が読めない (空) ときは、
// 169.254 でない帯の番号なら固定と見る (分からないまま固定し直さない。未固定と見て、前は書けた台帳の内部 IP が書けなくなった)
func SelfFixed(band, method, ip string) bool {
	if _, err := netinfo.ParseBand(band); err != nil || !netinfo.InBand(band, ip) {
		return false
	}
	return netinfo.FixedIP(method) || (method == "" && !netinfo.IsLinkLocal(ip))
}

// Plan は観測から手順を組む。何度呼んでも、済んだ手順は Done で返す (途中で止まっても、観測し直して続きから流せる)。
func Plan(o Observed) []Step {
	_, bandErr := netinfo.ParseBand(o.Band)
	inBand := func(ip string) bool { return netinfo.InBand(o.Band, ip) }
	self := Step{ID: StepSelfStatic, Title: "この Mac の有線を固定 IP にする", Done: SelfFixed(o.Band, o.SelfMethod, o.SelfIP)}
	switch {
	case bandErr == nil && (inBand(o.SelfMainIP) || netinfo.Overlaps(o.Band, o.SelfMainIP, o.SelfMainMask)):
		// 帯がメインの網と重なると、同じ番号の網が有線と Wi-Fi の両方に乗り、メインの網の宛先が有線へ流れる (前の調べで挙げた欠陥)
		self.Blocked = "帯 " + o.Band + " がこの Mac のメインの網 (" + o.SelfMainIP + ") と重なる (, の設定で別の帯にする)"
	case bandErr != nil:
		self.Blocked = "内部の網の帯が無いか使えない形 (, の設定で帯を決める。192.168.100.0/24 の形で /16〜/28)" // 帯が無いと「未固定」と誤った理由で止まった (fable の敵対レビュー)
	case o.SelfService == "" && o.SelfNoWire:
		self.Skipped = noWireSkip
	case o.SelfService == "":
		self.Blocked = "この Mac にルーターの無い有線のサービスが無い (ケーブル・アダプタを確かめる)"
	case !self.Done && o.Peer.WifiIP == "" && !netinfo.LinkLocalBand(o.Band):
		// 帯が 169.254 の中なら止めない: マスクは /16 で、固定した後も 169.254 の相手と同じ網に居る
		// 相手に有線 (169.254) でしか入れないのに、この Mac を先に帯の固定 IP にすると、169.254 の相手に届かなくなる
		self.Blocked = "相手に Wi-Fi (メイン) で入れない。この Mac を先に固定すると有線の 169.254 の相手に届かなくなる (相手を Wi-Fi に繋ぐ)"
	}
	key := Step{ID: StepKey, Title: "相手に公開鍵を置く", Done: o.Peer.KeyOK}
	switch {
	case key.Done:
	case !o.Peer.SSHOpen:
		key.Blocked = "相手のリモートログインがオフ (相手の前でシステム設定 → 共有 → リモートログインをオンに)"
	case o.Peer.WifiIP == "" && o.Peer.LanIP == "": // Wi-Fi で入れなければ有線から入る
		key.Blocked = "相手の IP が分からない"
	}
	// この Mac と同じ IP は済みにしない (衝突)
	peerStatic := o.Peer.NetKnown && netinfo.FixedIP(o.Peer.Method) && inBand(o.Peer.IP) && o.Peer.IP != o.SelfIP
	peer := Step{ID: StepPeerStatic, Title: "相手の有線を固定 IP にする", Done: peerStatic}
	switch {
	case peer.Done:
	case o.Peer.NetKnown && o.Peer.IP != "" && o.Peer.IP == o.SelfIP:
		peer.Blocked = "相手の有線の IP がこの Mac と同じ (" + o.Peer.IP + ")。番号を振り直す"
	case !self.Settled():
		peer.Blocked = "先にこの Mac の有線を固定 IP にする (この Mac が 169.254 のままだと、変えた後の相手に有線で届かない)"
	case !key.Done:
		peer.Blocked = "先に公開鍵を置く"
	case !o.Peer.NetKnown:
		peer.Blocked = "相手のネットワークを読めていない"
	case o.Peer.Service == "":
		peer.Blocked = "相手にルーターの無い有線のサービスが無い"
	}
	ledger := Step{ID: StepLedger, Title: "台帳に内部 IP 付きで載せる", Done: o.Peer.Managed && peerStatic && o.Peer.LedgerLAN == o.Peer.IP}
	if !ledger.Done && !peer.Done {
		ledger.Blocked = "先に相手の有線を固定 IP にする"
	}
	selfName := nameStep(StepSelfName, "この Mac の", o.SelfHost, o.SelfIP, self.Done, o.SelfAdvertKnown, o.SelfAdvert,
		"先にこの Mac の有線を固定 IP にする", "この Mac の名前の広告の状態を読めていない (r でリロード)")
	peerName := nameStep(StepPeerName, "相手の", o.Peer.Host, o.Peer.IP, ledger.Done, o.Peer.AdvertKnown, o.Peer.Advert,
		"先に台帳に内部 IP 付きで載せる", "相手の名前の広告の状態を読めていない (r でリロード)")
	if self.Skipped != "" {
		selfName = Step{ID: StepSelfName, Title: selfName.Title, Skipped: self.Skipped} // 広告する有線の IP が無い
	}
	// 公開鍵を先に置く: 鍵の手順 (端末を持つ ssh-copy-id) で人が相手の指紋を確かめるまで、どちらの Mac の設定も変えない
	// (この Mac の固定を先にしていたので、初めての相手ではホスト鍵を確かめる前に自動では戻らない変更が走った)。
	// 鍵はこの Mac の固定に依らない (相手の Wi-Fi か 169.254 で入る)
	// 有線の名前は最後: 仕上げの手順なので、読めない・止まっていても固定 IP と台帳の手順を止めない (x は済んでいない最初の手順を出すので、
	// 前に置くと「この Mac の名前の広告の状態を読めていない」で相手の固定 IP まで断った)
	return []Step{key, self, peer, ledger, selfName, peerName}
}

// nameStep は有線の名前を広告する手順 (この Mac / 相手で同じ形)。済みは「広告する Mac の launchd が、今の有線の IP でその名前を
// 広告している」(名前の解決では見ない: 止めた名前も問い合わせた側のキャッシュに約 4 分残った。wiredname.go)。
func nameStep(id StepID, who, host, ip string, ready, known bool, st NameState, notReady, unknown string) Step {
	name, err := WiredName(host)
	title := who + "有線の名前を広告する"
	if err == nil {
		title = who + "有線の名前 (" + name + ") を広告する"
	}
	s := Step{ID: id, Title: title, Done: err == nil && ready && known && st.Advertises(NameAdvert{Name: name, IP: ip})}
	switch {
	case s.Done:
	case err != nil:
		s.Blocked = err.Error()
	case !ready:
		s.Blocked = notReady
	case !known:
		s.Blocked = unknown
	}
	return s
}
