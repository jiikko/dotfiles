package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/store"
)

// 「次回」は dispatcher の判定と同じ材料で出す: まだ回していない・前回がこの枠より前ならすぐ回る、回した後は次の時刻、
// off なら回さない、設定を読めないなら回さない。前回が失敗なら Failed。
func TestScheduleRowsNext(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.Local)
	next := func() ScheduleRow {
		t.Helper()
		rows, err := ScheduleRows(dir, now)
		if err != nil || len(rows) == 0 {
			t.Fatalf("rows=%v err=%v", rows, err)
		}
		return rows[0]
	}
	if r := next(); r.Last != "まだ回していない" || !strings.Contains(r.Next, "次に dispatcher が回る Tick") || r.Command != "pro-con worktree clean --yes" {
		t.Errorf("記録が無い: %+v", r)
	}
	start := time.Date(2026, 9, 27, 4, 0, 0, 0, time.Local)
	if err := store.SaveScheduleRun(dir, "worktree-clean", store.ScheduleRun{Start: start.AddDate(0, 0, -1), RC: -1}); err != nil {
		t.Fatal(err)
	}
	if r := next(); !strings.Contains(r.Next, "前回が終わってから") { // 前回の子がまだ lock を持っていれば dispatcher は起こさない
		t.Errorf("終わりの記録が無い: %+v", r)
	}
	if err := store.SaveScheduleRun(dir, "worktree-clean", store.ScheduleRun{Start: start, End: start.Add(time.Minute), RC: 1, Note: "x"}); err != nil {
		t.Fatal(err)
	}
	if r := next(); !strings.HasPrefix(r.Next, "09-28 04:00") || !r.Failed || !strings.Contains(r.Last, "rc=1 x") {
		t.Errorf("今日回した後: %+v", r)
	}
	if err := os.WriteFile(filepath.Join(dir, store.SettingsFile), []byte(`{"schedule_off":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := next(); !strings.Contains(r.Next, "回さない (schedule off)") {
		t.Errorf("off: %+v", r)
	}
	if err := os.WriteFile(filepath.Join(dir, store.SettingsFile), []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := next(); !strings.Contains(r.Next, "設定を読めないので回さない") {
		t.Errorf("設定を読めない: %+v", r)
	}
}
