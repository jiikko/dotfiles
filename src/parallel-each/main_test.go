package main

import "testing"

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name    string
		argv    []string
		wantErr bool
		check   func(*testing.T, Config)
	}{
		{
			name: "minimal",
			argv: []string{"-F", "in.txt", "dm {item}"},
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
				if c.DryRun || c.NoTUI || c.Fresh || c.SkipUniqueCheck {
					t.Errorf("unexpected flag set: %+v", c)
				}
			},
		},
		{
			name: "all flags",
			argv: []string{
				"-P", "8",
				"-F", "data.txt",
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
			argv: []string{"-F", "in.txt", "curl", "-sSfL", "-o", "/dev/null", "{item}"},
			check: func(t *testing.T, c Config) {
				want := "curl -sSfL -o /dev/null {item}"
				if c.Template != want {
					t.Errorf("Template = %q, want %q", c.Template, want)
				}
			},
		},
		{
			name:    "missing -F",
			argv:    []string{"dm {item}"},
			wantErr: true,
		},
		{
			name:    "missing template",
			argv:    []string{"-F", "in.txt"},
			wantErr: true,
		},
		{
			name:    "template without {item}",
			argv:    []string{"-F", "in.txt", "echo hello"},
			wantErr: true,
		},
		{
			name:    "negative parallelism",
			argv:    []string{"-P", "-1", "-F", "in.txt", "dm {item}"},
			wantErr: true,
		},
		{
			name: "zero parallelism allowed",
			argv: []string{"-P", "0", "-F", "in.txt", "dm {item}"},
			check: func(t *testing.T, c Config) {
				if c.Parallelism != 0 {
					t.Errorf("Parallelism = %d, want 0", c.Parallelism)
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
