package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pro-con/metrics"
	"pro-con/store"
)

func statsFixture(t *testing.T, now time.Time) string {
	t.Helper()
	dir := t.TempDir()
	usd := 7.0
	pct := usd / metrics.FiveHourUSDPerPct
	rows := []metrics.Row{
		{Card: "C-001", Points: 3, Repo: "dotfiles", RequestedAt: now.Add(-50 * time.Hour), ClosedAt: now.Add(-48 * time.Hour),
			Stay: &metrics.Stays{Running: 3600}, Human: &metrics.Stays{Waiting: 600}, Usage: &metrics.Usage{USD: &usd, FiveHourPct: &pct}},
		{Card: "C-002", Points: 3, Repo: "dotfiles", RequestedAt: now.Add(-40 * 24 * time.Hour), ClosedAt: now.Add(-40*24*time.Hour + 30*time.Minute), UsageMissing: "transcript が無い"},
		{Card: "C-003", Points: 1, Repo: "glogx", RequestedAt: now.Add(-2 * time.Hour), ClosedAt: now.Add(-time.Hour), Stay: &metrics.Stays{}, Human: &metrics.Stays{}},
	}
	if err := store.AppendMetrics(dir, rows); err != nil {
		t.Fatal(err)
	}
	return dir
}

func statsCmd(t *testing.T, dir string, now time.Time, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	rc := runStats(args, dir, func() time.Time { return now }, &out, &errb)
	return out.String(), errb.String(), rc
}

// ポイントごとに件数と中央値 / 最大を 1 行ずつ出す。--since 30d は閉じた時刻で 30 日前から絞る。枠の取れない束ねは「取れなかった」と出す。
func TestStatsByPointsAndSince(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local)
	dir := statsFixture(t, now)
	out, errs, rc := statsCmd(t, dir, now)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%s", rc, errs)
	}
	for _, want := range []string{"閉じたカード 3 枚", "1pt: 1 枚  所要 1h00m / 1h00m  作業中 0s / 0s  人の番 0s / 0s  枠 — (取れなかった)",
		"3pt: 2 枚  所要 1h15m / 2h00m  作業中 1h00m / 1h00m (1 枚から)  人の番 10m / 10m (1 枚から)  枠 $7.00 / $7.00 (5 時間枠 2.0%) (1 枚から)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("出力に %q が無い:\n%s", want, out)
		}
	}
	if strings.Index(out, "1pt:") > strings.Index(out, "3pt:") {
		t.Fatalf("ポイントの小さい順に並べていない:\n%s", out)
	}
	out, _, rc = statsCmd(t, dir, now, "--since", "30d", "--by", "repo")
	if rc != 0 || !strings.Contains(out, "閉じたカード 2 枚") || !strings.Contains(out, "dotfiles: 1 枚") || !strings.Contains(out, "glogx: 1 枚") {
		t.Fatalf("--since 30d --by repo:\n%s", out)
	}
	out, _, _ = statsCmd(t, dir, now, "--json", "--by", "all")
	var gs []metrics.Group
	if err := json.Unmarshal([]byte(out), &gs); err != nil || len(gs) != 1 || gs[0].N != 3 {
		t.Fatalf("--json = %v %s", err, out)
	}
	if _, _, rc := statsCmd(t, dir, now, "--by", "moon"); rc != 2 {
		t.Fatalf("知らない束ね方で rc=%d", rc)
	}
	if out, _, rc := statsCmd(t, t.TempDir(), now); rc != 0 || !strings.Contains(out, "所要の記録がまだ無い") {
		t.Fatalf("記録が無いとき: rc=%d %s", rc, out)
	}
}

// pro-con stats は読むだけ (置き場のファイルを増やさず、変えない)。
func TestStatsDoesNotWrite(t *testing.T) {
	now := time.Now()
	dir := statsFixture(t, now)
	before := snapshotTree(t, dir)
	for _, args := range [][]string{{}, {"--json"}, {"--since", "7d"}, {"--by", "week"}, {"--help"}, {"--by", "moon"}} {
		statsCmd(t, dir, now, args...)
	}
	after := snapshotTree(t, dir)
	if len(before) != len(after) {
		t.Fatalf("置き場のファイルが増減した: 前 %d / 後 %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("pro-con stats が %s を変えた", p)
		}
	}
}
