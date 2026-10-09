//go:build ruleguard

// Package gorules は gocritic の ruleguard checker が読むカスタム lint ルール (.golangci.yml から参照)。
// 規則と理由の正本は src/glogx/gorules/rules.go。treefiler の描画 (glogx の全画面と単体の両方で毎フレーム走る) に
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

// pathPrefixViaWithSep: 「dir の中か」のための区切りの手組みは withSep (filer/git.go) を通す。root が / のとき
// "/" + "/" = "//" になり、前方一致が全部外れる (issue 691 の 3。findNode・forget・nodeFor が別々に踏んだ)。
func pathPrefixViaWithSep(m dsl.Matcher) {
	m.Match(`$x + string(filepath.Separator)`, `$x + string(os.PathSeparator)`).
		Where(!m.File().Name.Matches(`^git\.go$`)).
		Report(`区切りを足すのは withSep($x) を通す (root が / のとき // にしない)`)
	// "/" の手組みは表示や git の相対パスにも正当に使うので、前方一致の判定に渡す形だけを止める。
	// 検出しない形: 区切りを変数に入れてから足す・fmt.Sprintf で組む・+= で足す・(x+"/") と括弧で包む・string('/')・
	// Contains / Index に渡す (うっかりの範囲で頻度が低い。review で見る)
	m.Match(`strings.HasPrefix($y, $x + "/")`, `strings.CutPrefix($y, $x + "/")`, `strings.TrimPrefix($y, $x + "/")`).
		Report(`前方一致の区切りは withSep($x) を通す (root が / のとき // にしない)`)
}
