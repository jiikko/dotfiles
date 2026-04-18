package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

const helpText = `parallel-each — run a command template against each line of a file in parallel

USAGE
  parallel-each [-P jobs] -F file [OPTIONS] "<command with {item}>"

OPTIONS
  -P <jobs>              Parallel job count. Default: 4. 0 = unbounded.
  -F <file>              Input file. One item per line. Blank and '#' lines
                         are skipped.
  --timeout <d>          Per-attempt timeout (REQUIRED; e.g. 30s, 5m). Each
                         attempt that runs longer is SIGTERMed and counted as
                         a failure (eligible for --retries). Not required in
                         --wizard mode (you'll be prompted) or with -n.
  --retries <n>          Additional attempts after a non-zero exit. Default: 5
                         (so up to 6 total tries). 0 disables retries. A
                         single per-job log file records all attempts,
                         separated by "=== retry N/M ===" headers.
  -n                     Dry run. Print resolved commands without executing.
  --no-tui               Disable the TUI (use line-based output even on a TTY).
  --fresh                Ignore any existing parallel-each-log/result.log:
                         truncate it and run every input item. Default is to
                         resume, skipping items already present in result.log.
  --skip-unique-txt-rows Don't validate that input rows are unique. By default
                         the run aborts if the input file has duplicate lines.
  --wizard               Interactively prompt for each value (input file,
                         template, parallelism, timeout, retries) before
                         running. Useful when you can't remember flag names.
  -h, --help             Show this help and exit.

COMMAND TEMPLATE
  Single shell string containing one or more {item} placeholders. Each {item}
  is substituted with the current input line, safely double-quoted, then
  executed via 'sh -c'. Pipes, redirections, and multi-step commands work.

    parallel-each -P 4 -F urls.txt "dm {item}"
    parallel-each -P 8 -F urls.txt "curl -sSfL -o /dev/null {item}"
    parallel-each -P 2 -F files.txt "gzip -c {item} > {item}.gz"

  Quoting: {item} expands to "$1". Don't wrap {item} in single quotes in the
  template — $1 won't expand inside single quotes.

LOGS
  Written under ./parallel-each-log/ (created if missing).
    <NNNN>-<escaped-line>.log   per-job stdout+stderr with a header
    result.log                  appended per completion (TSV):
                                  status<TAB>exit_code<TAB>input<TAB>log_abs_path

RESUME / IDEMPOTENT RERUNS
  On every run, parallel-each reads parallel-each-log/result.log (if present)
  and skips any input line already listed there — including failed ones. To
  retry failures, edit result.log (remove the FAIL rows for lines you want to
  retry) or delete result.log, or use --fresh to ignore it entirely.

INPUT VALIDATION
  By default the run aborts if the input file contains duplicate lines. This
  prevents accidentally processing the same item twice (and writing to the
  same per-job log file). Use --skip-unique-txt-rows to disable.

TUI
  When stdout is a TTY (and --no-tui is not set), an interactive TUI shows a
  progress bar, one row per active slot with elapsed time, the last couple of
  stdout/stderr lines under each active slot, and recent completions.

  Keys:
    1-9        Focus on the slot with that ID. In focus mode the slot's log
               tail fills the lower half of the screen.
    esc, 0     Exit focus mode and return to the overview.
    q, ctrl-c  Stop (see SHUTDOWN below).

SHUTDOWN (two-stage)
  The same semantics apply in TUI keypresses and plain-mode signals.

    1st press of q / Ctrl-C (TUI) or SIGINT / SIGTERM (plain):
      Graceful stop. Dispatcher stops queuing new jobs and queued-but-not-yet-
      started items are dropped. Already-running sh subprocesses are left to
      finish naturally. Completed items are recorded in result.log as usual,
      so a later run resumes from where this one stopped.

    2nd press / signal:
      Force-kill. SIGTERM is sent to each still-running sh subprocess (via the
      process group), causing them to exit non-zero. Those entries are written
      to result.log as FAIL rows; to retry them, remove their rows from
      result.log or use --fresh.

EXIT STATUS
  0   all jobs succeeded
  1   at least one job failed (or was cancelled)
  2   usage error
`

