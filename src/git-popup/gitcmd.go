package main

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

const logFormat = "--pretty=format:%H%x1f%h%x1f%s%x1e"

// Commit は一覧表示に必要な情報と、将来の API 呼び出し用の full SHA を持つ。
type Commit struct {
	SHA      string
	ShortSHA string
	Subject  string
}

type Change struct {
	Index    byte
	Worktree byte
	Path     string
	OldPath  string // rename/copy 元パス (無ければ "")
}

type gitError struct {
	Stderr string
	Code   int
}

func (e *gitError) Error() string {
	return strings.TrimSpace(e.Stderr)
}

func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		code := 1
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		return "", &gitError{Stderr: stderr.String(), Code: code}
	}
	return stdout.String(), nil
}

func loadCommits() ([]Commit, error) {
	out, err := runGit("log", logFormat, "--color=never")
	if err != nil {
		return nil, err
	}
	return parseLog(out)
}

func parseLog(out string) ([]Commit, error) {
	if out == "" {
		return nil, nil
	}
	commits := make([]Commit, 0)
	for record := range strings.SplitSeq(out, "\x1e") {
		// git log は各レコード (%x1e 終端) の後ろにデフォルトの改行を足すため、
		// %x1e で分割すると 2 件目以降の先頭に \n が残る。これを落とさないと SHA に
		// 改行が混入し、後段の graphql oid 文字列が malformed になる (実測)。
		record = strings.Trim(record, "\n\r")
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x1f")
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" {
			return nil, errors.New("git-popup: git log の出力を解析できません")
		}
		commits = append(commits, Commit{SHA: fields[0], ShortSHA: fields[1], Subject: fields[2]})
	}
	return commits, nil
}

func loadPreview(sha string) (string, error) {
	return runGit("show", "--color=always", "--no-ext-diff", sha)
}

func push() error {
	_, err := runGit("push")
	return err
}

func loadChanges() ([]Change, error) {
	// -z (NUL 区切り) を使う。理由: (1) パスの quoting が無効化され `\"`/octal escape や
	// 空白入りパスをデコード不要で正確に扱える (2) rename/copy は元パスが後続の NUL
	// トークンで来るため新旧両方を取れる (3) ` -> ` を含むパスの誤認が起きない。
	// 色は git に付けさせず (porcelain は無色) View 側で staged=緑/unstaged=赤 を自前描画する。
	out, err := runGit("status", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parseStatus(out)
}

func parseStatus(out string) ([]Change, error) {
	var changes []Change
	tokens := strings.Split(out, "\x00")
	for i := 0; i < len(tokens); i++ {
		entry := tokens[i]
		if entry == "" {
			continue
		}
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, errors.New("git-popup: git status の出力を解析できません")
		}
		change := Change{Index: entry[0], Worktree: entry[1], Path: entry[3:]}
		// rename/copy (R/C) は -z では元パスが次の NUL トークンで来る
		if change.Index == 'R' || change.Index == 'C' || change.Worktree == 'R' || change.Worktree == 'C' {
			if i+1 < len(tokens) {
				i++
				change.OldPath = tokens[i]
			}
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func stageChange(change Change) error {
	args := toggleGitArgs(change)
	_, err := runGit(args...)
	return err
}

func toggleGitArgs(change Change) []string {
	if change.Worktree == ' ' {
		// staged のみ → unstage。rename/copy は新旧両パスを対象にしないと片側だけ残る。
		args := []string{"restore", "--staged", "--", change.Path}
		if change.OldPath != "" {
			args = append(args, change.OldPath)
		}
		return args
	}
	return []string{"add", "--", change.Path}
}

func stageAll() error {
	_, err := runGit("add", "-A")
	return err
}

func commitChanges(message string) error {
	_, err := runGit("commit", "-m", message)
	return err
}

func loadChangePreview(change Change) (string, error) {
	if change.Index == '?' {
		return runGitNoIndex(change.Path)
	}
	var b strings.Builder
	if change.Index != ' ' {
		b.WriteString("\x1b[2m── staged ──\x1b[0m\n")
		text, err := runGit("diff", "--color=always", "--no-ext-diff", "--cached", "--", change.Path)
		if err != nil {
			return "", err
		}
		b.WriteString(text)
	}
	if change.Worktree != ' ' {
		b.WriteString("\x1b[2m── unstaged ──\x1b[0m\n")
		text, err := runGit("diff", "--color=always", "--no-ext-diff", "--", change.Path)
		if err != nil {
			return "", err
		}
		b.WriteString(text)
	}
	return b.String(), nil
}

func runGitNoIndex(path string) (string, error) {
	cmd := exec.Command("git", "diff", "--color=always", "--no-ext-diff", "--no-index", "--", "/dev/null", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return stdout.String(), nil
		}
		code := 1
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		return "", &gitError{Stderr: stderr.String(), Code: code}
	}
	return stdout.String(), nil
}
