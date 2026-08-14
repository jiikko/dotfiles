package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glogx/issues"
)

// CI の Bench workflow が回す issue scan ベンチ (issue 065)。一覧生成のホットパス
// (issues.Scan + 全件 LoadMeta) を測る。起動時と issues_watch の外部編集検知のたびに
// 走る経路で、050 の実測 (50 件 395KB の実 repo) で 2 倍動いた実績がある。
//
// scanIssues そのものは測らない: RepoRoot が git を fork し、CI では fork がハイライト
// (chroma) と同じ flake 枠になる (bench_glogx.sh ヘッダの判断と同型)。ファイル I/O は
// 含む (LoadMeta の open + 先頭 read がこの経路のコストの一部なので外さない)。
//
// 「全文を読む」形の退行はここではなく TestScanIssuesDoesNotReadFullBody (issue 052) が
// read バイト数で守る。ここが守るのは「読む量は同じままコストが緩やかに増える」退行
// (正規表現の追加・PlainLine の多重適用・sort の劣化など)。

// benchIssuesDir は実測分布 (050: 50 件 計 395KB) を模した合成 issue ディレクトリを作る。
// 件数 50 (直下 40 + done/ 10)、サイズは index で決まる決定的な散らし (平均 ~8KB)。
// fixture は ASCII のみ (issue 055 の方針に合わせる)。
func benchIssuesDir(b *testing.B) string {
	b.Helper()
	root := b.TempDir()
	dir := filepath.Join(root, "issues")
	doneDir := filepath.Join(dir, "done")
	if err := os.MkdirAll(doneDir, 0o755); err != nil {
		b.Fatal(err)
	}
	bodyLine := "body line with some plain ascii text to fill the file with bytes\n"
	for i := range 50 {
		place := dir
		if i >= 40 {
			place = doneDir
		}
		name := fmt.Sprintf("%03d-feat-bench-fixture.md", i+1)
		var sb strings.Builder
		// 半数は front matter 付き (LoadMeta の status: パースを通す)
		if i%2 == 0 {
			sb.WriteString("---\nstatus: open\n---\n")
		}
		fmt.Fprintf(&sb, "# %03d feat: bench fixture\n\n", i+1)
		for range 20 + (i%40)*6 {
			sb.WriteString(bodyLine)
		}
		if err := os.WriteFile(filepath.Join(place, name), []byte(sb.String()), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}

func BenchmarkIssueScan(b *testing.B) {
	dir := benchIssuesDir(b)
	dirs := []string{dir}
	b.ReportAllocs()
	for b.Loop() {
		found, _ := issues.Scan(dirs)
		for _, iss := range found {
			// scanIssues (issues_view.go) と同じく読み取り失敗は無視する
			_ = iss.LoadMeta()
		}
	}
}
