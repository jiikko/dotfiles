package setup

import "mac-lan-manager/netinfo"

// Observed は初期設定の手順を組む材料 (観測した今の状態)。観測できなかったものは空のまま渡す (推測で埋めない)。
type Observed struct {
	Band string // 内部の網の帯 ("192.168.100.0/24"。設定の帯)

	// この Mac の有線 (内部の網に使う I/F のサービス)。空 = ルーターの無い有線のサービスが無い。SelfMask は戻し方の案内に使う
	SelfService, SelfIP, SelfMethod, SelfMask string
	// SelfMainIP はこの Mac のメイン (ルーターのある網。Wi-Fi) の IP。帯がこの網と重なると、その網の宛先が有線へ流れる
	SelfMainIP string

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
	Method    string // "DHCP" / "manual"
	Managed   bool   // 台帳に載っている
	LedgerLAN string // 台帳の内部 IP
}

// StepID は手順の名前 (画面と台帳の進み具合のキー)。
type StepID string

// 手順の ID。並びは Plan が返す順 (鍵 → この Mac → 相手 → 台帳)。前の手順が済まないと後ろは Blocked。
const (
	StepSelfStatic StepID = "self-static" // この Mac の有線を帯の固定 IP にする (先にしないと、相手を変えた後に有線で届かない)
	StepKey        StepID = "key"         // 相手に自分の公開鍵を置く
	StepPeerStatic StepID = "peer-static" // 相手の有線を帯の固定 IP にする (デッドマンスイッチ付き)
	StepLedger     StepID = "ledger"      // 台帳に内部 IP 付きで載せる
)

// Step は手順の 1 つ。Done なら済んでいる。Blocked が空でなければ、その理由で今は進めない。
type Step struct {
	ID      StepID
	Title   string
	Done    bool
	Blocked string
	Via     string // 相手に入る先の IP (StepKey / StepPeerStatic。Wi-Fi で入れるなら Wi-Fi)
}

// Plan は観測から手順を組む。何度呼んでも、済んだ手順は Done で返す (途中で止まっても、観測し直して続きから流せる)。
func Plan(o Observed) []Step {
	_, bandErr := netinfo.ParseBand(o.Band)
	inBand := func(ip string) bool { return netinfo.InBand(o.Band, ip) }
	via := o.Peer.WifiIP
	if via == "" {
		via = o.Peer.LanIP
	}
	self := Step{ID: StepSelfStatic, Title: "この Mac の有線を固定 IP にする", Done: o.SelfMethod == "manual" && inBand(o.SelfIP)}
	switch {
	case bandErr == nil && inBand(o.SelfMainIP):
		// 帯がメインの網と重なると、同じ番号の網が有線と Wi-Fi の両方に乗り、メインの網の宛先が有線へ流れる (前の調べで挙げた欠陥)
		self.Blocked = "帯 " + o.Band + " がこの Mac のメインの網 (" + o.SelfMainIP + ") と重なる (, の設定で別の帯にする)"
	case bandErr != nil:
		self.Blocked = "内部の網の帯が無いか使えない形 (, の設定で帯を決める。192.168.100.0/24 の形で /16〜/28)" // 帯が無いと「未固定」と誤った理由で止まった (fable の敵対レビュー)
	case o.SelfService == "":
		self.Blocked = "この Mac にルーターの無い有線のサービスが無い (ケーブル・アダプタを確かめる)"
	case !self.Done && o.Peer.WifiIP == "" && !netinfo.LinkLocalBand(o.Band):
		// 帯が 169.254 の中なら止めない: マスクは /16 で、固定した後も 169.254 の相手と同じ網に居る
		// 相手に有線 (169.254) でしか入れないのに、この Mac を先に帯の固定 IP にすると、169.254 の相手に届かなくなる (敵対レビュー)
		self.Blocked = "相手に Wi-Fi (メイン) で入れない。この Mac を先に固定すると有線の 169.254 の相手に届かなくなる (相手を Wi-Fi に繋ぐ)"
	}
	key := Step{ID: StepKey, Title: "相手に公開鍵を置く", Done: o.Peer.KeyOK, Via: via}
	switch {
	case key.Done:
	case !o.Peer.SSHOpen:
		key.Blocked = "相手のリモートログインがオフ (相手の前でシステム設定 → 共有 → リモートログインをオンに)"
	case via == "":
		key.Blocked = "相手の IP が分からない"
	}
	// この Mac と同じ IP は済みにしない (衝突。fable の敵対レビュー)
	peerStatic := o.Peer.NetKnown && o.Peer.Method == "manual" && inBand(o.Peer.IP) && o.Peer.IP != o.SelfIP
	peer := Step{ID: StepPeerStatic, Title: "相手の有線を固定 IP にする", Done: peerStatic, Via: via}
	switch {
	case peer.Done:
	case o.Peer.NetKnown && o.Peer.IP != "" && o.Peer.IP == o.SelfIP:
		peer.Blocked = "相手の有線の IP がこの Mac と同じ (" + o.Peer.IP + ")。番号を振り直す"
	case !self.Done:
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
	// 公開鍵を先に置く: 鍵の手順 (端末を持つ ssh-copy-id) で人が相手の指紋を確かめるまで、どちらの Mac の設定も変えない
	// (この Mac の固定を先にしていたので、初めての相手ではホスト鍵を確かめる前に自動では戻らない変更が走った。敵対レビュー)。
	// 鍵はこの Mac の固定に依らない (相手の Wi-Fi か 169.254 で入る)
	return []Step{key, self, peer, ledger}
}
