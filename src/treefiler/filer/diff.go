package filer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"subproc"
)

// diff.go はタイルの `d` (git の diff と本体の切り替え。spec §8.3)。diff を出せるのは git の状態が
// staged / modified / conflict のファイルだけ (spec §5.3 の「>= Staged」)。取得は裏の goroutine で行い、終わるまで読み込み中を出す。

// gitDiffCommand は repo の根 top で rel の diff を取る (テストで差し替える)。
// --color=never で受けて、色は tuikit/highlight.Diff が付ける。
var gitDiffCommand = func(ctx context.Context, top, rel string, unborn bool) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	base := "HEAD"
	if unborn {
		var err error
		if base, err = emptyTree(ctx, top); err != nil {
			return nil, err
		}
	}
	// GitRepoCommand: 作業ツリーとの diff は clean filter を通すので、repo のローカルの設定の filter を止める (issue 732)
	cmd, err := subproc.GitRepoCommand(ctx, top, gitDiffArgs(rel, base)...)
	if err != nil {
		return nil, err
	}
	cmd.Stdin = nil
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b, rerr := io.ReadAll(io.LimitReader(out, diffMaxBytes)) // 巨大な diff (生成物・lock ファイル) を全部は読まない
	if len(b) == diffMaxBytes {
		cancel() // 残りは読まない (読まずに Wait すると git がパイプで詰まる)
		_ = cmd.Wait()
		return b, nil
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	return b, rerr
}

// gitHeadExists は top の repo に HEAD の commit があるか (差し替え口。テストが偽の diff と組で替える)。
var gitHeadExists = func(ctx context.Context, top string) (bool, error) {
	cmd := subproc.GitCommand(ctx, "-C", top, "rev-parse", "--verify", "-q", "HEAD")
	cmd.Stdin = nil
	err := cmd.Run()
	var ee interface{ ExitCode() int } // *exec.ExitError (filer は os/exec を import しない。exec_boundary_test)
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &ee) && ee.ExitCode() == 1: // --verify -q: 無ければ rc=1 で何も出さない
		return false, nil
	}
	return false, err
}

// gitDiffArgs は diff の引数。
// 🚨 --literal-pathspecs: `--` の後ろも pathspec なので、名前の * ? [ が glob になり、他のファイルの diff が混ざる
// (`*` という名前で repo 全体が出た。レビューで再現 2026-10-09)。--no-ext-diff: diff.external (difftastic 等) の出力は
// unified でなく、tuikit/highlight.Diff が読めない。--ignore-submodules=dirty: submodule の中の filter を走らせない (fetchRepo の status と同じ。issue 732)
// base は比べる先 (HEAD か、HEAD が無い repo では空の tree。どちらも作業ツリーと比べるので、未 stage の変更も出る)。
func gitDiffArgs(rel, base string) []string {
	return []string{"--no-optional-locks", "--literal-pathspecs", "diff", "--no-ext-diff", "--no-textconv", "--color=never", "--ignore-submodules=dirty", base, "--", rel}
}

// emptyTree は top の repo の空の tree の名前 (object の形式 (SHA-1 / SHA-256) で違うので git に出させる。書き込みはしない)。
func emptyTree(ctx context.Context, top string) (string, error) {
	cmd := subproc.GitCommand(ctx, "-C", top, "hash-object", "-t", "tree", "--stdin")
	cmd.Stdin = nil // 空の入力
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// diffMaxLines は diff の行数の上限 (巨大な生成物の diff を全部抱えない)。
const diffMaxLines = 20000

// diffMaxBytes は diff を読む量の上限。
const diffMaxBytes = 8 << 20

type diffJob struct {
	mu     sync.Mutex
	done   bool
	lines  []string
	cancel context.CancelFunc // 本体へ戻る・取り直す・隣へ送るときに git を止める (d の連打で git を重ねない。issue 693)
}

func (j *diffJob) result() ([]string, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lines, j.done
}

// diffable は n の diff を出せるか。
func (m *Model) diffable(n *node) bool { return gitKinds[m.gitState(n)].diffable }

func startDiff(path string) *diffJob {
	ctx, cancel := context.WithCancel(context.Background())
	j := &diffJob{cancel: cancel}
	go func() {
		defer cancel()
		lines := fetchDiff(ctx, path)
		j.mu.Lock()
		defer j.mu.Unlock()
		j.lines, j.done = lines, true
	}()
	return j
}

func fetchDiff(parent context.Context, path string) []string {
	top := findRepoTop(filepath.Dir(path))
	rel, err := filepath.Rel(top, path)
	if top == "" || err != nil {
		return []string{"(git の repo の外です)"}
	}
	ctx, cancel := context.WithTimeout(parent, subproc.GitOpTimeout)
	defer cancel()
	out, err := gitDiffCommand(ctx, top, rel, false)
	// HEAD がまだ無い repo (最初の commit の前) だけ、空の tree と作業ツリーを比べる。🚨 失敗を全部「HEAD が無い」と読まない: HEAD が在るのに
	// timeout で失敗したときに別の差を出すと、未 stage の変更が隠れる (issue 731)。index との差 (--cached) にも落とさない: HEAD が無い repo でも
	// 未 stage の変更を隠す。空の tree との差は作業ツリーの中身を全部出すので、ref が壊れて「HEAD が無い」と見えた repo でも何も隠さない
	if err != nil {
		if exists, herr := gitHeadExists(ctx, top); herr == nil && !exists {
			out, err = gitDiffCommand(ctx, top, rel, true)
		}
	}
	if err != nil {
		return []string{"(diff を取れません: " + cleanLine(err.Error()) + ")"}
	}
	cut := len(out) >= diffMaxBytes // 上限で読むのをやめた
	if cut {
		if i := bytes.LastIndexByte(out, '\n'); i >= 0 {
			out = out[:i] // 途中で切れた最後の行は出さない
		}
	}
	out = bytes.TrimRight(out, "\n")
	if len(out) == 0 {
		return []string{"(HEAD との差分はありません)"}
	}
	raw := bytes.Split(out, []byte("\n"))
	if len(raw) > diffMaxLines {
		raw, cut = raw[:diffMaxLines], true
	}
	lines := make([]string, len(raw), len(raw)+1)
	for i, l := range raw {
		lines[i] = cleanLine(string(l))
	}
	if cut {
		lines = append(lines, "(長いので先頭の "+itoa(len(raw))+" 行だけ出しています)")
	}
	return lines
}
