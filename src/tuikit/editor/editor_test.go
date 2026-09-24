package editor

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestCommandPicksEditor(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"どちらも空なら nvim", nil, []string{"nvim", "/a b/x.md"}},
		{"空白だけも空と同じ", map[string]string{"VISUAL": "  ", "EDITOR": " "}, []string{"nvim", "/a b/x.md"}},
		{"EDITOR", map[string]string{"EDITOR": "vim"}, []string{"vim", "/a b/x.md"}},
		{"VISUAL が先", map[string]string{"VISUAL": "hx", "EDITOR": "vim"}, []string{"hx", "/a b/x.md"}},
		{"引数つき", map[string]string{"EDITOR": "code -w"}, []string{"code", "-w", "/a b/x.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := Command("/a b/x.md", func(k string) string { return tc.env[k] })
			got := append([]string{filepath.Base(cmd.Path)}, cmd.Args[1:]...)
			if !slices.Equal(got, tc.want) || cmd.Args[0] != tc.want[0] {
				t.Fatalf("got %q (Args %q) want %q", got, cmd.Args, tc.want)
			}
		})
	}
}
