// schedkeys — tmux の予約入力ウィザード (prefix+m / Enter / C-m)。
//
// 対話 UI だけを持ち、tmux にも job ファイルにも触れない。結果は --out のファイルへ 1 行書く:
//
//	new\t<発火 epoch>\t<送る文字列>
//	cancel\t<予約 id>
//
// 中止 (Esc / Ctrl-C) は out を空のままにして exit 1。呼び出し側 (scripts/tmux_schedule_keys.sh) が
// job ファイルの作成・sleeper の起動・取消の確認 (gum confirm --default=false) と実行を行う。
// この分担にしているのは、破壊的な操作をシェル側のテスト済み経路に残すため。
//
// ⚠️ UI は stdout ではなく端末 (/dev/tty) へ描く。呼び出し側が $(...) で捕まえてもよいように、
// 結果は out ファイル経由にしてある (stdout に結果を書くと TUI の描画と混ざる)。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

func main() {
	label := flag.String("label", "", "送り先 pane の表示名")
	jobsPath := flag.String("jobs", "", "予約一覧の TSV (id/epoch/label/text)")
	outPath := flag.String("out", "", "結果を書くファイル (必須)")
	togglePrefix := flag.String("toggle-prefix", "", "tmux の prefix キー (例 C-t)。これに続けて m / Enter を押すと閉じる")
	flag.Parse()

	if *outPath == "" {
		fmt.Fprintln(os.Stderr, "schedkeys: --out が要る")
		os.Exit(2)
	}
	jobs, err := readJobs(*jobsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "schedkeys: 予約一覧が読めない: %v\n", err)
		os.Exit(2)
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "schedkeys: 端末が開けない: %v\n", err)
		os.Exit(2)
	}
	defer tty.Close()

	m := newModel(*label, time.Now(), jobs)
	m.togglePrefix = teaKeyName(*togglePrefix)
	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "schedkeys: %v\n", err)
		os.Exit(2)
	}
	line := formatResult(m.res)
	if line == "" {
		os.Exit(1) // 中止
	}
	if err := os.WriteFile(*outPath, []byte(line+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "schedkeys: 結果が書けない: %v\n", err)
		os.Exit(2)
	}
}

// teaKeyName は tmux のキー名 (C-t / M-x / F1) を bubbletea の String() 形式へ直す。
// 対応できない形は空を返す (トグルが効かないだけで、他の操作には影響しない)。
func teaKeyName(tmuxKey string) string {
	k := strings.TrimSpace(tmuxKey)
	switch {
	case k == "":
		return ""
	case strings.HasPrefix(k, "C-") && len(k) > 2:
		return "ctrl+" + strings.ToLower(k[2:])
	case strings.HasPrefix(k, "M-") && len(k) > 2:
		return "alt+" + strings.ToLower(k[2:])
	default:
		return strings.ToLower(k)
	}
}

// formatResult は out ファイルの 1 行を組み立てる。中止なら空文字列。
// 送る文字列にタブや改行が入ると読み手が誤読するので、ここで空白へ潰す (末尾の Enter は
// シェル側が別に送るので、改行を残す意味がない)。
func formatResult(r result) string {
	switch r.action {
	case "new":
		text := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(r.text)
		return fmt.Sprintf("new\t%d\t%s", r.at.Unix(), text)
	case "cancel":
		return "cancel\t" + r.id
	default:
		return ""
	}
}
