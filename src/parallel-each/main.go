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
  --attempt-timeout <d>  Per-attempt timeout (REQUIRED; e.g. 30s, 5m, 1h,
                         1h30m). Each attempt running longer than this is
                         SIGTERMed and counted as a failure (eligible for
                         --retries). Not required in --wizard mode or with -n.
                         Applies independently to each retry, so the worst-
                         case time for one input is attempt-timeout * (retries+1).
  --total-timeout <d>    Overall wall-clock limit for the whole run (optional;
                         e.g. 1h, 2h30m). When exceeded, all in-flight jobs
                         are force-killed (SIGTERM) and queued jobs are
                         dropped. 0 or omitted = no limit.
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
    a          Append a new item to the TAIL of the queue (see LIVE ADD below).
    A          Prepend a new item to the HEAD of the queue — it will be the
               next item dispatched after the currently running jobs. Multi-
               line pastes are prepended as a block, preserving the paste
               order at the queue head.
    P, space   Pause / resume dispatching. Unrelated to shutdown: paused
               means no new jobs are handed to workers, but running jobs
               continue and the program stays alive indefinitely. Use for
               "hold on a sec" situations (CPU relief, manual inspection,
               etc.). Cannot be used once shutdown has begun.
    p          Change parallelism. Opens a prompt — type the new target
               worker count, press Enter to preview, Enter again to confirm
               (or Esc to cancel). Increases spawn a new worker immediately.
               Decreases are graceful: excess workers retire after their
               current job completes; running jobs are NOT interrupted.
               Minimum 1.
    r          Open the full recent view — a scrollable list of every
               completed job. Navigate with ↑/↓ or j/k, pgup/pgdown, g/G.
               Press Enter on a row to open its per-job log in $EDITOR
               (falls back to $VISUAL, then vi). Press 'd' on a row to
               forget it: its row is removed from result.log AND the dedup
               set, so the same input can be added again via 'a' / 'A'.
               (The per-job log file under parallel-each-log/ is kept.)
               '/' opens a live filter (case-insensitive substring);
               Enter commits, Esc clears. esc/r/q to close.
    l          Open the queue view — a scrollable list of pending items
               (not yet started). Auto-refreshes as workers pick them up.
               Same navigation as recent, plus '/' for live filter.
               esc/l/q to close.
    e          (focus mode) Open the focused slot's per-job log in $EDITOR.
    o          Open the "other actions" menu. Currently contains:
                 1) Export wrapper script — writes a zsh wrapper into
                    ./bin/ (the bin/ directory of your current working
                    directory) that bakes in the current -P /
                    --attempt-timeout / --total-timeout / --retries (when
                    non-default) / -F (absolute path) / template, and
                    forwards extra args via "$@". Next time, run it with
                    ./bin/<name> from that project root.
    q, ctrl-c  Stop (see SHUTDOWN below).

LIVE ADD (TUI only)
  Press 'a' from the overview to open an inline prompt and type a new input
  line. Enter pushes it into the live queue; esc / ctrl-c cancels; ctrl-u
  clears the buffer; backspace deletes.

  Paste multi-line text directly into the prompt: each line is auto-submitted
  as a separate queue item. A trailing partial line (without newline) stays
  in the buffer for you to finish or clear. A single batch flash message
  reports counts of added / duplicate / failed.

  Added items are checked against the resume set and the current queue — a
  duplicate is rejected with an inline error flash. Each accepted item gets
  the next sequential job number and its own per-job log, and a row in
  result.log on completion.

  Accepted items are also appended to the -F input file (atomic O_APPEND
  write, one line per entry). This keeps the on-disk list a faithful record
  of everything the run processed.

  In live mode the TUI does not auto-exit when all items finish; press q to
  exit, or press a to add more. Plain mode does not support live add.

