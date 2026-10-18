package main

import (
	"strings"
	"testing"

	"glogx/internal/term"
	"glogx/internal/testenv"
)

// 外部由来の文字列が「無害化を通らずに端末へ出る」sink が残っていないことを、sink ごとに固定する。
// 2026-08-05 に実際に見つかった素通し経路の回帰テスト。

// トーストの文言も表示 sink。gh / git のエラー出力や claude のバージョン文字列を素で
// 埋め込む呼び出しが多いので、setNotice と同じく show 自体を関門にする。
// lastWarning は w でクリップボードへ出るため、そちらも無害化する。
func TestToastAndWarningAreSanitized(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.showWarning("push に失敗: fatal" + testenv.OSC8 + "0;PWNED" + testenv.ST8 + " remote rejected")
	if testenv.HasTerminalControl(m.toast.Text()) {
		t.Errorf("トーストに制御シーケンスが残った: %q", m.toast.Text())
	}
	if testenv.HasTerminalControl(m.lastWarning) {
		t.Errorf("コピー対象の警告に制御シーケンスが残った: %q", m.lastWarning)
	}
	if strings.Contains(m.lastWarning, "PWNED") {
		t.Errorf("OSC の中身が残った: %q", m.lastWarning)
	}
	// 陽性対照: 細工した文言が実際に保持されている。無害化が「丸ごと捨てる」形へ
	// 退行すると上の 3 つの assert は全部素通りする。
	for _, want := range []string{"push に失敗", "remote rejected"} {
		if !strings.Contains(m.lastWarning, want) {
			t.Fatalf("警告に %q が残っていない: %q (検査対象が空)", want, m.lastWarning)
		}
		if !strings.Contains(m.toast.Text(), want) {
			t.Fatalf("トーストに %q が残っていない: %q (検査対象が空)", want, m.toast.Text())
		}
	}
}

// 色なしモードでは外部由来の SGR も枠へ通さない。
//
// term.Paint も scrollbarColumn も colored=false のとき reset を出さないので、閉じていない SGR が
// 1 行でもあると padding・枠・スクロールバー列・後続行まで属性が続く (NO_COLOR 起動時に
// 「以降が全部消える」形の画面破壊になる)。
func TestPanelBoxDropsANSIWhenNotColored(t *testing.T) {
	rows := []string{"\x1b[41;30m閉じていない SGR", "無関係な次の行"}
	for _, line := range term.BuildShadowPanelBox(" title ", rows, 40, false) {
		if strings.Contains(line, "\x1b") {
			t.Errorf("色なしモードなのに ANSI が枠へ出た: %q", line)
		}
	}
	// 色ありモードでは従来どおり通す (色を出すのが仕事なので落とさない)
	got := strings.Join(term.BuildShadowPanelBox(" title ", rows, 40, true), "\n")
	if !strings.Contains(got, "\x1b[41;30m") {
		t.Errorf("色ありモードで外部の SGR まで落とした: %q", got)
	}
}
