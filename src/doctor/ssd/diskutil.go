package ssd

import (
	"bufio"
	"context"
	"strconv"
	"strings"

	"doctor/runner"
)

type diskutilInfo struct {
	model    string
	internal bool
	check    Check
}

// readDiskutil は `diskutil info disk0` の SMART Status を読む。
//
// 判定: Verified = ok / Failing・About to Fail = 異常 / 知らない値・欄が無い・失敗 = 判定不能。
// 🚨 Verified は合否だけの粗い信号。摩耗や整合性エラーは smartctl の行で見る (総合はそちらと合わせて出す)。
func readDiskutil(ctx context.Context, run runner.Runner) diskutilInfo {
	out := diskutilInfo{check: Check{Label: "SMART Status", Status: StatusUnknown, Note: "diskutil"}}
	stdout, stderr, rc, err := runner.WithTimeout(ctx, run, scanTimeout, "diskutil", "info", Device)
	switch {
	case err != nil:
		out.check.Value = "読めません"
		out.check.Note = "diskutil info " + Device + ": " + err.Error()
		return out
	case rc != 0:
		out.check.Value = "読めません"
		out.check.Note = "diskutil info " + Device + ": rc=" + strconv.Itoa(rc) + " " + firstLine(stderr)
		return out
	}
	fields := parseDiskutil(stdout)
	out.model = fields["Device / Media Name"]
	out.internal = fields["Device Location"] == "Internal"
	status, ok := fields["SMART Status"]
	switch {
	case !ok || status == "":
		out.check.Value = "欄がありません"
	case status == "Verified":
		out.check.Value, out.check.Status = status, StatusOK
	case status == "Failing" || status == "About to Fail":
		out.check.Value, out.check.Status = status, StatusFail
	default:
		// 🚨 知らない値 (Not Supported / 言い回しの変更) は判定不能。異常にすると、SMART を
		// 返さないディスクを「壊れている」と言ってしまう (敵対レビュー 2026-09-29 の P2)
		out.check.Value, out.check.Status = status, StatusUnknown
	}
	return out
}

// parseDiskutil は「   Key:   Value」の行を map にする (値の中の : は残す)。
func parseDiskutil(s string) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
