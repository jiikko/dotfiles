package oracle

import (
	"encoding/json"
	"strings"
)

// 値を作る小さな関数 (ハッシュ・実行 ID・記録の digest・状態の行・環境の値・時刻・--fail-on・--timeout・--since) の case。

func rawJSON(s string) json.RawMessage { return json.RawMessage(s) }

func checkValueCases() []VisCase {
	return []VisCase{checkHashCase(), checkRunIDCase(), checkRecordDigestCase(), checkStatusLineCase(), checkHostValueCase()}
}

func checkHashCase() VisCase {
	var calls []Call
	for _, b := range [][]byte{nil, []byte("a"), []byte("hello\n"), {0, 1, 2, 255}, []byte("日本語"), []byte(strings.Repeat("あ", 5000))} {
		calls = append(calls, ckeep(ccall("_sha256", Bytes(b))))
	}
	// 末尾の改行を全部外して 1 つ付ける (空と改行だけは空のまま)。\r は外さない。サロゲートは surrogatepass で 3 バイトになる
	for _, s := range []any{
		"", "\n", "\n\n\n", "a", "a\n", "a\n\n\n", "a\r\n", "a\r", "\na", "\n\na\n", "a\nb", "a \n", "a\n \n", "日本語\n\n", "é\n", "é\n", "\ufeffa\n", "a\u2028", "a\x0b\n", "a\n\x0c",
		cps("a", rune(0xD800), "\n"), cps(rune(0xD800)), cps("\n", rune(0xDFFF), "\n\n"), cps("a", rune(0xD83D), rune(0xDE00)), "😀\n",
	} {
		calls = append(calls, ckeep(ccall("_text_sha256", s)))
	}
	return VisCase{Name: "check/hash", Calls: calls}
}

// checkRunIDs は実行 ID の誤りの検査に通す文字列。末尾に改行を持つ ID は、本家の re.match の $ が末尾の改行の手前に当たるので形の検査を通る。
var checkRunIDs = []string{
	"abc", "a", "A", "0", "a1", "run-1", "run_1", "run.1", "a.b.c", "A-b_c.d", "2026-10-13", strings.Repeat("a", 64), strings.Repeat("a", 65), strings.Repeat("a", 63) + ".", "",
	" ", "  a", "a ", "a b", "-a", "_a", ".a", "..", ".", "a.", "a..", "a/b", "a\\b", "a:b", "a*b", "日本", "a日", "ａｂｃ", "abc\n", "abc\n\n", "\nabc", "a\nb", "abc\r", "abc\r\n",
	"CON", "con", "Con", "con.txt", "con.tar.gz", "CON.", "conx", "xcon", "PRN", "prn.log", "AUX", "aux.a", "NUL", "nul.", "nul.x", "COM1", "com9", "COM0", "COM10", "com1.txt", "LPT1", "lpt9", "LPT9.x", "LPT0", "lpt", "com", "com1x", "co", "コン",
	"conn", "aux\n", "con\n", "com1\n", "ｃｏｎ", "ſ", "ı", "K", "\u212a", "run\u3000", "a\x00b", "tab\tx",
}

func checkRunIDCase() VisCase {
	var calls []Call
	for _, id := range checkRunIDs {
		calls = append(calls, ckeep(ccall("_run_id_problem", id)))
	}
	return VisCase{Name: "check/runid", Calls: calls}
}

// checkRunIDCaseCases は _run_id_case_problem の case。runs/ の名前は、大文字と小文字を区別しない FS で同時に作れない組を含めない。
func checkRunIDCaseCases() []VisCase {
	var cases []VisCase
	build := func(name string, names []string, queries []string, setup func(f *fixtureCase)) VisCase {
		f := newFixtureCase(name)
		if setup != nil {
			setup(f)
		}
		for _, n := range names {
			f.mkdir("state/runs/" + n)
		}
		for _, q := range queries {
			f.call(ckeep(ccall("_run_id_case_problem", f.dir("state"), q)))
		}
		return f.done()
	}
	cases = append(cases, build("runid-case-ascii", []string{"Foo", "bar", "BAZ"}, []string{"foo", "Foo", "FOO", "bar", "BAR", "Bar", "baz", "other", "", "fo", "Fooo"}, nil))
	cases = append(cases, build("runid-case-unicode", []string{"ÄB", "STRASSE", "Ω", "ǅx"}, []string{"äb", "ÄB", "straße", "STRASSE", "strasse", "ω", "ǆx", "ǄX", "ǅX", "ab"}, nil))
	cases = append(cases, build("runid-case-sorted", []string{"b", "A", "a1"}, []string{"B", "a", "A1", "a1"}, nil))
	cases = append(cases, build("runid-case-no-runs", nil, []string{"x", "X"}, nil))
	f := newFixtureCase("runid-case-runs-is-file")
	f.write("state/runs", "x")
	f.call(ckeep(ccall("_run_id_case_problem", f.dir("state"), "x")))
	f.call(ckeep(ccall("_run_id_case_problem", f.dir("state", "runs"), "x")))
	f.call(ckeep(ccall("_run_id_case_problem", f.dir("missing"), "x")))
	cases = append(cases, f.done())
	f = newFixtureCase("runid-case-files-too")
	f.write("state/runs/Report", "x")
	f.mkdir("state/runs/dir")
	f.call(ckeep(ccall("_run_id_case_problem", f.dir("state"), "report")))
	f.call(ckeep(ccall("_run_id_case_problem", f.dir("state"), "DIR")))
	cases = append(cases, f.done())
	return cases
}

