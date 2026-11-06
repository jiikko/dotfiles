package homepath

import "testing"

func TestTilde(t *testing.T) {
	for _, tc := range []struct{ home, p, want string }{
		{"/Users/k", "/Users/k/.local/x", "~/.local/x"},
		{"/Users/k", "/Users/kx/y", "/Users/kx/y"}, // 別のユーザーの home に当てない
		{"/Users/k", "/Users/k", "/Users/k"},
		{"", "/Users/k/x", "/Users/k/x"},
	} {
		if got := Tilde(tc.home, tc.p); got != tc.want {
			t.Errorf("Tilde(%q, %q) = %q; want %q", tc.home, tc.p, got, tc.want)
		}
	}
}
