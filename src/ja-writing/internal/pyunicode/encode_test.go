package pyunicode_test

import (
	"testing"

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

func TestEncodeSurrogateEscapeMatchesPython(t *testing.T) {
	for _, c := range measuredFSEncode {
		v, err := pyjson.Loads(c.json)
		if err != nil {
			t.Fatalf("%s: %v", c.json, err)
		}
		got, encErr := pyunicode.EncodeSurrogateEscape(v.(string))
		msg := ""
		if encErr != nil {
			msg = encErr.Error()
		}
		if got != c.bytes || msg != c.err {
			t.Errorf("%s: % x %q、本家は % x %q", c.json, got, msg, c.bytes, c.err)
		}
	}
}

// UTF-8 として読めない生のバイト (OS から受けた値) は、本家が os.fsdecode で読んだ U+DC80..U+DCFF の字と同じに扱い、そのバイトのまま出す。
func TestEncodeSurrogateEscapeKeepsRawBytes(t *testing.T) {
	for _, c := range []struct{ in, want, err string }{
		{"a\xffb", "a\xffb", ""},
		{"\x80\xc3", "\x80\xc3", ""},
		// 本家の "\udcff\ud800" と同じ (生の 0xff はサロゲートの並びに入る)
		{"\xff" + pyunicode.FromRunes([]rune{0xD800}), "", "'utf-8' codec can't encode character '\\ud800' in position 1: surrogates not allowed"},
		{pyunicode.FromRunes([]rune{0xD800}) + "\xff", "", "'utf-8' codec can't encode characters in position 0-1: surrogates not allowed"},
	} {
		got, err := pyunicode.EncodeSurrogateEscape(c.in)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if got != c.want || msg != c.err {
			t.Errorf("% x: % x %q、期待 % x %q", c.in, got, msg, c.want, c.err)
		}
	}
}
