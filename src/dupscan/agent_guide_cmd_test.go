package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentGuideOnlyAdvertisesExecutableRegisteredCommands(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Sources/A.swift": "struct Location {}\n/// A bucket location\nfunc makeLocation() -> Location { Location() }\nfunc use() { _ = makeLocation() }\n",
		"pkg/a.go":        "package a\ntype Location string\n// makeLocation joins a bucket.\nfunc makeLocation() Location { return Location(\"bucket\") }\nfunc use() { _ = makeLocation() }\n",
	})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "guide baseline")
	for _, target := range []string{"claude", "codex"} {
		for _, language := range []string{"all", "swift", "go"} {
			t.Run(target+"/"+language, func(t *testing.T) {
				var out, errs bytes.Buffer
				if rc := runAgentGuide([]string{"-target", target, "-lang", language, "-print"}, &out, &errs); rc != 0 || errs.Len() != 0 {
					t.Fatalf("rc=%d stderr=%s", rc, &errs)
				}
				if out.Len() > 4096 {
					t.Fatal("guide exceeded bound")
				}
				actions := 0
				for _, line := range strings.Split(out.String(), "\n") {
					raw, ok := strings.CutPrefix(line, "argv: ")
					if !ok {
						continue
					}
					var argv []string
					if err := json.Unmarshal([]byte(raw), &argv); err != nil {
						t.Fatal(err)
					}
					if len(argv) < 2 || argv[0] != "dupscan" {
						t.Fatalf("invalid argv %q", argv)
					}
					if language == "go" && (argv[1] == "context" || argv[1] == "def" || argv[1] == "refs") {
						t.Fatalf("unsupported Go command %q", argv)
					}
					for i, arg := range argv {
						switch arg {
						case "TYPE":
							argv[i] = "Location"
						case "TERM":
							argv[i] = "bucket"
						case "SYMBOL":
							argv[i] = "makeLocation"
						}
					}
					if argv[1] == "gate" {
						argv = append(argv, "-base", "HEAD")
					}
					argv = append(argv, root)
					rc, result, errout := runReport(t, argv[1:]...)
					if rc != 0 || errout != "" && (argv[1] != "context" || !strings.Contains(errout, "go のファイル 1 個は def / refs の対象外")) {
						t.Fatalf("guide command fails rc=%d stderr=%q argv=%q", rc, errout, argv)
					}
					if argv[1] != "gate" && !strings.Contains(result, "makeLocation") {
						t.Fatalf("guide did not reach known helper: %q", result)
					}
					actions++
				}
				if actions != 4 {
					t.Fatalf("got %d guide actions: %s", actions, &out)
				}
				if target == "codex" && !strings.Contains(out.String(), "Claude固有hookがCodexで動くとは仮定しない") {
					t.Fatal("unsupported integration claim")
				}
			})
		}
	}
}

func TestAgentGuideRejectsUnsupportedModesWithoutPartialOutput(t *testing.T) {
	for _, args := range [][]string{nil, {"-target", "other", "-print"}, {"-target", "codex"}, {"-target", "claude", "-lang", "javascript", "-print"}, {"-target", "codex", "-print", "extra"}, {"-target", "codex", "-print=false"}, {"-target", "codex", "-print", "-format", "json-v2"}} {
		var out, errs bytes.Buffer
		if rc := runAgentGuide(args, &out, &errs); rc != 2 || out.Len() != 0 || errs.Len() == 0 {
			t.Fatalf("args=%q rc=%d out=%q stderr=%q", args, rc, &out, &errs)
		}
	}
}
