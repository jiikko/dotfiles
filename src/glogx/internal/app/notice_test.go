package app_test

import (
	"testing"

	"glogx/internal/app"
)

// ClampIdx は添字を [0, total) へ寄せる。空 (total が 0 以下) は 0 (-1 を返すと、呼び出し側の添字が範囲の外を指す)。
func TestClampIdx(t *testing.T) {
	for _, tc := range []struct{ i, total, want int }{
		{5, 0, 0}, {0, 0, 0}, {-1, 3, 0}, {9, 3, 2}, {1, 3, 1},
	} {
		if got := app.ClampIdx(tc.i, tc.total); got != tc.want {
			t.Errorf("ClampIdx(%d, %d) = %d, want %d", tc.i, tc.total, got, tc.want)
		}
	}
}
