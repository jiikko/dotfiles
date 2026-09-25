package ui

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
)

// pgPanel は画面から PG の一覧の枠の中だけを切り出す (カンバンにも同じカード ID が出るので、画面全体では探さない)。
func pgPanel(m *Model) string {
	out := ansi.Strip(m.render())
	i := strings.Index(out, "PG (consumer)")
	if i < 0 {
		return ""
	}
	out = out[i:]
	if j := strings.Index(out, "╰"); j >= 0 {
		out = out[:j]
	}
	return out
}

// s で PG の一覧を開閉する。PG ごとに担当カードと実体 (本物なら pid、模擬なら「模擬」) が出る。
// pro-con の外の session は、名前も本数も出さない (「ほかの session」の行も、ゲージの claude の本数も無い)。
func TestPGPanel(t *testing.T) {
	m := New(newSpy(), nil)
	m.snap.Consumers = []backend.Consumer{
		{Session: "pg-1", CardID: "R1", Status: "busy"},
		{Session: "3feb603f", CardID: "W1", Status: "waiting", PID: 4242},
	}
	press(m, "s")
	settle(m)
	out := pgPanel(m)
	// 見出しの数は枠を使っている PG だけ (issue 455)。W1 は作業中の列に居ないので、一覧には出すが数えない
	for _, want := range []string{"PG (consumer) 1/2", "pg-1", "R1", "模擬", "3feb603f", "pid 4242"} {
		if !strings.Contains(out, want) {
			t.Fatalf("PG の一覧に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ほかの") || strings.Contains(ansi.Strip(m.gauge()), "claude ") {
		t.Fatalf("pro-con の外の session の本数を出している:\n%s\n%s", out, ansi.Strip(m.gauge()))
	}
	press(m, "s")
	settle(m)
	if strings.Contains(ansi.Strip(m.render()), "PG (consumer)") {
		t.Fatal("閉じたのにパネルが残っている")
	}
}

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
