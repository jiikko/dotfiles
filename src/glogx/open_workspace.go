package main

// git log 一覧から nvim / ファイラーを repo root で起動する (e / E キー)。
// 起動は runEditorCmd (tea.ExecProcess) 経由: TUI を suspend して端末を明け渡し、
// 終了で glogx へ復帰する。C-g の tmux popup 内でもそのまま popup 内で開く (実 pty)。
// 復帰後の取り直しはしない: 編集で履歴が変わる操作 (commit 等) は status viewer /
// pull 系の既存導線が担い、ここは「ちょっとファイルを見る・直す」用の脇道のため。

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// filerCandidates は E で起動するファイラーの探索順 (先勝ち)。
var filerCandidates = []string{"yazi", "ranger", "lf", "nnn", "vifm"}

// lookPathFn はテストで PATH 探索を差し替えるための注入点。
var lookPathFn = exec.LookPath

// repoRoot は repo root (rev-parse --show-toplevel)。取れないとき (裸 repo 等) は
// カレントディレクトリのまま開く。
func repoRoot() string {
	if root, err := runGitTimeout("rev-parse", "--show-toplevel"); err == nil {
		if r := strings.TrimSpace(root); r != "" {
			return r
		}
	}
	return "."
}

// openEditorAtRoot は nvim を repo root で開く (e)。`nvim .` なのでファイラー系
// プラグイン (oil / netrw) がそのまま入口になる。
//
// ⚠️ ここは $EDITOR を見ずに nvim 固定にする: 引数がファイルでなくディレクトリで、
// 「ディレクトリを開くとファイラーになる」のは nvim/vim 固有の機能 (nano . 等は失敗する)。
// 任意のエディタで開きたくなったら、それは editorCommand ではなく隣の openFilerAtRoot
// (filerCandidates) の系統。実ファイルを開く経路の $EDITOR 対応は editorCommand の doc を参照。
func (m *browseModel) openEditorAtRoot() tea.Cmd {
	cmd := exec.Command("nvim", ".")
	cmd.Dir = repoRoot()
	return runEditorCmd(cmd)
}

// openFilerAtRoot は最初に見つかったファイラーを repo root で開く (E)。
// 1 つも無ければ理由をトーストで案内する (no-op 通知の既定経路)。
func (m *browseModel) openFilerAtRoot() tea.Cmd {
	for _, name := range filerCandidates {
		path, err := lookPathFn(name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path)
		cmd.Dir = repoRoot()
		return runEditorCmd(cmd)
	}
	m.toast.show("ファイラーが見つかりません ("+strings.Join(filerCandidates, "/")+")", false)
	return m.maybeTick()
}
