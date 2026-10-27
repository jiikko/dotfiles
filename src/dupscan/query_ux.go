package main

import (
	"dupscan/internal/jsonout"
	"fmt"
	"io"
	"path/filepath"

	"dupscan/internal/repo"
)

type queryUX struct {
	Approximation string
	ParseErrors   int
	Unsupported   bool
	Truncated     bool
}

func (s queryUX) suffix() string {
	omitted, unsupported := "なし", "なし"
	if s.Truncated {
		omitted = "あり"
	}
	if s.Unsupported {
		unsupported = "あり"
	}
	return fmt.Sprintf(" / 近似: %s / 省略: %s / 構文解析失敗: %d ファイル / 未対応言語: %s", s.Approximation, omitted, s.ParseErrors, unsupported)
}

type nextAction struct {
	Kind   string   `json:"kind"`
	Reason string   `json:"reason"`
	Argv   []string `json:"argv"`
}

func queryNextAction(sc repo.Scope, command, query, reason string, flags ...string) nextAction {
	args := append([]string{}, flags...)
	if command == "index" {
		args = append(args, query, "--")
	} else {
		args = append(args, "--", query)
	}
	return scopedNextAction(sc, command, reason, args...)
}

func scopedNextAction(sc repo.Scope, command, reason string, args ...string) nextAction {
	argv := append([]string{"dupscan", command}, args...)
	if sc.Whole() {
		argv = append(argv, sc.Root)
	} else {
		for _, prefix := range sc.Prefixes {
			argv = append(argv, filepath.Join(sc.Root, prefix))
		}
	}
	return nextAction{Kind: command, Reason: reason, Argv: argv}
}

func writeNextAction(w io.Writer, action nextAction) error {
	encoded, err := jsonout.Line(action)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "next_action: %s\n", encoded)
	return err
}
