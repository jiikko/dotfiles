package main

import (
	"doctor/exitcode"
	"doctor/health"
)

// healthExitCode は Report から終了コードを決める (語彙は diskdoctor / svcdoctor と共通の doctor/exitcode)。
// 判定不能が 1 件でもあれば、注意・異常より優先して 2 を返す: 見えていない項目に異常があるかもしれない。
// 情報 (info) は判定していないので、0 / 1 を左右しない。
func healthExitCode(rep health.Report) int {
	n := rep.Counts()
	switch {
	case n[health.StatusUnknown] > 0:
		return exitcode.Undiagnosed
	case n[health.StatusBad]+n[health.StatusWarn] > 0:
		return exitcode.Findings
	}
	return exitcode.NoFindings
}
