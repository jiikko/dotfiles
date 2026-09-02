package exitcode

import "testing"

// 終了コードの数字は外部との契約 (--help / README / CI / 呼び出し側のスクリプトが依存する)。
// 変えたら README と両 CLI の --help も同じ変更で直すこと。
func TestVocabulary(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"候補なし", NoFindings, 0},
		{"候補あり", Findings, 1},
		{"診断できず", Undiagnosed, 2},
		{"実行環境・出力の失敗", EnvFailure, 3},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got=%d want=%d", tc.name, tc.got, tc.want)
		}
	}
	// ⚠️ 「Undiagnosed > Findings」の assert は置かない。上の表が数字を厳密に固定しているので
	// 独立に発火しえず、かつ判定側の優先順位は**数字の大小ではなく if の順序**で実装されている
	// (svcExitCode / diskExitCode)。その優先順位を守っているのは
	// TestSvcExitCode / TestDiskExitCode の「候補あり + 診断できず」のケース (敵対レビュー 2026-09-03)。
}
