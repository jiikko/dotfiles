package sgr

import "testing"

func TestXtermRGB(t *testing.T) {
	cases := map[int][3]int{
		-1: {0, 0, 0}, 0: {0, 0, 0}, 1: {205, 0, 0}, 4: {0, 0, 238}, 8: {127, 127, 127}, 12: {92, 92, 255}, 15: {255, 255, 255},
		16: {0, 0, 0}, 17: {0, 0, 95}, 22: {0, 95, 0}, 52: {95, 0, 0}, 196: {255, 0, 0}, 231: {255, 255, 255},
		232: {8, 8, 8}, 255: {238, 238, 238}, 256: {0, 0, 0},
	}
	for n, want := range cases {
		if r, g, b := XtermRGB(n); [3]int{r, g, b} != want {
			t.Errorf("XtermRGB(%d) = %v, want %v", n, [3]int{r, g, b}, want)
		}
	}
}
