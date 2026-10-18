package subproc

import (
	"os"
	"slices"
	"strings"
)

// gitLocationEnv は継承すると、`git -C <dir>` の先ではなく別の repo を読み・書きさせる環境変数 (`git rev-parse --local-env-vars` の全部と、
// GIT_NAMESPACE。TestGitLocationEnvCoversLocalEnvVars が手元の git の出力と突き合わせる)。hook の中・`git rebase -x`・`git bisect run` から起動されると設定されている。
// GIT_DIR を継承した `git -C <一時dir> init` は一時 dir ではなく継承した repo を再 init し、add / commit もそこへ入る (2.55.0 で再現)。
var gitLocationEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	"GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE", "GIT_SHALLOW_FILE",
}

// IsGitLocationEnv は環境変数の名前 k が、継承すると git の対象の repo・設定を変えるものか。
func IsGitLocationEnv(k string) bool {
	return slices.Contains(gitLocationEnv, k) || strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_")
}

// UnsetGitLocationEnv は IsGitLocationEnv の環境変数を今のプロセスから外す。本物の git を起こすテストのパッケージが TestMain で呼ぶ
// (tests/scripts/test_git_tests_unset_location_env.sh が呼び忘れを止める)。
func UnsetGitLocationEnv() error {
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if IsGitLocationEnv(k) {
			if err := os.Unsetenv(k); err != nil {
				return err
			}
		}
	}
	return nil
}
