package ui

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
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

// 「開いた」の欄は 1 時間以上の経過を時間・日に丸め、出る形はどれも欄の幅に区切りを残して収まる (issue 780。案C の「23時間59分前」は切れていた)。
func TestOpenedTextFitsColumn(t *testing.T) {
	now := time.Date(2026, 10, 16, 15, 30, 0, 0, time.Local)
	for d, want := range map[time.Duration]string{
		5 * time.Second:                 "(5秒前)",
		42 * time.Minute:                "(42分前)",
		65 * time.Minute:                "(1時間前)",
		23*time.Hour + 59*time.Minute:   "(23時間前)",
		74 * time.Hour:                  "(3日前)",
		999*24*time.Hour + 23*time.Hour: "(999日前)",
	} {
		got := openedText(now.Add(-d), now)
		if !strings.HasSuffix(got, want) || !strings.HasPrefix(got, now.Add(-d).Format("15:04")+" ") {
			t.Errorf("%v 前に開いた画面: %q (want 時刻 + %q)", d, got, want)
		}
		if w := termwidth.Of(got); w > openedWidth-2 {
			t.Errorf("%q は %d 桁で、欄 (%d) に区切りを残して収まらない", got, w, openedWidth)
		}
	}
}
