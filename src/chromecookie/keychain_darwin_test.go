//go:build darwin

package chromecookie

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 🚨 Keychain から暗号鍵を取れないときは EnvError（探索全体を止める）で返すこと。
// 暗号鍵は全プロファイル共通なので、次のプロファイルを試しても直らない。
// 本物の security コマンド・Keychain には触れない（実行部分を差し替える）。
func TestKeychainFailureIsEnvError(t *testing.T) {
	_, err := keychainPasswordFrom(func() ([]byte, error) {
		return nil, errors.New("exit status 51")
	})
	if !IsEnvError(err) {
		t.Fatalf("EnvError であるべき: %T %v", err, err)
	}
	if want := fmt.Sprintf("security find-generic-password -w -a %q -s %q", chromeKeychainAccount, chromeKeychainService); !strings.Contains(err.Error(), want) {
		t.Errorf("許可の手順（%s）が案内に無い: %v", want, err)
	}

	got, err := keychainPasswordFrom(func() ([]byte, error) { return []byte("pw\n"), nil })
	if err != nil || string(got) != "pw" {
		t.Errorf("成功時は末尾の改行を落として返す: %q %v", got, err)
	}
}
