//go:build race

package filer

// raceEnabled は -race でビルドされたテストか。確保の回数は -race の計装で揺れるので、回数の予算は
// -race なしの run で判定する (alloc_budget_test.go。Makefile の test がその run を足す)。
const raceEnabled = true
