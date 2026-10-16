package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 操作のログは 1 行ずつ追記し、自分だけが読める権限で作り、上限を超えたら新しい方の半分だけを残す。
func TestFileLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "logs", "mac-lan-manager.log")
	log := FileLog(p)
	log("a")
	log("b")
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "a\nb\n" {
		t.Fatalf("ログ = %q %v", b, err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("権限 = %v (0600 にする)", fi.Mode().Perm())
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("古い行\n", LogMax/10)+strings.Repeat("新しい行\n", LogMax/20)), 0o600); err != nil {
		t.Fatal(err)
	}
	log("c")
	b, _ = os.ReadFile(p)
	if len(b) > LogMax/2+10 || !strings.HasSuffix(string(b), "新しい行\nc\n") || strings.HasPrefix(string(b), "行\n") {
		t.Errorf("上限を超えても新しい方の半分を残さない (%d 字、行の頭から始まるか)", len(b))
	}
}
