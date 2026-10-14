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

// execViaForeground: 端末を外のコマンドへ渡す実行は foreground.NewExec の値を tea.Exec に渡す (issue 707)。
// tea.ExecProcess は戻った後に前面を確かめないので、子 (treefiler の `!` の `$SHELL -ic` 等) が前面を持ち去ったまま終わると、
// 入れ直しで SIGTTOU に止まるか、EIO で何も読めず固まる。treefiler の pty テスト (filer/shell_pty_test.go) が見ているのは
// foreground.Exec の振る舞いで、アプリがそれを通っていることはこの規則が止める。
// 検出しない形: bubbletea を tea 以外の名前で import する・ExecProcess を関数値として変数へ入れてから呼ぶ (敵対レビューで実測。
// import は全部 tea なので、うっかりでは起きない)・tea.Exec に別の ExecCommand (自作の型) を渡す・bubbletea を経ずに
// exec.Cmd.Run で端末を渡す (review で見る)
func execViaForeground(m dsl.Matcher) {
	m.Import("charm.land/bubbletea/v2")
	m.Match(`tea.ExecProcess($*_)`).
		Report(`端末を渡す実行は x := foreground.NewExec(cmd) を tea.Exec(x, fn) に渡し、fn で x.Err を読む (戻った後に端末の前面を取り戻す。issue 707・757)`)
}
