package store

import (
	"testing"

	"pro-con/schedule"
)

// 失敗として出すか (dispatcher の出来事の種類と画面の赤が同じ判定を使う)。
func TestScheduleRunFailed(t *testing.T) {
	for _, c := range []struct {
		name string
		r    ScheduleRun
		want bool
	}{
		{"成功", ScheduleRun{RC: 0}, false},
		{"失敗", ScheduleRun{RC: 1}, true},
		{"起こせなかった", ScheduleRun{RC: -1}, true},
		{"重なった 1 回目", ScheduleRun{RC: schedule.ExitLocked, Locked: 1}, false},
		{"重なったが続いた", ScheduleRun{RC: schedule.ExitLocked, Locked: schedule.LockedAlarm}, true},
		{"待てなかったが成功の結果の行まで出ていた", ScheduleRun{RC: schedule.RCUnknown, Note: "待てなかった: " + schedule.ResultLine("worktree 消した 1", 0)}, false},
		{"待てなかった・結果の行は失敗", ScheduleRun{RC: schedule.RCUnknown, Note: "待てなかった: " + schedule.ResultLine("worktree 消した 0・失敗 2", 1)}, true},
		{"待てなかった・rc の無い結果の行", ScheduleRun{RC: schedule.RCUnknown, Note: "待てなかった: " + schedule.ResultPrefix + "worktree 消した 1"}, true},
		{"待てなかった・結果の行が無い", ScheduleRun{RC: schedule.RCUnknown, Note: "子を起こした記録が無い"}, true},
	} {
		if got := c.r.Failed(); got != c.want {
			t.Errorf("%s: Failed = %v; want %v", c.name, got, c.want)
		}
	}
}
