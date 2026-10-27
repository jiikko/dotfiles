package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDoctorReadOnlyWithBrokenSourcesAndGitAllowlist(t *testing.T) {
	root := repoWith(t, map[string]string{"broken.go": "package broken\nfunc {", "broken.swift": "func {", ".dupscan.json": `{"version":1}`})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "baseline")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	wrapper := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCTOR_GIT_LOG"
locks=no
lazy=no
while [ "$#" -gt 0 ]; do
 case "$1" in
  -c) shift 2 ;;
  --no-optional-locks) locks=yes; shift ;;
  --no-lazy-fetch) lazy=yes; shift ;;
  -C) target="$2"; shift 2; break ;;
  *) exit 90 ;;
 esac
done
if [ "$locks/$lazy" != "yes/yes" ]; then exit 91; fi
case "$1" in rev-parse|ls-files|merge-base) ;; *) exit 92;; esac
exec "$DOCTOR_REAL_GIT" --no-optional-locks --no-lazy-fetch -c core.fsmonitor=false -C "$target" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCTOR_GIT_LOG", log)
	t.Setenv("DOCTOR_REAL_GIT", realGit)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	snapshot := func() map[string]string {
		tree := map[string]string{}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			var content []byte
			if info.Mode().IsRegular() {
				content, err = os.ReadFile(path)
				if err != nil {
					return err
				}
			}
			tree[path] = fmt.Sprintf("%s %d %s %s", info.Mode(), info.Size(), info.ModTime(), content)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return tree
	}
	before := snapshot()
	for _, format := range []string{"text", "json"} {
		var out, errs bytes.Buffer
		if rc := runDoctor([]string{"-repo", root, "-base", "HEAD", "-format", format}, &out, &errs); rc != 0 {
			t.Fatalf("rc=%d output=%s stderr=%s", rc, &out, &errs)
		}
		if !strings.Contains(out.String(), "not_checked") || !strings.Contains(out.String(), "baseline equals HEAD") || strings.Contains(out.String(), "syntax error") {
			t.Fatalf("misleading report: %s", &out)
		}
		if format == "json" {
			var report doctorReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Checks) != 8 {
				t.Fatalf("report=%s err=%v", &out, err)
			}
		}
	}
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("doctor modified repository files, metadata, or git state")
	}
	calls, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(calls), "merge-base") {
		t.Fatalf("git calls=%s err=%v", calls, err)
	}
}

func TestDoctorConfigurationAndReadiness(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		base  string
		rc    int
		want  string
	}{
		{"empty unborn", nil, "", 0, "no supported source files"},
		{"optional config", map[string]string{"a.go": "broken"}, "", 0, "defaults apply"},
		{"invalid config", map[string]string{".dupscan.json": "{"}, "", 2, "config: error"},
		{"no upstream", map[string]string{"a.go": "package a"}, "", 0, "gate baseline unavailable"},
		{"missing base", map[string]string{"a.go": "package a"}, "missing-ref", 0, "no fetch attempted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repoWith(t, tc.files)
			if tc.name == "no upstream" || tc.name == "missing base" {
				gitRun(t, root, "add", ".")
				gitRun(t, root, "commit", "-qm", "base")
			}
			var out, errs bytes.Buffer
			rc := runDoctor([]string{"-repo", root, "-base", tc.base}, &out, &errs)
			if rc != tc.rc || !strings.Contains(out.String(), tc.want) {
				t.Fatalf("rc=%d output=%s stderr=%s", rc, &out, &errs)
			}
		})
	}
}

func TestDoctorRejectsArgumentsAndNonRepository(t *testing.T) {
	for _, args := range [][]string{{"-format", "json-v2"}, {"unexpected"}, {"-unknown"}, {"-repo", t.TempDir()}} {
		var out, errs bytes.Buffer
		if rc := runDoctor(args, &out, &errs); rc != 2 {
			t.Fatalf("args=%v rc=%d output=%s", args, rc, &out)
		}
	}
}

func TestDoctorScopesForeignWorkingDirectoryAndWorktree(t *testing.T) {
	root := committedRepo(t)
	writeFile(t, root, "other/a.go", "package other")
	worktree := filepath.Join(t.TempDir(), "linked")
	gitRun(t, root, "worktree", "add", "--detach", worktree, "HEAD")
	t.Chdir(t.TempDir())
	for _, target := range []string{filepath.Join(root, "other"), worktree} {
		var out, errs bytes.Buffer
		if rc := runDoctor([]string{"-repo", target, "-base", "HEAD"}, &out, &errs); rc != 0 || !strings.Contains(out.String(), "1 supported source files") {
			t.Fatalf("target=%s rc=%d output=%s stderr=%s", target, rc, &out, &errs)
		}
	}
}

func TestDoctorShallowBaselineStaysLocal(t *testing.T) {
	origin := committedRepo(t)
	writeFile(t, origin, "Sources/B.swift", helperB)
	gitRun(t, origin, "add", ".")
	gitRun(t, origin, "commit", "-qm", "tip")
	clone := filepath.Join(t.TempDir(), "shallow")
	gitRun(t, origin, "clone", "-q", "--depth=1", "file://"+origin, clone)
	var out, errs bytes.Buffer
	if rc := runDoctor([]string{"-repo", clone, "-base", "HEAD~1"}, &out, &errs); rc != 0 || !strings.Contains(out.String(), "base: warning") {
		t.Fatalf("rc=%d output=%s stderr=%s", rc, &out, &errs)
	}
}
