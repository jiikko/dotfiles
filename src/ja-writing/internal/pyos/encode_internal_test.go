package pyos

import (
	"testing"

	"ja-writing/internal/pyfs"
)

// OS に渡すバイトは os.fsencode と同じ: '\udcff' は 0xff、'\udced\udca0\udc80' (OS から受けた ED A0 80 を os.fsdecode した字) は ED A0 80。
// APFS は 0xff も WTF-8 の 3 バイトも同じ誤り (ENOENT・EILSEQ) で返すので、OS の答えからは見分けられない。渡すバイトそのものを見る。
// 戻せないサロゲート (U+D800) は本家の UnicodeEncodeError を写さず、字の WTF-8 のまま渡す。
func TestEncodeGivesFsencodeBytes(t *testing.T) {
	s := func(rs ...rune) string {
		var b []byte
		for _, r := range rs {
			b = pyfs.AppendSurrogate(b, r)
		}
		return string(b)
	}
	for in, want := range map[string]string{
		"z" + s(0xDCFF):                 "z\xff",
		"q" + s(0xDCED, 0xDCA0, 0xDC80): "q\xed\xa0\x80",
		"日本":                            "日本",
		s(0xD800):                       s(0xD800),
	} {
		if got := encode(in); got != want {
			t.Errorf("% x: % x、期待 % x", in, got, want)
		}
	}
}
