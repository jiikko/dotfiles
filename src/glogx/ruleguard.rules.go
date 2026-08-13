//go:build ruleguard

// gocritic の ruleguard checker が読むカスタム lint ルール (.golangci.yml の
// settings.gocritic.settings.ruleguard.rules から参照)。ビルド対象ではない
// (build tag ruleguard で通常ビルド・テストから除外される)。
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