func checkRecordDigestCase() VisCase {
	var calls []Call
	for _, s := range []string{
		`{}`,
		`{"record_digest":"sha256:zz"}`,
		`{"a":1,"b":[1,2,{"c":null}],"record_digest":"sha256:zz"}`,
		`{"z":1,"a":2,"m":{"y":1,"b":2}}`,
		`{"x":{"record_digest":1}}`,
		`{"日本":"語","é":"é","😀":"😀","ｚ":1,"z":2}`,
		`{"f":1.0,"g":1e22,"h":1.5e-7,"i":-0.0,"j":100000000000000000000.0,"k":0.1,"l":1e-5,"m":123456789.123456789,"n":5e-324,"o":1.7976931348623157e308}`,
		`{"n":12345678901234567890123,"neg":-12345678901234567890123,"zero":0,"one":1}`,
		`{"t":true,"f":false,"n":null,"e":"","a":[],"o":{}}`,
		`{"s":"a\"b\\c\n\t\u0001\u001f/<>&\u007f\u0080\u2028"}`,
		`{"s":"\ud800","t":"\udfff\ud800"}`,
		`{"nested":{"a":{"b":{"c":{"d":[1,[2,[3,[4]]]]}}}}}`,
		`{"format":"yomiyasu-check","format_version":1,"run_id":"r","attempt":1,"fix_count":0,"previous":null,"created_at":"2026-10-13T01:45:00Z","tools":{"yomiyasu_check.py":{"version":"3.1.0","sha256":"ab"}},"record_digest":"sha256:old"}`,
		`{"b":1,"a":1,"B":1,"A":1,"_":1,"0":1,"~":1,"":1," ":1}`,
		`{"a":1,"a":2}`,
	} {
		calls = append(calls, ckeep(ccall("_record_digest", rawJSON(s))))
	}
	return VisCase{Name: "check/record-digest", Calls: calls}
}

func checkStatusLineCase() VisCase {
	status := func(args []any, kwargs map[string]any) Call {
		c := ckeep(ccall("_status_line", args...))
		c.Kwargs = kwargs
		return c
	}
	all := map[string]any{"run_id": "r1", "attempt": 2, "final": "abc", "expected": "e", "mode": "rewrite", "doc_type": "blog", "reader": "expert",
		"checks": "lint,diff", "meaning_risk": 3, "replay": "ok", "failed": "lint", "reason": "x"}
	calls := []Call{
		status([]any{"executed", "hook"}, nil),
		status([]any{"no_record", "run"}, nil),
		status([]any{"executed", "verify", "r1", 1, "abc"}, nil),
		status([]any{"executed", "verify"}, all),
		status([]any{"x", "y"}, map[string]any{"run_id": "", "attempt": 0, "final": ""}),
		status([]any{"x", "y"}, map[string]any{"run_id": nil, "attempt": nil, "final": nil}),
		status([]any{"x", "y"}, map[string]any{"attempt": 12, "meaning_risk": 0}),
		status([]any{"x", "y"}, map[string]any{"meaning_risk": "none"}),
		status([]any{"x", "y"}, map[string]any{"meaning_risk": ""}),
		status([]any{"x", "y"}, map[string]any{"expected": "", "mode": "", "doc_type": "", "reader": "", "checks": "", "replay": "", "failed": "", "reason": ""}),
		status([]any{"x", "y"}, map[string]any{"reader": "r", "doc_type": "d", "mode": "m"}),
		status([]any{"x", "y"}, map[string]any{"reason": "日本語 with space"}),
		status([]any{"partial", "run", "r", 3, "f"}, map[string]any{"checks": "lint", "failed": "diff", "reason": "時間切れ"}),
		status([]any{"", ""}, nil),
	}
	for _, key := range []string{"expected", "mode", "doc_type", "reader", "checks", "meaning_risk", "replay", "failed", "reason"} {
		calls = append(calls, status([]any{"s", "v"}, map[string]any{key: "v-" + key}))
	}
	return VisCase{Name: "check/status-line", Calls: calls}
}

