package main

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestQualifiedRefsRejectsFalseAbsenceAndOffersScopedRetry(t *testing.T) {
	root := repoWith(t, map[string]string{
		"one space[1]/A.swift": "struct Worker { func run() {} }\nfunc use(_ w: Worker) { w.run() }\n",
		"two/A.swift":          "func other(_ w: Worker) { w.run() }\n",
		"outside/A.swift":      "func outside(_ w: Worker) { w.run() }\n",
	})
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			rc, out, errs := refs(t, "Worker.run", "-format", format, filepath.Join(root, "one space[1]"), filepath.Join(root, "two"))
			if rc != 2 || out != "" || !strings.Contains(errs, "非対応の問い合わせ") || !strings.Contains(errs, "同名の別型") {
				t.Fatalf("unsupported must not become no_match: rc=%d stdout=%q stderr=%q", rc, out, errs)
			}
			_, actionJSON, ok := strings.Cut(errs, "next_action: ")
			if !ok {
				t.Fatalf("missing retry: %q", errs)
			}
			var action nextAction
			if err := json.Unmarshal([]byte(actionJSON), &action); err != nil {
				t.Fatal(err)
			}
			if len(action.Argv) == 0 || action.Argv[0] != "dupscan" {
				t.Fatalf("unexpected argv: %#v", action)
			}
			rc, out, errs = runReport(t, action.Argv[1:]...)
			if rc != 0 || errs != "" || !strings.Contains(out, "one space[1]/A.swift") || !strings.Contains(out, "two/A.swift") || strings.Contains(out, "outside/A.swift") {
				t.Errorf("retry must find references in exactly the original scope: rc=%d stdout=%q stderr=%q", rc, out, errs)
			}
		})
	}
}

func TestRefsKeepsBareUnicodeEscapedAndOperatorNames(t *testing.T) {
	root := repoWith(t, map[string]string{"A.swift": "func use() { 日本語(); `repeat`() }\n"})
	for _, query := range []string{"日本語", "`repeat`", ".+.", "missing"} {
		t.Run(query, func(t *testing.T) {
			rc, out, errs := refs(t, query, root)
			want := 0
			if query == ".+." || query == "missing" {
				want = 1
			}
			if rc != want || errs != "" || !strings.Contains(out, query) {
				t.Errorf("bare-name semantics unchanged: rc=%d want=%d stdout=%q stderr=%q", rc, want, out, errs)
			}
		})
	}
}

func TestExplorationAcceptsTrailingFlagsAndLiteralScopes(t *testing.T) {
	root := repoWith(t, map[string]string{
		"scope space[1]/A.swift": "func run() {}\nfunc use() { run(); run() }\n",
		"-literal/A.swift":       "func run() {}\nfunc use() { run() }\n",
	})
	for _, command := range []string{"index", "callers"} {
		t.Run(command, func(t *testing.T) {
			prefix := []string{command}
			if command == "callers" {
				prefix = append(prefix, "run")
			}
			scope := filepath.Join(root, "scope space[1]")
			before := []string{command, "-limit", "1"}
			if command == "callers" {
				before = append(before, "run")
			}
			before = append(before, scope)
			rc, out, errs := runReport(t, before...)
			rc2, out2, errs2 := runReport(t, append(append(prefix, scope), "-limit", "0", "-limit=1")...)
			if rc != 0 || rc2 != rc || out2 != out || errs2 != errs || !strings.Contains(out, "… 他") {
				t.Errorf("trailing repeated flags must match last-value semantics: before=(%d,%q,%q) after=(%d,%q,%q)", rc, out, errs, rc2, out2, errs2)
			}
			t.Chdir(root)
			rc, out, errs = runReport(t, append(prefix, "--", "-literal")...)
			if rc != 0 || errs != "" || !strings.Contains(out, "-literal/A.swift") || strings.Contains(out, "scope space") {
				t.Errorf("-- must preserve literal scope: rc=%d stdout=%q stderr=%q", rc, out, errs)
			}
		})
	}
}

