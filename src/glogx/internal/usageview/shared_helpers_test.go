package usageview

import (
	"errors"
	"glogx/internal/extcmd"
	"glogx/internal/testenv"
	"ratelimit/usage"
	"strings"
	"testing"
	"time"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (issue 817 で画面を移したとき、
// 両方のテストが使っていたもの)。

// assertSanitizedAndVisible は無害化テストの 2 つの主張を対で確かめる (issue 284):
//
//  1. 制御シーケンス / OSC の中身が残っていない (本来の主張)
//  2. 🚨 **細工した値が実際に描かれている** (陽性対照)
//
// 2 が無いと、箱が空を返した瞬間に 1 の assert が 1 つも実行されず vacuous に緑を返す
// (実測: markNextBox の先頭に `return nil` を入れても PASS した)。
// 同じ罠は TestDoctorLiveDockerScanIsSanitized が sawItem で 1 本だけ塞いでいたもの。
func assertSanitizedAndVisible(t *testing.T, where string, lines []string, wantVisible ...string) {
	t.Helper()
	for _, line := range lines {
		if testenv.HasTerminalControl(line) {
			t.Errorf("%s に制御シーケンスが残った: %q", where, line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("%s に OSC の中身が残った: %q", where, line)
		}
	}
	joined := strings.Join(lines, "\n")
	for _, w := range wantVisible {
		if !strings.Contains(joined, w) {
			t.Fatalf("%s に %q が描かれていない (検査対象が空 = 上の assert は 1 つも sink を守っていない)",
				where, w)
		}
	}
}

func stubLookPath(t *testing.T, available map[string]string) {
	t.Helper()
	orig := extcmd.LookPath
	extcmd.LookPath = func(name string) (string, error) {
		if p, ok := available[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { extcmd.LookPath = orig })
}

// usageSnapFixture は本物の /usage 出力を usage.Parse に通して作る。手組みの Snapshot だと
// 「実際に Fetch が返す形」と食い違っても気づけない: usage.Snapshot / Window は json タグを
// 持たずフィールド名で符号化され、ResetAt は time.Time。ここが往復で壊れると症状は
// 「永久に cache miss = 毎起動 claude を起こし続ける」で、どこも失敗しないまま劣化する。
func usageSnapFixture(t *testing.T) *usage.Snapshot {
	t.Helper()
	const realResult = `You are currently using your subscription to power your Claude Code usage

Current session: 2% used · resets Jul 22 at 3:09am (Asia/Tokyo)
Current week (all models): 29% used · resets Jul 24 at 8am (Asia/Tokyo)
Current week (Fable): 48% used · resets Jul 24 at 8am (Asia/Tokyo)

What's contributing to your limits usage?
Last 24h · 875 requests · 7 sessions`
	snap, err := usage.Parse(realResult, time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("fixture の Parse に失敗: %v", err)
	}
	snap.Version = "2.1.216"
	return snap
}
