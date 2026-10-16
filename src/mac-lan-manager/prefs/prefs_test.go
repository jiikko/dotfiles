package prefs

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var sandbox string

func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "mac-lan-manager-prefs-")
	if err == nil {
		sandbox, err = filepath.EvalSymlinks(d)
	}
	if err == nil {
		err = os.Setenv(EnvDir, filepath.Join(sandbox, "config"))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	// 一時ディレクトリの配下だけに書かせる (_claude/rules/sandbox-real-destructive-test-apis.md)。親を実体にしてから比べる
	writeGuard = func(path string) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		dir := filepath.Dir(abs)
		for {
			if real, err := filepath.EvalSymlinks(dir); err == nil {
				abs = filepath.Join(real, strings.TrimPrefix(abs, dir))
				break
			}
			if dir == filepath.Dir(dir) {
				break
			}
			dir = filepath.Dir(dir)
		}
		if !strings.HasPrefix(abs, sandbox+string(filepath.Separator)) {
			return fmt.Errorf("テストの書き込みが一時ディレクトリの外を指している: %s", path)
		}
		return nil
	}
	code := m.Run()
	_ = os.RemoveAll(d)
	os.Exit(code)
}

func TestDefaultPathUsesEnvDir(t *testing.T) {
	p, err := DefaultPath()
	if err != nil || !strings.HasPrefix(p, sandbox) {
		t.Fatalf("DefaultPath() = %s %v: テスト中に本物の置き場所を指している", p, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(sandbox, "rt", "settings.toml")
	want := Prefs{ShowMain: false, ShowLan: true, ShowOthers: true, LanByBand: false, Band: "10.0.5.0/24", LanIF: "en13", Kinds: []string{"virtual", "vpn"}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(want.Kinds)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestLoadMissingIsDefault(t *testing.T) {
	got, err := Load(filepath.Join(sandbox, "none", "settings.toml"))
	if err != nil || fmt.Sprint(got) != fmt.Sprint(Default()) {
		t.Errorf("got %+v %v", got, err)
	}
}

func TestBandPrefix(t *testing.T) {
	for band, want := range map[string]string{"192.168.100.0/24": "192.168.100.", "10.0.0.0/16": "", "x": ""} {
		if got := (Prefs{Band: band}).BandPrefix(); got != want {
			t.Errorf("BandPrefix(%q) = %q, want %q", band, got, want)
		}
	}
}

func TestSaveGuardRejectsOutside(t *testing.T) {
	outside := filepath.Join(os.TempDir(), fmt.Sprintf("mac-lan-manager-prefs-guard-%d-%d", os.Getpid(), time.Now().UnixNano()), "settings.toml")
	if err := Save(outside, Default()); err == nil {
		t.Fatal("一時ディレクトリの外への書き込みを拒否しなかった")
	}
	if _, err := os.Stat(filepath.Dir(outside)); !os.IsNotExist(err) {
		t.Errorf("拒否したのにディレクトリが作られた: %v", err)
	}
}

// 前の版は種類を画面の名前 (「有線」) で書いていた。それも Key に読み替え、知らない名前は捨てる (語を直しても設定が外れない)。
func TestLoadOldKindNames(t *testing.T) {
	path := filepath.Join(sandbox, "old", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("kinds = \"有線,自動割り当て,VPN,なにか,wired\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"wired", "link-local", "vpn"}; !slices.Equal(got.Kinds, want) {
		t.Errorf("Kinds = %v, want %v", got.Kinds, want)
	}
}
