package main

import "testing"

// pull/push 完了トーストの退場 (leaving) が tick で hidden まで到達することの回帰テスト。
// 「入場は出るが退場が一瞬で消える/動かない」不具合の Update レベル保証 (2026-07-23)。
//
// holding 中の spinnerActive の値で tick チェーンの生存経路が変わる:
//   - usage グランス表示中 (loading=true): holding 中も spinnerActive=true でチェーンが生き続け、
//     退場は「既存チェーン」が運ぶ (toastMsg 時の maybeTick は single-flight で nil を返す)。
//   - usage idle (loading=false): holding で spinnerActive=false になりチェーンが自然停止するので、
//     toastMsg 時の maybeTick が tick を貼り直して退場を駆動する。
// どちらの経路でも退場が完走することを両方回して担保する (片方だけ通って油断しないため)。
func TestToastLeavingReachesHiddenViaTick(t *testing.T) {
	cases := []struct {
		name        string
		usageGlance bool
	}{
		{"usage-loading", true},
		{"usage-idle", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestBrowse(t, 5, nil, nil)
			m.usageOv.visible = tc.usageGlance
			m.toast.show("pulled", true)
			m.maybeTick() // pull/push ハンドラが最初に Batch する tick を開始

			// tea ランタイム模倣: チェーンが生きている (ticking) 限り tickMsg を配信し、
			// 入場を holding まで進める。idle 系は holding で spinnerActive=false になり止まる。
			for i := 0; i < 200 && m.ticking; i++ {
				m.Update(tickMsg{})
			}
			if m.toast.phase != toastHolding {
				t.Fatalf("入場が holding に到達しない: phase=%d shown=%d", m.toast.phase, m.toast.shown)
			}

			// 静止明け (toastHold 経過相当) の toastMsg で退場開始。
			m.Update(toastMsg{seq: m.toast.seq})
			if m.toast.phase != toastLeaving {
				t.Fatalf("toastMsg 後に leaving に入らない: phase=%d", m.toast.phase)
			}

			// 退場チェーンを回し、shown が減って hidden まで到達することを確認。
			for i := 0; i < 200 && m.ticking; i++ {
				m.Update(tickMsg{})
			}
			if m.toast.visible() {
				t.Fatalf("退場が hidden まで到達しない: phase=%d shown=%d", m.toast.phase, m.toast.shown)
			}
		})
	}
}
