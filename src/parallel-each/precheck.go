package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// PreCheckResult is the per-line outcome of an --input-type pre-check.
type PreCheckResult struct {
	Line   string
	OK     bool
	Reason string // empty when OK; one-line failure description otherwise
}

// preCheckURLs runs an HTTP 200 check against each line in parallel.
// Lines that respond 200 are returned in `kept` (in original order).
// Lines that fail are returned in `dropped` with a brief reason.
//
// `parallelism` caps concurrent curl invocations. perItemTimeout is the
// max wall-clock for a single line's curl request (curl's --max-time
// plus a small grace period).
func preCheckURLs(lines []string, perItemTimeout time.Duration, parallelism int) (kept []string, dropped []PreCheckResult) {
	if parallelism <= 0 {
		parallelism = 16
	}
	results := make([]PreCheckResult, len(lines))
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for i, line := range lines {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, url string) {
			defer wg.Done()
			defer func() { <-sem }()
			ok, reason := curlCheck200(url, perItemTimeout)
			results[i] = PreCheckResult{Line: url, OK: ok, Reason: reason}
		}(i, line)
	}
	wg.Wait()
	for i, r := range results {
		if r.OK {
			kept = append(kept, lines[i])
			continue
		}
		dropped = append(dropped, r)
	}
	return kept, dropped
}

// curlCheck200 issues `curl -sS -o /dev/null -L -w %{http_code}
// --max-time T URL` and returns (true, "") iff the response was 200.
// Anything else (non-200, network error, malformed URL, timeout) yields
// (false, reason).
//
// We use GET (not HEAD) since some servers reject HEAD with 405; the
// body is discarded with -o /dev/null so size is not material. -L
// follows redirects so the recorded code is the FINAL response.
func curlCheck200(url string, timeout time.Duration) (bool, string) {
	if strings.TrimSpace(url) == "" {
		return false, "empty URL"
	}
	// Give curl --max-time slightly less than our context deadline so
	// curl can exit cleanly with a network error message rather than
	// being SIGTERMed by us.
	curlMax := int(timeout.Seconds())
	if curlMax < 1 {
		curlMax = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "curl",
		"-sS",
		"-o", "/dev/null",
		"-L",
		"--max-time", fmt.Sprintf("%d", curlMax),
		"-w", "%{http_code}",
		url,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Non-zero exit (DNS, TLS, refused, timeout, malformed URL,
		// etc.). curl writes a brief message to stderr — surface it,
		// trimmed to one line.
		msg := strings.TrimSpace(string(out))
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return false, "curl: " + msg
	}
	code := strings.TrimSpace(string(out))
	if code == "200" {
		return true, ""
	}
	return false, "HTTP " + code
}
