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
		_, _ = fmt.Fprintln(help.Output(), "Usage: git-popup\n\nKeys: j/k or arrows move, Ctrl-b push, q/Esc/Ctrl-g quit")
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
