package card

import (
	"slices"
	"time"
)

// 役 (PM / 取り込みの係) の様子と、カードの担当の出し方 (issue 476)。

// RolePhase は dispatcher が起こす役の今の様子。
type RolePhase string

const (
	RoleOff      RolePhase = "off"  // 起こさない (設定 = off / PM の repo が無い)
	RoleChecking RolePhase = "確かめ中" // dispatcher が起動してからまだ 1 度も確かめていない
	RoleIdle     RolePhase = "idle" // 生きていて turn を終えている
	RoleBusy     RolePhase = "作業中"  // 生きていて turn の途中
	RoleAsking   RolePhase = "入力待ち" // 権限の確認か質問で止まっている (人が attach して答える)
	RoleLaunch   RolePhase = "起動中"  // 起動・再開を始めて、一覧で確かめている
	RoleBlocked  RolePhase = "起こせない"
	RoleDead     RolePhase = "落ちた" // 生きていない (自動の再開を待つか、知らせる物が来たら起こし直す)
	RoleStopped  RolePhase = "止めた" // pro-con の終了で止めた
	RoleNone     RolePhase = "未起動" // 知らせる物が来ていないので起こしていない
	RoleBroken   RolePhase = "読めない"
)

// RoleState は役の、dispatcher が最後に回ったときの様子。dispatcher が Tick ごとに dispatcher-state.json へ書き、画面と card list が読む。
type RoleState struct {
	Name    string    `json:"name"` // PM / 取り込みの係
	Phase   RolePhase `json:"phase"`
	Max     int       `json:"max"`               // 同時に動かす数の上限 (PM は 1 つ。415 の論点 6 / 456 で人が変える)
	Session string    `json:"session,omitempty"` // 今の session の短い id
	Cards   []string  `json:"cards,omitempty"`   // 知らせ済みで、まだ役の列に残っているカード (上ほど優先)
	Why     string    `json:"why,omitempty"`     // 起こせない・起こさない理由 / 様子を読めない理由
	// Current は役が今の turn で扱っている Cards のカード (transcript で最後に対象に ID が出たもの。issue 480)。turn の途中でなければ空
	Current string `json:"current,omitempty"`
	// Last / LastAt は今の turn の最後の道具の呼び出し (「Bash: pro-con card show C-018」) と、呼んだ時刻。Current のカードに添える
	Last   string    `json:"last,omitempty"`
	LastAt time.Time `json:"lastAt,omitzero"`
}

// Alive は session が生きているか (ゲージの「PM n/m」の n)。
func (s RoleState) Alive() bool {
	return s.Phase == RoleIdle || s.Phase == RoleBusy || s.Phase == RoleAsking
}

// PMName は PM の役の名前 (RoleState.Name。dispatcher の役と画面が同じ名前で引く)。
const PMName = "PM"

// FindRole は名前 name の役の様子 (無ければ偽。古い dispatcher の書いた様子には役が無い)。
func FindRole(ss []RoleState, name string) (RoleState, bool) {
	for _, s := range ss {
		if s.Name == name {
			return s, true
		}
	}
	return RoleState{}, false
}

// Working は役が turn の途中か (入力待ちで止まっている turn も含む。Current はこのときだけ意味を持つ)。
func (s RoleState) Working() bool { return s.Phase == RoleBusy || s.Phase == RoleAsking }

// roleOf はカード c が今どの役の番か、その役の様子 (役の番でない・様子が無ければ偽)。
// 人の番 (r で決める) のカードは役の手元に残っていても役の番ではない (人に回した質問・起こさない役の仕事)。
func (c Card) roleOf(r Roles, ss []RoleState) (RoleState, bool) {
	t := c.Turn(r)
	if t != TurnPM && t != TurnIntegrator {
		return RoleState{}, false
	}
	return FindRole(ss, t.Label())
}

// Handling は役が今カード c を扱っていれば、その仕事の名前 (分解中 / 回答中 / レビュー中)。扱っていなければ空。
// 扱っている = 役が turn の途中で、今の turn で扱っているカード (RoleState.Current) が c。🚨 知らせ済みのカードを全部「分解中」にしない:
// PM は 1 回の turn で知らせた数枚を順に扱う (2026-09-25 22:12:16 は 3 枚まとめて知らせた。issue 480)。
func (c Card) Handling(r Roles, ss []RoleState) string {
	s, ok := c.roleOf(r, ss)
	if !ok || s.Phase != RoleBusy || s.Current != c.ID { // 入力待ちの役は手を止めている (人が attach して答えるまで動かない。RoleStep が入力待ちと出す)
		return ""
	}
	switch c.State {
	case Requested:
		return "分解中"
	case Waiting:
		return "回答中"
	case Review:
		return "レビュー中"
	case Planned, Running, Done: // 役の列を離れた (Turn が役の番を返さない)
	}
	return ""
}

// RoleStep は役の番のカードが役の側のどの段階にあるか (issue 480。依頼の列・質問待ち・レビュー待ちのバッジと card list)。
//   - 知らせ待ち: 役がまだ知らない (知らせ済みの Cards に無い。役が落ちている・枠で起こさないときも。理由はゲージ)
//   - 知らせた: 知らせを渡している最中か、役の turn に入ったがまだ扱っていない
//   - 知らせた (手を止めた): 役が turn を終えたのにまだ列に残っている
//   - 分解中 / 回答中 / レビュー中: 今の turn で扱っている (Handling)
//   - 入力待ち: 扱っている最中に権限の確認か質問で止まった
//
// 積んだ・その場で答えて閉じたカードは役の列を離れるので、列の移動と出来事で分かる (ここでは出さない)。役の番でない・役を起こさないなら空。
func (c Card) RoleStep(r Roles, ss []RoleState) string {
	s, ok := c.roleOf(r, ss)
	if !ok || s.Phase == RoleOff {
		return ""
	}
	who := c.Turn(r).Label() + " "
	switch {
	case c.Handling(r, ss) != "":
		return who + c.Handling(r, ss)
	case !slices.Contains(s.Cards, c.ID):
		return who + "知らせ待ち"
	case s.Phase == RoleAsking && s.Current == c.ID:
		return who + "入力待ち"
	case s.Phase == RoleIdle:
		return who + "知らせた (手を止めた)"
	}
	return who + "知らせた"
}

// LastCall は役が今扱っているカード c での最後の道具の呼び出しと時刻 (Handling が空でないときだけ ok)。
func (c Card) LastCall(r Roles, ss []RoleState) (string, time.Time, bool) {
	if c.Handling(r, ss) == "" {
		return "", time.Time{}, false
	}
	s, _ := c.roleOf(r, ss)
	return s.Last, s.LastAt, s.Last != ""
}

// Assignee はカードの担当 = 今そのカードで手を動かす者 (Turn から決め、役が手に取っていれば仕事の名前を添える。誰の番でもなければ空)。
// 🚨 記録の Owner は作った・受けた者で、分解済みでも「PM」のまま残る。担当として出さない (2026-09-25 のボードで C-013〜C-016 が「分解済み 担当: PM」)
func (c Card) Assignee(r Roles, ss []RoleState) string {
	t := c.Turn(r)
	if t == TurnPG && c.State == Planned {
		return "PG 待ち" // PG の空き・前のカードを待っている (まだ誰も手を動かしていない)
	}
	if h := c.Handling(r, ss); h != "" {
		return t.Label() + " " + h
	}
	return t.Label()
}
