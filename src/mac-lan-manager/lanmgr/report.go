package lanmgr

import (
	"slices"

	"mac-lan-manager/discover"
	"mac-lan-manager/netinfo"
)

// Report は画面と同じ判定で組んだ一覧の写し (`mac-lan-manager dump` が文字と JSON で出す)。TUI を起こさずに「何が見えて、
// どう分類したか」を確かめるための口 (LLM・人のデバッグ用。2026-10-16 ユーザー要望: デバッグを安くする CUI モード)。
// 一覧の行は rows() をそのまま使う (別に組み直すと、画面と dump の判定が食い違う)。
type Report struct {
	Host       string            `json:"host"`      // この Mac の台帳の鍵 (.local のホスト名)
	NetReady   bool              `json:"net_ready"` // この Mac のネットワークを読み終えたか
	NetError   string            `json:"net_error,omitempty"`
	ScanError  string            `json:"scan_error,omitempty"` // Bonjour の走査そのものの失敗 (0 台が「居ない」のか「探せていない」のか)
	Busy       bool              `json:"busy"`                 // まだ走査中 (待ちの上限で打ち切った)
	Services   []netinfo.Service `json:"services"`
	Rows       []ReportRow       `json:"rows"`  // 先頭はこの Mac
	Peers      []discover.Peer   `json:"peers"` // Bonjour の生の結果 (分類の前)
	Notices    []Notice          `json:"notices,omitempty"`
	LedgerSize int               `json:"ledger_size"`
}

// ReportRow は一覧の 1 行。
type ReportRow struct {
	Self      bool       `json:"self,omitempty"`
	Host      string     `json:"host"`
	Alias     string     `json:"alias,omitempty"`
	Instance  string     `json:"instance,omitempty"`
	User      string     `json:"user,omitempty"`
	Managed   bool       `json:"managed"`
	Seen      bool       `json:"seen"`
	SSH       bool       `json:"ssh"`
	VNC       bool       `json:"vnc"`
	SMB       bool       `json:"smb"`
	MainIP    string     `json:"main_ip,omitempty"`
	MainLink  string     `json:"main_link,omitempty"` // メイン IP が見えたこの Mac の I/F の線 (有線 / 無線。分からなければ空)
	LanIP     string     `json:"lan_ip,omitempty"`
	LanLink   string     `json:"lan_link,omitempty"`
	ConnectIP string     `json:"connect_ip,omitempty"`    // 繋ぐ IP (有線か Wi-Fi か)
	Target    string     `json:"target,omitempty"`        // s / Y / y / o が実際に使う宛先 (有線の名前が引ければ名前。wiredname.go)
	Listed    bool       `json:"listed"`                  // 一覧に出る (有線だけに絞る設定で隠れていない)
	LanFixed  bool       `json:"lan_fixed"`               // 内部 IP が固定 (未固定の札・有線の名前を持つかを決める)
	Pending   bool       `json:"setup_pending,omitempty"` // 管理対象の初期設定が途中 (一覧の ! と黄の地。setupPending)
	WiredName string     `json:"wired_name,omitempty"`    // 有線の名前 (内部 IP が固定のときだけ)
	WiredOK   bool       `json:"wired_ok,omitempty"`      // 有線の名前が今その内部 IP だけに引ける
	WiredNote string     `json:"wired_note,omitempty"`    // 引けない理由 (詳細のカードと同じ文言)
	Others    []ReportIP `json:"others,omitempty"`
}

// ReportIP は「その他の IP」の 1 つ (種類は画面に出す名前)。
type ReportIP struct {
	Device string `json:"device"`
	Addr   string `json:"addr"`
	Kind   string `json:"kind"`
	Link   string `json:"link,omitempty"`
}

// Report は今の状態の写し。積んだ知らせは覗くだけで取り出さない (取り出すと、待ちのたびに Report を呼ぶ dump で、開いたときの
// 「台帳が読めない」などが最後の出力から消えた)。
func (m *Model) Report() Report {
	r := Report{Host: m.host, NetReady: m.netReady, Busy: m.Busy(), Services: m.self.Services, Peers: m.peers,
		Notices: slices.Clone(m.notices), LedgerSize: len(m.hosts)}
	if m.selfErr != nil {
		r.NetError = m.selfErr.Error()
	}
	if m.scan != nil && m.scan.Err() != nil {
		r.ScanError = m.scan.Err().Error()
	}
	for _, x := range m.rows() {
		row := ReportRow{Self: x.self, Host: x.host, Alias: x.alias, Instance: x.instance, User: x.user, Managed: x.managed, Seen: x.seen,
			SSH: x.ssh, VNC: x.vnc, SMB: x.smb, MainIP: x.mainIP, LanIP: x.lanIP, ConnectIP: x.connectIP(), Target: m.target(x),
			Listed: x.self || m.listed(x), LanFixed: m.lanFixed(x), Pending: m.setupPending(x),
			MainLink: x.mainLink.Label(), LanLink: x.lanLink.Label()}
		row.WiredName, row.WiredOK, row.WiredNote = m.wiredState(x)
		if x.self {
			row.Target = "" // この Mac の行では s / Y / y / o を断る (宛先は無い)
		}
		for _, o := range x.others {
			row.Others = append(row.Others, ReportIP{Device: o.Device, Addr: o.Addr, Kind: o.Kind.String(), Link: m.self.Link(o.Device).Label()})
		}
		r.Rows = append(r.Rows, row)
	}
	return r
}
