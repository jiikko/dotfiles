package testenv

import (
	"errors"
	"glogx/internal/extcmd"
	"go/ast"
	"os"
	"os/exec"
	"path/filepath"
	"ratelimit/usage"
	"strings"
	"testing"
	"time"
)

// 複数のパッケージのテストが同じ形で使っていた部品 (写しをここへ寄せた)。

func AssertNoTermControl(t *testing.T, what, got string) {
	t.Helper()
	if strings.ContainsAny(got, "\x1b\a\u009b\u202e") {
		t.Errorf("%s に制御シーケンスが残っている: %q", what, got)
	}
	if !strings.Contains(got, "evil") {
		t.Errorf("%s の本文まで落ちている: %q", what, got)
	}
}

// CommitLines は file.txt を lines 行にして subject でコミットする (コミットを 1 本足す)。
func CommitLines(t *testing.T, dir string, lines int, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(strings.Repeat("line\n", lines)), 0o644); err != nil {
		t.Fatal(err)
	}
	GitInRepo(t, dir, "add", ".")
	GitInRepo(t, dir, "commit", "-q", "-m", subject)
}

// GitInRepo は dir を作業ディレクトリとして git を走らせる (author / committer を固定)。
func GitInRepo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // subproc: no-waitdelay — テストの fixture の repo への短い git。パイプを子へ渡さない
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// HeadSHA は現在の HEAD の完全 SHA を返すテストヘルパー。
func HeadSHA(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "HEAD").Output() // subproc: no-waitdelay — テストの fixture の repo への短い git
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// NewTempRepo はコミット付きの一時リポジトリを作り、そのディレクトリへ chdir する。
func NewTempRepo(t *testing.T, subjects []string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	GitInRepo(t, dir, "init", "-q", "-b", "main")
	for i, subject := range subjects {
		CommitLines(t, dir, i+1, subject)
	}
	return dir
}

// MaxRowShift は窓の各行の右へのずれ (先頭の空白幅) の最大値。空行は演出で消えた行なので除く。
func MaxRowShift(window []string) int {
	shift := 0
	for _, ln := range window {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		shift = max(shift, len(ln)-len(strings.TrimLeft(ln, " ")))
	}
	return shift
}

// RepoTmpDir は使い捨て repo の置き場 (dotfiles の ./tmp)。
//
// 🚨 無ければ作る。tmp/ は gitignore なので**新品チェックアウトには存在しない**: 開発マシンには
// 常にあるため絶対に再現しない条件で、CI だけが落ちた (run 30823977760 "stat ../../tmp: no such
// file or directory")。相対パスで決め打ちせず repo root から引くのは、テストが t.Chdir で cwd を
// 移すため (ヘルパーの呼び出し順に依存させない)。
func RepoTmpDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output() // subproc: no-waitdelay — テストの一時 dir を repo の外に取るための短い git
	if err != nil {
		t.Fatalf("repo root を解決できない: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// StatusRec は `git status --porcelain -z` の 1 レコード (NUL 終端) を組む。
func StatusRec(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
		b.WriteByte(0)
	}
	return b.String()
}

func RatelimitTestSnap() *usage.Snapshot {
	now := time.Date(2026, 8, 31, 22, 14, 0, 0, time.Local)
	return &usage.Snapshot{
		Version: "2.1.216",
		Windows: []usage.Window{
			{Label: "5h", Percent: 62, ResetAt: now.Add(108 * time.Minute), WindowMins: 300},
			{Label: "7d", Percent: 78, ResetAt: now.Add(3420 * time.Minute), WindowMins: 7 * 24 * 60},
		},
	}
}

func StubLookPath(t *testing.T, available map[string]string) {
	t.Helper()
	orig := extcmd.LookPath
	extcmd.LookPath = func(name string) (string, error) {
		if p, ok := available[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { extcmd.LookPath = orig })
}

// UsageSnapFixture は本物の /usage 出力を usage.Parse に通して作る。手組みの Snapshot だと
// 「実際に Fetch が返す形」と食い違っても気づけない: usage.Snapshot / Window は json タグを
// 持たずフィールド名で符号化され、ResetAt は time.Time。ここが往復で壊れると症状は
// 「永久に cache miss = 毎起動 claude を起こし続ける」で、どこも失敗しないまま劣化する。
func UsageSnapFixture(t *testing.T) *usage.Snapshot {
	t.Helper()
	const realResult = `You are currently using your subscription to power your Claude Code usage

Current session: 2% used · resets Jul 22 at 3:09am (Asia/Tokyo)
Current week (all models): 29% used · resets Jul 24 at 8am (Asia/Tokyo)
Current week (Fable): 48% used · resets Jul 24 at 8am (Asia/Tokyo)

What's contributing to your limits usage?
Last 24h · 875 requests · 7 sessions`
	snap, err := usage.Parse(realResult, time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("fixture の Parse に失敗: %v", err)
	}
	snap.Version = "2.1.216"
	return snap
}

// RecvTypeName は型の式から型の名前を取る (`*T` / `T[K]` / `(T)` も T)。取れなければ ""。
func RecvTypeName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}
