package main

import "io"

type commandSpec struct {
	Name        string
	Run         func([]string, io.Writer, io.Writer) int
	Formats     []string
	SymbolsOnly bool
	Query       string
	Summary     string
}

// Dispatch and discovery share this list so an advertised command always has an entry point.
func commandSpecs() []commandSpec {
	return []commandSpec{
		{Name: "report", Run: runReportCommand, Formats: []string{"text", "json", "xcode"}, Summary: "Find structurally duplicate units"},
		{Name: "gate", Run: runGate, Formats: []string{"text", "json", "xcode"}, Summary: "Check new duplicates against a base"},
		{Name: "near", Run: runNear, Formats: []string{"text", "json"}, Query: "file:unit-or-line", Summary: "Find units resembling a selected unit"},
		{Name: "explain", Run: runExplain, Formats: []string{"text", "json"}, Query: "file:unit-or-line", Summary: "Explain normalization and matching"},
		{Name: "index", Run: runIndex, Formats: []string{"text", "json"}, Query: "optional metadata filters", Summary: "Find existing units before writing"},
		{Name: "callers", Run: runCallers, Formats: []string{"text", "json"}, Query: "name-or-signature", Summary: "Find approximate call sites"},
		{Name: "def", Run: runDef, Formats: []string{"text", "json"}, SymbolsOnly: true, Query: "name-or-Type.member-or-signature", Summary: "Read definitions and type declarations"},
		{Name: "refs", Run: runRefs, Formats: []string{"text", "json"}, SymbolsOnly: true, Query: "name", Summary: "Find approximate identifier references"},
		{Name: "context", Run: runContext, Formats: []string{"text"}, SymbolsOnly: true, Query: "name-or-Type.member-or-signature", Summary: "Read definitions, references and callers together"},
		{Name: "evidence", Run: runEvidence, Formats: []string{"text"}, Query: "term", Summary: "Find lexical evidence in docs, tests and source"},
		{Name: "capabilities", Run: runCapabilities, Formats: []string{"text", "json"}, Summary: "Discover compiled commands and language support"},
		{Name: "doctor", Run: runDoctor, Formats: []string{"text", "json"}, Summary: "Check local analysis prerequisites without parsing source"},
		{Name: "agent-guide", Run: runAgentGuide, Formats: []string{"text"}, Summary: "Print a bounded guide for analysis agents"},
	}
}

func runNear(args []string, stdout, stderr io.Writer) int {
	return runPoint("near", args, stdout, stderr)
}

func runExplain(args []string, stdout, stderr io.Writer) int {
	return runPoint("explain", args, stdout, stderr)
}
