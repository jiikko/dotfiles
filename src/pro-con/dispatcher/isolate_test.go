package dispatcher

import (
	"os"
	"testing"

	"pro-con/foreground/ptytest"
	"pro-con/wake"
)

// 🚨 socket の逃がし先を本物の /tmp/pro-con-<uid> にしない (t.TempDir の置き場は長く、逃がし先へ倒れる)。
// 擬似端末の上の helper (ptytest) として起こされたときは、その結果だけを書いて抜ける (execrunner_test.go の runOnTerminal)。
func TestMain(m *testing.M) {
	ptytest.Helper(runOnTerminal)
	os.Exit(wake.RunIsolated(m.Run))
}
