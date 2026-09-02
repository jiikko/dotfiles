package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Env は展開に使う環境 (テストで偽 HOME / 偽 TMPDIR を差す)。
type Env struct {
	Home    string
	TmpDir  string
	Getenv  func(string) string
	AppDirs []string // orphan-container が .app を実走査するディレクトリ
}

func realEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, TmpDir: os.Getenv("TMPDIR"), Getenv: os.Getenv,
		AppDirs: []string{"/Applications", filepath.Join(home, "Applications")}}
}

// expand はテンプレート (~ / $TMPDIR) を展開して glob する。
// 変数が空なら error (空パス展開で `rm -rf /foo` が組み立たる事故の入口を塞ぐ。走査でも同じ規律)。
// glob の結果 0 件は空スライス (エラーではない)。
func expand(env Env, tmpl string) ([]string, error) {
	p := tmpl
	if strings.HasPrefix(p, "~/") || p == "~" {
		if env.Home == "" {
			return nil, errors.New("HOME が空です")
		}
		p = env.Home + p[1:]
	}
	if strings.Contains(p, "$TMPDIR") {
		if env.TmpDir == "" {
			return nil, errors.New("TMPDIR が空です")
		}
		p = strings.ReplaceAll(p, "$TMPDIR", strings.TrimRight(env.TmpDir, "/"))
	}
	if strings.Contains(p, "$") {
		return nil, fmt.Errorf("未対応の変数があります: %s", tmpl)
	}
	matches, err := filepath.Glob(p)
	if err != nil {
		return nil, fmt.Errorf("glob が不正: %w", err)
	}
	return matches, nil
}

// systemLinkPrefixes は macOS がルート直下に置く symlink。/var → /private/var 等。
// 「経路の途中に symlink があれば拒否」の規律にこれが引っかかると TMPDIR (/var/folders/...) が
// 全部拒否されるので、この 3 つだけは先に実体へ書き換える (他の symlink は拒否のまま)。
var systemLinkPrefixes = map[string]string{"/var": "/private/var", "/tmp": "/private/tmp", "/etc": "/private/etc"}

func normalizeSystemLinks(p string) string {
	for from, to := range systemLinkPrefixes {
		if p == from || strings.HasPrefix(p, from+"/") {
			return to + p[len(from):]
		}
	}
	return p
}

// minDepth は拒否する浅さ (/Users/<name> = 深さ 2 は拒否)。深さ 3 は HOME 直下で、ドットで始まる
// ツールの root (~/.goenv / ~/.npm) だけを許す。~/Library / ~/Documents のような HOME 直下の
// 通常ディレクトリは深さ 3 でも拒否する。
const minDepth = 3

// validateTarget は候補パスが「消してよい形」かを、削除の有無に関わらず走査の時点で確認する。
//
//   - 絶対パスで、.. を含まない (filepath.EvalSymlinks は使わない: 最終ターゲットまで解決して
//     リンク先を消す経路が生まれる)
//   - / , /Users, /Users/<name>, HOME そのもの、深さ minDepth 未満は問答無用で拒否
//   - 経路の途中 (親ディレクトリ) に symlink が挟まっていたら拒否 (Lstat で辿る)。対象自身が
//     symlink なのは可 (走査はリンクを辿らず、④ の削除もリンク自体だけを消す)
func validateTarget(env Env, p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("絶対パスでない: %s", p)
	}
	// .. の検査は Clean の前に行う (Clean は .. を畳んでしまい、検査が空振りする)
	for _, seg := range strings.Split(p, string(filepath.Separator)) {
		if seg == ".." {
			return "", fmt.Errorf(".. を含む: %s", p)
		}
	}
	p = normalizeSystemLinks(filepath.Clean(p))
	home := filepath.Clean(normalizeSystemLinks(env.Home))
	if p == "/" || p == "/Users" || p == home || (env.Home != "" && p == filepath.Dir(home)) || filepath.Dir(p) == "/Users" {
		return "", fmt.Errorf("ルート / ホームそのものは対象にしない: %s", p)
	}
	if depth := len(strings.Split(strings.Trim(p, "/"), "/")); depth < minDepth {
		return "", fmt.Errorf("浅すぎる (深さ %d < %d): %s", depth, minDepth, p)
	}
	if home != "" && filepath.Dir(p) == home && !strings.HasPrefix(filepath.Base(p), ".") {
		return "", fmt.Errorf("HOME 直下の通常ディレクトリは対象にしない: %s", p)
	}
	for dir := filepath.Dir(p); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		if err != nil {
			return "", fmt.Errorf("親ディレクトリを確認できない: %w", err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("経路の途中に symlink がある: %s", dir)
		}
	}
	return p, nil
}
