package main

import (
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name    string
		argv    []string
		wantErr bool
		check   func(*testing.T, Config)
	}{
		{
			name: "minimal",
			argv: []string{"-F", "in.txt", "--timeout", "30s", "dm {item}"},
			check: func(t *testing.T, c Config) {
				if c.Parallelism != 4 {
					t.Errorf("Parallelism = %d, want 4", c.Parallelism)
				}
				if c.File != "in.txt" {
					t.Errorf("File = %q", c.File)
				}
				if c.Template != "dm {item}" {
					t.Errorf("Template = %q", c.Template)
				}
				if c.Retries != 5 {
					t.Errorf("Retries = %d, want 5 (default)", c.Retries)
				}
				if c.Timeout != 30*time.Second {
					t.Errorf("Timeout = %v, want 30s", c.Timeout)
				}
				if c.DryRun || c.NoTUI || c.Fresh || c.SkipUniqueCheck || c.Wizard {
					t.Errorf("unexpected flag set: %+v", c)
				}
			},
		},
		{
			name: "all flags",
			argv: []string{
				"-P", "8",
				"-F", "data.txt",
				"--timeout", "2m",
				"--retries", "3",
				"-n",
				"--no-tui",
				"--fresh",
				"--skip-unique-txt-rows",
				"echo {item}",
			},
			check: func(t *testing.T, c Config) {
				if c.Parallelism != 8 {
					t.Errorf("P = %d", c.Parallelism)
				}
				if c.Timeout != 2*time.Minute {
					t.Errorf("Timeout = %v, want 2m", c.Timeout)
				}
				if c.Retries != 3 {
					t.Errorf("Retries = %d, want 3", c.Retries)
				}
				if !c.DryRun {
					t.Error("DryRun not set")
				}
				if !c.NoTUI {
					t.Error("NoTUI not set")
				}
				if !c.Fresh {
					t.Error("Fresh not set")
				}
				if !c.SkipUniqueCheck {
					t.Error("SkipUniqueCheck not set")
				}
			},
		},
		{
			name: "multi-word template joined",
			argv: []string{"-F", "in.txt", "--timeout", "10s", "curl", "-sSfL", "-o", "/dev/null", "{item}"},
			check: func(t *testing.T, c Config) {
				want := "curl -sSfL -o /dev/null {item}"
				if c.Template != want {
					t.Errorf("Template = %q, want %q", c.Template, want)
				}
			},
		},
		{
			name:    "missing -F",
			argv:    []string{"--timeout", "10s", "dm {item}"},
			wantErr: true,
		},
		{
			name:    "missing template",
			argv:    []string{"-F", "in.txt", "--timeout", "10s"},
			wantErr: true,
		},
		{
			name:    "template without {item}",
			argv:    []string{"-F", "in.txt", "--timeout", "10s", "echo hello"},
			wantErr: true,
		},
		{
			name:    "missing --timeout",
			argv:    []string{"-F", "in.txt", "dm {item}"},
			wantErr: true,
		},
		{
			name: "-n skips timeout requirement",
			argv: []string{"-F", "in.txt", "-n", "dm {item}"},
			check: func(t *testing.T, c Config) {
				if !c.DryRun {
					t.Error("DryRun not set")
				}
			},
		},
		{
			name: "--wizard skips all requirements",
			argv: []string{"--wizard"},
			check: func(t *testing.T, c Config) {
				if !c.Wizard {
					t.Error("Wizard not set")
				}
			},
		},
		{
			name:    "negative parallelism",
			argv:    []string{"-P", "-1", "-F", "in.txt", "--timeout", "10s", "dm {item}"},
			wantErr: true,
		},
		{
			name:    "negative retries",
			argv:    []string{"-F", "in.txt", "--timeout", "10s", "--retries", "-1", "dm {item}"},
			wantErr: true,
		},
		{
			name: "zero parallelism allowed",
			argv: []string{"-P", "0", "-F", "in.txt", "--timeout", "10s", "dm {item}"},
			check: func(t *testing.T, c Config) {
				if c.Parallelism != 0 {
					t.Errorf("Parallelism = %d, want 0", c.Parallelism)
				}
			},
		},
		{
			name: "zero retries allowed",
			argv: []string{"-F", "in.txt", "--timeout", "10s", "--retries", "0", "dm {item}"},
			check: func(t *testing.T, c Config) {
				if c.Retries != 0 {
					t.Errorf("Retries = %d, want 0", c.Retries)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseArgs(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; cfg=%+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}
