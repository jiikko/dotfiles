//go:build !darwin

package chromecookie

import "runtime"

// KeychainPassword は macOS 以外では使えない。
//
// Chrome の Cookie 暗号鍵の置き場所（macOS=Keychain / Linux=libsecret 等）は OS ごとに
// 違い、実機で確認していない値を並べると「動くように見えて別の領域を読む」事故になる。
// ビルドは通す（go vet / test を全 platform で回すため）が、実行時に明示的に断る。
func KeychainPassword() ([]byte, error) {
	return nil, &EnvError{Msg: "macOS 専用です（現在の GOOS=" + runtime.GOOS + "）。Chrome の Cookie 復号に macOS Keychain を使います"}
}
