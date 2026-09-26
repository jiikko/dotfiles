package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/metrics"
)

// 同じカードの行は後の行を正とし (書いた後、印を持つ前に落ちた 2 度書き)、書きかけの末尾の行に次の行をつなげない。
func TestMetricsLaterRowWinsAndSurvivesTornTail(t *testing.T) {
	dir := t.TempDir()
	if err := AppendMetrics(dir, []metrics.Row{{Card: "C-001", Title: "前"}, {Card: "C-002"}}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, MetricsFile), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"card":"C-0`) // 書きかけで落ちた
	_ = f.Close()
	if err := AppendMetrics(dir, []metrics.Row{{Card: "C-001", Title: "後"}}); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadMetrics(dir)
	if err == nil {
		t.Fatal("読めない行を知らせない")
	}
	if len(rows) != 2 || rows[0].Card != "C-001" || rows[0].Title != "後" || rows[1].Card != "C-002" {
		t.Fatalf("行 = %+v", rows)
	}
}

// 閉じてから MetricsKeep たった行だけを消す (ちょうどで消す)。閉じた時刻を読めない行は残す。
func TestPruneMetricsDropsOnlyExpired(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)
	if err := AppendMetrics(dir, []metrics.Row{
		{Card: "C-001", ClosedAt: now.Add(-MetricsKeep)},
		{Card: "C-002", ClosedAt: now.Add(-MetricsKeep + time.Second)},
		{Card: "C-003", ClosedAt: now.Add(-MetricsKeep - 24*time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(filepath.Join(dir, MetricsFile), os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("{\"card\":\"C-004\",\"closedAt\":\"読めない\"}\n")
	_ = f.Close()
	if n, err := OldMetrics(dir, now); err != nil || n != 2 {
		t.Fatalf("古い行の数 = %d %v", n, err)
	}
	if err := PruneMetrics(dir, now); err != nil {
		t.Fatal(err)
	}
	rows, _ := LoadMetrics(dir)
	if len(rows) != 1 || rows[0].Card != "C-002" {
		t.Fatalf("残った行 = %+v", rows)
	}
	data, _ := os.ReadFile(filepath.Join(dir, MetricsFile))
	if n, _ := OldMetrics(dir, now); n != 0 || !strings.Contains(string(data), "C-004") {
		t.Fatalf("消した後に古い行が残る / 読めない行を消した: %s", data)
	}
}
