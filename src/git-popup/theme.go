package main

import "strconv"

const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"
)

// fg は theme role (theme/colors.yml 由来・themeCterm 経由) の 256 色を前景色にする SGR を返す。
// role が未知なら空文字 (= 色を付けない)。色語彙は単一ソース theme/colors.yml に集約する。
func fg(role string) string {
	c, ok := themeCterm[role]
	if !ok {
		return ""
	}
	return "\x1b[38;5;" + strconv.Itoa(c) + "m"
}

// paintFg は s を theme role の前景色で包む (reset 付き)。role が未知なら素の s を返す。
func paintFg(role, s string) string {
	color := fg(role)
	if color == "" {
		return s
	}
	return color + s + ansiReset
}
