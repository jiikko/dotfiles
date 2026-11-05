package health

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	freeBadPct  = 5
	freeBadKB   = 10 * 1024 * 1024
	freeWarnPct = 15
	freeWarnKB  = 30 * 1024 * 1024
	dataVolume  = "/System/Volumes/Data"
)

func (s *scanner) disk(ctx context.Context) []Check {
	return []Check{s.free(ctx), s.snapshots(ctx), {
		ID: "disk.candidates", Label: "掃除の候補", Value: "diskdoctor / tmpclean count で数える", Status: StatusInfo,
		Note: "内訳と削除は既存の道具が持つ (この検査は数えない)",
	}}
}

// free は Data ボリュームの空き。/ はシステムの封印されたボリュームで、使用量が実態を表さない。
func (s *scanner) free(ctx context.Context) Check {
	const id, label = "disk.free", "ディスクの空き (" + dataVolume + ")"
	out, stderr, rc, err := s.cmd(ctx, 10*time.Second, "/bin/df", "-k", dataVolume)
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("df", rc, err, stderr))
	}
	size, avail, ok := parseDF(out)
	if !ok || size <= 0 {
		return unknown(id, label, "df の出力が読めない")
	}
	pct := int(avail * 100 / size)
	c := Check{ID: id, Label: label, Value: fmt.Sprintf("空き %s (%d%%)", fmtMB(avail), pct), Status: StatusOK}
	switch {
	case pct < freeBadPct || avail < freeBadKB:
		c.Status = StatusBad
	case pct < freeWarnPct || avail < freeWarnKB:
		c.Status = StatusWarn
	}
	if c.Status != StatusOK {
		c.Note = "空きが少ないとスワップ・更新・スナップショットが書けず、動作が遅くなる"
		c.Fix = "diskdoctor で候補を見る / cleanup-tmp-dir skill で一時領域を片付ける"
	}
	return c
}

// parseDF は df -k の 2 行目の合計と空き (KB)。
func parseDF(out string) (size, avail int64, ok bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, 0, false
	}
	f := strings.Fields(lines[len(lines)-1])
	if len(f) < 4 {
		return 0, 0, false
	}
	a, e1 := strconv.ParseInt(f[1], 10, 64)
	b, e2 := strconv.ParseInt(f[3], 10, 64)
	return a, b, e1 == nil && e2 == nil
}

func (s *scanner) snapshots(ctx context.Context) Check {
	const id, label = "disk.snapshots", "ローカルスナップショット"
	out, stderr, rc, err := s.cmd(ctx, 20*time.Second, "/usr/bin/tmutil", "listlocalsnapshots", dataVolume)
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("tmutil", rc, err, stderr))
	}
	n := 0
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "com.apple.") {
			n++
		}
	}
	return Check{ID: id, Label: label, Value: fmt.Sprintf("%d 個", n), Status: StatusInfo,
		Note: "空きが足りなくなると OS が自分で消す (Finder の「空き」に含まれる)"}
}
