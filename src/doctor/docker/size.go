package docker

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// docker はサイズを 10 進 (1kB = 1000B) で出す (`docker/go-units` の HumanSize)。
// 🚨 disk パッケージの HumanSize は 1024 進なので、**docker の数字をそちらで丸め直さない**
// (同じ資源が画面上で 2 通りの GB 表記になる)。この群の合計はここで組む。
var sizeUnits = map[string]float64{
	"b": 1, "kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12, "pb": 1e15,
}

// parseSize は docker の表記をバイト数にする。読めない / "N/A" は 0
// (合計に足さないだけで、行は落とさない)。
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}
	if i := strings.IndexByte(s, '('); i > 0 { // "255.7MB (5%)"
		s = strings.TrimSpace(s[:i])
	}
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s[:i]), 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	mul, ok := sizeUnits[unit]
	if !ok {
		return 0
	}
	// 🚨 切り捨てない: 65.46 * 1e9 は 65459999999.99… になり、docker の申告と 1 バイトずれる
	return int64(math.Round(n * mul))
}

// HumanSize は docker と同じ 10 進表記に戻す (合計の表示用)。
func HumanSize(n int64) string {
	f := float64(n)
	for _, u := range []string{"B", "kB", "MB", "GB", "TB"} {
		if f < 1000 || u == "TB" {
			if u == "B" {
				return fmt.Sprintf("%dB", n)
			}
			return fmt.Sprintf("%.1f%s", f, u)
		}
		f /= 1000
	}
	return ""
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
