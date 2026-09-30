package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/jiikko/dotfiles/src/restartable/internal/control"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
)

func main() { os.Exit(realMain(os.Args[1:])) }

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
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "connection refused") {
			return 2
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return 124
		}
		return 1
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
	headless := !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd())
	code, err := runner.Run(runner.Config{
		BuildCommand: *build, RunArgs: runArgs, StopCommand: *stop,
		StopCommandTimeout: *stopTimeout, TermGrace: *termGrace,
		IDEnv: *idEnv, ControlPath: resolved, Stdin: os.Stdin,
		Stdout: os.Stdout, Stderr: os.Stderr, Headless: headless,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
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
