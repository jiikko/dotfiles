package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(argv []string) int {
	help := flag.NewFlagSet("git-popup", flag.ContinueOnError)
	help.SetOutput(os.Stderr)
	help.Usage = func() {
		_, _ = fmt.Fprintln(help.Output(), `Usage: git-popup

git log の TUI。一覧 (行頭に CI 成否マーク・未 push は橙 SHA) と詳細 (diff + CI job) の 2 ペイン。

Keys:
  j/k ↑/↓ C-n/C-p     一覧の上下移動
  g/Home  G/End        先頭 / 末尾へ
  Enter → C-f          詳細 (右ペイン) へフォーカス
    j/k ↑/↓ C-n/C-p   1 行スクロール
    Space/f/C-d/PgDn   半ページ下  (b/C-u/PgUp = 上)
    g/Home  G/End      先頭 / 末尾
    o                  CI job 選択 → j/k で選び Enter/o でブラウザで開く
    Esc/h/←/C-g        1 段戻る (詳細からは Enter でも一覧へ)
  C-b                  push (y/N 確認)
  q / C-c              終了 (一覧では Esc/C-g でも)`)
	}
	showHelp := help.Bool("help", false, "show this help")
	help.BoolVar(showHelp, "h", false, "show this help")
	if err := help.Parse(argv); err != nil {
		return 2
	}
	if *showHelp || help.Arg(0) == "-h" {
		help.Usage()
		return 0
	}
	commits, err := loadCommits()
	if err != nil {
		printGitError(err)
		return 1
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
		for _, commit := range commits {
			fmt.Printf("  %s %s\n", commit.ShortSHA, commit.Subject)
		}
		return 0
	}
	if _, err := tea.NewProgram(newRootModel(commits), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "git-popup:", err)
		return 1
	}
	return 0
}

func printGitError(err error) {
	var gitErr *gitError
	if errors.As(err, &gitErr) && gitErr.Stderr != "" {
		fmt.Fprint(os.Stderr, gitErr.Stderr)
		return
	}
	fmt.Fprintln(os.Stderr, err)
}
