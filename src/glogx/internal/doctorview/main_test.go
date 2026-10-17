package doctorview

import (
	"fmt"
	"os"
	"testing"

	"doctor/testtmp"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
	"subproc"
)

// TestMain は main パッケージ (tui_helpers_test.go) の TestMain と同じ隔離をこのパッケージのテストにも掛ける (issue 742 で
// doctor のテストの一部をここへ移したとき、main の TestMain を通らなくなった)。
//   - 支持しない幅 env (RUNEWIDTH_EASTASIAN) では理由を 1 度だけ言って止める (issue 054)
//   - キャッシュ置き場 (XDG_CACHE_HOME) を一時ディレクトリへ逃がす: doctor の走査の記憶・通知の印は app.CacheBaseDir の下に
//     書くので、隔離しないと make test が開発者の ~/.cache/glog を書き換える
//   - 継承した GIT_DIR などを外す (issue 740)
func TestMain(m *testing.M) {
	if err := subproc.UnsetGitLocationEnv(); err != nil {
		panic(err)
	}
	widthenv.ExitIfUnsupported()
	// 後始末を下の cleanup だけに預けない理由は tui_helpers_test.go の TestMain と同じ (issue 305)。
	dir, swept, cleanup, err := testtmp.Setup("glogx-doctorview")
	if err != nil {
		panic(err) // 隔離できないまま走らせると実ユーザーのキャッシュを触る
	}
	if swept > 0 {
		fmt.Fprintf(os.Stderr, "[testtmp] 前回の残骸を %d 件回収した\n", swept)
	}
	if err := os.Setenv("XDG_CACHE_HOME", dir); err != nil {
		panic(err)
	}
	if err := os.Setenv("GOTMPDIR", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}
