// Package hintfit は最下行のキーの案内を幅に収める。入らない項目は切らずに落とし、優先度 1 (抜ける手段) は最後まで残す。
//
// glogx (status / doctor / issues / lan など 10 か所) と mac-lan-manager の最下行が使う。2 つの module が別々に持っていたころ、
// lanmgr の側だけ「? が真っ先に消える」「長い名前で動作のキーが押し出される」を踏み直した。
// treefiler の最下行は凡例 → 右の欄 → 案内の段で落とす別の方式 (パンくずと幅を分け合う) なので、ここを使わない。
package hintfit

import (
	"slices"
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// Item は案内の 1 項目。Prio は小さいほど残す (1 = 抜ける手段。同じ優先度は左を先に採る)。Text は SGR を含んでよい (幅は termwidth で測る)。
// Prio は 1 以上にし、優先度 1 の Text は空にしない (空だと ShortestExit の「無い」と区別できず、席の予約が外れる)。
type Item struct {
	Text string
	Prio int
}

// Fit は幅 width に収まるところまで項目を採り、元の並び順で sep を挟んで繋ぐ。
//
// 🚨 「入らないから末尾を切る」ではなく「入らない項目を落とす」。切ると語の途中で切れて意味が壊れ ("d: diff" が "d…" になった)、
// しかも切れるのは常に並びの末尾 = 幅に関係なく最後の項目が消える。入らない項目は飛ばし、より短い後続は入れる。
//
// 🚨 優先度 1 は席を予約する: 優先度 1 が 1 つでもあるのに 1 つも入らない幅では、低優先で埋めずに最短の優先度 1 を単独で返す
// (優先度は採る順序を決めるだけなので、出口 (17 桁) が入らない幅で、より短い低優先 (9 桁) だけが残った。抜ける手段の無い案内は
// 短い案内より悪い)。返した文字列が幅を超えることがあるので、呼び出し側は最後に幅で切る。
func Fit(width int, sep string, items []Item) string {
	keep := make([]bool, len(items))
	total, sepW := 0, termwidth.Of(sep)
	prios := make([]int, 0, len(items))
	for _, it := range items {
		prios = append(prios, it.Prio)
	}
	slices.Sort(prios)
	for _, prio := range slices.Compact(prios) {
		for i, it := range items {
			if keep[i] || it.Prio != prio {
				continue
			}
			w := termwidth.Of(it.Text)
			if total > 0 {
				w += sepW
			}
			if total+w > width {
				continue // この項目は入らない。より短い後続は入るかもしれないので続ける
			}
			keep[i], total = true, total+w
		}
	}
	if best := ShortestExit(items); best != "" {
		kept := false
		for i, it := range items {
			if keep[i] && it.Prio == 1 {
				kept = true
				break
			}
		}
		if !kept {
			return best
		}
	}
	out := make([]string, 0, len(items))
	for i, it := range items {
		if keep[i] {
			out = append(out, it.Text)
		}
	}
	if len(out) == 0 && len(items) > 0 {
		return items[len(items)-1].Text // 1 つも入らない極端な幅でも無言にしない (優先度 1 が無いときだけここへ来る)
	}
	return strings.Join(out, sep)
}

// ShortestExit は優先度 1 の項目のうち最も短いもの ("" = 優先度 1 が無い。狭い幅で案内が丸ごと消える面を検査で見つけるのに使う)。
func ShortestExit(items []Item) string {
	best, bestW := "", 0
	for _, it := range items {
		if it.Prio != 1 {
			continue
		}
		if w := termwidth.Of(it.Text); best == "" || w < bestW {
			best, bestW = it.Text, w
		}
	}
	return best
}
