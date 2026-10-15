package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// テーマの色 (style.go の col*) が theme/colors.yml の role の cterm と一致する (issue 770)。
// Go の生成器は無いので手で写している。yml を変えたときに黙ってずれないよう、ここで突き合わせる (glogx の TestFrameBorderMatchesThemeYML と同じ形)。
func TestThemeColorsMatchYML(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "theme", "colors.yml"))
	if err != nil {
		t.Fatalf("theme/colors.yml を読めない: %v", err)
	}
	for _, tc := range []struct {
		role string
		got  int
	}{
		{"current_accent", colCurrent}, {"quantity_yellow", colAttention}, {"info_cyan", colInfo},
		{"active_green", colActive}, {"cold_gray", colCold}, {"error_red", colError},
	} {
		want, ok := ymlCterm(string(data), tc.role)
		if !ok {
			t.Fatalf("theme/colors.yml に %s の cterm が無い", tc.role)
		}
		if tc.got != want {
			t.Errorf("%s: style.go は %d、theme/colors.yml は %d (yml を変えたなら style.go の col* も揃える)", tc.role, tc.got, want)
		}
	}
}

// ymlCterm は role の行の後に続く最初の "cterm: <n>" を返す。
func ymlCterm(yml, role string) (int, bool) {
	lines := strings.Split(yml, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, role+":") {
			continue
		}
		for _, next := range lines[i+1:] {
			if s := strings.TrimSpace(next); strings.HasPrefix(s, "cterm:") {
				n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "cterm:")))
				return n, err == nil
			}
		}
	}
	return 0, false
}

// テーマの色の番号を col* を通さずに直に書いた所が無い (写しが 2 つになると、上のテストが見ない方がずれる)。
var themeLiteral = regexp.MustCompile(`\b(fg|bg|bgDim)\((202|214|51|46|240|196)\b|;5;(202|214|51|46|240|196)m`)

func TestNoThemeColorLiterals(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("ui の .go を列挙できない: %v %v", files, err)
	}
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		seen++
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(string(b), "\n") {
			if themeLiteral.MatchString(l) {
				t.Errorf("%s:%d: テーマの色の番号を直に書いている (style.go の col* を使う): %s", f, i+1, strings.TrimSpace(l))
			}
		}
	}
	if seen == 0 {
		t.Fatal("検査した .go が 0 件")
	}
}
