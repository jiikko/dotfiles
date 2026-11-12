// Package oracle は本家 yomiyasu (Python) を正解 (oracle) として動かし、出力を記録して Go の実装と突き合わせる仕組み。
//
// 本家のファイルは固定の commit から取ってきて SHA-256 の一覧 (upstream.sha256) と照合し、環境を固定して python3 を動かす。
// 記録は 3 つの単位 (パターン・関数・CLI) で testdata/golden/ に置く。Go のテストは記録と突き合わせるだけで、Python を動かさない。
// 記録を作る道具は oracle/cmd/golden (make golden)。
package oracle

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// Repo は本家の GitHub の repo。
	Repo = "nanaism/yomiyasu"
	// Commit は本家 v3.1.0 の commit。branch や tag でなく commit で固定する (動く参照は取り直すたびに中身が変わりうる)。
	Commit = "db78b9bc5e868549b517a037df7a4d1c93edac23"
	// ScriptsDir・ListsDir は取ってきた木の中の、本家の道具と語の一覧の置き場。道具は ../references/lists を相対で読む。
	ScriptsDir = "skills/yomiyasu/scripts"
	ListsDir   = "skills/yomiyasu/references/lists"
)

// UpstreamFiles は取ってくる本家のファイル (Commit の木の中のパス)。これ以外は取らない。
var UpstreamFiles = []string{
	"skills/yomiyasu/LICENSE",
	"skills/yomiyasu/UNICODE-LICENSE.txt",
	ScriptsDir + "/markdown_visibility.py",
	ScriptsDir + "/yomiyasu_check.py",
	ScriptsDir + "/yomiyasu_diff.py",
	ScriptsDir + "/yomiyasu_hook.py",
	ScriptsDir + "/yomiyasu_lint.py",
	ListsDir + "/fact-gaps.txt",
	ListsDir + "/feelings.txt",
	ListsDir + "/hedges.txt",
	ListsDir + "/intensifiers.txt",
	ListsDir + "/katakana-terms.txt",
	ListsDir + "/nominalization.txt",
	ListsDir + "/renyo-nouns.txt",
	ListsDir + "/thin-evaluation.txt",
}

//go:embed upstream.sha256
var manifestText string

// Manifest は upstream.sha256 の中身 (パス → SHA-256)。UpstreamFiles と過不足なく同じでなければエラー。
func Manifest() (map[string]string, error) {
	return parseManifest(manifestText)
}

func parseManifest(text string) (map[string]string, error) {
	sums := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		sum, path, ok := strings.Cut(line, "  ")
		if !ok || len(sum) != 64 || path == "" {
			return nil, fmt.Errorf("upstream.sha256 の行が「<sha256>  <パス>」の形ではありません: %q", line)
		}
		if _, dup := sums[path]; dup {
			return nil, fmt.Errorf("upstream.sha256 に同じパスが 2 回あります: %s", path)
		}
		sums[path] = sum
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("upstream.sha256 を読めません: %w", err)
	}
	for _, p := range UpstreamFiles {
		if _, ok := sums[p]; !ok {
			return nil, fmt.Errorf("upstream.sha256 に %s がありません (make pin-upstream)", p)
		}
	}
	if len(sums) != len(UpstreamFiles) {
		return nil, fmt.Errorf("upstream.sha256 に UpstreamFiles に無いパスがあります (%d 行、期待 %d 行)", len(sums), len(UpstreamFiles))
	}
	return sums, nil
}

// manifestLines は parseManifest の逆で、パス順の行を返す。
func manifestLines(sums map[string]string) string {
	paths := make([]string, 0, len(sums))
	for p := range sums {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(sums[p] + "  " + p + "\n")
	}
	return b.String()
}

// Tree は取ってきた本家のファイルの置き場 (<cache>/<Commit>/)。中のファイルには手を入れない。
type Tree struct{ Root string }

// Path は木の中のパスを実パスにする。
func (t Tree) Path(rel string) string { return filepath.Join(t.Root, filepath.FromSlash(rel)) }

// DefaultCacheRoot は取ってきたファイルを置く場所 (${XDG_CACHE_HOME:-~/.cache}/yomiyasu-oracle)。
func DefaultCacheRoot() (string, error) {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "yomiyasu-oracle"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("キャッシュの場所を決められません: %w", err)
	}
	return filepath.Join(home, ".cache", "yomiyasu-oracle"), nil
}
