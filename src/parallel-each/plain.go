package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
)

// runPlain runs without TUI: one-line-per-completion log to stderr, like the
// original zsh script. Used for non-TTY stdout or --no-tui.
//
// Shutdown is two-stage, mirroring the TUI:
//   1st SIGINT/SIGTERM  -> graceful stop (no new jobs, wait for running)
//   2nd                 -> force-kill running subprocesses
func runPlain(ctx context.Context, cfg Config, lines []string, skipped int) int {
	if skipped > 0 {
		fmt.Fprintf(os.Stderr,
			"resumed: skipped %d items already in %s/result.log (use --fresh to rerun all)\n",
			skipped, logDir)
	}
	r := NewRunner(cfg, lines)
	if err := r.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var sigCount int32
	go func() {
		for sig := range sigCh {
			if atomic.AddInt32(&sigCount, 1) == 1 {
				fmt.Fprintf(os.Stderr,
					"\ngot %s: gracefully stopping. %s again to force-kill running jobs.\n",
					sig, sig)
				r.RequestStop()
			} else {
				fmt.Fprintln(os.Stderr, "force-killing running jobs...")
				r.ForceKill()
			}
		}
	}()

	// Parent context cancel also forces a stop (e.g. --total-timeout or
	// external cancellation).
	go func() {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fmt.Fprintln(os.Stderr, "\n--total-timeout reached; force-killing running jobs...")
		}
		r.ForceKill()
	}()

	width := digitWidth(len(lines))
	total := len(lines)
	fail := 0
	done := 0

	for ev := range r.Events() {
		if ev.Kind != EventEnd {
			continue
		}
		done++
		if ev.ExitCode == 0 {
			fmt.Fprintf(os.Stderr, "[%0*d/%d] ok  %s\n", width, ev.JobIndex, total, ev.Line)
		} else {
			fail++
			fmt.Fprintf(os.Stderr, "[%0*d/%d] FAIL(exit=%d) %s -> %s\n",
				width, ev.JobIndex, total, ev.ExitCode, ev.Line, ev.LogPath)
		}
	}

	interrupted := atomic.LoadInt32(&sigCount) > 0 ||
		errors.Is(ctx.Err(), context.DeadlineExceeded)

	if interrupted {
		reason := "cancelled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "timed out"
		}
		skipped := total - done
		fmt.Fprintf(os.Stderr,
			"%s: %d/%d completed (%d ok, %d failed, %d not run) (logs: %s/)\n",
			reason, done, total, done-fail, fail, skipped, logDir)
		return 1
	}
	if fail > 0 {
		fmt.Fprintf(os.Stderr, "summary: %d/%d failed (logs: %s/)\n", fail, total, logDir)
		return 1
	}
	fmt.Fprintf(os.Stderr, "summary: all %d ok (logs: %s/)\n", total, logDir)
	return 0
}
