// 予約時刻の解釈。ここは純関数だけを置き、time.Now() を触らない (呼び出し側が now を渡す)。
// UI から独立させることで、書式ごとの境界をテストで固定できる。
package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxDigits は入力できる数値の桁数上限。無制限だと掛け算があふれて別の (短い) 時刻に予約される。
const maxDigits = 5

var errBadSpec = errors.New("bad spec")

var (
	// 相対時間: "90" (分) / "1h30m" / "1h" / "30m" / "1h30" / "1:30" (h:mm)
	reMinutes = regexp.MustCompile(`^([0-9]{1,5})$`)
	reHM      = regexp.MustCompile(`^([0-9]{1,5})[hH]([0-9]{1,5})[mM]?$`)
	reHour    = regexp.MustCompile(`^([0-9]{1,5})[hH]$`)
	reMin     = regexp.MustCompile(`^([0-9]{1,5})[mM]$`)
	reColon   = regexp.MustCompile(`^([0-9]{1,5}):([0-9]{1,2})$`)
	// 時刻: "HH:MM" (1〜2 桁の時 + 2 桁の分)
	reClock = regexp.MustCompile(`^([0-9]{1,2}):([0-9]{2})$`)
)

// parseDuration は相対時間の文字列を秒に直す。0 以下・不正・桁超は error。
func parseDuration(in string) (time.Duration, error) {
	s := strings.TrimSpace(in)
	s = strings.ReplaceAll(s, " ", "")
	var h, m int
	switch {
	case reMinutes.MatchString(s):
		m = atoi(reMinutes.FindStringSubmatch(s)[1])
	case reHM.MatchString(s):
		g := reHM.FindStringSubmatch(s)
		h, m = atoi(g[1]), atoi(g[2])
	case reHour.MatchString(s):
		h = atoi(reHour.FindStringSubmatch(s)[1])
	case reMin.MatchString(s):
		m = atoi(reMin.FindStringSubmatch(s)[1])
	case reColon.MatchString(s):
		g := reColon.FindStringSubmatch(s)
		h, m = atoi(g[1]), atoi(g[2])
		if m > 59 {
			return 0, errBadSpec
		}
	default:
		return 0, errBadSpec
	}
	total := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
	if total <= 0 {
		return 0, errBadSpec
	}
	return total, nil
}

// parseClock は "HH:MM" を発火時刻に直す。今日のその時刻が過ぎていれば翌日。
// 秒以下は落とす (分単位の予約なので、00 秒ちょうどに送る)。
func parseClock(in string, now time.Time) (time.Time, error) {
	s := strings.TrimSpace(in)
	g := reClock.FindStringSubmatch(s)
	if g == nil {
		return time.Time{}, errBadSpec
	}
	h, m := atoi(g[1]), atoi(g[2])
	if h > 23 || m > 59 {
		return time.Time{}, errBadSpec
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if !target.After(now) {
		target = target.AddDate(0, 0, 1)
	}
	return target, nil
}

// formatRemaining は残り時間を "1h23m" / "45m" / "30s" / "まもなく" に整える。
func formatRemaining(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s <= 0:
		return "まもなく"
	case s >= 3600:
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	case s >= 60:
		return fmt.Sprintf("%dm", s/60)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
