//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
)

type processResult struct {
	Code int
	Err  error
}

type process struct {
	cmd       *exec.Cmd
	pid       int
	finished  atomic.Bool
	result    processResult
	done      chan processResult
	reader    *os.File
	outputEnd chan struct{}
	closeOnce sync.Once
}

func startProcess(argv []string, shell bool, env []string, stdin io.Reader, sink *logSink, headless bool) (*process, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	var cmd *exec.Cmd
	if shell {
		cmd = exec.Command("/bin/sh", "-c", argv[0])
	} else {
		cmd = exec.Command(argv[0], argv[1:]...)
	}
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Start(); err != nil {
		_ = r.Close()
		_ = w.Close()
		return nil, err
	}
	_ = w.Close()
	p := &process{cmd: cmd, pid: cmd.Process.Pid, done: make(chan processResult, 1), reader: r, outputEnd: make(chan struct{})}
	go func() {
		defer close(p.outputEnd)
		defer p.closeOutput()
		sink.CopyFrom(p.reader, headless)
	}()
	go func() {
		err := cmd.Wait()
		result := processResult{Code: processExitCode(cmd.ProcessState, err), Err: err}
		p.result = result
		p.finished.Store(true)
		p.done <- result
		close(p.done)
	}()
	return p, nil
}

func (p *process) closeOutput() { p.closeOnce.Do(func() { _ = p.reader.Close() }) }

func processExitCode(state *os.ProcessState, err error) int {
	if state == nil {
		return 1
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok {
		if status.Signaled() {
			return 128 + int(status.Signal())
		}
		return status.ExitStatus()
	}
	if err == nil {
		return 0
	}
	return 1
}

func signalGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	err := syscall.Kill(-pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func groupExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(-pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
