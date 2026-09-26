package card

// 列の足跡 (issue 516)。カードが列を移った時刻と、そのとき人の番だったかを 1 件ずつ残す。所要の記録 (package metrics) が
// 列ごとに居た時間と人の番だった時間をここから出す (新しい時計は足さない: 列を移った時刻 = Since を書く時刻そのもの)。
//
// 🚨 列を移すのは Enter だけにする (State と Since を直に書き換えると足跡が抜け、その列に居た時間が前の列に数えられる)。
// 列を変えずに人の番が変わる (PM が人に回した) ときは Mark を呼ぶ。

import "time"

// Step は足跡 1 件。Human は、役を全部起こしている (Roles{}) として人の番だったか (役を起こさない設定は読む側が重ねる)。
type Step struct {
	At    time.Time
	State State
	Human bool `json:",omitempty"`
}

// Enter はカードを to の列へ移し、足跡を残す。待ち (Wait) と履歴は呼ぶ前に整えておく (人の番かはそれで決まる)。
func (c *Card) Enter(to State, now time.Time) {
	c.State, c.Since = to, now
	c.Mark(now)
}

// Mark は今の列と人の番を足跡に残す (最後の足跡と同じなら足さない)。
func (c *Card) Mark(now time.Time) {
	s := Step{At: now, State: c.State, Human: c.Turn(Roles{}) == TurnHuman}
	if n := len(c.Trail); n > 0 && c.Trail[n-1].State == s.State && c.Trail[n-1].Human == s.Human {
		return
	}
	c.Trail = append(c.Trail, s)
}

// HumanUnder は役の設定 r のもとで、この足跡の間が人の番だったか (card.Turn と同じ判定を、足跡に残した分から組み直す)。
func (s Step) HumanUnder(r Roles) bool {
	switch s.State {
	case Requested, Waiting:
		return s.Human || r.PMOff
	case Review:
		return s.Human || r.IntegratorOff
	case Planned, Running, Done:
	}
	return s.Human
}

// 所要の記録が回数を数える履歴の文 (書く側と数える側で同じ文を使う)。
const (
	ResumedPrefix  = "PG を再開した (session " // dispatcher の settle が書く
	ReworkedPrefix = "差し戻した: "
	AskedPrefix    = "質問: " // PG の質問 (PM が依頼について人に聞いた問いは PMAskedPrefix)
	PMAskedPrefix  = "PM が人に質問した: "
	AnsweredMark   = " が回答した: " // 「<答えた者> が回答した: 」。答えた者が PMName なら PM
	RunAskedPrefix = "テストの係に頼んだ: "
)

// LaunchedText は PG を起動・再開した (how = "起動" / "再開") ときの履歴の文。
func LaunchedText(how, session string) string {
	return "PG を" + how + "した (session " + session + ")"
}
