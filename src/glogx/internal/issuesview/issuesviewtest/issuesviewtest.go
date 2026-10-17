// Package issuesviewtest は issues 画面のテストが共有する issue の fixture (issues.Issue を組む・実ファイルを置く)。
// main のテスト (browseModel と組み合わせるもの) と internal/issuesview の中のテストの両方から使う。
// 🚨 本番のコードから import しない (testing を import する。depguard が止める)。
// 🚨 issuesview を import しない: import すると issuesview の中のテストから使えなくなる (import の循環。doctortest と同じ。issue 817)。
package issuesviewtest

import (
	"fmt"
	"glogx/issues"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// FakeIssue はファイルシステムを使わずに Issue を作る (一覧の表示・フィルタのテスト用)。
func FakeIssue(number, category, slug string, st issues.Status) *issues.Issue {
	rel := number + "-" + category + "-" + slug + ".md"
	if st != issues.StatusOpen {
		rel = st.String() + "/" + rel
	}
	return &issues.Issue{
		Path:     "/repo/issues/" + rel,
		Dir:      "/repo/issues",
		Rel:      rel,
		Number:   number,
		Category: category,
		Slug:     slug,
		Status:   st,
	}
}

func SampleIssues() []*issues.Issue {
	return []*issues.Issue{
		FakeIssue("030", "feat", "a", issues.StatusOpen),
		FakeIssue("029", "feat", "b", issues.StatusOpen),
		FakeIssue("028", "refactor", "c", issues.StatusPending),
		FakeIssue("027", "refactor", "d", issues.StatusDone),
		FakeIssue("026", "bug", "e", issues.StatusDone),
	}
}

// RealIssue はディスク上に実体を持つ issue を 1 件作る。editCmd は起動前に実体を確認するので、
// エディタ起動が絡むテストは合成パスの Issue では「起動しない」側に倒れてしまう。
func RealIssue(t *testing.T) *issues.Issue {
	t.Helper()
	return WriteRealIssue(t, t.TempDir(), "001-feat-real.md", "# 001 feat: real\n")
}

// Path2Issue は dir/rel の Issue を組む (ファイルは作らない)。番号・カテゴリ・slug はファイル名、
// Status は rel の先頭ディレクトリ (done/ pending/ …) から取る。
func Path2Issue(dir, rel string) *issues.Issue {
	base := filepath.Base(rel)
	parts := strings.SplitN(strings.TrimSuffix(base, ".md"), "-", 3)
	iss := &issues.Issue{Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: parts[0]}
	if len(parts) > 1 {
		iss.Category = parts[1]
	}
	if len(parts) > 2 {
		iss.Slug = parts[2]
	}
	if sub, _, ok := strings.Cut(filepath.ToSlash(rel), "/"); ok {
		if st, known := issues.StatusOfDir(sub); known {
			iss.Status = st // 走査と同じ表 (別綴り・大文字小文字も同じに扱う)
		}
	}
	return iss
}

// WatchTree は issues ディレクトリを 1 つ持つ木を作り (root, issue のパス) を返す。
func WatchTree(t *testing.T, body string) (root, path string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "001-feat-x.md")
	WriteIssue(t, path, body, time.Now().Add(-time.Hour))
	return root, path
}

// WriteIssue は本文を書いて mtime を固定する (指紋の比較を時計に依存させない)。
func WriteIssue(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// ManyIssues は滑走が観測できるだけの行数を持つ一覧を作る (窓より十分多い件数)。
func ManyIssues(n int) []*issues.Issue { return ManyIssuesOf(n, "bug") }

// ManyIssuesOf は番号 n..1 (3 桁) の open な issue を降順に並べる (スクロール・窓の検査用)。
func ManyIssuesOf(n int, category string) []*issues.Issue {
	out := make([]*issues.Issue, 0, n)
	for i := n; i > 0; i-- {
		out = append(out, FakeIssue(fmt.Sprintf("%03d", i), category, "x", issues.StatusOpen))
	}
	return out
}

// WriteRealIssue は dir/rel に本文 body の issue ファイルを実際に置き、対応する Issue を返す
// (番号・カテゴリ・slug はファイル名から取る。状態ディレクトリの下なら Status もそこから)。
// 実ファイルが要るテスト (editCmd の実体確認・本文の読み込み・移動) の唯一の土台。手組みすると
// 状態や Dir の付け忘れが黙ってずれる (issue 667)。
func WriteRealIssue(t *testing.T, dir, rel, body string) *issues.Issue {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Path2Issue(dir, rel)
}
