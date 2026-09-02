package disk

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

func RealEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, TmpDir: os.Getenv("TMPDIR"), Getenv: os.Getenv,
		AppDirs: []string{"/Applications", filepath.Join(home, "Applications")}}
}

// escapeGlobMeta は環境変数由来の literal 部分を glob パターンとして解釈させないための escape。
// filepath.Glob には Escape が無いので自前でやる (macOS なので `\` が escape 文字として効く)。
// これをしないと HOME に `[` が入っているだけで Glob が 0 件を返し、実在するキャッシュが
// 「候補なし」に化ける (issue 175 / false green)。
func escapeGlobMeta(s string) string {
	// byte 単位で回す (rune で回すと不正 UTF-8 が U+FFFD に置換され、pattern が実バイト列と
	// 食い違ってマッチしなくなる)。メタ文字はすべて ASCII なので継続バイトを誤爆しない。
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(`*?[\`, s[i]) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// expand はテンプレート (~ / $TMPDIR) を展開して glob する。
// 変数が空なら error (空パス展開で `rm -rf /foo` が組み立たる事故の入口を塞ぐ。走査でも同じ規律)。
// 展開結果が絶対パスでなければ error (相対 TMPDIR 等。glob が 0 件になって無音で消えるのを防ぐ:
// 「診断できず」と「候補なし」を混ぜない)。
// 変数由来の部分は escapeGlobMeta で literal 化するので、glob として効くのはテンプレート側の
// メタ文字だけになる。
// glob の結果 0 件は空スライス (エラーではない)。
func expand(env Env, tmpl string) ([]string, error) {
	logical, pattern := tmpl, tmpl // logical = 判定用の素のパス / pattern = Glob に渡す escape 済み
	if strings.HasPrefix(tmpl, "~/") || tmpl == "~" {
		if env.Home == "" {
			return nil, errors.New("HOME が空です")
		}
		logical = env.Home + logical[1:]
		pattern = escapeGlobMeta(env.Home) + pattern[1:]
	}
	if strings.Contains(tmpl, "$TMPDIR") {
		if env.TmpDir == "" {
			return nil, errors.New("TMPDIR が空です")
		}
		t := strings.TrimRight(env.TmpDir, "/")
		logical = strings.ReplaceAll(logical, "$TMPDIR", t)
		pattern = strings.ReplaceAll(pattern, "$TMPDIR", escapeGlobMeta(t))
	}
	if strings.Contains(logical, "$") {
		return nil, fmt.Errorf("未対応の変数があります: %s", tmpl)
	}
	if !filepath.IsAbs(logical) {
		return nil, fmt.Errorf("展開結果が絶対パスでない (診断できず): %s", logical)
	}
	matches, err := filepath.Glob(pattern)
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
