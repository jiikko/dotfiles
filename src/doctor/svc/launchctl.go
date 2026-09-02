package svc

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"doctor/runner"
)

// launchctlTimeout は launchctl 1 回あたりの上限。print は候補に絞ってから呼ぶので回数は少ない。
const launchctlTimeout = 10 * time.Second

// jobStatus は `launchctl list` の 1 行。
type jobStatus struct {
	PID int // 0 = 動いていない ("-")
	// Exit は Status 列。負値はシグナル停止、正値はプロセス自身の終了コード。
	// 負値から異常かは判別できないので候補にしない (issue 148 の「最大の罠」)。
	Exit    int
	HasExit bool // "-" なら false
}

// parseLaunchctlList は `launchctl list` の出力 (PID\tStatus\tLabel) を label で引ける形にする。
func parseLaunchctlList(out string) map[string]jobStatus {
	m := map[string]jobStatus{}
	for i, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || (i == 0 && f[0] == "PID") {
			continue
		}
		st := jobStatus{}
		if pid, err := strconv.Atoi(f[0]); err == nil {
			st.PID = pid
		}
		if f[1] != "-" {
			if n, err := strconv.Atoi(f[1]); err == nil {
				st.Exit, st.HasExit = n, true
			}
		}
		m[f[2]] = st
	}
	return m
}

// launchctlList は `launchctl list` を 1 回だけ呼ぶ。失敗は「診断できず」(呼び出し側が B を
// 評価せず、その旨を表示する)。候補 0 件には畳まない。
func launchctlList(ctx context.Context, run runner.Runner) (map[string]jobStatus, error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, launchctlTimeout, "launchctl", "list")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("launchctl list: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	return parseLaunchctlList(out), nil
}

// printInfo は `launchctl print <domain>/<label>` から拾う補助情報。形式は非公開なので、
// 取れなければゼロ値のまま (診断は落とさない)。
type printInfo struct {
	PenaltyBox bool
	Properties string
}

func parseLaunchctlPrint(out string) printInfo {
	info := printInfo{}
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "properties = ") {
			info.Properties = strings.TrimPrefix(t, "properties = ")
			info.PenaltyBox = strings.Contains(info.Properties, "penalty box")
		}
	}
	return info
}

// launchctlPrint は候補に絞ってから呼ぶ (全ラベルに対して呼ばない)。失敗は無視して続行する
// (補助情報なので、無くても A / B の判定は成立する)。
func launchctlPrint(ctx context.Context, run runner.Runner, target string) printInfo {
	out, _, rc, err := runner.WithTimeout(ctx, run, launchctlTimeout, "launchctl", "print", target)
	if err != nil || rc != 0 {
		return printInfo{}
	}
	return parseLaunchctlPrint(out)
}
