package prefs

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mac-lan-manager/internal/testsandbox"
)

var sandbox string

func TestMain(m *testing.M) {
	var err error
	if sandbox, err = testsandbox.New("mac-lan-manager-prefs-"); err == nil {
		err = os.Setenv(EnvDir, filepath.Join(sandbox, "config"))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	writeGuard = testsandbox.Guard(sandbox)
	code := m.Run()
	_ = os.RemoveAll(sandbox)
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
	want := Prefs{ShowMain: false, ShowLan: true, ShowOthers: true, LanByBand: false, WiredOnly: true, Band: "10.0.5.0/24", LanIF: "en13", Kinds: []string{"virtual", "vpn"}}
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

func TestSaveGuardRejectsOutside(t *testing.T) {
	outside := filepath.Join(os.TempDir(), fmt.Sprintf("mac-lan-manager-prefs-guard-%d-%d", os.Getpid(), time.Now().UnixNano()), "settings.toml")
	if err := Save(outside, Default()); err == nil {
		t.Fatal("一時ディレクトリの外への書き込みを拒否しなかった")
	}
	if _, err := os.Stat(filepath.Dir(outside)); !os.IsNotExist(err) {
		t.Errorf("拒否したのにディレクトリが作られた: %v", err)
	}
}

// 前の版は種類を画面の名前 (「有線」) で書いていた。それも Key に読み替える (語を直しても設定が外れない)。
// 知らない名前 (新しい版が足した種類) は残し、Save でも書き戻す (古い版で設定を変えても消えない)。
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
	if want := []string{"wired", "link-local", "vpn", "なにか"}; !slices.Equal(got.Kinds, want) {
		t.Errorf("Kinds = %v, want %v", got.Kinds, want)
	}
	if err := Save(path, got); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(again.Kinds, "なにか") {
		t.Errorf("Save と Load の後に知らない種類が消えた: %v", again.Kinds)
	}
}

// 手で書いた帯も、判定と同じ形 (Masked) にそろえて読む。読めない値はそのまま残す (画面が「使えない形」と出す)。
func TestLoadNormalizesBand(t *testing.T) {
	for in, want := range map[string]string{"192.168.100.5/24": "192.168.100.0/24", "abc": "abc"} {
		path := filepath.Join(sandbox, "band", in[:3], "settings.toml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("band = \""+in+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || got.Band != want {
			t.Errorf("Load(band=%q).Band = %q (%v), want %q", in, got.Band, err, want)
		}
	}
}
