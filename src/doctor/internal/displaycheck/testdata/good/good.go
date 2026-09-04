// Package good は displaycheck の自己テスト用の fixture (違反なし)。
package good

// Kind は underlying が string の named type。
type Kind string

// Report は表示用の構造体。
type Report struct {
	Free   string
	Lines  []string
	Ident  string
	Nested []Item
	Count  int
}

// Item はネストした表示用の構造体。
type Item struct {
	Detail string
	Name   string
	Kind   Kind
}

// SanitizeForDisplay は関門。
func SanitizeForDisplay(rep Report) Report {
	out := rep
	out.Free = sanitizeLine(rep.Free)
	out.Lines = sanitizeLines(rep.Lines)
	items := make([]Item, 0, len(rep.Nested))
	for _, it := range rep.Nested {
		it.Detail = sanitizeLine(it.Detail)
		items = append(items, it)
	}
	out.Nested = items
	return out
}

func sanitizeLine(s string) string { return s }

func sanitizeLines(ss []string) []string { return ss }
