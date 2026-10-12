package listnav

import "testing"

func TestCycle(t *testing.T) {
	cases := []struct{ i, d, n, want int }{
		{0, 1, 3, 1}, {2, 1, 3, 0}, {0, -1, 3, 2}, {1, -1, 3, 0},
		{0, 0, 1, 0}, {0, -1, 1, 0},
		{1, 7, 3, 2}, {0, -7, 3, 2}, // |delta| が n より大きくても輪に収まる
	}
	for _, c := range cases {
		if got := Cycle(c.i, c.d, c.n); got != c.want {
			t.Errorf("Cycle(%d, %d, %d) = %d, want %d", c.i, c.d, c.n, got, c.want)
		}
	}
}
