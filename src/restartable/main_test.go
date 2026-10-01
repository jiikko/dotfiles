package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSIGPIPEMainHelper(t *testing.T) {
	if os.Getenv("RESTARTABLE_SIGPIPE_HELPER") != "1" {
		return
	}
	code := runMain([]string{"--control", "/tmp/" + strings.Repeat("x", 120), "--", "/bin/true"})
	os.Exit(code)
}

func TestSIGPIPERemainsHandledAfterRunnerReturns(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := readEnd.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSIGPIPEMainHelper$")
	cmd.Env = append(os.Environ(), "RESTARTABLE_SIGPIPE_HELPER=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = writeEnd
	if err := cmd.Start(); err != nil {
		_ = writeEnd.Close()
		t.Fatal(err)
	}
	_ = writeEnd.Close()
	err = cmd.Wait()
	if err == nil {
		t.Fatal("helper succeeded, want the runner setup error code 1")
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("helper exit = %v, want exit code 1 (not SIGPIPE)", err)
	}
}
