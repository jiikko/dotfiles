package ui

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 画面 (ui) は claude agents を自分で読まない (pro-con の外の session に触れる経路を作らない)。PG の情報は backend の Snapshot だけ。
// 🚨 この検査は import を見る静的な固定。agents を使わずに exec で claude を呼ぶ形は検出しない (その形は review で見る)。
func TestUIDoesNotListSessionsItself(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("ui の Go ファイルを列挙できない: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		pf, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range pf.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); p == "pro-con/agents" {
				t.Fatalf("%s が pro-con/agents を import している (画面が claude agents を自分で読む経路になる)", f)
			}
		}
	}
}
