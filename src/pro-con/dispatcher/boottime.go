package dispatcher

import (
	"time"

	"golang.org/x/sys/unix"
)

// KernBootTime はマシンの起動時刻 (sysctl kern.boottime。起動時の確かめ = recover.go が使う)。
func KernBootTime() (time.Time, error) {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(tv.Unix()), nil
}
