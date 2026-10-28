package chromecookie

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Local State が読めないときの走査は、実在する Default / Profile N だけを Dir の昇順で返し、
// 何も無ければ Default だけに倒す。

func TestFallbackProfilesListsOnlyChromeProfileDirsSorted(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Profile 3", "Default", "Profile 10", "Crashpad", "System Profile"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Profile 2"), nil, 0o600); err != nil { // ディレクトリでないものは数えない
		t.Fatal(err)
	}
	got := fallbackProfiles(root)
	want := []Profile{{Dir: "Default"}, {Dir: "Profile 10"}, {Dir: "Profile 3"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallbackProfiles = %v, want %v", got, want)
	}
}

func TestFallbackProfilesDefaultsWhenNothingToList(t *testing.T) {
	want := []Profile{{Dir: "Default"}}
	if got := fallbackProfiles(t.TempDir()); !reflect.DeepEqual(got, want) {
		t.Fatalf("empty dir: got %v, want %v", got, want)
	}
	if got := fallbackProfiles(filepath.Join(t.TempDir(), "missing")); !reflect.DeepEqual(got, want) {
		t.Fatalf("missing dir: got %v, want %v", got, want)
	}
}
