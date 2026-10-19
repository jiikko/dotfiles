package extcmd_test

import (
	"testing"

	"glogx/internal/testenv"
)

// TestMain は隔離した環境でテストを走らせる (前処理の中身と理由は internal/testenv の Main が正本)。
// 外部テストの package に置くのは、testenv が extcmd を import していて内部テストからは import できないため。
func TestMain(m *testing.M) { testenv.Main(m, "glogx-extcmd", nil) }
