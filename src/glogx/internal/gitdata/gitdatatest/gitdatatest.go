// Package gitdatatest は gitdata のテストが共有する fake (外部コマンドの CommandRunner)。
// main のテスト (browseModel と組み合わせるもの) と internal/gitdata の中のテストの両方から使う。
// 🚨 本番のコードから import しない (depguard が止める)。
// 🚨 gitdata を import しない: import すると gitdata の中のテストから使えなくなる (import の循環。issuesviewtest と同じ)。
// そのため CommandRunner ではなく同じ形の名前の無い関数型を返す (代入できる)。
package gitdatatest

import "context"

// FakeRunner は fixture を返す CommandRunner。通常テストで外部通信しない (issue のテスト方針)。
func FakeRunner(stdout, stderr string, err error) func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	return func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		return []byte(stdout), []byte(stderr), err
	}
}
