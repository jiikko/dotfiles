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

// Foldable は「候補 0 件なので一覧から畳んでよい行か」。CLI と TUI の唯一の出典。
//
// 🚨 **検出条件そのものが未実測のエントリ (Entry.Unverified) は畳まない** (issue 169 / 207)。
// 畳むと「名前が違って 1 件も当たらなかった」が「候補なし = きれい」と同じ見え方になり、
// 探せていないことが画面から永久に消える (false green)。
func Foldable(r Result) bool {
	return r.Status == StatusOK && len(r.Items) == 0 && len(r.Failures) == 0 && r.Entry.Unverified == ""
}

// MarkVocabulary は Mark が返しうる語をすべて返す (幅の見積もりに使う。glogx の
// doctorMaxMarkWidth が唯一の利用者)。
//
// 🚨 **手で並べない**。以前は呼び出し側が 5 語をハードコードしていて「🔎 未検証」が抜けており、
// 語が増えても幅の見積もりだけが古くなる形だった (issue 238)。ここは Mark を実際に通して作る。
// 🚨 それでも「**新しい Risk / Status が増えたとき**」は自動では入らないので、下の
// markVocabularyGuard が exhaustive (default なし switch) で気づかせる。
func MarkVocabulary() []string {
	item := []Item{{Path: "/p"}}
	rs := []Result{
		{Entry: Entry{ID: "x"}, Status: StatusBlocked},
		{Entry: Entry{ID: "x"}, Status: StatusFailed},
		{Entry: Entry{ID: "x", Unverified: "未実測"}, Status: StatusOK},
	}
	for _, risk := range []Risk{RiskSafe, RiskCaution, RiskConfirm} {
		rs = append(rs, Result{Entry: Entry{ID: "x", Risk: risk}, Status: StatusOK, Items: item})
	}
	markVocabularyGuard(RiskSafe, StatusOK) // 番人を配線に載せる (実行時の意味は無い)
	out := make([]string, 0, len(rs))
	seen := map[string]bool{}
	for _, r := range rs {
		if m := Mark(r); !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// markVocabularyGuard は「MarkVocabulary の列挙が enum に追いついているか」の番人。
// 🚨 **default を書かない**。Risk / Status に値を足すと exhaustive (.golangci.yml) がここで
// 赤くなり、MarkVocabulary の列挙を直す必要に気づける (敵対レビュー 2026-09-04 の P2)。
// 実行時の意味は無い (呼ばれない側の網羅性だけを固定する)。
func markVocabularyGuard(r Risk, s Status) {
	switch r {
	case RiskSafe:
	case RiskCaution:
	case RiskConfirm:
	}
	switch s {
	case StatusOK:
	case StatusBlocked:
	case StatusFailed:
	}
}

// Mark は状態を表す**固定語彙** (記号 + 語)。CLI (Format) と TUI (glogx の doctorRiskMark) の
// 唯一の出典。**色は持たない**: この module は表示幅・色の依存を持たない方針なので、
// 色付けは語を受け取った側 (glogx) の責務。
//
// 🚨 語を増やす / 変えるときはここだけを直す。以前は CLI と TUI に同じ写像が 2 つあり、
// 「相手と同じ語彙を使うこと」というコメントだけが両者を結んでいた (issue 222)。
func Mark(r Result) string {
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
func Format(rep Report, env Env, now time.Time) string {
	// 🚨 **stdout へ直接書く経路なので、ここが最後の関門**。走査した値 (パス / ReadDir の名前 /
	// OS のエラー文) は自分以外が書いた文字列で、そのまま出すと OSC52 やタイトル書き換えが
	// 「表示しただけ」で発火する。TUI (glogx) の受け口も同じ関数を通す (issue 228)。
	rep = SanitizeForDisplay(rep)
	var b strings.Builder
	fmt.Fprintf(&b, "ディスク診断   合計 %s 解放可能 (blocked / 走査できず は含まない)\n", HumanSize(rep.Total))
	if rep.Partial {
		b.WriteString("🚨  途中で中断されました (部分結果)\n")
	}
	shown := 0
	for _, r := range rep.Results {
		// 候補 0 件は畳む。🚨 **検出条件そのものが未実測のエントリは畳まない** (issue 169 / 207)。
		// 畳むと「名前が違って 1 件も当たらなかった」が「候補なし = きれい」と同じ見え方になる。
		// UI 側 (src/glogx/doctor_view.go) と同じ規律。実測で Entry.Unverified が空になれば畳まれる側へ戻る
		if Foldable(r) {
			continue
		}
		shown++
		size := HumanSize(r.Size)
		if r.Status == StatusFailed {
			size = "---"
		}
		// 🚨 ラベルを `%-48s` で pad しない。Go の幅指定は**バイト数**なので、日本語ラベルでは
		// 1 文字 3 バイトと数えられて列が行ごとにずれる (issue 182)。doctor module は幅計算の
		// 依存を持たないので、**揃えるのを諦めて**マークを先に出す (size は ASCII なので %9s が効く)。
		// 揃えたくなったら表示幅を測る依存を足すこと。UI 側 (glogx) は termwidth を持つので揃えている
		fmt.Fprintf(&b, "\n%9s  %s  %s\n", size, Mark(r), r.Entry.Label)
		// 🚨 注記は**ヘッダの直後**に出す (助言や最終更新の後ろではない)。下の分岐は
		// blocked / failed / 未実測 0 件で `continue` するので、後ろに置くと
		// **実機では一度も出ない**: この機の $TMPDIR エントリは chrome-tmp (Chrome 起動中 =
		// blocked) と finder-nsird (0 件 = 畳まれる) の 2 つしか無く、2026-09-04 の実走査で
		// 出現数 0 を実測した。しかも「起動中で今は消せない」ときこそ
		// 「放置すれば消える」が一番効く。畳まれた 0 件のエントリには出さない (言うことが無い)
		// 🚨 failed では出さない。走査に失敗した = **その置き場がどこか分かっていない**ので、
		// 性質を断言できない (Paths 空を false にしているのと同じ論理)
		if note := RebootNote(r.Entry, env); note != "" && r.Status != StatusFailed {
			fmt.Fprintf(&b, "           %s\n", note)
		}
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
