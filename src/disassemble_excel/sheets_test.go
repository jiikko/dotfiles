package main

import "testing"

func TestColToNum(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"A", 1}, {"Z", 26}, {"AA", 27}, {"AZ", 52}, {"BA", 53}, {"II", 243},
	}
	for _, c := range cases {
		if got := colToNum(c.in); got != c.want {
			t.Errorf("colToNum(%q) got=%d want=%d", c.in, got, c.want)
		}
	}
}

func TestNumToCol(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{1, "A"}, {26, "Z"}, {27, "AA"}, {52, "AZ"}, {53, "BA"}, {243, "II"},
	}
	for _, c := range cases {
		if got := numToCol(c.in); got != c.want {
			t.Errorf("numToCol(%d) got=%q want=%q", c.in, got, c.want)
		}
	}
}

func TestColRoundTrip(t *testing.T) {
	for n := 1; n <= 1000; n++ {
		if got := colToNum(numToCol(n)); got != n {
			t.Errorf("roundtrip n=%d got=%d", n, got)
		}
	}
}

func TestSplitAddr(t *testing.T) {
	cases := []struct {
		in       string
		row, col int
	}{
		{"A1", 1, 1}, {"E3", 3, 5}, {"AA10", 10, 27}, {"II68", 68, 243},
	}
	for _, c := range cases {
		row, col := splitAddr(c.in)
		if row != c.row || col != c.col {
			t.Errorf("splitAddr(%q) got=(row=%d,col=%d) want=(row=%d,col=%d)", c.in, row, col, c.row, c.col)
		}
	}
}
