package lanview

import (
	"testing"

	"glogx/internal/testenv"
)

// TestMain は隔離した環境でテストを走らせる (前処理の中身と理由は internal/testenv の Main が正本)。
// パッケージ固有の前処理: mac-lan-manager の本物のネットワークの読み取り (networksetup) と Bonjour の走査
// (終わらない dns-sd) を起こさない。
// 🚨 今のこのパッケージのテストは L を開かないので、この前処理を外しても緑のまま通る。それでも外さない: L を開くテストを
// ここに足した瞬間に本物の networksetup / dns-sd が起き、検査 (no-real-commands の shim) はその両方を見ていない。
func TestMain(m *testing.M) {
	testenv.Main(m, "glogx-lanview", func(string) { lanOptions = fakeLanOptions })
}
