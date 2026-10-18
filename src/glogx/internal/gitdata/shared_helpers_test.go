package gitdata

import (
	"context"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (両方のテストが使っていたもの)。

// fakeRunner は fixture を返す CommandRunner。通常テストで外部通信しない (issue のテスト方針)。
func fakeRunner(stdout, stderr string, err error) CommandRunner {
	return func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		return []byte(stdout), []byte(stderr), err
	}
}
