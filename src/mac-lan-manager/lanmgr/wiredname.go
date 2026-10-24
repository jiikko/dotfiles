package lanmgr

import (
	"context"
	"slices"
	"time"

	"mac-lan-manager/setup"
)

// 有線の名前 (<ホスト名>.wired.local。setup/wiredname.go) を一覧で使う。名前が今その Mac の内部 IP だけに引けるなら、繋ぐ・コピー・
// 画面共有の宛先を IP から名前に替える (人が覚えて打てる形で渡す。ユーザー指示 2026-10-17「使いやすいように関連の機能に組み込んで」)。
//
// 引けるかは名前の解決 (Options.LookupHost) で見る。初期設定の済み (広告する Mac の launchd の状態) と違い、一覧を開くたびに相手へ
// ssh しないための軽い確かめ。引き直す時期 (wiredRefreshAge) と使えなくなる時期 (wiredMaxAge) を分ける: 引き直しの間は前の結果を使う
// (期限が来てから引き直すと、引いている数秒は IP に戻り、放置した後の最初のキーと c の板が名前と IP で食い違った)。
// 期限が無いと、広告が止まった (相手がログアウトした等) 後も読み直すまで名前を使い続けた (1 周目)。古い・無い結果は tick とキーの入口で
// 引き直す。時刻は壁時計で比べる (単調時計は Mac が眠っている間に進まず、蓋を閉じた数時間を 0 と数えた)。
// 受容: 止めた名前も問い合わせた側のキャッシュに約 4 分残る (実測) ので、その間は止めた名前を宛先に使いうる (IP は同じなので、キャッシュが
// 切れるまでは繋がる)。wiredMaxAge を過ぎるまで放置した後の最初のキーは IP で繋ぐ (画面は前に描いた名前のまま。押した後に描き直す)。

// wiredLookup は 1 台の名前の解決の結果。
type wiredLookup struct {
	name string
	ips  []string
	err  error
	at   time.Time // 引いた時刻 (壁時計)
}

const (
	// wiredRefreshAge を過ぎた結果は引き直す (Bonjour の名前の TTL と同じ 240 秒。実測)
	wiredRefreshAge = 240 * time.Second
	// wiredMaxAge を過ぎた結果は使わない (引き直しが届かないまま放置された)
	wiredMaxAge = 10 * time.Minute
)

// wiredAge は結果の古さ (壁時計。Round(0) で単調時計を外す)。
func (m *Model) wiredAge(w wiredLookup) time.Duration { return m.o.Now().Round(0).Sub(w.at) }

type wiredResult struct {
	host string
	wiredLookup
}

// wiredLookupTimeout は名前 1 つを引く上限 (dns-sd の問い合わせは広告が無いと数秒待つ)。
const wiredLookupTimeout = 3 * time.Second

// startWiredLookups は内部 IP が固定の Mac (この Mac を含む) の有線の名前を裏で引き直す。結果は takeWired が取り込む。
// 前の結果は引き直しの間も残す (引き直すたびに名前が IP に戻って見えると、宛先がちらつく)。
func (m *Model) startWiredLookups() {
	if m.o.LookupHost == nil {
		return
	}
	targets := m.wiredNames()
	lookup := m.o.LookupHost
	ch := make(chan wiredResult, len(targets)) // 前の回のチャネルへ遅れて書く goroutine も、容量があるので止まらない
	m.wiredCh, m.wiredWait = ch, len(targets)
	for host, name := range targets {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), wiredLookupTimeout)
			defer cancel()
			ips, err := lookup(ctx, name)
			ch <- wiredResult{host, wiredLookup{name: name, ips: ips, err: err}}
		}()
	}
}

// takeWired は引き終えた名前を取り込む。
func (m *Model) takeWired() {
	for m.wiredWait > 0 {
		select {
		case r := <-m.wiredCh:
			if m.wired == nil {
				m.wired = map[string]wiredLookup{}
			}
			r.at = m.o.Now().Round(0)
			m.wired[r.host] = r.wiredLookup
			m.wiredWait--
		default:
			return
		}
	}
}

// wiredNames は名前を持つ行 (内部 IP が固定の Mac) の台帳の鍵 → 有線の名前。
func (m *Model) wiredNames() map[string]string {
	out := map[string]string{}
	for _, r := range m.rows() {
		if r.host == "" || !m.lanFixed(r) {
			continue
		}
		if name, err := setup.WiredName(r.host); err == nil {
			out[r.host] = name
		}
	}
	return out
}

// refreshWired は、名前を持つ行に結果が無い (後から見つかった・内部 IP が固定になった) か古い (wiredTTL を過ぎた) なら引き直す。
// 引いている途中なら待つ。tick とキーの入口から呼ぶ。
func (m *Model) refreshWired() {
	if m.o.LookupHost == nil || m.wiredWait > 0 {
		return
	}
	for host := range m.wiredNames() {
		if w, ok := m.wired[host]; !ok || m.wiredAge(w) > wiredRefreshAge {
			m.startWiredLookups()
			return
		}
	}
}

