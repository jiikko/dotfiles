package card

// カードの流れ (issue 515)。どの順にレーンを渡り、誰が何で動かすかの正本。画面の ? の「流れ」のタブと pro-con help flow がこれを並べる
// (文面を写さない。直すのはここだけ)。遷移の規則そのものの正本は store の transition で、ここはその説明。

// FlowEnd は移り変わりの端。Other が空ならレーン (Lane)、空でなければレーンでない端 (依頼を出す人・どこからでも・削除)。
type FlowEnd struct {
	Lane  State
	Other string
}

// Label は端の名前 (レーンなら State.Label)。
func (e FlowEnd) Label() string {
	if e.Other != "" {
		return e.Other
	}
	return e.Lane.Label()
}

// FlowStep は移り変わり 1 つ。Who が動かす役、How がその手段 (画面のキー・pro-con card の操作) と注意。
type FlowStep struct {
	From, To FlowEnd
	Who, How string
}

// Stays は列を変えない移り変わり (人に回すだけ)。
func (s FlowStep) Stays() bool { return s.From == s.To }

// Heading は移り変わりの見出しの素の文 (「依頼 → 分解済み」。列を変えないものは「質問待ちのまま」)。画面は色を付けて同じ形に組む。
func (s FlowStep) Heading() string {
	if s.Stays() {
		return s.From.Label() + "のまま"
	}
	return s.From.Label() + " → " + s.To.Label()
}

func at(s State) FlowEnd { return FlowEnd{Lane: s} }

// MainFlow は通常の流れ (左のレーンから右へ)。
var MainFlow = []FlowStep{
	{FlowEnd{Other: "人・外の Claude"}, at(Requested), "人・外の Claude", "画面の n (issue からなら i)・card add"},
	{at(Requested), at(Planned), "PM", "issue に分け、順番と見積もりを付けて積む (card plan)"},
	{at(Planned), at(Running), "dispatcher", "空いた PG を上から起こす (↻ の再開が先。--after の相手が完了するまで待つ)"},
	{at(Running), at(Review), "PG", "終えて出す (card review)"},
	{at(Review), at(Done), "取り込みの係", "diff とテストを確かめ、master へ push して閉じる (card close)"},
}

// FlowDetours は寄り道 (質問・差し戻し・人に回す・その場で閉じる・削除)。
var FlowDetours = []FlowStep{
	{at(Running), at(Waiting), "PG", "質問して turn を終える (card ask)。まず PM が受け、答えるか人に回す"},
	{at(Running), at(Waiting), "dispatcher", "権限の確認で止まった・落ち続けた PG を止めた (どちらも人の番)"},
	{at(Waiting), at(Planned), "PM・人", "答える (r・card answer)。↻ が付き、空いた PG で同じ session を再開"},
	{at(Waiting), at(Running), "人", "権限の確認は a で attach して答えると、そのまま続く"},
	{at(Requested), at(Waiting), "PM", "依頼について人に聞く (card ask。人の番)"},
	{at(Waiting), at(Requested), "人", "答える (r)。PM が回答を読んで分ける"},
	{at(Review), at(Planned), "取り込みの係", "差し戻す (card rework。衝突・テストの失敗)。↻ で同じ PG が直す"},
	{at(Waiting), at(Waiting), "PM", "人に回す (card handoff)。列は変えずに人の番の印が付く"},
	{at(Review), at(Review), "取り込みの係", "人に回す (card handoff)。列は変えずに人の番の印が付き、人が閉じるか差し戻す"},
	{at(Requested), at(Done), "PM", "その場で答えて閉じる・却下する (card close --ending)"},
	{FlowEnd{Other: "どこからでも"}, FlowEnd{Other: "削除"}, "人", "d・card delete (依頼の列はすぐ。ほかは PG を止めてから。worktree とブランチは残る)"},
}

// AfterDone は完了の後に起きること (片付けの時間は store.AutoClearAfter / store.PurgeAfter。issue 497)。
var AfterDone = []string{
	"24 時間 (か x) で書庫へ移り、完了から 1 週間で記録から消える",
	"worktree・ブランチ・session は残る。消すのは人が pro-con worktree clean --yes を打ったときだけ",
}
