package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func box(typ string, payload int) []byte {
	b := make([]byte, 8+payload)
	binary.BigEndian.PutUint32(b, uint32(8+payload))
	copy(b[4:], typ)
	return b
}

func extBox(typ string, size uint64, payload int) []byte {
	b := make([]byte, 16+payload)
	binary.BigEndian.PutUint32(b, 1)
	copy(b[4:], typ)
	binary.BigEndian.PutUint64(b[8:], size)
	return b
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// 期待値は Python 版 (zshlib/_concat_helpers.zsh の python3 -c) と同じ規則で数えた値。旧版との突き合わせは issue 670 の進捗
func TestEffectiveSize(t *testing.T) {
	ftyp := box("ftyp", 8) // 16
	size0 := []byte{0, 0, 0, 0, 'm', 'd', 'a', 't'}
	cases := map[string]struct {
		data []byte
		want uint64
	}{
		"正常":           {cat(ftyp, box("moov", 100), box("mdat", 1000)), 16 + 108 + 1008},
		"末尾のごみ (型が 0)": {cat(ftyp, box("mdat", 50), make([]byte, 40)), 16 + 58},
		"64 bit の大きさ":  {cat(ftyp, extBox("mdat", 16+40, 40)), 16 + 56},
		"64 bit の大きさが途中で切れる": {cat(ftyp, []byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0}), 16},
		"大きさ 0 は終わりまで":       {cat(ftyp, size0, make([]byte, 77)), 16 + 8 + 77},
		"型が印字できない":           {cat(ftyp, box("\x00\x01ab", 10)), 16},
		"型に 0x7f":            {cat(ftyp, box("ab\x7fc", 10)), 16},
		"型に空白は可":             {box("f  e", 4), 12},
		"大きさが 8 未満":          {cat(ftyp, []byte{0, 0, 0, 4, 'f', 'r', 'e', 'e'}), 16},
		"ファイルの外へ出る":          {cat(ftyp, []byte{0, 0, 0x27, 0x0f, 'm', 'd', 'a', 't'}), 16},
		"ヘッダが 8 バイトに満たない":    {cat(ftyp, []byte{0, 0, 0}), 16},
		"64 bit の大きさが最大値":    {cat(ftyp, extBox("mdat", 1<<64-1, 0)), 16},
		"空":                  {nil, 0},
	}
	dir := t.TempDir()
	for name, tc := range cases {
		p := filepath.Join(dir, "f")
		if err := os.WriteFile(p, tc.data, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := effectiveSize(p); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
	// 開けない・ディレクトリは 0 (Python 版は例外で 0)
	if got := effectiveSize(filepath.Join(dir, "none")); got != 0 {
		t.Errorf("無いファイルで %d", got)
	}
	if got := effectiveSize(dir); got != 0 {
		t.Errorf("ディレクトリで %d", got)
	}
}
