package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// コミット境界の識別は人間向け出力の正規表現ではなく制御文字レコードで行う (issue の設計)。
// %x1e = record separator (コミット先頭)、%x1f = field separator。
// --stat / -p の本文は最後の %x1f 以降に続き、次の %x1e までがそのコミットのレコード。
// %B (フルメッセージ) はセパレータを含みうる唯一のフィールドなので最後に置く。コミット
// メッセージに \x1f/\x1e が入っている病的なケースは解析エラーとして明示的に落ちる。
const (
	recordSep    = "\x1e"
	fieldSep     = "\x1f"
	prettyFormat = "--pretty=format:%x1e%H%x1f%h%x1f%s%x1f%an%x1f%ae%x1f%ad%x1f%ar%x1f%D%x1f%B%x1f"
	numFields    = 9
)

// Commit は git log 1 レコード分。
type Commit struct {
	SHA         string
	ShortSHA    string
	Subject     string
	Author      string
	AuthorEmail string
	Date        string // %ad (git 既定の絶対日時。例: "Thu Jul 16 19:12:47 2026 +0900")
	RelDate     string // %ar (相対日時。--oneline 表示で使う)
	Decoration  string // %D (例: "HEAD -> master, origin/master")、無ければ空
	Message     string // %B (subject を含むフルメッセージ)
	Body        string // --stat / -p の本文 (ヘッダー行以外)。無ければ空
}

// GitExitError は git コマンド自体の失敗。stderr と終了コードをそのまま伝播する。
type GitExitError struct {
	Stderr string
	Code   int
}

func (e *GitExitError) Error() string { return e.Stderr }

// runGit は git を実行して stdout を返す。失敗時は *GitExitError。
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
		return "", &GitExitError{Stderr: stderr.String(), Code: code}
	}
	return stdout.String(), nil
}

// BuildLogArgs は allowlist 済みオプションから git log の引数列を組み立てる。
// colored は --stat / -p の本文 (git が着色する部分) にのみ効く。ヘッダー行の色は
// render 側で自前で付けるため、format 文字列には色コードを入れない。
func BuildLogArgs(opts *Options, colored bool) []string {
	args := []string{"log", prettyFormat}
	args = append(args, fmt.Sprintf("--max-count=%d", opts.MaxCount))
	if colored {
		args = append(args, "--color=always")
	} else {
		args = append(args, "--color=never")
	}
	if opts.Stat {
		args = append(args, "--stat")
	}
	if opts.Patch {
		args = append(args, "--patch")
	}
	args = append(args, opts.Revs...)
	if len(opts.Paths) > 0 {
		args = append(args, "--")
		args = append(args, opts.Paths...)
	}
	return args
}

// ParseLog は prettyFormat 付き git log の出力をコミット列へ解析する。
func ParseLog(out string) ([]Commit, error) {
	if out == "" {
		return nil, nil
	}
	var commits []Commit
	for rec := range strings.SplitSeq(out, recordSep) {
		if rec == "" {
			continue // 先頭レコード前の空文字列
		}
		parts := strings.SplitN(rec, fieldSep, numFields+1)
		if len(parts) != numFields+1 {
			return nil, fmt.Errorf("glog: git log の出力を解析できません (フィールド数 %d)", len(parts))
		}
		body := strings.TrimPrefix(parts[9], "\n")
		body = strings.TrimRight(body, "\n")
		commits = append(commits, Commit{
			SHA:         parts[0],
			ShortSHA:    parts[1],
			Subject:     parts[2],
			Author:      parts[3],
			AuthorEmail: parts[4],
			Date:        parts[5],
			RelDate:     parts[6],
			Decoration:  parts[7],
			Message:     strings.TrimRight(parts[8], "\n"),
			Body:        body,
		})
	}
	return commits, nil
}

// LoadCommits は git log を実行して解析まで行う。
func LoadCommits(opts *Options, colored bool) ([]Commit, error) {
	out, err := runGit(BuildLogArgs(opts, colored)...)
	if err != nil {
		return nil, err
	}
	return ParseLog(out)
}

// LoadHeadCommit は --cached モード用に HEAD 1 件を取得する。
func LoadHeadCommit() (*Commit, error) {
	out, err := runGit("log", prettyFormat, "--max-count=1", "--color=never")
	if err != nil {
		return nil, err
	}
	commits, err := ParseLog(out)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, errors.New("glog: HEAD コミットがありません")
	}
	return &commits[0], nil
}

// LoadStagedDiff は --cached モードの本文 (git diff --cached) を取得する。
// フラグ未指定時は --stat 相当を出す (staged 変更の一覧が目的で、既定でフル patch は過剰なため)。
func LoadStagedDiff(opts *Options, colored bool) (string, error) {
	args := []string{"diff", "--cached"}
	if colored {
		args = append(args, "--color=always")
	} else {
		args = append(args, "--color=never")
	}
	if opts.Patch {
		// -p: フル patch (git diff --cached の既定出力)
	} else {
		args = append(args, "--stat")
	}
	out, err := runGit(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}
