package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"dupscan/internal/lang"
	"dupscan/internal/langs"
)

func TestCapabilitiesRegistryAndLanguageSupport(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := run([]string{"capabilities", "-format", "json"}, &out, &errb); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, &errb)
	}
	var got capabilityDocument
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Tool != "dupscan" || got.Version == "" || len(got.Commands) != len(commandSpecs()) {
		t.Fatalf("invalid metadata: %+v", got)
	}
	seen := map[string]bool{}
	for i, spec := range commandSpecs() {
		cmd := got.Commands[i]
		if seen[cmd.Name] || cmd.Name != spec.Name || spec.Run == nil || !slices.Equal(cmd.Formats, spec.Formats) || cmd.FlagsStatus != "help_text" || cmd.FlagsHelp == "" {
			t.Fatalf("registry mismatch: %+v / %+v", spec, cmd)
		}
		var help bytes.Buffer
		spec.Run([]string{"-h"}, &bytes.Buffer{}, &help)
		if !strings.HasSuffix(help.String(), cmd.FlagsHelp) || strings.Contains(cmd.FlagsHelp, "usage: dupscan [report]") {
			t.Errorf("%s flags differ from actual command help: %q", cmd.Name, cmd.FlagsHelp)
		}
		seen[cmd.Name] = true
		if spec.SymbolsOnly && (!slices.Contains(cmd.Languages, "swift") || slices.Contains(cmd.Languages, "go")) {
			t.Errorf("symbol-only command advertised wrong languages: %+v", cmd)
		}
		if cmd.Name == "evidence" && !slices.Contains(cmd.Languages, "go") {
			t.Errorf("partial Go evidence support missing: %+v", cmd)
		}
	}
	if out.Len() > capabilitiesMaxBytes {
		t.Fatalf("output too large: %d", out.Len())
	}
}

func TestCapabilitiesDoesNotInspectRepositoryOrCreateParsers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".dupscan.json"), []byte("invalid config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.swift"), []byte("invalid source"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	t.Setenv("PATH", t.TempDir())
	saved := langs.All
	t.Cleanup(func() { langs.All = saved })
	langs.All = append([]lang.Adapter(nil), langs.All...)
	for i := range langs.All {
		langs.All[i].NewParser = func(lang.Options) (lang.Parser, error) {
			t.Fatal("capabilities created a parser")
			return nil, nil
		}
	}
	for _, format := range []string{"text", "json"} {
		var out, errb bytes.Buffer
		if rc := run([]string{"capabilities", "-format", format}, &out, &errb); rc != 0 || out.Len() == 0 || errb.Len() != 0 {
			t.Errorf("%s: rc=%d stdout=%s stderr=%s", format, rc, &out, &errb)
		}
	}
}

func TestCapabilitiesRejectsInvalidArgumentsWithoutOutput(t *testing.T) {
	for _, args := range [][]string{{"-format", "json-v2"}, {"-format", "xml"}, {"."}, {"-unknown"}} {
		var out, errb bytes.Buffer
		if rc := runCapabilities(args, &out, &errb); rc != 2 || out.Len() != 0 || errb.Len() == 0 {
			t.Errorf("args=%q rc=%d stdout=%s stderr=%s", args, rc, &out, &errb)
		}
	}
}

func TestCapabilitiesRejectsOversizeBeforeWriting(t *testing.T) {
	saved := langs.All
	t.Cleanup(func() { langs.All = saved })
	langs.All = append([]lang.Adapter(nil), langs.All...)
	langs.All[0].Extensions = []string{strings.Repeat("x", capabilitiesMaxBytes)}
	for _, format := range []string{"text", "json"} {
		var out, errb bytes.Buffer
		if rc := runCapabilities([]string{"-format", format}, &out, &errb); rc != 2 || out.Len() != 0 || !strings.Contains(errb.String(), "65536") {
			t.Errorf("%s: rc=%d stdout bytes=%d stderr=%s", format, rc, out.Len(), &errb)
		}
	}
}

func TestRegisteredCommandsDispatchToEntryPoints(t *testing.T) {
	for _, spec := range commandSpecs() {
		var directOut, directErr, dispatchedOut, dispatchedErr bytes.Buffer
		wantRC := spec.Run([]string{"-h"}, &directOut, &directErr)
		gotRC := run([]string{spec.Name, "-h"}, &dispatchedOut, &dispatchedErr)
		if gotRC != wantRC || directOut.String() != dispatchedOut.String() || directErr.String() != dispatchedErr.String() {
			t.Errorf("%s dispatched to a different entry point: rc=%d/%d stdout=%q/%q stderr=%q/%q", spec.Name, gotRC, wantRC, dispatchedOut.String(), directOut.String(), dispatchedErr.String(), directErr.String())
		}
	}
}

func TestCapabilitiesIncludesActualCommandFlags(t *testing.T) {
	want := map[string][]string{
		"report":       {"-min-nodes int", "-min-closure-nodes int"},
		"gate":         {"-base string", "-fail-new-exclusions"},
		"context":      {"-sections string", "-max-lines int"},
		"capabilities": {"-format string"},
		"doctor":       {"-repo string", "-base string"},
		"agent-guide":  {"-target string", "-print"},
	}
	for _, cmd := range buildCapabilities().Commands {
		for _, flag := range want[cmd.Name] {
			if !strings.Contains(cmd.FlagsHelp, "  "+flag+"\n") {
				t.Errorf("%s missing actual flag %q: %s", cmd.Name, flag, cmd.FlagsHelp)
			}
		}
		delete(want, cmd.Name)
	}
	if len(want) != 0 {
		t.Fatalf("commands missing from capabilities: %v", want)
	}
}
