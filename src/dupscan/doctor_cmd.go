package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"dupscan/internal/jsonout"
	"dupscan/internal/langs"
	"dupscan/internal/repo"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type doctorReport struct {
	Checks []doctorCheck `json:"checks"`
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "text|json")
	directory := fs.String("repo", ".", "repository directory or subdirectory")
	base := fs.String("base", "", "gate baseline ref (default: upstream)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || (*format != "text" && *format != "json") {
		fmt.Fprintln(stderr, "dupscan doctor: use -repo directory and -format text|json")
		return 2
	}
	report := doctorReport{Checks: []doctorCheck{}}
	rc := 0
	add := func(name, status, reason string) {
		reason = strings.ToValidUTF8(reason, "�")
		if len(reason) > 4096 {
			reason = reason[:4093]
			for !utf8.ValidString(reason) {
				reason = reason[:len(reason)-1]
			}
			reason += "…"
		}
		report.Checks = append(report.Checks, doctorCheck{name, status, reason})
		if status == "error" {
			rc = 2
		}
	}
	if executable, err := os.Executable(); err != nil {
		add("executable", "error", err.Error())
	} else {
		add("executable", "ok", executable)
	}
	if git, err := exec.LookPath("git"); err != nil {
		add("git", "error", err.Error())
	} else {
		add("git", "ok", git)
	}
	var adapters []string
	for _, adapter := range langs.All {
		adapters = append(adapters, adapter.Name+" ("+strings.Join(adapter.Extensions, ", ")+")")
	}
	add("adapters", "info", strings.Join(adapters, "; ")+"; registered parsers, not instantiated")
	sc, err := repo.Resolve([]string{*directory})
	if err != nil {
		add("repository", "error", err.Error())
		for _, name := range []string{"sources", "config", "base"} {
			add(name, "not_checked", "repository could not be resolved")
		}
	} else {
		add("repository", "ok", sc.Root+"; scope: "+strings.Join(sc.Prefixes, ", "))
		files, err := repo.List(sc, langs.Extensions())
		if err != nil {
			add("sources", "error", err.Error())
		} else if len(files) == 0 {
			add("sources", "warning", "no supported source files in scope")
		} else {
			add("sources", "ok", fmt.Sprintf("%d supported source files; syntax and readability not checked", len(files)))
		}
		cfg, err := loadConfig(repo.Worktree{Scope: sc, Exts: langs.Extensions()}, false)
		if err != nil {
			add("config", "error", err.Error())
		} else if cfg == nil {
			add("config", "info", "optional .dupscan.json absent; defaults apply")
		} else {
			add("config", "ok", "root .dupscan.json parsed; inline exclusions and staleness not checked")
		}
		head, err := repo.CommitHash(sc.Root, "HEAD")
		if err != nil {
			add("base", "warning", "gate baseline unavailable: "+err.Error())
		} else if baseline, err := resolveBase(sc.Root, *base, head); err != nil {
			add("base", "warning", "gate baseline unavailable; no fetch attempted: "+err.Error())
		} else if baseline == head {
			add("base", "info", "gate baseline equals HEAD; only working-tree changes would be compared; gate not run")
		} else {
			add("base", "ok", "local gate baseline resolved to "+baseline+"; gate not run")
		}
	}
	add("analysis", "not_checked", "source parsing, build, duplicate detection, and gate were not run")
	var output bytes.Buffer
	if *format == "json" {
		if err := jsonout.Write(&output, report); err != nil {
			fmt.Fprintln(stderr, "dupscan doctor:", err)
			return 2
		}
	} else {
		for _, check := range report.Checks {
			fmt.Fprintf(&output, "%s: %s — %s\n", check.Name, check.Status, strings.Join(strings.Fields(check.Reason), " "))
		}
	}
	if output.Len() > 65536 {
		fmt.Fprintln(stderr, "dupscan doctor: diagnostic output exceeds 65536 bytes")
		return 2
	}
	if _, err := stdout.Write(output.Bytes()); err != nil {
		fmt.Fprintln(stderr, "dupscan doctor:", err)
		return 2
	}
	return rc
}
