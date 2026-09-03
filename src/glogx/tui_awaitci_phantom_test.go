package main

import (
	"errors"
	"strings"
	"testing"
)

// errUsageProbeSettled は「usage の取得は終わっている」を表すだけの番兵
// (spinnerActive() の usageOv.loading() 項を下ろすために使う)。
var errUsageProbeSettled = errors.New("usage settled (test probe)")

// awaitCI の不変条件は **「awaitCI ⊆ commits の SHA」**。破ると、その要素は
// **どの経路でも取り除かれない** (issue 223):
//   - ciPollTargets は commits を走査するので、commits 外の SHA は追従対象にならない
//   - statuses も永久に現れないので settleAwaitCI の「CI が見えた」分岐に落ちない
//   - ciPollMsg の打ち切り (awaitAttempts) は targets が空だと早期 return で到達しない
//
// 結果 spinnerActive() が下りず、tickMsg の invalidateLines が 80ms ごとに全行を
// 組み直し続ける (画面は静止しているのにアイドルへ戻らない。silent に壊れる)。
const phantomSHA = "0123456789012345678901234567890123456789"

// 入口: refetchAfterPush は commits に無い pushAnimTip を awaitCI へ入れない。
//
// 踏む筋: push 演出中に u で pull → applyLogData が awaitCI を nil にするが pushAnimTip は
// 残る → その pull で履歴が書き換わり tip の SHA が消える → 演出の着地でここへ来る。
func TestRefetchAfterPushRejectsShaOutsideCommits(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.pushAnimTip = phantomSHA // commits に無い SHA (履歴が書き換わって消えた tip)
	m.refetchAfterPush()

	if m.awaitCI[phantomSHA] {
		t.Error("commits に無い SHA が awaitCI に入った (どの経路でも取り除かれない)")
	}
	if m.pushAnimTip != "" {
		t.Error("pushAnimTip が消費されていない (次の演出へ持ち越す)")
	}

	// 対照: commits に在る SHA なら入る (上の assert が「常に空」で通らないことの確認)
	m2 := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m2.pushAnimTip = m2.commits[0].SHA
	m2.refetchAfterPush()
	if !m2.awaitCI[m2.commits[0].SHA] {
		t.Error("commits に在る SHA が awaitCI に入らない (追従が張られない)")
	}
}

// 掃除: settleAwaitCI は commits に無い SHA を毎周期落とす (入口をすり抜けた分の最後の砦)。
func TestSettleAwaitCIDropsShaOutsideCommits(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	live := m.commits[0].SHA
	m.awaitCI = map[string]bool{phantomSHA: true, live: true}

	m.settleAwaitCI()

	if m.awaitCI[phantomSHA] {
		t.Error("commits に無い SHA が残った")
	}
	if !m.awaitCI[live] {
		t.Error("commits に在って CI 未出現の SHA まで落とした (追従が止まる)")
	}
}

// 打ち切り: targets が空なら、残っている awaitCI は定義上 commits の外にいるので空にする。
//
// ⚠️ 打ち切り (awaitAttempts) を早期 return の前へ動かしても効かない。この分岐は
// ciPolling=false でチェーンごと止めるので、1 回数えても次の周期が来ないため。
func TestCIPollClearsPhantomAwaitCIWhenNoTargets(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.awaitCI = map[string]bool{phantomSHA: true}
	m.awaitAttempts = 1
	m.ciPolling = true

	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd != nil {
		t.Error("追従対象が無いのにチェーンを繋いだ")
	}
	if len(m.awaitCI) != 0 {
		t.Errorf("targets が空なのに awaitCI が残った: %v", m.awaitCI)
	}
	if m.awaitAttempts != 0 {
		t.Errorf("awaitAttempts が下りていない: %d", m.awaitAttempts)
	}
}

// 🚨 **両向きを 1 つの表で見る** (issue 223 / 032)。tick チェーンの契約は
// 「必要なときに回る」と「不要になったら止まる」の両方で、これまで前者しか守られていなかった。
// 片側だけのテストは、述語が下りない退行 (= 本件) を通す。
func TestSpinnerActiveBothDirectionsForAwaitCI(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(m *browseModel)
		want  bool
	}{
		{"commits 内の SHA を待っている: 回る", func(m *browseModel) {
			m.awaitCI = map[string]bool{m.commits[0].SHA: true}
		}, true},
		{"待ちが無い: 止まる", func(m *browseModel) {
			m.awaitCI = nil
		}, false},
		{"commits 外の SHA を掃除した後: 止まる", func(m *browseModel) {
			m.awaitCI = map[string]bool{phantomSHA: true}
			m.settleAwaitCI()
		}, false},
		{"commits 外の SHA を ciPollMsg が掃除した後: 止まる", func(m *browseModel) {
			m.awaitCI = map[string]bool{phantomSHA: true}
			m.ciPolling = true
			m.Update(ciPollMsg{gen: m.ciPollGen})
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestBrowse(t, 3, map[string]CIState{}, nil)
			// 🚨 **観測点を飽和させない**。newTestBrowse は usageOv を visible で作るので
			// usageOv.loading() が常に true になり、spinnerActive() は awaitCI に関わらず
			// true のまま = この表は何も測れない (最初に書いたときはこれで 3 ケースが
			// 落ちて気づいた)。usage を「取得済み」にして、awaitCI だけが効く状態にする。
			m.usageOv.err = errUsageProbeSettled
			tc.setup(m)
			if got := m.spinnerActive(); got != tc.want {
				t.Errorf("spinnerActive() = %v, want %v (awaitCI=%v)", got, tc.want, m.awaitCI)
			}
		})
	}
}

// phantomSHA が fixture の SHA と衝突していないこと (衝突すると全部のテストが無意味になる)。
func TestPhantomSHAIsNotInFixtureCommits(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	for _, c := range m.commits {
		if c.SHA == phantomSHA {
			t.Fatalf("fixture の SHA と衝突している: %s", phantomSHA)
		}
	}
	if len(phantomSHA) != len(m.commits[0].SHA) {
		t.Errorf("SHA の長さが違う (%d vs %d)。実物と同じ形にすること",
			len(phantomSHA), len(m.commits[0].SHA))
	}
	if strings.Trim(phantomSHA, "0123456789abcdef") != "" {
		t.Error("SHA が 16 進でない")
	}
}
