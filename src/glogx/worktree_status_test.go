package main

import (
	"strings"
)

// statusRec は `git status --porcelain -z` の 1 レコード (NUL 終端) を組む。
func statusRec(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
		b.WriteByte(0)
	}
	return b.String()
}
