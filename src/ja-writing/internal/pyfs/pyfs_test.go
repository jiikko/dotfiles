package pyfs_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"testing"

	"ja-writing/internal/pyfs"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
)

// measuredFSEncode は python3.15 (3.15.0) で JSON の文字列を json.loads で読み、os.fsencode に通した結果 (バイト列か UnicodeEncodeError の文)。
// 記録の JSON から読んだ値を子の argv に渡す経路と同じ読み方 (pyjson) で読んでから比べる (JSON の上位と下位のサロゲートの組は 1 字に合わさる)。
var measuredFSEncode = []struct{ json, bytes, err string }{
	{`""`, "", ""},
	{`"a"`, "\x61", ""},
	{`"\u3042\ud83d\ude00"`, "\xe3\x81\x82\xf0\x9f\x98\x80", ""},
	{`"\udc80"`, "\x80", ""},
	{`"\udcff"`, "\xff", ""},
	{`"a\udcffb"`, "\x61\xff\x62", ""},
	{`"\udcff\udcff"`, "\xff\xff", ""},
	{`"\udc7f"`, "", "'utf-8' codec can't encode character '\\udc7f' in position 0: surrogates not allowed"},
	{`"\udd00"`, "", "'utf-8' codec can't encode character '\\udd00' in position 0: surrogates not allowed"},
	{`"\ud800"`, "", "'utf-8' codec can't encode character '\\ud800' in position 0: surrogates not allowed"},
	{`"\udbff"`, "", "'utf-8' codec can't encode character '\\udbff' in position 0: surrogates not allowed"},
	{`"\udc00"`, "", "'utf-8' codec can't encode character '\\udc00' in position 0: surrogates not allowed"},
	{`"\udfff"`, "", "'utf-8' codec can't encode character '\\udfff' in position 0: surrogates not allowed"},
	{`"a\ud800b"`, "", "'utf-8' codec can't encode character '\\ud800' in position 1: surrogates not allowed"},
	{`"\udcff\ud800"`, "", "'utf-8' codec can't encode character '\\ud800' in position 1: surrogates not allowed"},
	{`"ab\ud800\ud801c"`, "", "'utf-8' codec can't encode characters in position 2-3: surrogates not allowed"},
	{`"\ud800x\ud801"`, "", "'utf-8' codec can't encode character '\\ud800' in position 0: surrogates not allowed"},
	{`"\udc80\ud800\udcff"`, "\x80\xf0\x90\x83\xbf", ""},
	{`"\udc80\udbff\udbff\udcff"`, "", "'utf-8' codec can't encode character '\\udbff' in position 1: surrogates not allowed"},
	{`"a\udc80\udc00\udbffb"`, "", "'utf-8' codec can't encode characters in position 2-3: surrogates not allowed"},
	{`"\ud83d\ude00\ud800"`, "", "'utf-8' codec can't encode character '\\ud800' in position 1: surrogates not allowed"},
	{`"\u3042\udcff\u3044\udbff"`, "", "'utf-8' codec can't encode character '\\udbff' in position 3: surrogates not allowed"},
	{`"\udcff\udc80\udc81"`, "\xff\x80\x81", ""},
	{`"\u0000\ud800"`, "", "'utf-8' codec can't encode character '\\ud800' in position 1: surrogates not allowed"},
	{`"\ud800\u0000"`, "", "'utf-8' codec can't encode character '\\ud800' in position 0: surrogates not allowed"},
	{`"\udcff\u0000"`, "\xff\x00", ""},
	{`"\ufffd\udcff"`, "\xef\xbf\xbd\xff", ""},
	{`"\udced\udca0\udc80"`, "\xed\xa0\x80", ""},
	{`"\ud800\udcff"`, "\xf0\x90\x83\xbf", ""},
	{`"\ud83d\ude00"`, "\xf0\x9f\x98\x80", ""},
	{`"\udcff\ud800\udc00"`, "\xff\xf0\x90\x80\x80", ""},
	{`"\ud800\ud800\udc00"`, "", "'utf-8' codec can't encode character '\\ud800' in position 0: surrogates not allowed"},
	{`"\udbff\udfff\udfff"`, "", "'utf-8' codec can't encode character '\\udfff' in position 1: surrogates not allowed"},
	{`"\udbff\udbff\udbff"`, "", "'utf-8' codec can't encode characters in position 0-2: surrogates not allowed"},
	{`"x\udc7f\udc80\udd00"`, "", "'utf-8' codec can't encode characters in position 1-3: surrogates not allowed"},
}

func TestEncodeMatchesPython(t *testing.T) {
	for _, c := range measuredFSEncode {
		v, err := pyjson.Loads(c.json)
		if err != nil {
			t.Fatalf("%s: %v", c.json, err)
		}
		got, encErr := pyfs.Encode(v.(string))
		msg := ""
		if encErr != nil {
			msg = encErr.Error()
		}
		if got != c.bytes || msg != c.err {
			t.Errorf("%s: % x %q、本家は % x %q", c.json, got, msg, c.bytes, c.err)
		}
	}
}

