package sgr

import "testing"

// 行内のリセット (短縮形 ESC[m と ESC[0m の両方) の直後に bg を張り直し、リセットでない SGR には触れない。
func TestReapplyAfterReset(t *testing.T) {
	const bg = "\x1b[48;5;16m"
	cases := map[string]string{
		"plain":                         "plain",
		"a\x1b[0mb":                     "a\x1b[0m" + bg + "b",
		"a\x1b[mb":                      "a\x1b[m" + bg + "b",
		"\x1b[1ma\x1b[0m\x1b[2mb\x1b[m": "\x1b[1ma\x1b[0m" + bg + "\x1b[2mb\x1b[m" + bg,
	}
	for in, want := range cases {
		if got := ReapplyAfterReset(in, bg); got != want {
			t.Errorf("ReapplyAfterReset(%q) = %q, want %q", in, got, want)
		}
	}
}
