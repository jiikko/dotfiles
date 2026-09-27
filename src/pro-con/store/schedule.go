package store

// 予定 (package schedule。issue 550) を回した記録。書くのは dispatcher だけ、読むのは pro-con config と画面。

import (
	"path/filepath"
	"time"

	"pro-con/schedule"
)

const (
	ScheduleFile   = "schedule.json" // 予定ごとの最後の実行
	ScheduleLogDir = "schedule"      // 予定ごとの出力 (<Name>.out が stdout、<Name>.err が stderr。毎回上書き)
)

// ScheduleRun は予定 1 つの最後の実行。
type ScheduleRun struct {
	Start time.Time `json:"start"`          // 起こした時刻 (起こす前に書く = 途中で落ちても同じ枠で 2 回回さない)
	End   time.Time `json:"end,omitzero"`   // 終わった時刻 (zero なら走っている最中か、dispatcher が途中で落ちた)
	RC    int       `json:"rc"`             // 子の終了コード (起こせなかったは -1、dispatcher が終わりを待てなかったは schedule.RCUnknown)
	Note  string    `json:"note,omitempty"` // 結果の 1 行 (子が出した「結果: …」)。無ければ失敗の理由
	// Locked は続けて「別の実行が lock を持っていたので何もしなかった」(schedule.ExitLocked) で終わった回数。2 回目から失敗として出す
	// (止まったままの実行が lock を握っていると、毎日「重なった」になって片付けが永久に起きない)
	Locked int `json:"locked,omitempty"`
}

// Failed は終わった実行を失敗として出すか (dispatcher の出来事の種類と画面の赤が同じ判定を使う)。
func (r ScheduleRun) Failed() bool {
	switch r.RC {
	case 0:
		return false
	case schedule.ExitLocked: // 1 回なら人の手の実行と重なっただけ
		return r.Locked >= schedule.LockedAlarm
	case schedule.RCUnknown: // 待てなかった: 子が出した結果の行の rc で決める (行が無ければ、終わりまで走ったと示せない)
		rc, ok := schedule.ResultRC(r.Note)
		return !ok || rc != 0
	}
	return true
}

// Schedule は予定の名前 → 最後の実行。
type Schedule map[string]ScheduleRun

// ScheduleOutPath / ScheduleErrPath は予定 name の stdout / stderr の置き場。
func ScheduleOutPath(dir, name string) string { return filepath.Join(dir, ScheduleLogDir, name+".out") }
func ScheduleErrPath(dir, name string) string { return filepath.Join(dir, ScheduleLogDir, name+".err") }

// LoadSchedule は記録を読む。無ければ空。
func LoadSchedule(dir string) (Schedule, error) {
	s, err := loadJSON[Schedule](dir, ScheduleFile)
	if s == nil {
		s = Schedule{}
	}
	return s, err
}

// SaveScheduleRun は予定 name の最後の実行を書く (ほかの予定の記録は残す)。
func SaveScheduleRun(dir, name string, r ScheduleRun) error {
	s, err := LoadSchedule(dir)
	if err != nil {
		return err
	}
	s[name] = r
	return saveJSON(dir, ScheduleFile, s)
}
