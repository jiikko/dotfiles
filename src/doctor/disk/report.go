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
		// UI と同じく**固定語彙**。可変長の理由をここに置くと、幅の狭い端末で
		// 切れて意味が失われる (issue 182)。理由は Format が下の行に出す。
		// caution (🚨) と記号を分けるのは NO_COLOR で区別を残すため
		return "🚫 対象外"
	case StatusFailed:
		return "❓ 走査できず"
	case StatusOK:
		// 検出条件が未実測で候補 0 件。「✅ 安全」は「調べたうえで安全」の意味なので出さない。
		// 走査自体は成功しているので「❓ 走査できず」とも別語彙にする (UI 側と同じ記号)。
		if r.Entry.Unverified != "" && len(r.Items) == 0 {
			return "🔎 未検証"
		}
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
	shown := 0
	for _, r := range rep.Results {
		// 候補 0 件は畳む。⚠️ **検出条件そのものが未実測のエントリは畳まない** (issue 169 / 207)。
		// 畳むと「名前が違って 1 件も当たらなかった」が「候補なし = きれい」と同じ見え方になる。
		// UI 側 (src/glogx/doctor_view.go) と同じ規律。実測で Entry.Unverified が空になれば畳まれる側へ戻る
		if r.Status == StatusOK && len(r.Items) == 0 && len(r.Failures) == 0 &&
			r.Entry.Unverified == "" {
			continue
		}
		shown++
		size := HumanSize(r.Size)
		if r.Status == StatusFailed {
			size = "---"
		}
		// ⚠️ ラベルを `%-48s` で pad しない。Go の幅指定は**バイト数**なので、日本語ラベルでは
		// 1 文字 3 バイトと数えられて列が行ごとにずれる (issue 182)。doctor module は幅計算の
		// 依存を持たないので、**揃えるのを諦めて**マークを先に出す (size は ASCII なので %9s が効く)。
		// 揃えたくなったら表示幅を測る依存を足すこと。UI 側 (glogx) は termwidth を持つので揃えている
		fmt.Fprintf(&b, "\n%9s  %s  %s\n", size, riskMark(r), r.Entry.Label)
		if r.Status == StatusFailed || r.Status == StatusBlocked {
			fmt.Fprintf(&b, "           %s\n", r.Reason)
			continue
		}
		if r.Entry.Unverified != "" && len(r.Items) == 0 {
			// 消す対象が 0 件なのに Recover (「消しても再生成されます」) を出すと、
			// 検出できているように読める
			fmt.Fprintf(&b, "           0 件ですが「候補なし」ではありません: %s\n", r.Entry.Unverified)
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
	// 見出しだけで終わらせない (UI と svcdoctor は「見つかりませんでした」を出す。issue 177)
	if shown == 0 {
		b.WriteString("\n掃除の候補はありませんでした\n")
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
