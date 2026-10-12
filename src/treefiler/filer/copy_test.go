package filer

import (
	"errors"
	"path/filepath"
	"testing"
)

// fakeCopy は copyToClipboard を差し替え、コピーしようとした文字列を記録する。
func fakeCopy(t *testing.T, err error) *[]string {
	t.Helper()
	var got []string
	orig := copyToClipboard
	copyToClipboard = func(s string) error { got = append(got, s); return err }
	t.Cleanup(func() { copyToClipboard = orig })
	return &got
}

func TestCopyPathKeys(t *testing.T) {
	cases := []struct {
		key, at, want string // want は root からの相対 (絶対パスの場合は root を前に付けて比べる)
		abs           bool
	}{
		{"y", "a/one.txt", "a/one.txt", true},
		{"Y", "a/one.txt", "a/one.txt", false},
		{"Y", "a", "a", false},
	}
	for _, c := range cases {
		t.Run(c.key+" "+c.at, func(t *testing.T) {
			got := fakeCopy(t, nil)
			m := newTest(t)
			cdTo(t, m, c.at)
			m.HandleKey(c.key)
			want := c.want
			if c.abs {
				want = filepath.Join(m.root.abs, c.want)
			}
			if len(*got) != 1 || (*got)[0] != want {
				t.Fatalf("コピーした文字列 = %q, want %q", *got, want)
			}
			ns := m.TakeNotices()
			if len(ns) != 1 || !ns[0].OK {
				t.Fatalf("成功の知らせ = %+v", ns)
			}
		})
	}
}

func TestCopyFailureBecomesToast(t *testing.T) {
	fakeCopy(t, errors.New("pbcopy: not found\nsecond line"))
	m := newTest(t)
	cdTo(t, m, "c.txt")
	m.HandleKey("y")
	ns := m.TakeNotices()
	if len(ns) != 1 || ns[0].OK || ns[0].Text != "コピーに失敗しました: pbcopy: not found" {
		t.Fatalf("失敗の知らせ = %+v", ns)
	}
}
