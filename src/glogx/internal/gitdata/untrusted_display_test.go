package gitdata

import (
	"glogx/internal/testenv"
	"testing"
)

// 外部由来の文字列が termsafe を通らずに画面 / stderr へ届いていた 4 経路。
// どれも表示にしか使わない値なので、受け取った時点で平文にしている。
const evil417 = "\x1b]52;c;cHduZWQ=\a\u009b2J\u202eevil"

func TestGHWarningSanitizesStderr(t *testing.T) {
	for _, k := range []GHErrorKind{GHRateLimited, GHOther} {
		testenv.AssertNoTermControl(t, "GHError.Warning", (&GHError{Kind: k, Detail: "gh: " + evil417}).Warning())
	}
}
