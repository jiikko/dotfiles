package main

import (
	"time"

	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// usageRefreshMsg は usage オーバーレイの定期リフレッシュ発火 (app.UsageRefreshInterval ごと)。
type usageRefreshMsg struct{}

// usageRefreshTick は次回の usage リフレッシュを app.UsageRefreshInterval 後に予約する tea.Cmd。
// Init で 1 本起動し、usageRefreshMsg ハンドラが毎回 1 本張り直すことで cron 型の単一チェーンに
// なる (発火ごとに +1 予約なので二重化しない)。
// 非フォーカス中に止める案の判断は spinnerActive のコメント参照 (このチェーンも同じ扱い)。
func usageRefreshTick() tea.Cmd {
	return tea.Tick(app.UsageRefreshInterval, func(time.Time) tea.Msg { return usageRefreshMsg{} })
}

// wantsUsageRefresh は 1 分ごとの /usage 再取得を回すか。右上オーバーレイと全画面
// ダッシュボードのどちらかが見えていれば回す (どちらも同じ Snapshot を描くので、取得経路は
// 1 本のまま)。見えていないときに回さない理由は usageRefreshMsg ハンドラの注記。
func (m *browseModel) wantsUsageRefresh() bool {
	return m.usageOv.Visible() || m.rlDash.Visible()
}

// rlDashLoading はダッシュボードが取得待ち (スピナーを回す) か。usageview.UsageOverlay.Loading() は
// 右上オーバーレイの表示状態を見るので、ダッシュボードだけが開いているときは false になる。
func (m *browseModel) rlDashLoading() bool {
	return m.rlDash.Visible() && m.usageOv.Awaiting()
}

// toggleRatelimitDash は全画面 ratelimit ダッシュボードの開閉 (R)。開くときは右上の usage
// オーバーレイを引っ込め (同じ値を 2 か所に出さない)、Snapshot が無い / 古ければ取得を起こす。
// 🚨 取得の判定は usageOv に委ねる (single-flight ガードと cancel を 1 か所に保つため)。
func (m *browseModel) toggleRatelimitDash() tea.Cmd {
	m.rlDash.Toggle()
	if !m.rlDash.Visible() {
		return m.maybeTick()
	}
	m.usageOv.Dismiss()
	if m.usageOv.Snapshot() == nil {
		// 初回はディスクキャッシュを使う (fresh なら subprocess を起こさず即描ける)
		return tea.Batch(m.usageOv.FetchCmd(true), m.maybeTick())
	}
	if m.usageOv.Stale() {
		return tea.Batch(m.usageOv.FetchCmd(false), m.maybeTick())
	}
	return m.maybeTick()
}

// toggleUsage は usage オーバーレイの開閉 (U)。コミット一覧と issues viewer の両方から呼ぶ
// (どちらの画面でも同じ意味にする。viewer 側の合成は viewLines)。
//
// 非表示中は定期リフレッシュを止めているので、再表示のここで陳腐なら取り直す。表示は last-good
// を出したまま静かに差し替わる (snap があれば loading() は false のままでスピナーに落ちない =
// 開いた瞬間に数字が消えない)。
func (m *browseModel) toggleUsage() tea.Cmd {
	m.usageOv.Toggle()
	if !m.usageOv.Visible() {
		return nil
	}
	if m.usageOv.Stale() {
		return tea.Batch(m.usageOv.FetchCmd(false), m.maybeTick())
	}
	return m.maybeTick()
}