func TestLegacyExplorationWarnsOnParseErrorsIncludingZeroUnits(t *testing.T) {
	for _, command := range []string{"index", "def", "refs", "callers"} {
		for _, scenario := range []string{"hit", "no_match", "zero_units"} {
			for _, format := range []string{"text", "json"} {
				t.Run(command+"/"+scenario+"/"+format, func(t *testing.T) {
					files := map[string]string{}
					bad := []string{"A space.swift", "B\"quote.swift", "C.swift", "D.swift"}
					for _, path := range bad {
						// 問い合わせの名前 (target / missing) を含めて、結果に影響しうるファイルにする。含まないときの短い通知は parse_warning_relevance_test.go
						files[path] = "func broken( { // target missing\n"
					}
					if scenario != "zero_units" {
						files["Good.swift"] = "func target() {}\nfunc use() { target() }\n"
						files["tool.go"] = "package main\nfunc goOnly() {}\n"
					}
					root := repoWith(t, files)
					query := "target"
					if scenario != "hit" {
						query = "missing"
					}
					args := []string{command, "-format", format}
					if command == "index" {
						args = append(args, "-name", query)
					} else {
						args = append(args, query)
					}
					args = append(args, root)
					rc, out, errs := runReport(t, args...)
					want := 0
					if scenario != "hit" && (command == "def" || command == "refs") {
						want = 1
					}
					if rc != want || !strings.Contains(errs, "解析不完全") || !strings.Contains(errs, "ファイル 4 個") || !strings.Contains(errs, strconv.Quote(bad[0])) || !strings.Contains(errs, strconv.Quote(bad[1])) || !strings.Contains(errs, "ほか 1 個") || strings.Contains(errs, strconv.Quote(bad[3])) {
						t.Fatalf("file diagnostics must survive absent units/results: rc=%d want=%d stdout=%q stderr=%q", rc, want, out, errs)
					}
					if !strings.Contains(errs, "0件は不存在の証明ではない") {
						t.Errorf("missing absence caveat: %q", errs)
					}
					if format == "json" {
						var data struct {
							Matches int  `json:"matches"`
							Units   *int `json:"units"`
						}
						if err := json.Unmarshal([]byte(out), &data); err != nil {
							t.Fatal(err)
						}
						if scenario == "zero_units" && command == "index" && (data.Units == nil || *data.Units != 0) {
							t.Fatalf("fixture must yield zero extracted units: %q", out)
						}
						if (scenario == "hit") != (data.Matches > 0) {
							t.Errorf("partial result count changed: %q", out)
						}
					}
				})
			}
		}
	}
}

func TestExplorationRejectsMissingFlagValues(t *testing.T) {
	root := repoWith(t, map[string]string{"A.swift": "func use() {}\n"})
	for _, command := range []string{"index", "callers", "def", "refs", "context", "evidence"} {
		for _, flag := range []string{"-format", "-limit", "-name"} {
			if flag == "-name" && command != "index" || flag == "-format" && (command == "context" || command == "evidence") {
				continue
			}
			t.Run(command+"/"+flag, func(t *testing.T) {
				args := []string{command}
				if command != "index" {
					args = append(args, "use")
				}
				args = append(args, root, flag)
				rc, out, errs := runReport(t, args...)
				if rc != 2 || out != "" || !strings.Contains(errs, "flag needs an argument") {
					t.Errorf("delimiter must not become a missing flag's value: rc=%d stdout=%q stderr=%q", rc, out, errs)
				}
			})
		}
	}
}

// 位置引数を取るサブコマンドは flag を名前・パスの後ろに書いてよく、前に書いたときと同じ結果になる
// (後ろの `-v` をパスとして読むと「-v は git の repo の中ではない」になる)。
func TestTrailingFlagsMatchLeadingFlags(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/B.swift", helperB)
	cases := []struct {
		name        string
		flags, rest []string
		wantRC      int
	}{
		{"report", []string{"-v", "-format", "json"}, []string{root}, 0},
		{"gate", []string{"-v", "-base", "HEAD"}, []string{root}, 1},
		{"sweep", []string{"-min-nodes", "5"}, []string{root}, 0},
		{"search", []string{"-query", "location"}, []string{root}, 0},
		{"near", []string{"-format", "json"}, []string{filepath.Join(root, "Sources/A.swift") + ":location"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := []string{c.name}
			if c.name == "report" {
				cmd = nil
			}
			rc, out, errs := runReport(t, append(append(append([]string{}, cmd...), c.flags...), c.rest...)...)
			rc2, out2, errs2 := runReport(t, append(append(append([]string{}, cmd...), c.rest...), c.flags...)...)
			if rc != c.wantRC || out == "" {
				t.Fatalf("前置の flag で rc=%d (期待 %d): stdout=%q stderr=%q", rc, c.wantRC, out, errs)
			}
			if _, bare, _ := runReport(t, append(append([]string{}, cmd...), c.rest...)...); bare == out {
				t.Fatalf("flag を付けない run と出力が同じで、flag が効いたかを見られない: %q", out)
			}
			if rc2 != rc || out2 != out || errs2 != errs {
				t.Errorf("後置の flag が前置と違う: before=(%d,%q,%q) after=(%d,%q,%q)", rc, out, errs, rc2, out2, errs2)
			}
		})
	}
}
