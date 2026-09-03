//go:build ruleguard

// gocritic の ruleguard checker が読むカスタム lint ルール (.golangci.yml の
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
// ⚠️ go.mod の `go-ruleguard/dsl` のバージョンは**この DSL の型定義にしか効かない**。
// 実際にルールを評価するのは golangci-lint 同梱の go-ruleguard なので、
// dsl を上げても lint の挙動は変わらない。
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// toastEncapsulation: toast の内部状態は toast.go の外から触らない。
// 窓口は show / showInfo / advance / animating / startLeaving / boxLines に限定する。
// toast.go 自身とテスト (_test.go は埋め込みフィールドを直接読む設計。toast.go の
// 「⚠️ 最新の 1 枚を埋め込みで持つ」コメント参照) は .golangci.yml の exclusions で除外。
func toastEncapsulation(m dsl.Matcher) {
	m.Match(
		`$_.toast.text`, `$_.toast.ok`, `$_.toast.info`,
		`$_.toast.seq`, `$_.toast.seqGen`, `$_.toast.phase`,
		`$_.toast.shown`, `$_.toast.frame`, `$_.toast.older`,
		`$_.toast.toastItem`,
	).Report(`toast の内部状態は toast.go の外から触らない (窓口は show/advance/boxLines 等の公開メソッド)`)
}

// padViaPadSpaces: 空白の連結は termwidth.PadSpaces を使う (issue 047 / 118)。
//
// strings.Repeat(" ", n) は毎回確保するが、PadSpaces は事前確保した定数文字列のスライス
// (バッキング共有 = 無確保) を返し、上限を超えた分だけ Repeat に落ちる。描画は毎フレーム
// 全行で走るので、ここが積もると frame alloc 予算に効く。
//
// ⚠️ このルールが安くなったのは issue 106 で PadSpaces が leaf パッケージ (termwidth) へ
// 移り、**全パッケージから参照できるようになったから**。それ以前は「参照できない別パッケージ」
// のために例外を 4 つ足す必要があり、費用対効果が合わないとして 118 で一度見送っていた。
// 現在の例外は実装本体 (termwidth.go) とテストだけ (.golangci.yml の exclusions)。
func padViaPadSpaces(m dsl.Matcher) {
	m.Match(`strings.Repeat(" ", $n)`).
		Report(`空白の連結は termwidth.PadSpaces($n) を使う (無確保。main では padSpaces)`)
}
