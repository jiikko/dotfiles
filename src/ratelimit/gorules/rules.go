//go:build ruleguard

// gocritic の ruleguard checker が読むカスタム lint ルール (.golangci.yml から参照)。
// 規則と理由の正本は src/glogx/gorules/rules.go。usage の描画 (glogx のダッシュボードで毎フレーム走る) に
// 同じ規則を掛けるため、module を分けたときに写した (別 module の規則ファイルは ruleguard が dsl を
// 解決できず読めない)。
// 🚨 独立ディレクトリに置く理由・go.mod の dsl の版が lint の挙動に効かない理由も glogx 側と同じ。
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// padViaPadSpaces: 空白の連結は termwidth.PadSpaces を使う (無確保。glogx 側 issue 047 / 118)。
func padViaPadSpaces(m dsl.Matcher) {
	m.Match(`strings.Repeat(" ", $n)`).
		Report(`空白の連結は termwidth.PadSpaces($n) を使う (無確保)`)
}
