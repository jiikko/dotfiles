package monitor

import (
	"os"
	"testing"

	"subproc"
)

func TestMain(m *testing.M) {
	if err := subproc.UnsetGitLocationEnv(); err != nil { // 継承した GIT_DIR などで、一時 dir に作る repo の git が別の repo を書かない (issue 740)
		panic(err)
	}
	os.Exit(m.Run())
}
