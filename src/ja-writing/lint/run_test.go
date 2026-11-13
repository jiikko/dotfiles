package lint

import (
	"bytes"
	"strings"
	"testing"

	"ja-writing/cmdenv"
)

// injecting は検査の本体 (lintText) を、検出を注入した候補に差し替えた lintFunc にする。候補を出す群が入るまでの間も、run が rc と終了コードの行を配線していることを見るため。
func injecting(findings ...finding) lintFunc {
	return func(text string, c lintContext) (*result, error) {
		s, err := detect(text, c)
		if err != nil {
			return nil, err
		}
		s.findings = findings
		return s.finish(), nil
	}
}

func runInjected(t *testing.T, lint lintFunc, stdin string, args ...string) (stdout, stderr string, rc int) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := cmdenv.Env{Args: args, Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" }}
	rc, err := run(env, lint)
	if err != nil {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), rc
}

// rc と text の末尾の行 (「終了コード1: …」) は、条件に当たったときだけ出る。条件ごと・重さごとに、当たる・当たらないを両方見る。
func TestRunExitCodeAndExitLine(t *testing.T) {
	warn := newFinding("trailing_colon", 1, "warn", "m", "")
	errf := newFinding("emoji_prohibited", 1, "error", "m", "")
	info := newFinding("pause_comma", 1, "info", "m", "")
	cases := []struct {
		name     string
		findings []finding
		args     []string
		rc       int
		suffix   string
	}{
		{"条件なしは候補があっても 0", []finding{warn, errf}, nil, 0, ""},
		{"strict と warn", []finding{warn}, []string{"--strict"}, 1, "\n終了コード1: --strict の条件（warn か error の候補が1件以上）に当たりました（error 0件、warn 1件）。\n"},
		{"strict と error", []finding{errf}, []string{"--strict"}, 1, "\n終了コード1: --strict の条件（warn か error の候補が1件以上）に当たりました（error 1件、warn 0件）。\n"},
		{"strict と info だけ", []finding{info}, []string{"--strict"}, 0, ""},
		{"strict と候補なし", nil, []string{"--strict"}, 0, ""},
		{"fail-on=error と warn", []finding{warn}, []string{"--fail-on", "error"}, 0, ""},
		{"fail-on=error と error", []finding{errf, warn}, []string{"--fail-on=error"}, 1, "\n終了コード1: --fail-on=error の条件（error の候補が1件以上）に当たりました（error 1件、warn 1件）。\n"},
		{"fail-on=warn と warn", []finding{warn}, []string{"--fail-on=warn"}, 1, "\n終了コード1: --fail-on=warn の条件（warn か error の候補が1件以上）に当たりました（error 0件、warn 1件）。\n"},
		{"strict と fail-on の両方は文が並ぶ", []finding{errf}, []string{"--strict", "--fail-on=error"}, 1, "\n終了コード1: --strict の条件（warn か error の候補が1件以上）、--fail-on=error の条件（error の候補が1件以上）に当たりました（error 1件、warn 0件）。\n"},
	}
	for _, c := range cases {
		stdout, stderr, rc := runInjected(t, injecting(c.findings...), "本文です。\n", c.args...)
		if rc != c.rc || stderr != "" {
			t.Errorf("%s: rc=%d (期待 %d)・stderr %q", c.name, rc, c.rc, stderr)
			continue
		}
		if c.suffix == "" {
			if strings.Contains(stdout, "終了コード1") {
				t.Errorf("%s: 条件に当たらないのに終了コードの行が出た\n%s", c.name, stdout)
			}
		} else if !strings.HasSuffix(stdout, c.suffix) {
			t.Errorf("%s: 末尾が違う\n%s", c.name, stdout)
		}
		// --json は条件に当たっても行を足さない (rc だけが変わる)。
		jsonOut, _, jsonRC := runInjected(t, injecting(c.findings...), "本文です。\n", append([]string{"--json"}, c.args...)...)
		if jsonRC != c.rc || strings.Contains(jsonOut, "終了コード1") || !strings.HasSuffix(jsonOut, "}\n") {
			t.Errorf("%s (--json): rc=%d (期待 %d)\n%s", c.name, jsonRC, c.rc, jsonOut)
		}
	}
}
