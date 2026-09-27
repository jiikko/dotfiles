package backend

import (
	"fmt"
	"time"

	"pro-con/schedule"
	"pro-con/store"
)

// ScheduleRow は予定 (issue 550) の 1 行 (設定画面の予定のタブと pro-con config が同じ行を出す)。
type ScheduleRow struct {
	When    string // 「毎日 04:00」
	Command string // dispatcher が起こすコマンドの字面 (schedule.Job.Command。起こす argv と同じ引数から作る)
	Last    string // 前回 (まだ回していない / 走っている / 時刻・rc・結果の 1 行)
	Failed  bool   // 前回が失敗した (rc が 0 でない・重なったが続いた。待てなかったが結果の行が無い)
	Next    string // 次回 (時刻。回らない・すぐ回るならその理由)
	Out     string // 出力 (stdout) の置き場。stderr は同じ名前の .err。中で回す行 (schedule.Job.Internal) は空 (出力のファイルが無い)
}

// OutText は「出力」の欄の字面 (設定画面の予定のタブと pro-con config show が同じ字面を出す)。
func (r ScheduleRow) OutText() string {
	if r.Out == "" {
		return "無い (dispatcher の中で回す。結果は前回の欄と出来事)"
	}
	return r.Out + " / .err"
}

// ScheduleReader は設定画面の予定のタブが読む backend (任意。持たない backend はタブに「読めない」と出す)。
// 🚨 読むだけ (--view でも使う)。
type ScheduleReader interface {
	Schedule() ([]ScheduleRow, error)
}

// ScheduleRows は状態の置き場 dir の記録 (store.ScheduleFile)・設定 (schedule on / off)・予定の表 (schedule.Jobs) から行を作る。
// 記録を読めなければ、行は出して err を返す。🚨 「次回」は dispatcher の判定 (schedule.Job.Due と設定) と同じ材料で出す
// (時刻だけを出すと、初回・止めていた間に時刻を過ぎた後に「明日」と出して、実際はその場で回る)。
func ScheduleRows(dir string, now time.Time) ([]ScheduleRow, error) {
	rec, err := store.LoadSchedule(dir)
	set, serr := store.LoadSettings(dir)
	const at = "01-02 15:04:05"
	var rows []ScheduleRow
	for _, j := range schedule.Jobs {
		r, ok := rec[j.Name]
		row := ScheduleRow{When: j.When(), Command: j.Command()}
		if j.Internal == "" {
			row.Out = store.ScheduleOutPath(dir, j.Name)
		}
		switch {
		case err != nil:
			row.Last = "記録を読めない"
		case !ok:
			row.Last = "まだ回していない"
		case r.End.IsZero() && j.Internal != "": // 中で回す行は Tick の中で締めるので、終わりが無いのは途中で止まったか書けなかったとき
			row.Last = r.Start.Local().Format(at) + " に始めた (終わりの記録が無い = 回している最中か、dispatcher が途中で止まったか、終わりを書けなかった。止まっていれば dispatcher が次に起きたときに締める)"
		case r.End.IsZero():
			row.Last = r.Start.Local().Format(at) + " に起こした (終わりの記録はまだ無い = 走っている最中か、dispatcher が次に起きたときに締める)"
		default:
			row.Last = fmt.Sprintf("%s〜%s rc=%d %s", r.Start.Local().Format(at), r.End.Local().Format("15:04:05"), r.RC, r.Note)
			row.Failed = r.Failed()
		}
		switch {
		case serr != nil:
			row.Next = "設定を読めないので回さない (止めたかどうか分からない)"
		case set.ScheduleOff:
			row.Next = "回さない (schedule off)"
		case err != nil:
			row.Next = "記録を読めないので回さない"
		case ok && r.End.IsZero() && j.Internal == "":
			row.Next = "前回が終わってから (終わりの記録が無い間は、前回の子が lock を持っていれば待つ。持っていなければ次の Tick で締めて回す)"
		case j.Due(r.Start, now):
			row.Next = "次に dispatcher が回る Tick (動いていれば数秒のうちに。前回がこの枠より前か、まだ無い)"
		default:
			row.Next = j.Next(now).Format("01-02 15:04") + " (dispatcher が止まっていれば、次に起きた最初の Tick で 1 回だけ回す)"
		}
		rows = append(rows, row)
	}
	return rows, err
}
