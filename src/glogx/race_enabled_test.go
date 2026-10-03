//go:build race

package main

// raceEnabled は -race でビルドされたテストか。確保の回数は -race の計装で揺れるので、回数の予算は
// -race なしの run で判定する (frame_alloc_test.go の TestFrameAllocBudget の doc)。
const raceEnabled = true
