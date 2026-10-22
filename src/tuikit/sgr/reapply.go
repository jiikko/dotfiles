package sgr

import "strings"

// ReapplyAfterReset は text 中の SGR リセット (`ESC[m` / `ESC[0m`) の直後に bg を張り直した
// 文字列を返す。行全体を特定の背景で塗るとき、行内のリセットで背景が切れるのを防ぐ
// (glogx のカーソル行の強調と、layout.Panel の Fill が使う)。
//
// git log --color は短縮形 `ESC[m` を多用するため両形を拾う必要がある (literal 一致だと
// 色付き行の塗りが途中で切れる。実測 2026-07-19)。
//
// 🚨 正規表現ではなく手書きの走査にしている: この関数はカーソル行のために毎フレーム走り、
// regexp だと 1 フレームあたり約 0.9 KB (bitState + 置換結果) を確保していた (実測 2026-08-14)。
// 対象が固定 2 パターンしかないので走査で足りる。
func ReapplyAfterReset(text, bg string) string {
	if !strings.Contains(text, "\x1b[") {
		return text // リセット候補が無い = 確保せずそのまま
	}
	var b strings.Builder
	b.Grow(len(text) + len(bg)*2)
	last, i := 0, 0
	for i < len(text) {
		j := strings.Index(text[i:], "\x1b[")
		if j < 0 {
			break
		}
		i += j
		n := 0
		switch {
		case strings.HasPrefix(text[i:], "\x1b[m"):
			n = 3
		case strings.HasPrefix(text[i:], "\x1b[0m"):
			n = 4
		}
		if n == 0 {
			i += 2 // リセットでない SGR。次の候補へ
			continue
		}
		b.WriteString(text[last : i+n])
		b.WriteString(bg)
		i += n
		last = i
	}
	b.WriteString(text[last:])
	return b.String()
}
