package card

import (
	"testing"
	"time"
)

// 人の番は、PM に片付けられない待ち・PM が人に回したもの・起こさない役の仕事。それ以外は動いている係の番。
func TestTurn(t *testing.T) {
	since := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	handed := []Event{{At: since.Add(time.Minute), Text: HandoffText("PM", "方針はユーザーが決める")}}
	stale := []Event{{At: since.Add(-time.Minute), Text: HandoffText("PM", "前の質問")}} // 前の質問を回した記録 (今の質問ではない)
	q := func(h []Event) Card {
		return Card{State: Waiting, Since: since, Wait: Wait{Kind: WaitQuestion}, History: h}
	}
	on, pmOff, intOff := Roles{}, Roles{PMOff: true}, Roles{IntegratorOff: true}
	cases := []struct {
		name string
		c    Card
		r    Roles
		want Turn
	}{
		{"依頼は PM", Card{State: Requested}, on, TurnPM},
		{"PM を起こさないなら依頼は人", Card{State: Requested}, pmOff, TurnHuman},
		{"分解済みは PG", Card{State: Planned}, pmOff, TurnPG},
		{"作業中は PG", Card{State: Running, Wait: Wait{Kind: WaitResource}}, on, TurnPG},
		{"質問はまず PM", q(nil), on, TurnPM},
		{"前の質問を回した記録は今の質問に効かない", q(stale), on, TurnPM},
		{"人に回した質問は人", q(handed), on, TurnHuman},
		{"PM を起こさないなら質問は人", q(nil), pmOff, TurnHuman},
		{"権限の確認は人", Card{State: Waiting, Wait: Wait{Kind: WaitPermission}}, on, TurnHuman},
		{"落ちて止めた PG は人", Card{State: Waiting, Wait: Wait{Kind: WaitCrashed}}, on, TurnHuman},
		{"レビューは取り込みの係", Card{State: Review, Since: since}, on, TurnIntegrator},
		{"人に回したレビューは人", Card{State: Review, Since: since, History: handed}, on, TurnHuman},
		{"取り込みの係を起こさないならレビューは人", Card{State: Review}, intOff, TurnHuman},
		{"PM を起こさなくてもレビューは取り込みの係", Card{State: Review}, pmOff, TurnIntegrator},
		{"完了は誰の番でもない", Card{State: Done}, pmOff, TurnNone},
		{"削除中は誰の番でもない", Card{State: Waiting, Wait: Wait{Kind: WaitPermission}, DeleteAt: since}, on, TurnNone},
		{"片付けたカードは誰の番でもない", Card{State: Review, Archived: true}, intOff, TurnNone},
	}
	for _, tc := range cases {
		if got := tc.c.Turn(tc.r); got != tc.want {
			t.Errorf("%s: %q を返した (want %q)", tc.name, got.Label(), tc.want.Label())
		}
	}
}
