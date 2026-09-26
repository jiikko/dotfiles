package card

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Doing は PG が今走らせているもの 1 つ (issue 473)。dispatcher が集めて store.DoingFile に書き、画面と `pro-con card show` が読む
// (記録 cards.json には入れない)。テストの係に頼んだコマンドはカードの Run / Exec にあるので、ここには入れない (DoingLines が足す)。
type Doing struct {
	Kind  DoingKind `json:"kind"`
	Text  string    `json:"text"`            // コマンド (1 行) / サブエージェントの説明 / 道具の名前と引数の要約
	Since time.Time `json:"since"`           // 始まった時刻
	Depth int       `json:"depth,omitempty"` // プロセスの木の深さ (0 が PG が直接起こしたコマンド)
}

type DoingKind string

const (
	DoingProcess DoingKind = "process" // PG の session の子孫のプロセス (Bash のコマンド・裏の shell)
	DoingAgent   DoingKind = "agent"   // 裏で走らせたサブエージェント
	DoingTool    DoingKind = "tool"    // 結果がまだ返っていない道具の呼び出し (プロセスとして見えないもの)
)

// DoingStale を過ぎて集め直されていない様子は古いと出す (dispatcher は 10 秒ごとに集める。止まっていれば更新されない)。
const DoingStale = time.Minute

// DoingHead は詳細の「今走っているもの」の見出し (集めた時刻が古ければそう言う)。
func DoingHead(c Card, now time.Time, dur func(time.Duration) string) string {
	switch age := now.Sub(c.DoingAt); {
	case c.DoingAt.IsZero():
		return "今走っているもの"
	case age > DoingStale:
		return "今走っているもの (" + dur(age) + "前に集めたまま。dispatcher が止まっている?)"
	}
	return "今走っているもの (dispatcher が集めた様子)"
}

// DoingLines は詳細 (画面の引き出しと card show) に出す「今走っているもの」の行。テストの係の頼み (順番待ち / 実行中) を先に、
// 集めたもの (Doing) を後に並べる。dur は経過の書き方 (画面と CLI で粒度の表記が違う)。何も無ければ nil。
func (c Card) DoingLines(now time.Time, dur func(time.Duration) string) []string {
	var out []string
	switch {
	case c.Exec.Active():
		out = append(out, fmt.Sprintf("%s  テストの係が実行中: %s", dur(now.Sub(c.Exec.Since)), c.Exec.Command))
	case c.Run != "" && c.Wait.Kind == WaitResource:
		out = append(out, fmt.Sprintf("%s  テストの係の順番待ち (%d 番目): %s", dur(now.Sub(c.RunAt)), c.Wait.Position, c.Run))
	case c.Run != "":
		out = append(out, fmt.Sprintf("%s  テストの係に頼んだ (まだ始まっていない): %s", dur(now.Sub(c.RunAt)), c.Run))
	}
	for _, d := range c.Doing {
		text := d.Text
		switch d.Kind {
		case DoingAgent:
			text = "サブエージェント: " + text
		case DoingTool:
			text = "道具: " + text
		case DoingProcess:
		}
		out = append(out, strings.Repeat("  ", d.Depth)+dur(now.Sub(d.Since))+"  "+text)
	}
	return out
}

// DoingHeadline はボードのカードに 1 行で出す「今走っているもの」(例 `mut.sh 13分`)。PG が直接起こしたもの (深さ 0) のうち
// いちばん長く走っているもの。テストの係の実行はバッジが別に出すので含めない。無ければ ok = false。
func (c Card) DoingHeadline() (label string, since time.Time, ok bool) {
	for _, d := range c.Doing {
		if d.Depth != 0 || (ok && !d.Since.Before(since)) {
			continue
		}
		label, since, ok = ShortLabel(d), d.Since, true
	}
	return label, since, ok
}

// ShortLabel はボードの 1 行に載せる短い名前。プロセスはコマンドの名前 (シェルと環境変数の前置きを飛ばし、`go test` のように
// 下位のコマンドの名前が続けば 2 語まで)。ほかは説明の先頭。
func ShortLabel(d Doing) string {
	if d.Kind != DoingProcess {
		return clipRunes(d.Text, 20)
	}
	f := strings.Fields(d.Text)
	for len(f) > 0 && (isShell(filepath.Base(f[0])) || strings.HasPrefix(f[0], "-") || strings.Contains(f[0], "=")) {
		f = f[1:]
	}
	if len(f) == 0 {
		return clipRunes(d.Text, 20)
	}
	name := filepath.Base(f[0])
	if len(f) > 1 && isWord(f[1]) {
		name += " " + f[1]
	}
	return clipRunes(name, 20)
}

func isShell(s string) bool {
	switch s {
	case "bash", "sh", "zsh", "env", "nohup", "time", "timeout", "exec":
		return true
	}
	return false
}

// isWord は下位のコマンドの名前らしい語 (英小文字と - だけ。フラグ・パス・数・引用は含めない)。
func isWord(s string) bool {
	if s == "" || s[0] == '-' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
