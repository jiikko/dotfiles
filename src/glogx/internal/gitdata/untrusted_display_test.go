package gitdata

import (
	"strings"
	"testing"
)

// 外部由来の文字列が termsafe を通らずに画面 / stderr へ届いていた 4 経路。
// どれも表示にしか使わない値なので、受け取った時点で平文にしている。
const evil417 = "\x1b]52;c;cHduZWQ=\a\u009b2J\u202eevil"

func assertNoTermControl(t *testing.T, what, got string) {
	t.Helper()
	if strings.ContainsAny(got, "\x1b\a\u009b\u202e") {
		t.Errorf("%s に制御シーケンスが残っている: %q", what, got)
	}
	if !strings.Contains(got, "evil") {
		t.Errorf("%s の本文まで落ちている: %q", what, got)
	}
}

func TestGHWarningSanitizesStderr(t *testing.T) {
	for _, k := range []GHErrorKind{GHRateLimited, GHOther} {
		assertNoTermControl(t, "GHError.Warning", (&GHError{Kind: k, Detail: "gh: " + evil417}).Warning())
	}
}
