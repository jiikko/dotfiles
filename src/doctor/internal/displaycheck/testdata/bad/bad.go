// Package bad は displaycheck の自己テスト用の fixture (違反あり)。
package bad

// Kind は underlying が string の named type。
type Kind string

// Report は表示用の構造体。Missed が関門を通っていない。
type Report struct {
	Free   string
	Missed string
	Kind   Kind
}

// SanitizeForDisplay は関門 (Missed を忘れている)。
func SanitizeForDisplay(rep Report) Report {
	out := rep
	out.Free = sanitizeLine(rep.Free)
	// 🚨 「代入はあるが無害化を通っていない」形。右辺の判定を外すと、これが通ってしまう
	out.Missed = rep.Missed
	return out
}

func sanitizeLine(s string) string { return s }
