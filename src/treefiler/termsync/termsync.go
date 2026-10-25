// Package termsync は、外側の端末が同期出力を持たないときに bubbletea へ渡す設定 (bin/treefiler と glogx が使う)。
// filer は描画フレームワークに依存しない (bubbletea の面倒は呼び出し側が見る) ので、ここに分けた。
package termsync

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ProgramOptions は unsynced (tuikit/termcaps.SyncedOutput が false) のとき、bubbletea に同期出力を使わせない設定を返す。
func ProgramOptions(unsynced bool) []tea.ProgramOption {
	if !unsynced {
		return nil
	}
	return []tea.ProgramOption{tea.WithFilter(DropSyncReport)}
}

// DropSyncReport は端末からの同期出力 (DECSET 2026) の対応の答えを捨て、bubbletea に同期出力を使わせない。
// 外側の端末が同期出力を持たないのに tmux の中で使うと、tmux が外側へ書く量が約 5 倍になる (tmux 3.7c と Terminal.app 相当の
// クライアントで、兄弟の移動 1 回が 74〜86KB → 15〜16KB。外側へは書きかけのフレームが届くので、量がそのまま乱れになる)
func DropSyncReport(_ tea.Model, msg tea.Msg) tea.Msg {
	if r, ok := msg.(tea.ModeReportMsg); ok && r.Mode == ansi.ModeSynchronizedOutput {
		return nil
	}
	return msg
}
