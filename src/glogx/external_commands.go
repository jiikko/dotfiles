package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"glogx/usage"
)

// 外部プロセス (git / tmux / claude / ブラウザ / クリップボード) を叩くラッパー群。
// browseModel の状態には一切触れない (結合ゼロ) ため、Bubble Tea の状態機械本体
// (tui.go) から分離している。多くは `var f = func(...)` の形でテストの差し替え点になる。

// noPromptGitCmd は remote に触る git (push/pull) 用のコマンドを組む。GIT_TERMINAL_PROMPT=0
// で「認証情報が要るのに helper が無い」場合に /dev/tty へ対話プロンプトを出させず即エラーに
// する: bubbletea が同じ端末を raw mode で握っているため、git が tty を奪うと表示が壊れ入力
// 挙動が未定義になる (対話認証は TUI の外でやるべき作業)。
//
// ⚠️ ctx には deadline を付けない (レビュー K2: 正当な巨大 push が遅い回線で timeout 中断される
// 方が push 失敗として有害)。ただし cancel は張る: quit (Ctrl-C) 時に走行中の push/pull を
// cancel できないと、ネットワーク stall 中に抜けたとき git 子プロセスが孤児化して事実上無期限に
// 居残る (leak 監査 2026-07-23)。呼び出し側 (actionModal) が deadline 無しの cancel context を
// 渡し、quit からのみ cancel する。
func noPromptGitCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd
}

// runGitPush はテストで実 push しないための差し替え点。ctx は quit 中断用 (deadline 無し)。
var runGitPush = func(ctx context.Context) error {
	out, err := noPromptGitCmd(ctx, "push").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// runGitPullRebase はテストで実 pull しないための差し替え点。conflict で rebase が
// 途中停止したら自動で abort して pull 前の状態へ戻す (TUI 内に「rebase 進行中」の
// 壊れた状態を残さない。解決が必要な conflict はシェルでやるべき作業)。
//
// git pull --rebase は tracked の未コミット変更 (staged/unstaged) があると
// "cannot pull with rebase: You have unstaged changes" で拒否する (untracked は無害)。
// 素の git エラーは分かりにくいので、事前に検知して glogx らしい案内を返す (自動 stash は
// しない: 復元 pop の衝突で working tree に壊れた状態を残しうるため。ユーザー選定 2026-07-22)。
var runGitPullRebase = func(ctx context.Context) error {
	if st, stErr := noPromptGitCmd(ctx, "status", "--porcelain").Output(); stErr == nil && pullBlockedByDirtyTree(string(st)) {
		return errors.New("未コミットの変更があるため pull (--rebase) できません。commit か stash してから u で再度 pull してください")
	}
	out, err := noPromptGitCmd(ctx, "pull", "--rebase").CombinedOutput()
	if err == nil {
		return nil
	}
	gitDir, dirErr := exec.Command("git", "rev-parse", "--git-dir").Output()
	if dirErr == nil {
		dir := strings.TrimSpace(string(gitDir))
		if _, statErr := os.Stat(dir + "/rebase-merge"); statErr == nil {
			return abortRebase(out)
		}
		if _, statErr := os.Stat(dir + "/rebase-apply"); statErr == nil {
			return abortRebase(out)
		}
	}
	return fmt.Errorf("%s", strings.TrimSpace(string(out)))
}

// abortRebase は途中停止した rebase を中断し、結果に応じたメッセージを返す。
// abort 自体が失敗したら「元に戻した」とは主張せず (壊れた状態が残っている可能性がある)、
// 手動復旧を促す。out は pull --rebase の出力 (conflict 内容の提示用)。
func abortRebase(out []byte) error {
	conflict := firstLine(strings.TrimSpace(string(out)))
	if err := exec.Command("git", "rebase", "--abort").Run(); err != nil {
		return fmt.Errorf("conflict のため rebase 中断を試みましたが失敗しました。手動で `git rebase --abort` してください: %s", conflict)
	}
	return fmt.Errorf("conflict のため rebase を中断して元に戻しました: %s", conflict)
}

// pullBlockedByDirtyTree は git status --porcelain の出力に rebase を阻む tracked 変更
// (staged / unstaged) が含まれるかを返す純関数。untracked (先頭 "??") は rebase を阻まない
// ため無視する。git status は内部で index を refresh するので stat-dirty の偽陽性は出ない。
func pullBlockedByDirtyTree(porcelain string) bool {
	for _, line := range strings.Split(porcelain, "\n") {
		if line == "" || strings.HasPrefix(line, "??") {
			continue
		}
		return true
	}
	return false
}

// updateTimeout は claude update の上限。通常の自己更新 (npm/ダウンロード) はこれより十分速く
// 完了するため、到達したら更新が本当にハングしている合図。この上限が無いと updating 中は
// q/Ctrl-C を握りつぶす (handleKey の updating ガード) 設計上、無限ハング時に端末を外部から
// kill するしか脱出できず、子プロセスが孤児化し raw mode も残る。寛大な値で「更新中は中断させ
// ない」意図を保ちつつ、病的なハングだけを断ち切る。
const updateTimeout = 5 * time.Minute

// runClaudeUpdate はテストで実 update しないための差し替え点。update 前後の CLI バージョンを
// 挟んで取得し (何→何に変わったか表示するため)、CLI を自己更新する。remote に触るが git では
// ないので noPromptGitCmd は使わない (対話プロンプトは claude 側の責務)。updateTimeout で
// context を張り、無期限ブロックを防ぐ (超過時は updateMsg{err} 経由で updating が必ず解ける)。
var runClaudeUpdate = func() (before, after string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	before = usage.FetchVersion(ctx)
	out, e := exec.CommandContext(ctx, "claude", "update").CombinedOutput()
	if e != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return before, "", fmt.Errorf("claude update がタイムアウトしました (%s)", updateTimeout)
		}
		return before, "", fmt.Errorf("%s", lastLine(strings.TrimSpace(string(out))))
	}
	after = usage.FetchVersion(ctx)
	return before, after, nil
}

