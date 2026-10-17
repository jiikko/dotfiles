package term

import (
	"testing"

	"glogx/internal/testenv"
)

// TestMain は隔離した環境でテストを走らせる (前処理の中身と理由は internal/testenv の Main が正本。issue 742 でテストを
// このパッケージへ移したとき、main の TestMain の前処理が外れた。issue 817 で 1 か所に寄せた)。
func TestMain(m *testing.M) { testenv.Main(m, "glogx-term", nil) }
