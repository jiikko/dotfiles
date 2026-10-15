package lanmgr

import (
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
	LanIP     string     `json:"lan_ip,omitempty"`
	ConnectIP string     `json:"connect_ip,omitempty"` // s / y / o が繋ぐ先
	Others    []ReportIP `json:"others,omitempty"`
}

// ReportIP は「その他の IP」の 1 つ (種類は画面に出す名前)。
type ReportIP struct {
	Device string `json:"device"`
	Addr   string `json:"addr"`
	Kind   string `json:"kind"`
}

// Report は今の状態の写し。積んだ知らせも取り出して載せる。
func (m *Model) Report() Report {
	r := Report{Host: m.host, NetReady: m.netReady, Busy: m.Busy(), Services: m.self.Services, Peers: m.peers,
		Notices: m.TakeNotices(), LedgerSize: len(m.hosts)}
	if m.selfErr != nil {
		r.NetError = m.selfErr.Error()
	}
	if m.scan != nil && m.scan.Err() != nil {
		r.ScanError = m.scan.Err().Error()
	}
	for _, x := range m.rows() {
		row := ReportRow{Self: x.self, Host: x.host, Alias: x.alias, Instance: x.instance, User: x.user, Managed: x.managed, Seen: x.seen,
			SSH: x.ssh, VNC: x.vnc, SMB: x.smb, MainIP: x.mainIP, LanIP: x.lanIP, ConnectIP: x.connectIP()}
		for _, o := range x.others {
			row.Others = append(row.Others, ReportIP{Device: o.Device, Addr: o.Addr, Kind: o.Kind.String()})
		}
		r.Rows = append(r.Rows, row)
	}
	return r
}