// wiredStatus は有線の名前を確かめた結果の種類 (wiredCheck)。説明文 (note) で分岐しない (文言を直すと判定が黙って変わる)。
type wiredStatus int

const (
	wiredNone    wiredStatus = iota // 名前を持たない (内部 IP が固定でない・ホスト名から名前を作れない)
	wiredOK                         // 今その Mac の内部 IP だけに引ける
	wiredUnknown                    // 確かめていない・確かめている途中・Wi-Fi で繋ぐ相手 (有線の名前を確かめられない)
	wiredMissing                    // 広告されていない・別の IP に向いている (初期設定の最後の手順が済んでいない)
)

// wiredState は行の Mac の有線の名前と、それが今その Mac の内部 IP だけに引けるか。name が空なら名前を持たない (内部 IP が固定でない)。
// note は詳細のカードに出す状態。
func (m *Model) wiredState(r row) (name string, ok bool, note string) {
	name, st, note := m.wiredCheck(r)
	return name, st == wiredOK, note
}

// wiredCheck は wiredState の本体 (結果の種類も返す。一覧の初期設定の印 setupPending が使う)。
func (m *Model) wiredCheck(r row) (name string, st wiredStatus, note string) {
	if r.host == "" || !m.lanFixed(r) {
		return "", wiredNone, ""
	}
	name, err := setup.WiredName(r.host)
	if err != nil {
		return "", wiredNone, ""
	}
	w, found := m.wired[r.host]
	switch {
	case !r.self && r.noWire:
		return name, wiredUnknown, "この Mac に内部の網の有線が無い (Wi-Fi で繋ぐ)"
	case !r.self && r.connectIP() != r.lanIP:
		// 宛先 (target) と同じ条件で見る: 網が分かれて Wi-Fi で繋ぐ相手に「引ける」と出すと、カードの ssh の行 (Wi-Fi の IP) と食い違った
		return name, wiredUnknown, "有線で網が分かれている (Wi-Fi で繋ぐ)"
	case (!found || m.wiredAge(w) > wiredMaxAge) && m.wiredWait > 0:
		return name, wiredUnknown, "確かめている"
	case !found || m.wiredAge(w) > wiredMaxAge:
		return name, wiredUnknown, "確かめていない"
	case w.err != nil || len(w.ips) == 0:
		return name, wiredMissing, "まだ広告されていない (初期設定の最後の手順で広告する)"
	case slices.ContainsFunc(w.ips, func(ip string) bool { return ip != r.lanIP }):
		return name, wiredMissing, "内部 IP と違う IP に向いている (" + w.ips[0] + ")。初期設定で広告し直す"
	}
	return name, wiredOK, ""
}

// setupPending は管理対象の相手の初期設定が「途中」と言い切れるか (一覧の ! と黄の地。ユーザー選定 2026-10-17)。
// 一覧の材料だけで決める: 台帳の内部 IP・内部 IP が固定か・有線の名前がその内部 IP に引けるか (初期設定の最後の手順。前の手順が
// 済んでいないと立たない)。相手の公開鍵は一覧では読めない (ssh が要る) ので見ない。
// 確かめていない・確かめている途中は「途中」にしない (名前を引き終わるたびに印がちらつき、判定できていない行を途中と言い切ることになる)。
func (m *Model) setupPending(r row) bool {
	switch {
	case r.self || !r.managed:
		return false
	case r.ledgerLAN == "":
		return true
	}
	// 台帳に内部 IP があれば行の内部 IP は台帳の値 (rows の lanPicked) で、固定と見る。実機がその番号に居ない (169.254 のまま) なら
	// 名前がその番号に引けないので、下の確かめで途中になる
	_, st, _ := m.wiredCheck(r)
	return st == wiredMissing
}

// target は行の Mac へ繋ぐ宛先 (繋ぐ・コピー・画面共有の 1 か所の出典)。内部 IP で繋ぐ相手で、有線の名前がその IP だけに引けるなら
// 名前、それ以外は IP (Wi-Fi で繋ぐ相手に有線の名前を渡すと、有線が無い所で繋がらない)。
func (m *Model) target(r row) string {
	ip := r.connectIP()
	if ip == "" || ip != r.lanIP {
		return ip
	}
	if name, ok, _ := m.wiredState(r); ok {
		return name
	}
	return ip
}

// login は s・Y・詳細のカードの ssh の宛先 "<ユーザー名>@<target>"。target は呼び出し側が m.target(r) で 1 回だけ取って渡す
// (target は有線の名前の鮮度を時計で見るので、同じカードの中で 2 回取ると ssh と smb の行で宛先が食い違いうる)。
// sshdest.Dest を通さない: Dest は IPv4 だけを表し、target は有線の名前になりうる。断るかどうかは sshRefusal が決める
// (Y とカードは HostKeyAlias を使わないので、鍵の名前に使えないホスト名の機器も IP で案内する)。
func (m *Model) login(r row, target string) string {
	user, _ := m.userFor(r)
	return user + "@" + target
}