type Config struct {
	Parallelism     int
	File            string
	Template        string
	DryRun          bool
	NoTUI           bool
	Fresh           bool          // --fresh: ignore existing result.log and run everything
	SkipUniqueCheck bool          // --skip-unique-txt-rows: don't fail on duplicate input lines
	Retries         int           // --retries: additional attempts after an initial non-zero exit (0 disables)
	Timeout         time.Duration // --timeout: required; applied per attempt
	Wizard          bool          // --wizard: interactively prompt for values before running
}

func parseArgs(argv []string) (Config, error) {
	fs := flag.NewFlagSet("parallel-each", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, helpText) }

	var (
		jobs     = fs.Int("P", 4, "parallel jobs")
		file     = fs.String("F", "", "input file")
		dry      = fs.Bool("n", false, "dry run")
		noTUI    = fs.Bool("no-tui", false, "disable TUI")
		fresh    = fs.Bool("fresh", false, "ignore existing result.log")
		skipUniq = fs.Bool("skip-unique-txt-rows", false, "skip duplicate-row validation")
		retries  = fs.Int("retries", 5, "retries on non-zero exit")
		timeout  = fs.Duration("timeout", 0, "per-attempt timeout (required)")
		wizard   = fs.Bool("wizard", false, "interactive prompt for all values")
		help     = fs.Bool("h", false, "help")
		help2    = fs.Bool("help", false, "help")
	)

	if err := fs.Parse(argv); err != nil {
		return Config{}, err
	}
	if *help || *help2 {
		fmt.Fprint(os.Stdout, helpText)
		os.Exit(0)
	}

	rest := fs.Args()
	tmpl := strings.Join(rest, " ")

	// --wizard fills in missing fields interactively; defer most validation
	// until after the wizard runs.
	if !*wizard {
		if *file == "" {
			return Config{}, fmt.Errorf("-F <file> is required")
		}
		if len(rest) < 1 {
			return Config{}, fmt.Errorf("command template is required")
		}
		if !strings.Contains(tmpl, "{item}") {
			return Config{}, fmt.Errorf("command template must contain {item}")
		}
		if !*dry && *timeout <= 0 {
			return Config{}, fmt.Errorf("--timeout is required (e.g. --timeout 30s); use --wizard for interactive input")
		}
	}
	if *jobs < 0 {
		return Config{}, fmt.Errorf("-P must be >= 0")
	}
	if *retries < 0 {
		return Config{}, fmt.Errorf("--retries must be >= 0")
	}

	return Config{
		Parallelism:     *jobs,
		File:            *file,
		Template:        tmpl,
		DryRun:          *dry,
		NoTUI:           *noTUI,
		Fresh:           *fresh,
		SkipUniqueCheck: *skipUniq,
		Retries:         *retries,
		Timeout:         *timeout,
		Wizard:          *wizard,
	}, nil
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
		fmt.Fprint(os.Stderr, helpText)
		os.Exit(2)
	}

	if cfg.Wizard {
		cfg, err = runWizard(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wizard cancelled: %v\n", err)
			os.Exit(2)
		}
	}

	if _, statErr := os.Stat(cfg.File); statErr != nil {
		fmt.Fprintf(os.Stderr, "error: file not found: %s\n", cfg.File)
		os.Exit(2)
	}

	lines, err := readInput(cfg.File)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	if len(lines) == 0 {
		fmt.Fprintf(os.Stderr, "no items in %s\n", cfg.File)
		os.Exit(0)
	}

	if !cfg.SkipUniqueCheck {
		if dupes := findDuplicates(lines); len(dupes) > 0 {
			fmt.Fprintf(os.Stderr, "error: duplicate rows in %s (pass --skip-unique-txt-rows to ignore):\n", cfg.File)
			for _, d := range dupes {
				fmt.Fprintf(os.Stderr, "  %s\n", d)
			}
			os.Exit(2)
		}
	}

	if cfg.DryRun {
		runDryRun(cfg, lines)
		return
	}

	// Resume support: filter out lines already present in result.log.
	if !cfg.Fresh {
		processed, err := loadProcessedLines(filepath.Join(logDir, "result.log"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading previous result.log: %v\n", err)
			os.Exit(2)
		}
		if len(processed) > 0 {
			before := len(lines)
			lines = filterProcessed(lines, processed)
			skipped := before - len(lines)
			if skipped > 0 {
				fmt.Fprintf(os.Stderr, "resuming: skipped %d of %d items already in %s/result.log (use --fresh to rerun all)\n",
					skipped, before, logDir)
			}
			if len(lines) == 0 {
				fmt.Fprintf(os.Stderr, "nothing to do: all items already processed\n")
				os.Exit(0)
			}
		}
	}

	// runPlain and runTUI install their own two-stage signal handlers
	// (1st SIGINT = graceful stop, 2nd = force-kill).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	useTUI := !cfg.NoTUI && term.IsTerminal(int(os.Stdout.Fd()))

	var rc int
	if useTUI {
		rc = runTUI(ctx, cfg, lines)
	} else {
		rc = runPlain(ctx, cfg, lines)
	}
	os.Exit(rc)
}

