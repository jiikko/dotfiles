package disk

import (
	"fmt"
	"strings"
	"time"
)

func HumanSize(n int64) string {
	const unit = 1024.0
	f := float64(n)
	for _, u := range []string{"B", "KB", "MB", "GB", "TB"} {
		if f < unit || u == "TB" {
			if u == "B" {
				return fmt.Sprintf("%d%s", n, u)
			}
			return fmt.Sprintf("%.1f%s", f, u)
		}
		f /= unit
	}
	return ""
}

func riskMark(r Result) string {
	switch r.Status {
	case StatusBlocked:
		return "🚨 " + r.Reason
	case StatusFailed:
		return "❓ 走査できず"
	}
	switch r.Entry.Risk {
	case RiskSafe:
		return "✅ 安全"
	case RiskCaution:
		return "🚨 注意"
	case RiskConfirm:
		return "⛔ 要確認"
	}
	return string(r.Entry.Risk)
}

// Format は人が読む一覧 (占有量の降順)。各行にリスク記号と一行の助言を必ず添える。
// 候補 0 件のエントリは省く (表示が埋まる)。走査できなかったエントリは省かない。
func Format(rep Report, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ディスク診断   合計 %s 解放可能 (blocked / 走査できず は含まない)\n", HumanSize(rep.Total))
	if rep.Partial {
		b.WriteString("🚨  途中で中断されました (部分結果)\n")
	}
	for _, r := range rep.Results {
		if r.Status == StatusOK && len(r.Items) == 0 && len(r.Failures) == 0 {
			continue
		}
		size := HumanSize(r.Size)
		if r.Status == StatusFailed {
			size = "---"
		}
		fmt.Fprintf(&b, "\n%9s  %-48s %s\n", size, r.Entry.Label, riskMark(r))
		if r.Status == StatusFailed {
			fmt.Fprintf(&b, "           %s\n", r.Reason)
			continue
		}
		fmt.Fprintf(&b, "           %s", r.Entry.Recover)
		if r.Entry.Detail != "" {
			fmt.Fprintf(&b, "。%s", r.Entry.Detail)
		}
		b.WriteString("\n")
		if newest := newestMtime(r.Items); !newest.IsZero() {
			fmt.Fprintf(&b, "           最終更新 %s (%d日前)\n", newest.Format("2006-01-02"), int(now.Sub(newest).Hours()/24))
		}
		fmt.Fprintf(&b, "           削除経路: %s  (このツールはまだ削除しません)\n", r.Entry.DeleteVia)
		for _, f := range r.Failures {
			fmt.Fprintf(&b, "           ❓ 一部走査できず (合計に含めていません): %s\n", f)
		}
		if r.Entry.Inspect || r.Entry.Guard == GuardSimRuntime {
			for _, c := range r.Contents {
				fmt.Fprintf(&b, "             - %s\n", c)
			}
		} else if len(r.Items) > 1 {
			for _, it := range r.Items {
				fmt.Fprintf(&b, "             %9s  %s\n", HumanSize(it.Size), it.Path)
			}
		}
	}
	return b.String()
}

func newestMtime(items []Item) time.Time {
	var t time.Time
	for _, it := range items {
		if it.Mtime.After(t) {
			t = it.Mtime
		}
	}
	return t
}
