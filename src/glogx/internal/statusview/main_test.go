package statusview

import (
	"testing"

	"glogx/internal/testenv"
)

// TestMain は隔離した環境でテストを走らせる (前処理の中身と理由は internal/testenv の Main が正本。issue 817)。
func TestMain(m *testing.M) { testenv.Main(m, "glogx-statusview", nil) }
