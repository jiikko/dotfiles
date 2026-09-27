package backend

import (
	"os"
	"path/filepath"
	"slices"
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
		i := slices.IndexFunc(rows, func(r ScheduleRow) bool { return r.Command == "pro-con worktree clean --yes" })
		if err != nil || i < 0 {
			t.Fatalf("rows=%v err=%v", rows, err)
		}
		return rows[i]
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

// dispatcher の中で回す行 (1 週間の削除) は出力のファイルを持たず、画面と config show にそう出る (子の行は出力の置き場と .err)。
func TestScheduleRowsInternalHasNoOutput(t *testing.T) {
	dir := t.TempDir()
	rows, err := ScheduleRows(dir, time.Date(2026, 9, 27, 9, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	var internal, child *ScheduleRow
	for i := range rows {
		switch {
		case strings.HasPrefix(rows[i].Command, "(dispatcher の中) "):
			internal = &rows[i]
		case rows[i].Command == "pro-con worktree clean --yes":
			child = &rows[i]
		}
	}
	if internal == nil || child == nil {
		t.Fatalf("中で回す行か子の行が無い: %+v", rows)
	}
	if internal.Out != "" || internal.OutText() != "無い (dispatcher の中で回す。結果は前回の欄と出来事)" {
		t.Errorf("中で回す行の出力 = %q / %q", internal.Out, internal.OutText())
	}
	if want := store.ScheduleOutPath(dir, "worktree-clean"); child.Out != want || child.OutText() != want+" / .err" {
		t.Errorf("子の行の出力 = %q / %q", child.Out, child.OutText())
	}
}

// 中で回す行の終わりの記録が無い (途中で止まった / 書けなかった) とき、子の行の文言 (起こした・lock を持っていれば待つ) を出さない。
func TestScheduleRowsInternalUnfinished(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.Local)
	start := time.Date(2026, 9, 27, 3, 30, 0, 0, time.Local)
	if err := store.SaveScheduleRun(dir, "card-purge", store.ScheduleRun{Start: start, RC: -1}); err != nil {
		t.Fatal(err)
	}
	rows, err := ScheduleRows(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(rows, func(r ScheduleRow) bool { return strings.HasPrefix(r.Command, "(dispatcher の中) ") })
	if i < 0 {
		t.Fatalf("中で回す行が無い: %+v", rows)
	}
	r := rows[i]
	if strings.Contains(r.Last, "起こした") || !strings.Contains(r.Last, "に始めた (終わりの記録が無い") {
		t.Errorf("前回 = %q", r.Last)
	}
	if strings.Contains(r.Next, "lock") || !strings.HasPrefix(r.Next, "09-28 03:30") { // 同じ枠では回し直さない (dispatcher が締めた後も同じ)
		t.Errorf("次回 = %q", r.Next)
	}
}
