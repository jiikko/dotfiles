package lint

import (
	"errors"
	"slices"
	"testing"
)

// argparse の写しが、本家の記録 (scenario/ の case) にある振る舞いを持つこと (記録との突き合わせは TestCLIMatchesPythonRecording。ここは構文の分類を直接見る)。
func TestParseArgsFollowsArgparse(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name string
		args []string
		want options
		err  string
	}{
		{name: "file", args: []string{"a.md"}, want: options{file: str("a.md")}},
		{name: "file after option", args: []string{"a.md", "--json"}, want: options{file: str("a.md"), json: true}},
		{name: "fail-on equals", args: []string{"--fail-on=warn", "a.md"}, want: options{file: str("a.md"), conditions: []condition{{value: "warn"}}}},
		{name: "fail-on empty equals", args: []string{"--fail-on=", "a.md"}, want: options{file: str("a.md"), conditions: []condition{{value: ""}}}},
		{name: "strict and fail-on keep order", args: []string{"--fail-on", "error", "--strict", "--fail-on=warn"}, want: options{conditions: []condition{{value: "error"}, {strict: true}, {value: "warn"}}}},
		{name: "dash is stdin", args: []string{"-"}, want: options{file: str("-")}},
		{name: "dash value", args: []string{"--fail-on", "-"}, want: options{conditions: []condition{{value: "-"}}}},
		{name: "double dash", args: []string{"--", "--json"}, want: options{file: str("--json")}},
		{name: "negative number is a file", args: []string{"-1"}, want: options{file: str("-1")}},
		{name: "argument with space is a file", args: []string{"-a b"}, want: options{file: str("-a b")}},
		{name: "help wins over later errors", args: []string{"-h", "--bogus"}, want: options{help: true}},
		{name: "joined help", args: []string{"-hx"}, want: options{help: true}},
		{name: "abbreviation", args: []string{"--js"}, err: "unrecognized arguments: --js"},
		{name: "two files", args: []string{"a", "b", "--x"}, err: "unrecognized arguments: b --x"},
		{name: "option after double dash is extra", args: []string{"a", "--", "--json"}, err: "unrecognized arguments: --json"},
		{name: "missing value", args: []string{"--words"}, err: "argument --words: expected one argument"},
		{name: "option-like value", args: []string{"--doc-type", "-x"}, err: "argument --doc-type: expected one argument"},
		{name: "flag with value", args: []string{"--json=x"}, err: "argument --json: ignored explicit argument 'x'"},
		{name: "help with value", args: []string{"--help=x"}, err: "argument -h/--help: ignored explicit argument 'x'"},
	}
	for _, c := range cases {
		got, err := parseArgs(c.args)
		var perr parseError
		if c.err != "" {
			if !errors.As(err, &perr) || string(perr) != c.err {
				t.Errorf("%s: 誤り %v、期待 %q", c.name, err, c.err)
			}
			continue
		}
		if err != nil || got.help != c.want.help || got.json != c.want.json || !slices.Equal(got.conditions, c.want.conditions) ||
			(got.file == nil) != (c.want.file == nil) || (got.file != nil && *got.file != *c.want.file) {
			t.Errorf("%s: %+v (%v)、期待 %+v", c.name, got, err, c.want)
		}
	}
}

func TestFailConditions(t *testing.T) {
	names, problem := failConditions([]condition{{strict: true}, {value: "WARN, error"}, {value: "Error"}})
	if problem != "" || !slices.Equal(names, []string{"strict", "warn", "error", "error"}) {
		t.Errorf("%v %q", names, problem)
	}
	for value, want := range map[string]string{
		"":               "--fail-on に使えない値です: （空）。lint で使えるのは warn と error です。",
		" , ":            "--fail-on に使えない値です: （空）。lint で使えるのは warn と error です。",
		"x,meaning_risk": meaningRiskMessage,
		"ÀB":             "--fail-on に使えない値です: àb。lint で使えるのは warn と error です。",
	} {
		if _, problem := failConditions([]condition{{value: value}}); problem != want {
			t.Errorf("%q: %q、期待 %q", value, problem, want)
		}
	}
}

func TestHitConditionsAndExitLine(t *testing.T) {
	names := []string{"warn", "strict", "error", "warn"}
	hit := hitConditions(names, [3]int{0, 2, 5})
	want := []string{"--fail-on=warn の条件（warn か error の候補が1件以上）", "--strict の条件（warn か error の候補が1件以上）"}
	if !slices.Equal(hit, want) {
		t.Errorf("warn だけのとき: %v", hit)
	}
	if hit := hitConditions(names, [3]int{1, 0, 0}); len(hit) != 3 {
		t.Errorf("error があれば 3 つ (warn・strict・error): %v", hit)
	}
	if hit := hitConditions(names, [3]int{0, 0, 9}); len(hit) != 0 {
		t.Errorf("info だけでは当たらない: %v", hit)
	}
	if got := exitLine(want, [3]int{1, 2, 3}); got != "\n終了コード1: --fail-on=warn の条件（warn か error の候補が1件以上）、--strict の条件（warn か error の候補が1件以上）に当たりました（error 1件、warn 2件）。\n" {
		t.Errorf("%q", got)
	}
	if exitLine(nil, [3]int{}) != "" {
		t.Error("当たらないときは何も付けない")
	}
}
