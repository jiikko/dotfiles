package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseArgsCountForms(t *testing.T) {
	// -n 1 / -n1 / --max-count=1 が同じ意味に正規化される (issue のテスト方針)
	for _, argv := range [][]string{{"-n", "1"}, {"-n1"}, {"--max-count=1"}} {
		opts, err := ParseArgs(argv)
		if err != nil {
			t.Fatalf("ParseArgs(%v): %v", argv, err)
		}
		if opts.MaxCount != 1 || !opts.HasCount {
			t.Errorf("ParseArgs(%v) = MaxCount %d, HasCount %v; want 1, true", argv, opts.MaxCount, opts.HasCount)
		}
	}
}

func TestParseArgsDefaultCount(t *testing.T) {
	opts, err := ParseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.MaxCount != defaultMaxCount || opts.HasCount {
		t.Errorf("既定 MaxCount = %d, HasCount %v; want %d, false", opts.MaxCount, opts.HasCount, defaultMaxCount)
	}
}

func TestParseArgsNegativeCountUnlimited(t *testing.T) {
	opts, err := ParseArgs([]string{"-n", "-1"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.MaxCount != -1 {
		t.Errorf("MaxCount = %d; want -1", opts.MaxCount)
	}
}

func TestParseArgsFlags(t *testing.T) {
	opts, err := ParseArgs([]string{"--stat", "-p", "--refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Stat || !opts.Patch || !opts.Refresh {
		t.Errorf("flags = %+v; want Stat/Patch/Refresh true", opts)
	}
}

func TestParseArgsRevsAndPathspec(t *testing.T) {
	opts, err := ParseArgs([]string{"HEAD~10..HEAD", "--", "src/", "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Revs) != 1 || opts.Revs[0] != "HEAD~10..HEAD" {
		t.Errorf("Revs = %v", opts.Revs)
	}
	if len(opts.Paths) != 2 || opts.Paths[0] != "src/" {
		t.Errorf("Paths = %v", opts.Paths)
	}
}

func TestParseArgsPathspecCanContainDashes(t *testing.T) {
	// "--" 以降はフラグに見えても pathspec として扱う
	opts, err := ParseArgs([]string{"--", "--weird-file"})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Paths) != 1 || opts.Paths[0] != "--weird-file" {
		t.Errorf("Paths = %v", opts.Paths)
	}
}

func TestParseArgsUnsupported(t *testing.T) {
	// 未対応引数は黙って無視しない (issue の完了条件)
	for _, argv := range [][]string{{"--graph"}, {"--follow"}, {"--oneline"}} {
		_, err := ParseArgs(argv)
		var ua *UnsupportedArgError
		if !errors.As(err, &ua) {
			t.Errorf("ParseArgs(%v) = %v; want UnsupportedArgError", argv, err)
		}
	}
}

func TestParseArgsCachedExclusive(t *testing.T) {
	// --cached と git log モードの排他制御 (issue のテスト方針)
	for _, argv := range [][]string{
		{"--cached", "main"},
		{"--cached", "-n", "5"},
		{"--cached", "--", "src/"},
	} {
		if _, err := ParseArgs(argv); err == nil {
			t.Errorf("ParseArgs(%v): エラーになるべき組み合わせが通った", argv)
		}
	}
	// --stat / -p との併用は許可
	opts, err := ParseArgs([]string{"--cached", "--stat"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Mode != ModeCached || !opts.Stat {
		t.Errorf("opts = %+v", opts)
	}
}

func TestParseArgsBadCount(t *testing.T) {
	for _, argv := range [][]string{{"-n", "abc"}, {"-nxyz"}, {"--max-count=1.5"}} {
		if _, err := ParseArgs(argv); err == nil {
			t.Errorf("ParseArgs(%v): 不正な件数が通った", argv)
		}
	}
}

func TestUsageMentionsNonGoal(t *testing.T) {
	// 「git log の全引数互換を目標にしない」旨のヘルプ明記 (issue の完了条件)
	if got := Usage(); !strings.Contains(got, "全引数への互換は目標にしていません") {
		t.Errorf("Usage() に全引数非対応の明記がありません:\n%s", got)
	}
}
