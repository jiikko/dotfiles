package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
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
		{"current_accent", colCurrent}, {"quantity_yellow", colAttention}, {"marker_orange", colMarker}, {"info_cyan", colInfo},
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
			if next != "" && next[0] != ' ' && next[0] != '#' { // 次の role に入った (この role に cterm が無い。次の role の値を拾わない)
				break
			}
			if s := strings.TrimSpace(next); strings.HasPrefix(s, "cterm:") {
				n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "cterm:")))
				return n, err == nil
			}
		}
	}
	return 0, false
}

// テーマの色の番号を col* を通さずに直に書いた所が無い (写しが 2 つになると、上のテストが見ない方がずれる。issue 770)。
// ui の production の .go を AST で読み、テーマの番号と同じ整数リテラルと、文字列の中の `colour<番号>` / `;5;<番号>` / `<番号>m` を拾う。
// 拾わないもの: style.go の col* の定義そのもの・`theme-literal-ok:` の印 (理由を添える) がある行。
// 検出しない形: ui の外の package・数字を計算で作る形 (200+2)・番号を分けた文字列の連結 ("20"+"2m")。
var themeNums = map[string]bool{"202": true, "214": true, "208": true, "51": true, "46": true, "240": true, "196": true}

var themeInString = regexp.MustCompile(`(colour|;5;)(202|214|208|51|46|240|196)\b|^"(202|214|208|51|46|240|196)m`)

func TestNoThemeColorLiterals(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("ui の .go を列挙できない: %v %v", files, err)
	}
	fset := token.NewFileSet()
	seen, defs := 0, 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		seen++
		af, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		ok := map[int]bool{} // theme-literal-ok の印がある行
		for _, cg := range af.Comments {
			for _, c := range cg.List {
				if strings.Contains(c.Text, "theme-literal-ok:") {
					ok[fset.Position(c.Pos()).Line] = true
				}
			}
		}
		colDef := map[token.Pos]bool{} // col* の定義の値
		ast.Inspect(af, func(n ast.Node) bool {
			if vs, isVS := n.(*ast.ValueSpec); isVS {
				for i, name := range vs.Names {
					if strings.HasPrefix(name.Name, "col") && i < len(vs.Values) {
						colDef[vs.Values[i].Pos()] = true
						defs++
					}
				}
			}
			return true
		})
		ast.Inspect(af, func(n ast.Node) bool {
			bl, isBL := n.(*ast.BasicLit)
			if !isBL || colDef[bl.Pos()] {
				return true
			}
			hit := bl.Kind == token.INT && themeNums[bl.Value] || bl.Kind == token.STRING && themeInString.MatchString(bl.Value)
			if pos := fset.Position(bl.Pos()); hit && !ok[pos.Line] {
				t.Errorf("%s:%d: テーマの色の番号 %s を直に書いている (style.go の col* を使う。色でない数なら行に theme-literal-ok: <理由> を書く)", f, pos.Line, bl.Value)
			}
			return true
		})
	}
	if seen == 0 || defs < 7 {
		t.Fatalf("検査した .go が %d 件・col* の定義が %d 件 (style.go の col* を見つけられていない)", seen, defs)
	}
}
