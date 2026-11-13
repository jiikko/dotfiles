package lint

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"syscall"
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
	// 種類で外れない規則 (pause_comma のような種類を限る規則は、種類を渡さないと候補 0 件のテストになる)
	info := newFinding("instruction_residue", 1, "info", "m", "")
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
		// 注入した候補が後処理を経ても全部残っていること (外れると、条件の判定が候補なしのテストになる)
		count := fmt.Sprintf("\"candidate_count\": %d,", len(c.findings))
		if jsonRC != c.rc || strings.Contains(jsonOut, "終了コード1") || !strings.HasSuffix(jsonOut, "}\n") || !strings.Contains(jsonOut, count) {
			t.Errorf("%s (--json): rc=%d (期待 %d)\n%s", c.name, jsonRC, c.rc, jsonOut)
		}
	}
}

// failingWriter は書き込みのたびに err を返す (読む側が閉じたパイプ・空きの無いディスク)。
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// 書けなかったときの rc と標準エラーは、本家の経路で分かれる (Python 3.15 で実測):
//   - 本文の出力 (本家は自分で flush する) で EPIPE → BrokenPipeError を受けて rc 3 と 1 行。ほかの誤り (ENOSPC) は内部エラーの rc 3
//   - -h・--version・使い方の誤り (本家は flush せずに終わる) で EPIPE → インタプリタの終わり際の失敗で rc 120 (stdout なら知らせの 2 行)
func TestWriteFailuresFollowPythonExitPaths(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		stdout error
		stderr error
		rc     int
		errOut string
	}{
		{"本文の出力が EPIPE", []string{"--no-words"}, syscall.EPIPE, nil, 3, "yomiyasu_lint.py: 出力先が閉じられたため、結果を最後まで書けませんでした。\n"},
		{"本文の出力が ENOSPC", []string{"--no-words"}, syscall.ENOSPC, nil, 3, "yomiyasu_lint.py: 内部エラー: no space left on device\n"},
		{"--version が EPIPE", []string{"--version"}, syscall.EPIPE, nil, 120, "Exception ignored while flushing sys.stdout:\nBrokenPipeError: [Errno 32] Broken pipe\n"},
		{"-h が EPIPE", []string{"-h"}, syscall.EPIPE, nil, 120, "Exception ignored while flushing sys.stdout:\nBrokenPipeError: [Errno 32] Broken pipe\n"},
		{"使い方の誤りの文が EPIPE", []string{"--bogus"}, nil, syscall.EPIPE, 120, ""},
		{"--words と --no-words の文が EPIPE", []string{"--words", "w", "--no-words"}, nil, syscall.EPIPE, 120, ""},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		env := cmdenv.Env{Args: c.args, Stdin: strings.NewReader("本文です。\n"), Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" }}
		if c.stdout != nil {
			env.Stdout = failingWriter{c.stdout}
		}
		if c.stderr != nil {
			env.Stderr = failingWriter{c.stderr}
		}
		if rc := Main(context.Background(), env); rc != c.rc || errOut.String() != c.errOut {
			t.Errorf("%s: rc=%d (期待 %d)・stderr %q (期待 %q)", c.name, rc, c.rc, errOut.String(), c.errOut)
		}
	}
}
