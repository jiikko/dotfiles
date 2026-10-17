package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 実 git を相手にした破壊操作の検証。
//
// 🚨 このファイルだけ stub を使わない。理由: 守りたい不変条件が「pathspec の意味」だからで、
// スタブでは pathspec を git が**どう解釈するか**を検証できない (呼び出し引数の一致しか見られない)。
// 実際にこの層でバグを出している (2026-08-03: cwd 相対のパスを渡していて、サブディレクトリから
// 起動すると diff が空・add が失敗・clean が無言で何もしない)。
//
// 使い捨ての repo を ./tmp 配下に作って走らせるので、実ユーザーのツリーには触らない。

// repoTmpDir は使い捨て repo の置き場 (dotfiles の ./tmp)。
//
// 🚨 無ければ作る。tmp/ は gitignore なので**新品チェックアウトには存在しない**: 開発マシンには
// 常にあるため絶対に再現しない条件で、CI だけが落ちた (run 30823977760 "stat ../../tmp: no such
// file or directory")。相対パスで決め打ちせず repo root から引くのは、テストが t.Chdir で cwd を
// 移すため (ヘルパーの呼び出し順に依存させない)。
func repoTmpDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("repo root を解決できない: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}
