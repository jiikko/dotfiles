package card

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var d0 = time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)

// ボードの 1 行の名前はシェルと前置きを飛ばしたコマンドの名前 (下位のコマンドが続けば 2 語)。
func TestShortLabel(t *testing.T) {
	for in, want := range map[string]string{
		"bash /x/tmp/mut.sh --q":     "mut.sh",
		"bin/mutate-verify --name a": "mutate-verify",
		"go test ./...":              "go test",
		"FOO=1 make test":            "make test",
		"sleep 120":                  "sleep",
		"/bin/zsh -c 形の違う包み":         "形の違う包み",
		"--":                         "--",
	} {
		if got := ShortLabel(Doing{Kind: DoingProcess, Text: in}); got != want {
			t.Errorf("ShortLabel(%q) = %q (期待 %q)", in, got, want)
		}
	}
	if got := ShortLabel(Doing{Kind: DoingAgent, Text: "とても長いサブエージェントの説明で二十文字を超えるもの"}); got != "とても長いサブエージェントの説明で二十文…" {
		t.Errorf("説明は 20 文字で切る: %q", got)
	}
}

// ボードの 1 行は PG が直接起こしたもののうち、いちばん長く走っているもの (子孫は選ばない)。
func TestDoingHeadline(t *testing.T) {
	c := Card{Doing: []Doing{
		{Kind: DoingProcess, Text: "make test", Since: d0.Add(-2 * time.Minute)},
		{Kind: DoingProcess, Text: "go test ./...", Since: d0.Add(-30 * time.Minute), Depth: 1},
		{Kind: DoingAgent, Text: "敵対的レビュー", Since: d0.Add(-9 * time.Minute)},
	}}
	label, since, ok := c.DoingHeadline()
	if !ok || label != "敵対的レビュー" || !since.Equal(d0.Add(-9*time.Minute)) {
		t.Fatalf("見出し: %q %v %v", label, since, ok)
	}
	if _, _, ok := (Card{}).DoingHeadline(); ok {
		t.Fatal("何も走っていないのに見出しがある")
	}
}

// 詳細の行はテストの係の頼み (実行中 / 順番待ち / 頼んだだけ) を先に、集めたものを深さで字下げして後に並べる。
func TestDoingLines(t *testing.T) {
	dur := func(d time.Duration) string { return fmt.Sprint(d) }
	c := Card{Run: "make test", RunAt: d0.Add(-time.Minute), Wait: Wait{Kind: WaitResource, Position: 2},
		Doing: []Doing{{Kind: DoingProcess, Text: "mut.sh", Since: d0.Add(-time.Hour)}, {Kind: DoingProcess, Text: "mutate-verify", Since: d0.Add(-time.Minute), Depth: 1},
			{Kind: DoingTool, Text: "WebFetch: https://x", Since: d0}}}
	want := []string{"1m0s  テストの係の順番待ち (2 番目): make test", "1h0m0s  mut.sh", "  1m0s  mutate-verify", "0s  道具: WebFetch: https://x"}
	if got := c.DoingLines(d0, dur); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("詳細の行:\n%s", strings.Join(got, "\n"))
	}
	c.Exec = Exec{Command: "make test", Since: d0.Add(-3 * time.Minute)}
	if got := c.DoingLines(d0, dur); got[0] != "3m0s  テストの係が実行中: make test" {
		t.Fatalf("実行中は順番待ちより先に: %q", got[0])
	}
	c.Exec, c.Wait = Exec{}, Wait{}
	if got := c.DoingLines(d0, dur); !strings.Contains(got[0], "まだ始まっていない") {
		t.Fatalf("頼んだだけ: %q", got[0])
	}
	if got := (Card{}).DoingLines(d0, dur); got != nil {
		t.Fatalf("何も無ければ nil: %q", got)
	}
}

// 見出しは、集めた時刻が DoingStale より古ければそう言う。
func TestDoingHead(t *testing.T) {
	dur := func(d time.Duration) string { return fmt.Sprint(d) }
	if h := DoingHead(Card{DoingAt: d0.Add(-DoingStale)}, d0, dur); strings.Contains(h, "止まっている") {
		t.Fatalf("ちょうど DoingStale は古くない: %q", h)
	}
	if h := DoingHead(Card{DoingAt: d0.Add(-DoingStale - time.Second)}, d0, dur); !strings.Contains(h, "止まっている") {
		t.Fatalf("古い様子と言うはず: %q", h)
	}
}
