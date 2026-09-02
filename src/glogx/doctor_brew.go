package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"doctor/runner"
)

// brew doctor の出力転記 (issue 148 の 3 章、3 つ目のセクション。ユーザー要望 2026-09-02)。
//
// 判定は持たない。brew の出力を写すだけ (器が「自前の判定を持たない項目」も載せられるかの検証)。
// 実測 (Homebrew 6.0.20): 警告ありは stdout 空 / stderr に本文 / rc=1。先頭に定型の前置き 3 行。
// rc=1 は「警告あり」であって失敗ではないので、rc だけで色を決めない。

const brewDoctorTimeout = 60 * time.Second

// brewDoctorResult は 1 回の実行結果。Unavailable は brew 不在 / タイムアウト / 起動できず (= 診断できず)。
type brewDoctorResult struct {
	Warnings    []string // "Warning: ..." 見出しごとの本文 (前置きを落とし、空行で区切る)
	Clean       bool     // 警告なし (rc=0)
	Unavailable string   // 診断できなかった理由 ("" = 診断できた)
}

// runBrewDoctor は brew doctor を 1 回だけ ctx 付きで実行する。修復コマンドの提示だけで、実行はしない。
func runBrewDoctor(ctx context.Context, run runner.Runner) brewDoctorResult {
	stdout, stderr, rc, err := runner.WithTimeout(ctx, run, brewDoctorTimeout, "brew", "doctor")
	if err != nil {
		return brewDoctorResult{Unavailable: err.Error()}
	}
	return parseBrewDoctor(stdout, stderr, rc)
}

// parseBrewDoctor は出力を見出し単位にまとめる。rc=0 は警告なし。それ以外は本文を写す
// (rc が 0 でも stderr に Warning があれば写す。rc だけで決めない)。
func parseBrewDoctor(stdout, stderr string, rc int) brewDoctorResult {
	body := stderr
	if strings.TrimSpace(body) == "" {
		body = stdout
	}
	var blocks []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "Please note that these warnings"),
			strings.HasPrefix(line, "with debugging if you file an issue"),
			strings.HasPrefix(line, "working fine: please don't worry"):
			continue // 定型の前置き
		case strings.HasPrefix(line, "Warning:"):
			flush()
			cur = append(cur, line)
		case strings.TrimSpace(line) == "":
			flush()
		default:
			cur = append(cur, line)
		}
	}
	flush()
	if rc == 0 {
		// rc=0 の本文 ("Your system is ready to brew.") は警告ではない。Warning: で始まる塊だけを残す
		var warns []string
		for _, b := range blocks {
			if strings.HasPrefix(b, "Warning:") {
				warns = append(warns, b)
			}
		}
		if len(warns) == 0 {
			return brewDoctorResult{Clean: true}
		}
		return brewDoctorResult{Warnings: warns}
	}
	if rc != 0 && len(blocks) == 0 {
		// 非 0 なのに本文が無い = brew 自体の失敗 (診断できず)。0 件に畳まない
		return brewDoctorResult{Unavailable: "brew doctor が exit " + strconv.Itoa(rc) + " で本文なし"}
	}
	return brewDoctorResult{Warnings: blocks}
}