// runWizard prompts interactively for required/optional values and returns
// the completed Config. Reads from stdin; suitable only for an attached TTY.
func runWizard(defaults Config) (Config, error) {
	in := bufio.NewReader(os.Stdin)

	ask := func(prompt, def string) (string, error) {
		if def != "" {
			fmt.Fprintf(os.Stderr, "%s [%s]: ", prompt, def)
		} else {
			fmt.Fprintf(os.Stderr, "%s: ", prompt)
		}
		line, err := in.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return def, nil
		}
		return line, nil
	}

	fmt.Fprintln(os.Stderr, "parallel-each wizard — press Enter to accept a default shown in [brackets].")

	// Input file (required).
	for {
		v, err := ask("Input file (-F)", defaults.File)
		if err != nil {
			return defaults, err
		}
		if v == "" {
			fmt.Fprintln(os.Stderr, "  (required)")
			continue
		}
		if _, err := os.Stat(v); err != nil {
			fmt.Fprintf(os.Stderr, "  file not found: %v\n", err)
			continue
		}
		defaults.File = v
		break
	}

	// Command template (required, must contain {item}).
	for {
		v, err := ask("Command template (must contain {item})", defaults.Template)
		if err != nil {
			return defaults, err
		}
		if v == "" {
			fmt.Fprintln(os.Stderr, "  (required)")
			continue
		}
		if !strings.Contains(v, "{item}") {
			fmt.Fprintln(os.Stderr, "  template must contain {item}")
			continue
		}
		defaults.Template = v
		break
	}

	// Parallelism.
	for {
		v, err := ask("Parallelism (-P)", strconv.Itoa(defaults.Parallelism))
		if err != nil {
			return defaults, err
		}
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 0 {
			fmt.Fprintln(os.Stderr, "  enter a non-negative integer")
			continue
		}
		defaults.Parallelism = n
		break
	}

	// Timeout (required, no default).
	for {
		def := ""
		if defaults.Timeout > 0 {
			def = defaults.Timeout.String()
		}
		v, err := ask("Per-attempt timeout (e.g. 30s, 5m)", def)
		if err != nil {
			return defaults, err
		}
		if v == "" {
			fmt.Fprintln(os.Stderr, "  (required)")
			continue
		}
		d, perr := time.ParseDuration(v)
		if perr != nil || d <= 0 {
			fmt.Fprintf(os.Stderr, "  invalid duration: %v\n", perr)
			continue
		}
		defaults.Timeout = d
		break
	}

	// Retries.
	for {
		v, err := ask("Retries on non-zero exit", strconv.Itoa(defaults.Retries))
		if err != nil {
			return defaults, err
		}
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 0 {
			fmt.Fprintln(os.Stderr, "  enter a non-negative integer")
			continue
		}
		defaults.Retries = n
		break
	}

	fmt.Fprintf(os.Stderr, "\nabout to run:\n  %s\n  input=%s  P=%d  timeout=%s  retries=%d\n\n",
		defaults.Template, defaults.File, defaults.Parallelism, defaults.Timeout, defaults.Retries)

	return defaults, nil
}
