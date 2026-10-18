package ptytest

import (
	"testing"
)

// fakeTB は skip が Fatal と Skip のどちらへ進むかを記録する (本物の testing.T は Fatal / Skip で goroutine を抜けるので、
// その手前で記録して戻る)。
type fakeTB struct {
	testing.TB
	fatal, skipped bool
}

func (f *fakeTB) Helper()               {}
func (f *fakeTB) Fatalf(string, ...any) { f.fatal = true }
func (f *fakeTB) Skipf(string, ...any)  { f.skipped = !f.fatal }

// CI (環境変数 CI) では pty の検査を Skip せず落とす。手元では Skip する (CI は -v を付けないので、Skip のまま緑だと
// 検査が走っていないことがログから見えない)。
func TestSkipFailsOnCI(t *testing.T) {
	t.Setenv("CI", "true")
	ci := &fakeTB{}
	skip(ci, "script が無い")
	if !ci.fatal || ci.skipped {
		t.Fatalf("CI なのに Skip した: fatal=%v skipped=%v", ci.fatal, ci.skipped)
	}
	t.Setenv("CI", "")
	local := &fakeTB{}
	skip(local, "script が無い")
	if local.fatal || !local.skipped {
		t.Fatalf("手元なのに落とした: fatal=%v skipped=%v", local.fatal, local.skipped)
	}
}
