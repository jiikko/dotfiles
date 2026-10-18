package lanview

import (
	"testing"

	"glogx/internal/testenv"
)

// TestMain は隔離した環境でテストを走らせる (前処理の中身と理由は internal/testenv の Main が正本)。
// パッケージ固有の前処理: mac-lan-manager の本物のネットワークの読み取り (networksetup) と Bonjour の走査
// (終わらない dns-sd) を起こさない。
func TestMain(m *testing.M) {
	testenv.Main(m, "glogx-lanview", func(string) { lanOptions = fakeLanOptions })
}
