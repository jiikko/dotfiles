//go:build ruleguard

// Package gorules は gocritic の ruleguard checker が読むカスタム lint ルール (.golangci.yml の
// settings.gocritic.settings.ruleguard.rules から参照)。
//
// 🚨 **独立したディレクトリに置くこと** (issue 202)。glogx 直下に置くと `package gorules` が
// 同ディレクトリの `package main` と衝突し、**どのビルド構成でもコンパイルできない**
// (実測 2026-09-03: `go vet -tags ruleguard ./` が
// `found packages main and gorules` で落ちた)。その状態では型エラーや API 誤用を書いても
// Go のコンパイルに一度も載らず、気づけるのは gocritic が「ルールのロードに失敗した」と
// 言うときだけだった (.golangci.yml のコメントによれば過去にルールが無言で消えた前科がある)。
// ここに置いてあれば `go vet -tags ruleguard ./gorules` で型検査できる
// (`make lint` から回している。Makefile の vet-gorules を参照)。
//
// 🚨 go.mod の `go-ruleguard/dsl` のバージョンは**この DSL の型定義にしか効かない**。
// 実際にルールを評価するのは golangci-lint 同梱の go-ruleguard なので、
// dsl を上げても lint の挙動は変わらない。
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// padViaPadSpaces: 空白の連結は termwidth.PadSpaces を使う (issue 047 / 118)。
//
// strings.Repeat(" ", n) は毎回確保するが、PadSpaces は事前確保した定数文字列のスライス
// (バッキング共有 = 無確保) を返し、上限を超えた分だけ Repeat に落ちる。描画は毎フレーム
// 全行で走るので、ここが積もると frame alloc 予算に効く。
//
// 🚨 このルールが安くなったのは issue 106 で PadSpaces が leaf パッケージ (termwidth) へ
// 移り、**全パッケージから参照できるようになったから**。それ以前は「参照できない別パッケージ」
// のために例外を 4 つ足す必要があり、費用対効果が合わないとして 118 で一度見送っていた。
// 現在の例外はテストだけ (.golangci.yml の exclusions)。実装本体は github.com/jiikko/dotfiles/src/tuikit/termwidth にあり、このルールの外。
// 🚨 Go 1.26 の strings.Repeat(" ", n) は n <= 128 なら確保しない (実測 2026-09-24)。差が出るのは埋め草が
// 129〜256 桁のとき (広い端末の最外周の枠) で、このルールが守っているのはそこ。tuikit 側は同じ退行を
// 確保回数のテスト (layout/alloc_test.go) で止めている。
func padViaPadSpaces(m dsl.Matcher) {
	m.Match(`strings.Repeat(" ", $n)`).
		Report(`空白の連結は termwidth.PadSpaces($n) を使う (無確保。main では padSpaces)`)
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
