package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// pull/push 完了トーストの退場 (leaving) が tick で hidden まで到達することの回帰テスト。
// 「入場は出るが退場が一瞬で消える/動かない」不具合の Update レベル保証 (2026-07-23)。
//
// 退場は手作りの toastMsg を注入せず、tickMsg ハンドラが実際に返した Cmd を実行して回収する。
// これで「ハンドラが退場タイマー (toastHoldCmd) を tea.Batch に載せて返している」配線までを
// 回帰検証できる (載せ忘れると toastMsg が出てこず execForToastMsg が Fatal になる)。
//
// holding 中の spinnerActive の値で tick チェーンの生存経路が変わる:
//   - usage グランス表示中 (loading=true): holding 中も spinnerActive=true でチェーンが生き続け、
//     退場は「既存チェーン」が運ぶ (toastMsg 時の maybeTick は single-flight で nil を返す)。
//   - usage idle (loading=false): holding で spinnerActive=false になりチェーンが自然停止するので、
//     toastMsg 時の maybeTick が tick を貼り直して退場を駆動する。
// どちらの経路でも退場が完走することを両方回して担保する (片方だけ通って油断しないため)。
func TestToastLeavingReachesHiddenViaTick(t *testing.T) {
	orig := toastHold
	toastHold = time.Millisecond // 退場タイマー Cmd を実行しても実時間 (3s) を待たない
	t.Cleanup(func() { toastHold = orig })

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
			m.maybeTick()

			// 入場を holding まで進め、holding へ遷移したフレームで Update が返す Cmd を捕捉する。
			var enterCmd tea.Cmd
			for i := 0; i < 200 && m.ticking; i++ {
				_, enterCmd = m.Update(tickMsg{})
				if m.toast.phase == toastHolding {
					break
				}
			}
			if m.toast.phase != toastHolding {
				t.Fatalf("入場が holding に到達しない: phase=%d shown=%d", m.toast.phase, m.toast.shown)
			}

			// tickMsg ハンドラが実際に返した Cmd から退場タイマー (toastMsg) を回収する
			// (手作り注入だと、ハンドラが toastHoldCmd を Batch に載せ忘れても検出できない)。
			hold := execForToastMsg(t, enterCmd)
			if hold.seq != m.toast.seq {
				t.Fatalf("退場タイマーの seq が現行トーストと不一致: got=%d want=%d", hold.seq, m.toast.seq)
			}
			m.Update(hold)
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

// execForToastMsg は Update が返した Cmd を実行し、退場タイマー (toastMsg) を取り出す。
// tickMsg ハンドラの返りは tea.Batch(maybeTick, toastHoldCmd) なので、Batch を展開して各 Cmd を
// 並行実行し、最初に届いた toastMsg を返す (tick 系 Cmd の待ちに引きずられないように)。Batch に
// toastMsg を返す Cmd が無ければ (= 配線が壊れていれば) timeout して Fatal になる。
func execForToastMsg(t *testing.T, cmd tea.Cmd) toastMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("holding 遷移フレームが Cmd を返さない (退場タイマー未発行)")
	}
	var cmds []tea.Cmd
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		cmds = msg
	case toastMsg:
		return msg
	default:
		t.Fatalf("holding 遷移の Cmd が Batch でも toastMsg でもない: %T", msg)
	}
	ch := make(chan toastMsg, len(cmds))
	for _, c := range cmds {
		if c == nil {
			continue
		}
		go func(c tea.Cmd) {
			if tm, ok := c().(toastMsg); ok {
				ch <- tm
			}
		}(c)
	}
	select {
	case tm := <-ch:
		return tm
	case <-time.After(2 * time.Second):
		t.Fatal("Update の返り Batch に退場タイマー (toastMsg) が含まれていない")
		return toastMsg{}
	}
}