func checkHostValueCase() VisCase {
	var calls []Call
	for _, v := range []any{nil, "", "x", "${HOME}", "a${b}", " ", "$HOME", "{HOME}", "${", "$ {", "日本", "a b", "\t"} {
		calls = append(calls, ckeep(ccall("_host_value", v)))
	}
	return VisCase{Name: "check/host-value", Calls: calls}
}

// checkValidISO は _parse_iso が時刻にする入力 (続けて _iso と _stamp を通す)、checkInvalidISO は None にする入力。
var checkValidISO = []string{
	"2026-10-13T01:45:00Z", "2026-10-13t01:45:00z", "2026-1-3T1:5:0Z", "2026-10- 3T 1:45:00Z", "2024-02-29T00:00:00Z", "0001-01-01T00:00:00Z", "9999-12-31T23:59:59Z",
	"0999-01-01T00:00:00Z", "0005-06-07T08:09:10Z", "2026-12-31T23:59:59Z", "2026-10-13T1:45:00Z", "2026-10-13T 1:45:00Z", "2026-10-13T01:4:00Z", "2026-10-13T01:45:5Z",
	"1970-01-01T00:00:00Z", "2026-10-13T23:59:59Z", "２０２６-10-13T01:45:00Z", "٢٠٢٦-10-13T01:45:00Z", "𝟐𝟎𝟐𝟔-10-13T01:45:00Z", "2026-10-13T01:٥:00Z", "2026-10-13T01:45:٥Z", "2026-10-13T٥:45:00Z",
}

var checkInvalidISO = []string{
	"", "2026-10-13T01:45:00", "2026-10-13T01:45:00Zx", "2026-10-13T01:45:00Z\n", " 2026-10-13T01:45:00Z", "2026-02-30T00:00:00Z", "2025-02-29T00:00:00Z", "2026-13-01T00:00:00Z",
	"2026-00-10T00:00:00Z", "2026-10-00T00:00:00Z", "2026-10-32T00:00:00Z", "2026-10-13T24:00:00Z", "2026-10-13T01:60:00Z", "2026-10-13T01:45:60Z", "2026-10-13T01:45:61Z",
	"0000-01-01T00:00:00Z", "20261013T014500Z", "2026-10-13 01:45:00Z", "2026-10-13T01:45:00+09:00", "26-10-13T01:45:00Z", "2026-10-013T01:45:00Z", "²⁰²⁶-10-13T01:45:00Z",
	"2026-10-13T01:45:00Z ", "2026-10-13T01:45:62Z", "2026-10-13T01:45:00ZZ", "2026-1-13T01:45:00Z\n", "2026-10-13T001:45:00Z", "٢٠٢٦-١٠-١٣T٠١:٤٥:٠٠Z", "2026-١٠-13T01:45:00Z", "2026-٣-13T01:45:00Z",
	"2026-10-١٣T01:45:00Z", "2026-10-13T01:٤٥:00Z", "2026-10-13T01:45:٠٥Z", "2026-10-13T01:45:00z\n", "2026-10-13T01:45:00Z\x00", "2026-10-13T01:45:00\u3000Z",
}

var checkValidSince = []string{
	"2026-10-13T01:45:00Z", "2026-10-13T10:45:00+09:00", "2026-10-13 10:45:00+0900", "2026-10-13t10:45:00-05:30", "2026-10-13T01:45:00.123456Z", "2026-10-13T01:45:00,5Z",
	"  2026-10-13T01:45:00Z  ", "\t2026-10-13T01:45:00Z\n", "0001-01-01T00:00:00Z", "9999-12-31T23:59:59Z", "2026-10-13T01:45:00+23:59", "2026-10-13T01:45:00-23:59",
	"2026-10-13T01:45:00+00:59", "2026-10-13T01:45:00+0100", "0001-01-01T00:00:00-01:00", "9999-12-31T23:59:59+01:00", "2026-10-13T01:45:00+0000", "2026-10-13T01:45:00-00:00",
	"2026-10-13T01:45:00z", "2026-10-13T00:00:00+14:00", "0001-01-01T00:00:00+00:00", "0001-01-01T23:59:59-23:59", "9999-12-31T00:00:00+23:59",
}