// measuredStdio は python3.15 (3.15.0) で JSON の文字列を json.loads で読み、s.encode("utf-8") (strict。本家の標準出力) の UnicodeEncodeError の文と、
// s.encode("utf-8", "backslashreplace") (本家の標準エラー) のバイト列。
var measuredStdio = []struct{ json, strictErr, backslash string }{
	{`""`, "", ""},
	{`"a"`, "", "a"},
	{`"あ😀"`, "", "\xe3\x81\x82\xf0\x9f\x98\x80"},
	{`"\udcff"`, "'utf-8' codec can't encode character '\\udcff' in position 0: surrogates not allowed", "\x5cudcff"},
	{`"a\udcffb"`, "'utf-8' codec can't encode character '\\udcff' in position 1: surrogates not allowed", "a\x5cudcffb"},
	{`"x\udced\udca0\udc80y"`, "'utf-8' codec can't encode characters in position 1-3: surrogates not allowed", "x\x5cudced\x5cudca0\x5cudc80y"},
	{`"\ud800"`, "'utf-8' codec can't encode character '\\ud800' in position 0: surrogates not allowed", "\x5cud800"},
	{`"ab\ud800\ud801c"`, "'utf-8' codec can't encode characters in position 2-3: surrogates not allowed", "ab\x5cud800\x5cud801c"},
	{`"\ud800x\ud801"`, "'utf-8' codec can't encode character '\\ud800' in position 0: surrogates not allowed", "\x5cud800x\x5cud801"},
	{`"\udc80𐃿"`, "'utf-8' codec can't encode character '\\udc80' in position 0: surrogates not allowed", "\x5cudc80\xf0\x90\x83\xbf"},
	{`"😀\ud800"`, "'utf-8' codec can't encode character '\\ud800' in position 1: surrogates not allowed", "\xf0\x9f\x98\x80\x5cud800"},
	{`"あ\udcffい\udbff"`, "'utf-8' codec can't encode character '\\udcff' in position 1: surrogates not allowed", "\xe3\x81\x82\x5cudcff\xe3\x81\x84\x5cudbff"},
	{`"\u0000\udcff"`, "'utf-8' codec can't encode character '\\udcff' in position 1: surrogates not allowed", "\x00\x5cudcff"},
	{`"\\\udcff"`, "'utf-8' codec can't encode character '\\udcff' in position 1: surrogates not allowed", "\x5c\x5cudcff"},
	{`"�\udfff\n"`, "'utf-8' codec can't encode character '\\udfff' in position 1: surrogates not allowed", "\xef\xbf\xbd\x5cudfff\x0a"},
}

func TestStdioEncodingMatchesPython(t *testing.T) {
	for _, c := range measuredStdio {
		v, err := pyjson.Loads(c.json)
		if err != nil {
			t.Fatalf("%s: %v", c.json, err)
		}
		s := v.(string)
		got, strictErr := pyfs.EncodeStrict(s)
		msg := ""
		if strictErr != nil {
			msg = strictErr.Error()
			got = s
		}
		if msg != c.strictErr || got != s {
			t.Errorf("%s: strict は %q、本家は %q", c.json, msg, c.strictErr)
		}
		if b := pyfs.BackslashReplace(s); b != c.backslash {
			t.Errorf("%s: backslashreplace は % x、本家は % x", c.json, b, c.backslash)
		}
	}
	// Decode せずに持った生のバイトも、surrogateescape で読んだ字 (U+DC80..U+DCFF) と同じに扱う
	if b := pyfs.BackslashReplace("z\xff"); b != `z\udcff` {
		t.Errorf("生の 0xff: %q", b)
	}
	if _, err := pyfs.EncodeStrict("z\xff"); err == nil || err.Error() != "'utf-8' codec can't encode character '\\udcff' in position 1: surrogates not allowed" {
		t.Errorf("生の 0xff の strict: %v", err)
	}
}

// UTF-8 として読めない生のバイト (OS から受けた値) は、本家が os.fsdecode で読んだ U+DC80..U+DCFF の字と同じに扱い、そのバイトのまま出す。
func TestEncodeKeepsRawBytes(t *testing.T) {
	for _, c := range []struct{ in, want, err string }{
		{"a\xffb", "a\xffb", ""},
		{"\x80\xc3", "\x80\xc3", ""},
		// 本家の "\udcff\ud800" と同じ (生の 0xff はサロゲートの並びに入る)
		{"\xff" + pyunicode.FromRunes([]rune{0xD800}), "", "'utf-8' codec can't encode character '\\ud800' in position 1: surrogates not allowed"},
		{pyunicode.FromRunes([]rune{0xD800}) + "\xff", "", "'utf-8' codec can't encode characters in position 0-1: surrogates not allowed"},
	} {
		got, err := pyfs.Encode(c.in)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if got != c.want || msg != c.err {
			t.Errorf("% x: % x %q、期待 % x %q", c.in, got, msg, c.want, c.err)
		}
	}
}

