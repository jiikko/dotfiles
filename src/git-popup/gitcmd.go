package main

import (
	"bytes"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

const logFormat = "--pretty=format:%H%x1f%h%x1f%s%x1e"

// Commit は一覧表示に必要な情報と、将来の API 呼び出し用の full SHA を持つ。
type Commit struct {
	SHA      string
	ShortSHA string
	Subject  string
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

// pushedKeep は一覧に残す push 済みコミットの件数。ユースケースは「どこまで push して
// いて、push 済み先頭の CI がこけていないか」の確認なので、履歴を深く遡る必要はない
// (ユーザー判断 2026-07-19: 30 件以上見たいユースケースはない)。
const pushedKeep = 5

// loadCommits は「未 push 全部 + push 済み pushedKeep 件」に絞って取得する。
// upstream が無い (未 push 判定不能) ときは従来どおり無制限に degrade する。
func loadCommits() ([]Commit, error) {
	args := []string{"log", logFormat, "--color=never"}
	if unpushed := loadUnpushed(); unpushed != nil {
		args = append(args, "-n", strconv.Itoa(len(unpushed)+pushedKeep))
	}
	out, err := runGit(args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out)
}

// loadBranchLine は左ペインのヘッダ行 (例: "master → origin/master (ahead 2)")。
// upstream が無ければブランチ名だけに degrade する。
func loadBranchLine() string {
	branch, err := runGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch = strings.TrimSpace(branch)
	up, err := runGit("rev-parse", "--abbrev-ref", "@{upstream}")
	if err != nil {
		return branch
	}
	up = strings.TrimSpace(up)
	line := branch + " → " + up
	// --left-right --count で "behind<TAB>ahead" (upstream...HEAD の左右コミット数)
	if out, err := runGit("rev-list", "--left-right", "--count", up+"...HEAD"); err == nil {
		if f := strings.Fields(out); len(f) == 2 {
			var parts []string
			if f[1] != "0" {
				parts = append(parts, "ahead "+f[1])
			}
			if f[0] != "0" {
				parts = append(parts, "behind "+f[0])
			}
			if len(parts) > 0 {
				line += " (" + strings.Join(parts, ", ") + ")"
			}
		}
	}
	return line
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
	// --color=never で受けて highlightDiff で構造色 + シンタックスハイライトを付ける。
	out, err := runGit("show", "--color=never", "--no-ext-diff", sha)
	if err != nil {
		return "", err
	}
	return highlightDiffText(out), nil
}

func push() error {
	_, err := runGit("push")
	return err
}

// loadUnpushed は upstream に未 push のコミット SHA 集合を返す (@{upstream}..HEAD)。
// upstream が無い等で取得できなければ nil (= push 状態不明・色分けなし) に degrade する。
func loadUnpushed() map[string]bool {
	out, err := runGit("rev-list", "@{upstream}..HEAD")
	if err != nil {
		return nil
	}
	set := make(map[string]bool)
	for _, sha := range strings.Fields(out) {
		set[sha] = true
	}
	return set
}