var checkInvalidSince = []string{
	"", "  ", "x", "0001-01-01T00:00:00+01:00", "9999-12-31T23:59:59-01:00", "2026-10-13T01:45:00+24:00", "2026-10-13T01:45:00+09:60", "2026-10-13T01:45:00+9:00", "2026-10-13T01:45:00+09",
	"2026-10-13T01:45:60Z", "2026-02-30T01:45:00Z", "0000-01-01T00:00:00Z", "2026-10-13T01:45:00ZZ", "2026-10-13T01:45:00 Z", "2026-10-13T01:45:00", "２０２６-10-13T01:45:00Z",
	"2026-10-13T01:45:00+23:60", "2026-10-13T01:45:00.Z", "2026-10-13T01:45:00.5", "2026-10-13T01:45:00+99:99", "2026-10-13T01:45:00+9999", "2026-10-13T01:45:00-24:00", "2026-13-13T01:45:00Z",
	"2026-10-13T25:45:00Z", "2026-10-13T01:45:00Z\x00", "2026-10-13T01:45:00+09:0",
}

func checkTimeCases() []VisCase {
	var iso, since []Call
	for i, s := range checkValidISO {
		name := "iso" + string(rune('A'+i))
		iso = append(iso, ckeep(csave(ccall("_parse_iso", s), name)), ckeep(ccall("_iso", Ref(name))), ckeep(ccall("_stamp", Ref(name))))
	}
	for _, s := range checkInvalidISO {
		iso = append(iso, ckeep(ccall("_parse_iso", s)))
	}
	// 文字列でない値は None (isinstance の検査)
	for _, v := range []any{nil, 5, 1.5, true, []any{"2026-10-13T01:45:00Z"}} {
		iso = append(iso, ckeep(ccall("_parse_iso", v)))
	}
	for i, s := range checkValidSince {
		name := "since" + string(rune('A'+i))
		first := map[string]any{"$ref": name, "path": []any{0}}
		since = append(since, ckeep(csave(ccall("_since", s), name)), ckeep(ccall("_iso", first)), ckeep(ccall("_stamp", first)))
	}
	for _, s := range checkInvalidSince {
		since = append(since, ckeep(ccall("_since", s)))
	}
	since = append(since, ckeep(ccall("_since", nil)))
	return []VisCase{{Name: "check/parse-iso", Calls: iso}, {Name: "check/since", Calls: since}}
}

func checkOptionCases() []VisCase {
	var failOn, timeout []Call
	for _, v := range [][]string{
		{}, {"warn"}, {"WARN"}, {"Meaning_Risk"}, {"meaning-risk,warn"}, {"warn,warn", "error"}, {""}, {" "}, {","}, {"warn,,error"}, {"warn,x"}, {"x"}, {"meaning risk"}, {"ＷＡＲＮ"},
		{"error "}, {" error\u3000"}, {"meaning_ris\u212a"}, {"ERROR"}, {"wArn"}, {"İ"}, {"warn", ""}, {"a,b"}, {"error", "warn,meaning-risk", "error"}, {"mean_ing"}, {"warn,\t,error"},
		{"meaning-risk", "warn", "error"}, {"Warn,ERROR,meaning_risk"}, {"warn;error"}, {"warn error"}, {"ſ"}, {"ΑΣ"}, {"WARN\n"}, {"\nwarn"}, {"warn\x1c"}, {"a_b,c"}, {"-"}, {"_"},
	} {
		failOn = append(failOn, ckeep(ccall("_fail_on", v)))
	}
	timeout = append(timeout, ckeep(ccall("_timeout", nil)))
	for _, s := range []string{
		"90", "1", "0", "-1", "0.5", "1.5", "1e3", "1E22", "1e22", "1e300", "inf", "nan", "-inf", "infinity", " 5 ", "5\u3000", "٣", "１２", "1_0", "1__0", "", " ", "abc", "1e", "0x10",
		"1e-5", "5e-324", "1e999", "-1e999", "9007199254740993", "123456789012345678", "1.0", "2.50", "3.0000000000000004", "\x1c5", "+5", "--5", ".5", "5.", "1,5", "0.0", "-0.0", "1e0",
		"9.223372036854775807e18", "9.223372036854775808e18", "1.8446744073709552e19", "4503599627370496.5", "4503599627370497", "0.1", "1e-300", "00090", "9e15", "9e18", "5\n", "\t7\r", "1e+2", "3.14",
		"\u00a05", "5\u0085", "5 5", "five", "１.５", "𝟓", "1e１", "NaN", "INF", "+inf", "0e5", "1_000", "1_000.5_5", "1e1_0",
	} {
		timeout = append(timeout, ckeep(ccall("_timeout", s)))
	}
	return []VisCase{{Name: "check/fail-on", Calls: failOn}, {Name: "check/timeout", Calls: timeout}}
}
