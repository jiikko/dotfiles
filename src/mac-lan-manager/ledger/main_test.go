package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mac-lan-manager/internal/testsandbox"
)

// sandbox はこの package のテストが書いてよい唯一の場所。TestMain が作り、writeGuard はその外への書き込みを書く前に拒否する
// (_claude/rules/sandbox-real-destructive-test-apis.md)。台帳の置き場所 (EnvDir) もここへ向け、DefaultPath が本物を指さないようにする。
var sandbox string

func TestMain(m *testing.M) {
	var err error
	if sandbox, err = testsandbox.New("mac-lan-manager-ledger-"); err == nil {
		err = os.Setenv(EnvDir, filepath.Join(sandbox, "config"))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	writeGuard = testsandbox.Guard(sandbox)
	code := m.Run()
	_ = os.RemoveAll(sandbox) // テストが作った一時ディレクトリの後始末
	os.Exit(code)
}