// measuredFSDecode は python3.15 (3.15.0) の os.fsdecode に生のバイトを通した字 (コードポイントの並び)。不正なバイト・WTF-8 のサロゲートの形
// (ED A0..BF xx)・途中で切れた UTF-8・overlong・F5 以上を含む。
var measuredFSDecode = []struct {
	in   string
	want []rune
}{
	{"\xff", []rune{0xDCFF}},
	{"\x80", []rune{0xDC80}},
	{"a\xffb", []rune{'a', 0xDCFF, 'b'}},
	{"\xed\xa0\x80", []rune{0xDCED, 0xDCA0, 0xDC80}},
	{"\xed\xb3\xbf", []rune{0xDCED, 0xDCB3, 0xDCBF}},
	{"\xed\xb2\x80", []rune{0xDCED, 0xDCB2, 0xDC80}},
	{"\xed\xbf\xbf", []rune{0xDCED, 0xDCBF, 0xDCBF}},
	{"\xed\xa0\x80\xed\xb0\x80", []rune{0xDCED, 0xDCA0, 0xDC80, 0xDCED, 0xDCB0, 0xDC80}},
	{"\xe3\x81", []rune{0xDCE3, 0xDC81}},
	{"\xe3\x81A", []rune{0xDCE3, 0xDC81, 'A'}},
	{"\xf0\x9f\x98", []rune{0xDCF0, 0xDC9F, 0xDC98}},
	{"\xc0\xaf", []rune{0xDCC0, 0xDCAF}},
	{"\xe0\x80\xaf", []rune{0xDCE0, 0xDC80, 0xDCAF}},
	{"\xf0\x80\x80\xaf", []rune{0xDCF0, 0xDC80, 0xDC80, 0xDCAF}},
	{"\xf5\x80\x80\x80", []rune{0xDCF5, 0xDC80, 0xDC80, 0xDC80}},
	{"\xf8\x88\x80\x80\x80", []rune{0xDCF8, 0xDC88, 0xDC80, 0xDC80, 0xDC80}},
	{"\xf4\x90\x80\x80", []rune{0xDCF4, 0xDC90, 0xDC80, 0xDC80}},
	{"", nil},
	{"\xe3\x81\x82\xf0\x9f\x98\x80", []rune{0x3042, 0x1F600}},
}

func TestDecodeMatchesPython(t *testing.T) {
	for _, c := range measuredFSDecode {
		got := pyfs.Decode(c.in)
		if r := pyunicode.Runes(got); !slices.Equal(r, c.want) {
			t.Errorf("% x: %U、本家は %U", c.in, r, c.want)
		}
		if back, err := pyfs.Encode(got); err != nil || back != c.in {
			t.Errorf("% x を読んで戻すと % x (%v)", c.in, back, err)
		}
	}
}

// fsdecodeInputs は TestDecodeMatchesPythonDigest が通す生のバイト列 (1・2 バイトは全部、3・4 バイトは先頭のバイトを選んだもの)。
func fsdecodeInputs(yield func([]byte) bool) {
	for a := range 256 {
		if !yield([]byte{byte(a)}) {
			return
		}
	}
	for a := range 256 {
		for b := range 256 {
			if !yield([]byte{byte(a), byte(b)}) {
				return
			}
		}
	}
	for _, a := range []byte{0xC2, 0xDF, 0xE0, 0xE1, 0xEC, 0xED, 0xEE, 0xEF, 0xF0, 0xF4, 0xF5, 0xFF} {
		for b := range 256 {
			for _, c := range []byte{0x00, 0x41, 0x7F, 0x80, 0x8F, 0x90, 0x9F, 0xA0, 0xBF, 0xC0, 0xE3, 0xFF} {
				if !yield([]byte{a, byte(b), c}) {
					return
				}
			}
		}
	}
	for _, a := range []byte{0xF0, 0xF1, 0xF3, 0xF4, 0xF5, 0xF7} {
		for b := range 256 {
			for _, c := range []byte{0x41, 0x80, 0xBF, 0xC0} {
				for _, d := range []byte{0x41, 0x80, 0xBF, 0xC0} {
					if !yield([]byte{a, byte(b), c, d}) {
						return
					}
				}
			}
		}
	}
}

// fsdecode の結果 (WTF-8) を、長さ (4 バイトの big endian) と中身の並びにして SHA-256 で比べる。本家の値は python3.15 (3.15.0) で
// fsdecodeInputs と同じ入力を os.fsdecode に通し、s.encode("utf-8", "surrogatepass") を同じ形で足した digest (127,232 件。全部が
// os.fsencode で同じバイトに戻った)。
func TestDecodeMatchesPythonDigest(t *testing.T) {
	const pythonDigest = "1a2fc040a444566f9930ccf6d4063aa7bf894e547b720beb0bf8939112c58abd"
	h := sha256.New()
	n := 0
	for in := range fsdecodeInputs {
		out := pyfs.Decode(string(in))
		h.Write(binary.BigEndian.AppendUint32(nil, uint32(len(out))))
		h.Write([]byte(out))
		n++
		if back, err := pyfs.Encode(out); err != nil || back != string(in) {
			t.Fatalf("% x を読んで戻すと % x (%v)", in, back, err)
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); n != 127232 || got != pythonDigest {
		t.Fatalf("%d 件の digest %s、本家は 127232 件の %s", n, got, pythonDigest)
	}
}
