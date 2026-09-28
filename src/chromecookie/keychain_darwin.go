package chromecookie

import (
	"fmt"
	"os/exec"
	"strings"
)

// KeychainPassword は Keychain から "Chrome Safe Storage" のパスワードを取得する。
//
// 🚨 取得した値はプロセス内だけで使い、ファイルにもログにも出さない。
func KeychainPassword() ([]byte, error) {
	return keychainPasswordFrom(func() ([]byte, error) {
		return exec.Command("security", "find-generic-password",
			"-w",
			"-a", chromeKeychainAccount,
			"-s", chromeKeychainService).Output()
	})
}

// keychainPasswordFrom は KeychainPassword の本体。run は security コマンドの実行
// （テストが本物の Keychain に触れずに失敗経路を通すための引数。グローバル変数にしない）。
func keychainPasswordFrom(run func() ([]byte, error)) ([]byte, error) {
	out, err := run()
	if err != nil {
		// 🚨 EnvError で返す。暗号鍵は全プロファイル共通なので、プロファイルを変えても直らない。
		return nil, &EnvError{
			Msg: fmt.Sprintf(
				"Keychain から暗号化キーを取得できませんでした（service=%q account=%q）。\n"+
					"  - ターミナルで次を実行し、表示される許可ダイアログで「常に許可」を押してください:\n"+
					"      security find-generic-password -w -a %q -s %q\n"+
					"  - %s がインストールされ、ログインしているか確認してください（このツールは Chrome 専用です）。",
				chromeKeychainService, chromeKeychainAccount,
				chromeKeychainAccount, chromeKeychainService, ChromeName),
			Err: err,
		}
	}
	return []byte(strings.TrimRight(string(out), "\n")), nil
}
