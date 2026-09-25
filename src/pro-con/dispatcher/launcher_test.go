package dispatcher

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// 起動・再開の引数に、ユーザーの settings.json の language だけを --settings で渡す (461。hook・許可は持ち込まない)。
func TestLauncherArgsPassOnlyLanguage(t *testing.T) {
	p := writeSettings(t, `{"language": "日本語", "hooks": {"Stop": []}, "permissions": {"allow": ["Bash"]}, "model": "opus"}`)
	settings := languageSettings(p)
	if settings != `{"language":"日本語"}` {
		t.Fatalf("--settings の中身 = %q (言語だけのはず)", settings)
	}
	l := ExecLauncher{UserSettings: p}
	start := l.startArgs("pg-1", "依頼")
	want := []string{"--bg", "-w", "pg-1", "-n", "pg-1", "--setting-sources", "project,local", "--settings", `{"language":"日本語"}`, "依頼"}
	if !slices.Equal(start, want) {
		t.Fatalf("起動の引数 = %q\nwant %q", start, want)
	}
	resume := l.resumeArgs("sid", "回答")
	want = []string{"--bg", "--resume", "sid", "--setting-sources", "project,local", "--settings", `{"language":"日本語"}`, "回答"}
	if !slices.Equal(resume, want) {
		t.Fatalf("再開の引数 = %q\nwant %q", resume, want)
	}
}

// 読めない・壊れている・language が無い / 文字列でない / 空なら --settings を付けない (起動は止めない。値を固定で補わない)。
func TestLauncherArgsOmitSettingsWithoutLanguage(t *testing.T) {
	cases := map[string]string{
		"パスが空":        "",
		"ファイルが無い":     filepath.Join(t.TempDir(), "missing.json"),
		"壊れた JSON":    writeSettings(t, `{"language": `),
		"language 無し": writeSettings(t, `{"model": "opus"}`),
		"文字列でない":      writeSettings(t, `{"language": 1}`),
		"空":           writeSettings(t, `{"language": "  "}`),
	}
	for name, p := range cases {
		if s := languageSettings(p); s != "" {
			t.Errorf("%s: languageSettings = %q (渡さないはず)", name, s)
		}
		l := ExecLauncher{UserSettings: p}
		if args := l.startArgs("n", "p"); slices.Contains(args, "--settings") {
			t.Errorf("%s: 起動の引数に --settings が付いた: %q", name, args)
		}
		if args := l.resumeArgs("sid", "t"); slices.Contains(args, "--settings") {
			t.Errorf("%s: 再開の引数に --settings が付いた: %q", name, args)
		}
	}
}

// CLAUDE_CONFIG_DIR があれば claude と同じくその下の settings.json を読む。
func TestUserSettingsPath(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := UserSettingsPath("/h"); got != "/h/.claude/settings.json" {
		t.Fatalf("既定 = %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/c")
	if got := UserSettingsPath("/h"); got != "/c/settings.json" {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q", got)
	}
}
