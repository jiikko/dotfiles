package usage

import (
	"os"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
)

// TestMain は「glogx が支持しない幅 env」でテストを走らせない (issue 054)。
//
// このパッケージも描画幅を ansi.StringWidth で測る (render.go) ため、幅を主張するテストを
// 足した瞬間に main / issues と同じ「幅 1 前提の assert が落ちる生ログ」になる。今はまだ
// 幅に依存する assert が無く env 下でも green だが、幅を測るパッケージだけガードが無い状態を
// 残すと、次に足す人が混乱する側に落ちる (2026-08-15 の敵対的レビューの指摘)。
//
// あわせて XDG_CACHE_HOME を一時ディレクトリへ向ける。Fetch は全プロセス共有の
// claude-usage-shared.json (shared.go) を読み書きするので、隔離しないと実ユーザーの
// キャッシュを読んで偽の claude を起こさず通ったり、偽の枠で上書きしたりする。
// 個々のテストは isolateShared でさらに自分専用の置き場へ向ける (テスト間で結果を共有しない)。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	dir, err := os.MkdirTemp("", "usage-test-cache")
	if err != nil {
		panic(err) // 隔離できないまま走らせると実ユーザーのキャッシュを触る
	}
	if err := os.Setenv("XDG_CACHE_HOME", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
