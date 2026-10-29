package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"dupscan/internal/jsonout"
	"dupscan/internal/langs"
	"dupscan/internal/report"
)

const capabilitiesMaxBytes = 65536

type capabilityCommand struct {
	Name        string   `json:"name"`
	Formats     []string `json:"formats"`
	Languages   []string `json:"languages"`
	Query       string   `json:"query,omitempty"`
	Summary     string   `json:"summary"`
	FlagsStatus string   `json:"flags_status"`
	FlagsHelp   string   `json:"flags_help"`
}

type capabilityLanguage struct {
	Name        string   `json:"name"`
	Extensions  []string `json:"extensions"`
	Symbols     bool     `json:"symbols"`
	Limitations []string `json:"limitations"`
}

type capabilityDocument struct {
	SchemaVersion int                  `json:"schema_version"`
	Tool          string               `json:"tool"`
	Version       string               `json:"version"`
	Commands      []capabilityCommand  `json:"commands"`
	Languages     []capabilityLanguage `json:"languages"`
	Limitations   []string             `json:"limitations"`
}

func buildCapabilities() capabilityDocument {
	d := capabilityDocument{SchemaVersion: 1, Tool: "dupscan", Version: "unknown", Commands: []capabilityCommand{}, Languages: []capabilityLanguage{}, Limitations: []string{
		"Flags are the actual command help text, not a structured flag schema.",
		"Queries use syntax and lexical matching without type resolution; same-named symbols may match.",
		"External SDK definitions and project/target dependency resolution are not provided.",
		"This capability schema is not the planned json-v2 analysis envelope.",
	}}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		d.Version = info.Main.Version
	}
	for _, adapter := range langs.All {
		notes := []string{}
		if !adapter.Symbols {
			notes = append(notes, "def, refs and context do not read this language")
		}
		if adapter.Name == "go" {
			notes = append(notes, "evidence collects function docs only and does not collect type-construction calls")
		}
		d.Languages = append(d.Languages, capabilityLanguage{Name: adapter.Name, Extensions: adapter.Extensions, Symbols: adapter.Symbols, Limitations: notes})
	}
	for _, spec := range commandSpecs() {
		names := []string{}
		for _, adapter := range langs.All {
			if !spec.SymbolsOnly || adapter.Symbols {
				names = append(names, adapter.Name)
			}
		}
		if spec.Name == "capabilities" {
			names = []string{}
		}
		var help bytes.Buffer
		spec.Run([]string{"-h"}, io.Discard, &help)
		flagsHelp := help.String()
		if start := strings.LastIndex(flagsHelp, "\nflags:\n"); start >= 0 {
			flagsHelp = flagsHelp[start+len("\nflags:\n"):]
		}
		d.Commands = append(d.Commands, capabilityCommand{Name: spec.Name, Formats: spec.Formats, Languages: names, Query: spec.Query, Summary: spec.Summary, FlagsStatus: "help_text", FlagsHelp: flagsHelp})
	}
	return d
}

func runCapabilities(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dupscan capabilities", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "output format: text / json (capability schema version 1)")
	outputs := fs.String("outputs", "", "instead of the capability table, describe the output of this command: the json fields per type and the lines only -v prints ("+strings.Join(report.OutputCommands(), " / ")+")")
	if _, rc, ok := parseFlags(fs, args); !ok {
		return rc
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "dupscan capabilities: no positional arguments accepted")
		return 2
	}
	outputsSet := false
	fs.Visit(func(f *flag.Flag) { outputsSet = outputsSet || f.Name == "outputs" })
	var out bytes.Buffer
	if outputsSet {
		o, ok := report.OutputsFor(*outputs)
		if !ok {
			fmt.Fprintf(stderr, "dupscan capabilities: -outputs knows %s, not %q\n", strings.Join(report.OutputCommands(), " / "), *outputs)
			return 2
		}
		switch *format {
		case "json":
			if err := jsonout.Write(&out, o); err != nil {
				fmt.Fprintln(stderr, "dupscan capabilities:", err)
				return 2
			}
		case "text":
			writeOutputsText(&out, o)
		default:
			fmt.Fprintf(stderr, "dupscan capabilities: unknown format %q (text / json)\n", *format)
			return 2
		}
		return writeCapabilities(out, stdout, stderr)
	}
	d := buildCapabilities()
	switch *format {
	case "json":
		if err := jsonout.Write(&out, d); err != nil {
			fmt.Fprintln(stderr, "dupscan capabilities:", err)
			return 2
		}
	case "text":
		fmt.Fprintf(&out, "%s capabilities (schema %d; version %s)\n", d.Tool, d.SchemaVersion, d.Version)
		for _, c := range d.Commands {
			fmt.Fprintf(&out, "  %s: %s; formats=%s; languages=%s", c.Name, c.Summary, strings.Join(c.Formats, ","), strings.Join(c.Languages, ","))
			if c.Query != "" {
				fmt.Fprintf(&out, "; query=%s", c.Query)
			}
			fmt.Fprintf(&out, "; flags=%s\n%s", c.FlagsStatus, c.FlagsHelp)
		}
		for _, l := range d.Languages {
			fmt.Fprintf(&out, "  %s: extensions=%s; symbols=%t\n", l.Name, strings.Join(l.Extensions, ","), l.Symbols)
			for _, note := range l.Limitations {
				fmt.Fprintln(&out, "    "+note)
			}
		}
		for _, note := range d.Limitations {
			fmt.Fprintln(&out, "  "+note)
		}
	default:
		fmt.Fprintf(stderr, "dupscan capabilities: unknown format %q (text / json)\n", *format)
		return 2
	}
	return writeCapabilities(out, stdout, stderr)
}

func writeCapabilities(out bytes.Buffer, stdout, stderr io.Writer) int {
	if out.Len() > capabilitiesMaxBytes {
		fmt.Fprintln(stderr, "dupscan capabilities: output exceeds 65536 bytes")
		return 2
	}
	if _, err := out.WriteTo(stdout); err != nil {
		fmt.Fprintln(stderr, "dupscan capabilities:", err)
		return 2
	}
	return 0
}

// writeOutputsText は -outputs の text。型ごとに欄を 1 回だけ並べる (同じ型が何か所に出ても繰り返さない)。
func writeOutputsText(w io.Writer, o report.Outputs) {
	fmt.Fprintf(w, "dupscan %s の出力 (-format json の根は %s。欄は型ごとに 1 回。? は空なら欄ごと出ない)\n", o.Command, o.Root)
	for _, t := range o.Types {
		fmt.Fprintf(w, "%s:\n", t.Name)
		for _, f := range t.Fields {
			omit := ""
			if f.Omitted {
				omit = "?"
			}
			fmt.Fprintf(w, "  %s%s %s: %s\n", f.Name, omit, f.Type, f.Desc)
		}
	}
	fmt.Fprintf(w, "-v でだけ出る行 (見出しで始まる):\n")
	for _, v := range o.Verbose {
		json := ""
		if v.JSON != "" {
			json = " [json: " + v.JSON + "]"
		}
		fmt.Fprintf(w, "  %q: %s%s\n", v.Prefix, v.Desc, json)
	}
}