SHUTDOWN
  TUI: every shutdown step requires the user to TYPE the literal word
  "quit" (four keystrokes, q-u-i-t in sequence). Any other key aborts the
  in-progress quit and resets the buffer. Single q / Ctrl-C / Esc do NOT
  start a shutdown — this is deliberate, to prevent accidental quits.

    1st 'quit':       Graceful stop (final). Queued items are dropped,
                      running jobs finish, the program exits.
    2nd 'quit':       Arm force-kill confirmation. A 3-second window opens;
                      type 'quit' once more within it to actually SIGTERM
                      the running sh subprocesses. If the window expires
                      the attempt is cancelled (stopping state continues).
                      Forced jobs exit non-zero in result.log as FAIL.
    3rd 'quit' (in window): force-kill.

  While typing, a banner shows progress: "quit: qu__ — will stop gracefully".

  If you only want to hold the queue (not quit), use P or space to pause.

  Plain mode: two-stage (no quit-word protection, since there is no TTY
  typing guard).

    1st SIGINT / SIGTERM: Graceful stop. Queued items dropped, running
                           jobs finish, program exits.
    2nd SIGINT / SIGTERM: Force-kill.

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
	AttemptTimeout  time.Duration // --attempt-timeout: required; applied per attempt
	TotalTimeout    time.Duration // --total-timeout: optional; force-kill everything after this much wall-clock
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
		retries        = fs.Int("retries", 5, "retries on non-zero exit")
		attemptTimeout = fs.Duration("attempt-timeout", 0, "per-attempt timeout (required; e.g. 30s, 5m, 1h)")
		totalTimeout   = fs.Duration("total-timeout", 0, "overall wall-clock limit; 0 = no limit (e.g. 1h, 2h30m)")
		wizard         = fs.Bool("wizard", false, "interactive prompt for all values")
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
		if !*dry && *attemptTimeout <= 0 {
			return Config{}, fmt.Errorf("--attempt-timeout is required (e.g. --attempt-timeout 30s); use --wizard for interactive input")
		}
	}
	if *jobs < 0 {
		return Config{}, fmt.Errorf("-P must be >= 0")
	}
	if *retries < 0 {
		return Config{}, fmt.Errorf("--retries must be >= 0")
	}
	if *totalTimeout < 0 {
		return Config{}, fmt.Errorf("--total-timeout must be >= 0")
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
		AttemptTimeout:  *attemptTimeout,
		TotalTimeout:    *totalTimeout,
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

	useTUI := !cfg.NoTUI && term.IsTerminal(int(os.Stdout.Fd()))

	// Resume support: filter out lines already present in result.log. The
	// full processed map (line -> status) is also retained so it can seed
	// the dedup with per-entry wording for interactive Enqueue (see
	// runTUI / runPlain).
	skipped := 0
	var processedMap map[string]string
	if !cfg.Fresh {
		processed, err := loadProcessedLines(filepath.Join(logDir, "result.log"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading previous result.log: %v\n", err)
			os.Exit(2)
		}
		if len(processed) > 0 {
			processedMap = processed
			before := len(lines)
			lines = filterProcessed(lines, processed)
			skipped = before - len(lines)
			if len(lines) == 0 {
				if !useTUI {
					fmt.Fprintf(os.Stderr,
						"nothing to do: all %d items are already in %s/result.log (use --fresh to rerun all)\n",
						before, logDir)
					os.Exit(0)
				}
				fmt.Fprintf(os.Stderr,
					"all %d items already processed — launching TUI: press 'a' to append more, 'q' to exit.\n",
					before)
			}
		}
	}

	// runPlain and runTUI install their own two-stage signal handlers
	// (1st SIGINT = graceful stop, 2nd = force-kill). --total-timeout applies
	// at this outer layer and triggers force-kill when the deadline fires.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.TotalTimeout > 0 {
		var tcancel context.CancelFunc
		ctx, tcancel = context.WithTimeout(ctx, cfg.TotalTimeout)
		defer tcancel()
	}

	var rc int
	if useTUI {
		rc = runTUI(ctx, cfg, lines, skipped, processedMap)
	} else {
		rc = runPlain(ctx, cfg, lines, skipped, processedMap)
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

	// Per-attempt timeout (required, no default).
	for {
		def := ""
		if defaults.AttemptTimeout > 0 {
			def = defaults.AttemptTimeout.String()
		}
		v, err := ask("Per-attempt timeout (e.g. 30s, 5m, 1h)", def)
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
		defaults.AttemptTimeout = d
		break
	}

	// Total timeout (optional; empty disables).
	for {
		def := ""
		if defaults.TotalTimeout > 0 {
			def = defaults.TotalTimeout.String()
		}
		v, err := ask("Total timeout for entire run, empty = no limit (e.g. 1h, 2h30m)", def)
		if err != nil {
			return defaults, err
		}
		if v == "" {
			defaults.TotalTimeout = 0
			break
		}
		d, perr := time.ParseDuration(v)
		if perr != nil || d <= 0 {
			fmt.Fprintf(os.Stderr, "  invalid duration: %v\n", perr)
			continue
		}
		defaults.TotalTimeout = d
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

	totalStr := "none"
	if defaults.TotalTimeout > 0 {
		totalStr = defaults.TotalTimeout.String()
	}
	fmt.Fprintf(os.Stderr,
		"\nabout to run:\n  %s\n  input=%s  P=%d  attempt-timeout=%s  total-timeout=%s  retries=%d\n\n",
		defaults.Template, defaults.File, defaults.Parallelism,
		defaults.AttemptTimeout, totalStr, defaults.Retries)

	return defaults, nil
}