// runJobRerun はテストで実 rerun しないための差し替え点 (本体は jobRerun)。
var runJobRerun = func(ctx context.Context, repo Repo, jobID int64) error {
	return jobRerun(ctx, ExecRunner, repo, jobID)
}

// jobRerun は失敗 job を GitHub Actions 上で再実行する (`gh run rerun --job <id>`)。
// run 全体でなく job 単位なのは、パネルのフォーカス単位が job であり run id を
// 保持していないため (issue 019)。認証は gh へ委譲。
func jobRerun(ctx context.Context, run CommandRunner, repo Repo, jobID int64) error {
	_, stderr, err := run(ctx, "gh", "run", "rerun",
		"--job", strconv.FormatInt(jobID, 10), "-R", repo.Owner+"/"+repo.Name)
	if err != nil {
		detail := lastLine(string(stderr))
		if detail == "" {
			detail = err.Error()
		}
		return errors.New(detail)
	}
	return nil
}

// loadTmuxPrefix は tmux サーバの現在の prefix を bubbletea キー表記で返す
// ("" = tmux 外 / 取得失敗 / 未対応表記)。tmux.conf のパースはしない: conf は分割・
// ライブ変更されうるため、サーバの現在値だけが真実 (show-options で聞く)。
var loadTmuxPrefix = func() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "show-options", "-g", "prefix").Output()
	if err != nil {
		return ""
	}
	return parseTmuxPrefix(strings.TrimSpace(string(out)))
}

// parseTmuxPrefix は `prefix C-t` 形式の出力を bubbletea 表記 ("ctrl+t") へ変換する。
// C-<英字> 以外 (M- 系や None 等) は誤爆判定できないので "" (機能オフ) に落とす。
func parseTmuxPrefix(out string) string {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return ""
	}
	p := fields[1]
	if len(p) == 3 && strings.HasPrefix(p, "C-") {
		return "ctrl+" + strings.ToLower(p[2:])
	}
	return ""
}

// openInBrowser はテストで実ブラウザを開かないための差し替え点。
var openInBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}

// copyToClipboard はテストで実クリップボードを触らないための差し替え点。
// OS のクリップボードコマンド (pbcopy/xclip) を真実とし、tmux 内では tmux バッファへも
// 積む (tmux paste 用のおまけ・best effort)。本家 glog は load-buffer -w の成功 (exit 0)
// を「システム側にも届いた」とみなすが、-w の実体は OSC52 転送で、外側端末が OSC52 を
// 解釈しなければ exit 0 のままクリップボードに入らない (glogx で実測 2026-07-19)。
var copyToClipboard = func(text string) error {
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "load-buffer", "-w", "-")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run() // 失敗しても OS クリップボードが本命なので無視
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		cmd = exec.Command("xclip", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
