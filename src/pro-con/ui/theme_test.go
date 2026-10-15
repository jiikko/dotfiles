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

// themeRoles はテーマの role と style.go の col* の対応。TestThemeColorsMatchYML が yml と突き合わせ、TestNoThemeColorLiterals が
// ここから「直書きしてはいけない番号」を作る (番号を検査の側に書くと、テーマを変えたときに新しい番号の直書きを通す)。
var themeRoles = []struct {
	role string
	n    int
}{
	{"current_accent", colCurrent}, {"quantity_yellow", colAttention}, {"marker_orange", colMarker}, {"info_cyan", colInfo},
	{"active_green", colActive}, {"cold_gray", colCold}, {"error_red", colError},
}

// テーマの色 (style.go の col*) が theme/colors.yml の role の cterm と一致する (issue 770)。
// Go の生成器は無いので手で写している。yml を変えたときに黙ってずれないよう、ここで突き合わせる (glogx の TestFrameBorderMatchesThemeYML と同じ形)。
func TestThemeColorsMatchYML(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "theme", "colors.yml"))
	if err != nil {
		t.Fatalf("theme/colors.yml を読めない: %v", err)
	}
	for _, tc := range themeRoles {
		want, ok := ymlCterm(string(data), tc.role)
		if !ok {
			t.Fatalf("theme/colors.yml に %s の cterm が無い", tc.role)
		}
		if tc.n != want {
			t.Errorf("%s: style.go は %d、theme/colors.yml は %d (yml を変えたなら style.go の col* も揃える)", tc.role, tc.n, want)
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
// ui の production の .go を AST で読み、テーマの番号と同じ値の整数リテラル (16 進・8 進・桁区切りも値で比べる) と、
// 文字列の中の `colour<番号>` / `color<番号>` / `;5;<番号>` / `:5:<番号>` / 番号だけ・`<番号>m` の文字列を拾う。
// 拾わないもの: style.go の関数の外の const の col* の定義そのもの・`theme-literal-ok: <理由>` の印 (理由は必須) がある行。
// 検出しない形: ui の外の package・数字を計算で作る形 (200+2)・番号を分けた文字列の連結 ("20"+"2m")・rune や float で書いた番号・
// 1 行の印が同じ行のほかのリテラルまで外すこと。
var (
	themeNums     = map[int64]bool{}
	themeInString *regexp.Regexp
	themeOK       = regexp.MustCompile(`theme-literal-ok:\s*\S`)
)

func init() {
	var alts []string
	for _, r := range themeRoles {
		themeNums[int64(r.n)] = true
		alts = append(alts, strconv.Itoa(r.n))
	}
	n := "(" + strings.Join(alts, "|") + ")"
	themeInString = regexp.MustCompile(`(colou?r|[;:]5[;:])` + n + `([^0-9]|$)|^["\x60]` + n + `m?["\x60]$`)
}

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
		ok := map[int]bool{} // theme-literal-ok の印 (理由つき) がある行
		for _, cg := range af.Comments {
			for _, c := range cg.List {
				if themeOK.MatchString(c.Text) {
					ok[fset.Position(c.Pos()).Line] = true
				}
			}
		}
		colDef := map[token.Pos]bool{} // style.go の関数の外の const の col* の値
		if f == "style.go" {
			for _, d := range af.Decls {
				gd, isGD := d.(*ast.GenDecl)
				if !isGD || gd.Tok != token.CONST {
					continue
				}
				for _, sp := range gd.Specs {
					vs := sp.(*ast.ValueSpec)
					for i, name := range vs.Names {
						if strings.HasPrefix(name.Name, "col") && i < len(vs.Values) {
							colDef[vs.Values[i].Pos()] = true
							defs++
						}
					}
				}
			}
		}
		ast.Inspect(af, func(n ast.Node) bool {
			bl, isBL := n.(*ast.BasicLit)
			if !isBL || colDef[bl.Pos()] {
				return true
			}
			hit := false
			switch bl.Kind {
			case token.INT:
				v, err := strconv.ParseInt(bl.Value, 0, 64)
				hit = err == nil && themeNums[v]
			case token.STRING:
				hit = themeInString.MatchString(bl.Value)
			default: // rune・float は見ない (冒頭の「検出しない形」)
			}
			if pos := fset.Position(bl.Pos()); hit && !ok[pos.Line] {
				t.Errorf("%s:%d: テーマの色の番号 %s を直に書いている (style.go の col* を使う。色でない数なら行に theme-literal-ok: <理由> を書く)", f, pos.Line, bl.Value)
			}
			return true
		})
	}
	if seen == 0 || defs != len(themeRoles) {
		t.Fatalf("検査した .go が %d 件・style.go の col* の定義が %d 件 (themeRoles は %d 個。col* を足したら themeRoles にも足す)", seen, defs, len(themeRoles))
	}
}
