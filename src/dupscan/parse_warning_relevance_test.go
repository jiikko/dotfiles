package main

import (
	"reflect"
	"strings"
	"testing"
)

// 読めなかったファイルが問い合わせの名前を含まないときは、解析不完全の全文を出さず 1 行で示す。
// 名前の照合は単位名の小文字化した部分一致なので、名前を含まないファイルは結果に影響しえない。
// 含みうるときの全文は query_diagnostics_test.go が固定している (hit・0 件・抽出単位 0 のすべて)。

func TestUnreadableFilesThatCannotMatchGetOneShortLine(t *testing.T) {
	for _, command := range []string{"index", "def", "refs", "callers"} {
		for _, scenario := range []string{"hit", "no_match"} {
			t.Run(command+"/"+scenario, func(t *testing.T) {
				root := repoWith(t, map[string]string{
					"Good.swift": "func target() {}\nfunc use() { target() }\n",
					"Bad.swift":  "func broken( {\n",
					"Bad2.swift": "func alsoBroken( {\n",
				})
				query := "target"
				if scenario == "no_match" {
					query = "missing"
				}
				args := []string{command}
				if command == "index" {
					args = append(args, "-name", query)
				} else {
					args = append(args, query)
				}
				_, _, errs := runReport(t, append(args, root)...)
				want := "dupscan " + command + ": 読めないファイル 2 個は問い合わせの名前を含まない\n"
				if errs != want {
					t.Fatalf("stderr = %q, want %q", errs, want)
				}
				if strings.Contains(errs, "解析不完全") || strings.Contains(errs, "Bad.swift") {
					t.Errorf("the full warning must not be printed when no unreadable file can match: %q", errs)
				}
			})
		}
	}
}

func TestUnreadableFileThatMayMatchKeepsFullWarning(t *testing.T) {
	// 大文字小文字を無視した部分一致 (index -name は小文字化して比べる) なので、TARGET を含むファイルは影響しうる。
	root := repoWith(t, map[string]string{
		"Good.swift":  "func target() {}\n",
		"Bad.swift":   "func broken( {\n",
		"Maybe.swift": "func BROKEN_TARGETS( {\n",
	})
	_, _, errs := runReport(t, "index", "-name", "target", root)
	if !strings.Contains(errs, "解析不完全") || !strings.Contains(errs, "ファイル 2 個") || !strings.Contains(errs, "0件は不存在の証明ではない") {
		t.Fatalf("a file that may match must keep the full warning for every unreadable file: %q", errs)
	}
}

func TestIndexWithoutNameKeepsFullWarning(t *testing.T) {
	// 名前で絞らない問い合わせ (戻り値の型だけ等) は、どのファイルの単位も対象になりうる。
	root := repoWith(t, map[string]string{
		"Good.swift": "func target() -> Int { 1 }\n",
		"Bad.swift":  "func broken( {\n",
	})
	_, _, errs := runReport(t, "index", "-returns", "Int", root)
	if !strings.Contains(errs, "解析不完全") || strings.Contains(errs, "問い合わせの名前を含まない") {
		t.Fatalf("a query without a name cannot rule out unreadable files: %q", errs)
	}
}

func TestInitQueryKeepsFullWarningBecauseCallCountsUseTheTypeName(t *testing.T) {
	// `Widget.init` の「呼び出し ~N」は型名 Widget で他のファイルの呼び出しを数える。読めないファイルが `init` を含まなくても影響しうる。
	root := repoWith(t, map[string]string{
		"s.swift": "struct Widget { init(a: Int) {} }\nfunc ok() { _ = Widget(a: 1) }\n",
		"e.swift": "func caller() { _ = Widget(a: 2) }\nfunc broken( {\n",
	})
	for _, command := range []string{"index", "def"} {
		args := []string{command, "init", root}
		if command == "index" {
			args = []string{command, "-name", "init", root}
		}
		_, _, errs := runReport(t, args...)
		if !strings.Contains(errs, "解析不完全") || strings.Contains(errs, "問い合わせの名前を含まない") {
			t.Errorf("%s: an init query must keep the full warning: %q", command, errs)
		}
	}
}

func TestRelevantParseErrors(t *testing.T) {
	s := scanned{
		errFiles: []string{"a.go", "b.go", "gone.go"},
		contents: map[string][]byte{
			"a.go": []byte("package a // ReadCookies"),
			"b.go": []byte("package b"),
			// gone.go は本文を持っていない (読めていない) → 影響しうるものとして残す
		},
	}
	for _, c := range []struct {
		name string
		want []string
	}{
		{"readcookies", []string{"a.go", "gone.go"}}, // 小文字化して比べる
		{"READCOOKIES", []string{"a.go", "gone.go"}},
		{"Cookies", []string{"a.go", "gone.go"}}, // 部分一致
		{"absent", []string{"gone.go"}},
		{"", []string{"a.go", "b.go", "gone.go"}},     // 名前が空なら全部
		{"init", []string{"a.go", "b.go", "gone.go"}}, // 初期化子の呼び出し数は型名で数えるので、名前を含まなくても影響しうる
		{"ini", []string{"a.go", "b.go", "gone.go"}},
		{"initialize", []string{"gone.go"}}, // init の部分文字列ではない名前は通常どおり
	} {
		if got := s.relevantAmong(s.errFiles, []string{c.name}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("relevantAmong(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
