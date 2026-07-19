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
