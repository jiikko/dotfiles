package main

import (
	"strings"
	"testing"
)

// issue 171: brew の警告は本文に空行を含む (Homebrew の diagnostic.rb は \n\n 入りの heredoc を
// 多数持つ)。空行でブロックを切ると 2 塊目以降が Warning: で始まらず others として黙って捨てられ、
// 「Run `brew link` on these:」のような修復コマンドと対象一覧が消える。
// fixture は実機 (Homebrew 6.0.20 / rc=1) の stderr。
func TestParseBrewDoctorKeepsLinesAfterBlankLine(t *testing.T) {
	const fixture = "Please note that these warnings are just used to help the Homebrew maintainers\nwith debugging if you file an issue. If everything you use Homebrew for is\nworking fine: please don't worry or file an issue; just ignore this. Thanks!\n\nWarning: Some installed formulae are deprecated or disabled.\nYou should find replacements for the following formulae:\n    gemini-cli\n  tree-sitter@0.25\n\nWarning: You have unlinked kegs in your Cellar.\nLeaving kegs unlinked can lead to build-trouble and cause formulae that depend on\nthose kegs to fail to run properly once built.\n\nRun `brew link` on these:\n  ruby"
	res := parseBrewDoctor("", fixture, 1)
	joined := strings.Join(res.Warnings, "\n")
	// 前置き 3 行以外の非空行が 1 行も落ちていないこと (行単位の全数勘定)
	lines := strings.Split(fixture, "\n")
	want := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.TrimSpace(line) == "",
			strings.HasPrefix(line, "Please note that these warnings"),
			strings.HasPrefix(line, "with debugging if you file an issue"),
			strings.HasPrefix(line, "working fine: please don't worry"):
			continue
		}
		want = append(want, line)
	}
	got := 0
	for _, line := range strings.Split(joined, "\n") {
		if strings.TrimSpace(line) != "" {
			got++
		}
	}
	if got != len(want) {
		t.Errorf("非空行が落ちている: want=%d got=%d\n--- 出力 ---\n%s", len(want), got, joined)
	}
	for _, line := range want {
		if !strings.Contains(joined, line) {
			t.Errorf("落ちた行: %q", line)
		}
	}
	// 空行の後ろにある修復コマンドと対象一覧が、その警告の塊に残っていること
	if len(res.Warnings) != 2 {
		t.Fatalf("警告の数: got=%d %+v", len(res.Warnings), res.Warnings)
	}
	if !strings.Contains(res.Warnings[1], "Run `brew link` on these:") || !strings.Contains(res.Warnings[1], "  ruby") {
		t.Errorf("修復コマンド / 対象一覧が 2 番目の警告に残っていない: %q", res.Warnings[1])
	}
	if res.Unavailable != "" {
		t.Errorf("Unavailable になった: %s", res.Unavailable)
	}
}

// 敵対レビュー 2026-09-03: 前置き allowlist に無い行が警告のあいだに現れたとき、直前の警告の本文へ
// 地続きに繋がって見えないこと (段落の空行を残す)。塊の先頭・末尾に空行は持たない。
func TestParseBrewDoctorKeepsParagraphBreaks(t *testing.T) {
	res := parseBrewDoctor("", "Warning: A\ndetail1\n\nstray line not in the allowlist\n\nWarning: B\ndetail2\n\n", 1)
	if len(res.Warnings) != 2 {
		t.Fatalf("警告の数: %+v", res.Warnings)
	}
	if res.Warnings[0] != "Warning: A\ndetail1\n\nstray line not in the allowlist" {
		t.Errorf("段落の切れ目が消えて地続きになった: %q", res.Warnings[0])
	}
	if res.Warnings[1] != "Warning: B\ndetail2" {
		t.Errorf("末尾の空行が塊に残った: %q", res.Warnings[1])
	}
	// 連続する空行は 1 つに畳む (展開時の余白が段落数ぶん増えるだけで意味を持たない)
	res = parseBrewDoctor("", "Warning: A\ndetail1\n\n\n\nafter\n", 1)
	if len(res.Warnings) != 1 || res.Warnings[0] != "Warning: A\ndetail1\n\nafter" {
		t.Errorf("連続空行が畳まれていない: %q", res.Warnings)
	}
	// 見出し直後の空行だけの本文は、末尾トリムで見出しだけの塊になる
	res = parseBrewDoctor("", "Warning: A\n\n\nWarning: B\ndetail2\n", 1)
	if len(res.Warnings) != 2 || res.Warnings[0] != "Warning: A" {
		t.Errorf("見出しだけの塊にならない: %q", res.Warnings)
	}
}
