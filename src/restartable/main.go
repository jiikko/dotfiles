package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/jiikko/dotfiles/src/restartable/internal/control"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
	"github.com/jiikko/dotfiles/src/restartable/internal/ui"
)

func main() { os.Exit(runMain(os.Args[1:])) }

func runMain(args []string) int {
	pipeSignals := make(chan os.Signal, 1)
	signal.Notify(pipeSignals, syscall.SIGPIPE)
	go func() {
		for range pipeSignals {
		}
	}()
	return realMain(args)
}

func realMain(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "status":
			return controlCommand(args[1:], control.Status, 2*time.Second)
		case "restart":
			return controlCommand(args[1:], control.Restart, 15*time.Minute)
		case "run":
			args = args[1:]
		}
	}
	return runCommand(args)
}

func controlCommand(args []string, command control.Command, defaultTimeout time.Duration) int {
	name := string(command)
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("control", "", "control socket path")
	timeout := fs.Duration("timeout", defaultTimeout, "maximum time to wait for a response")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "%s: unexpected arguments\n", name)
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be positive")
		return 2
	}
	resolved := *path
	if resolved == "" {
		var err error
		resolved, err = control.DefaultPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	response, err := control.Call(ctx, resolved, command)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return callErrorCode(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !response.OK {
		return 1
	}
	return 0
}

func runCommand(args []string) int {
	fs := flag.NewFlagSet("restartable", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	build := fs.String("build", "", "shell command to build before running")
	stop := fs.String("stop-cmd", "", "shell command that asks the child to exit")
	stopTimeout := fs.Duration("stop-cmd-timeout", 5*time.Second, "maximum run time for --stop-cmd")
	termGrace := fs.Duration("term-grace", 5*time.Second, "wait after SIGTERM before SIGKILL")
	ready := fs.String("ready-cmd", "", "shell command that exits 0 when the child is ready")
	readyTimeout := fs.Duration("ready-timeout", 120*time.Second, "maximum time to wait for --ready-cmd")
	idEnv := fs.String("id-env", "", "environment variable receiving the instance ID")
	path := fs.String("control", "", "control socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	runArgs := fs.Args()
	if len(runArgs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: restartable [flags] -- <run command...>")
		return 2
	}
	if *stopTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "--stop-cmd-timeout must be positive")
		return 2
	}
	if *termGrace < 0 {
		fmt.Fprintln(os.Stderr, "--term-grace cannot be negative")
		return 2
	}
	if *readyTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "--ready-timeout must be positive")
		return 2
	}
	if *idEnv != "" && !validEnvName(*idEnv) {
		fmt.Fprintln(os.Stderr, "--id-env must be an environment variable name")
		return 2
	}
	resolved := *path
	if resolved == "" {
		var err error
		resolved, err = control.DefaultPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	cfg := withTerminals(runner.Config{
		BuildCommand: *build, RunArgs: runArgs, StopCommand: *stop,
		StopCommandTimeout: *stopTimeout, TermGrace: *termGrace,
		ReadyCommand: *ready, ReadyTimeout: *readyTimeout,
		IDEnv: *idEnv, ControlPath: resolved, Stdin: os.Stdin,
		Stdout: os.Stdout, Stderr: os.Stderr,
	}, term.IsTerminal(os.Stdin.Fd()), term.IsTerminal(os.Stdout.Fd()))
	if !cfg.Headless {
		cfg.Presenter = ui.New(os.Stdin, os.Stdout)
	}
	code, err := runner.Run(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}

// withTerminals は stdin / stdout が端末かを cfg に写す。UI を出すのは両方が端末のときだけ (片方でも違えば headless)。
// stdout が端末かは headless でも別に渡す: UI が無くても端末へ出す子の出力は無害化する (issue 601)
func withTerminals(cfg runner.Config, stdinIsTerminal, stdoutIsTerminal bool) runner.Config {
	cfg.Headless = !stdinIsTerminal || !stdoutIsTerminal
	cfg.StdinIsTerminal, cfg.StdoutIsTerminal = stdinIsTerminal, stdoutIsTerminal
	return cfg
}

func validEnvName(name string) bool {
	for index, char := range name {
		if index == 0 {
			if char != '_' && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') {
				return false
			}
			continue
		}
		if char != '_' && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return name != ""
}

// callErrorCode は control.Call の失敗を終了コードに写す (2 = runner が動いていない / 124 = 時間切れ / 1 = それ以外)。
// obaket の dev-restart が rc 2 を「ループが動いていない」と読むので、写しを変えると呼ぶ側の判断が黙って変わる (issue 598)。
func callErrorCode(err error) int {
	if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "connection refused") {
		return 2
	}
	// Call は接続に ctx と同じ期限の I/O deadline を置くので、時間切れは
	// context の期限切れより先に I/O の期限切れ (os.ErrDeadlineExceeded) として返ることがある。
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return 124
	}
	return 1
}
