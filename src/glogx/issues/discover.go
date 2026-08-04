package issues

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// issue ディレクトリの探索。
//
// 実測 (このマシンの 6 箇所の issues/ ディレクトリ・計 405 ファイル) に基づく設計判断:
//
//   - 起点は git repo の toplevel。glogx は tmux popup から `-d '#{pane_current_path}'` で
//     起動されるため cwd は repo のサブディレクトリになりうる (nvim を開いていたペイン等)。
//     cwd 起点にすると「repo に issues があるのに viewer が空」という壊れ方をする
//   - 深さ 1 まで掘る: root/issues の他に <sub>/issues を持つ repo が実在する
//     (DualNoteApp は root/issues に 3 件・macOS/issues に 102 件)
//   - 名前が issues でも issue 管理でないディレクトリが実在する
//     (ubiregi-server/script/issues/19951 は権限 fixture の .yml 置き場)。
//     .md を 1 つも持たないディレクトリは対象外にする

// issueDirNames は issue ディレクトリとして認識するディレクトリ名。
var issueDirNames = map[string]bool{"issues": true, "issue": true}

// skipDirs は深さ 1 を掘るときに入らないディレクトリ (巨大・生成物・VCS)。
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "Pods": true, "Carthage": true,
	"DerivedData": true, "build": true, ".build": true, "dist": true, "target": true,
	"tmp": true, ".venv": true, ".next": true, "coverage": true, ".terraform": true,
}

// repoRootTimeout は起点を解決する git の上限。
//
// TUI の tea.Cmd (goroutine) から呼ぶ git には timeout を付ける、が glogx の規律
// (gitlog.go の runGitTimeout / gitOpTimeout。ネットワークマウント・.git ロック競合・hook の
// stdin 待ちでハングした git が goroutine ごと残り続けるのを防ぐ)。このパッケージは glogx 本体を
// import できないので値だけ揃える。
const repoRootTimeout = 30 * time.Second

// RepoRoot は探索の起点を返す。git repo の toplevel を優先し、取れなければ cwd をそのまま
// 使う (git 管理外のディレクトリでも issues/ があれば見えるように)。
func RepoRoot(cwd string) string {
	ctx, cancel := context.WithTimeout(context.Background(), repoRootTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return cwd
	}
	if root := strings.TrimSpace(string(out)); root != "" {
		return root
	}
	return cwd
}

// FindDirs は root 直下と root/*/ から issue ディレクトリを探して返す (name 昇順)。
func FindDirs(root string) []string {
	found := make([]string, 0, 2)
	if dir := filepath.Join(root, "issues"); hasMarkdown(dir) {
		found = append(found, dir)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return found
	}
	for _, e := range entries {
		if !e.IsDir() || skipDirs[e.Name()] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if issueDirNames[e.Name()] {
			if dir := filepath.Join(root, e.Name()); e.Name() != "issues" && hasMarkdown(dir) {
				found = append(found, dir)
			}
			continue
		}
		for name := range issueDirNames {
			dir := filepath.Join(root, e.Name(), name)
			if hasMarkdown(dir) {
				found = append(found, dir)
			}
		}
	}
	// map の走査順は不定なので並べ直す (タブ順・表示順が起動ごとに変わらないように)
	sort.Strings(found)
	return found
}

// hasMarkdown は dir が issue ディレクトリとして扱えるか (直下または直下のサブディレクトリに
// .md が 1 つでもあるか) を返す。全 issue が done/ に片付いた状態でも拾えるようサブ
// ディレクトリまで見る。
func hasMarkdown(dir string) bool {
	// ⚠️ ディレクトリ自体が symlink なら対象外にする。ここは FindDirs が候補を採用するかの
	// 唯一のゲートなので、ファイル単位の symlink 拒否 (isIssueFile) だけでは塞げない穴を
	// ここで閉じる: `issues -> /Users/victim/Documents` を 1 本足した PR を checkout すると、
	// repo 外の .md が一覧・本文として読めてしまう (実機で再現確認済み)。
	// git は mode 120000 でディレクトリ symlink を表現できるので PR 経由で入ってくる。
	if fi, err := os.Lstat(dir); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	subdirs := make([]string, 0, 4)
	for _, e := range entries {
		if e.IsDir() {
			if !skipDirs[e.Name()] {
				subdirs = append(subdirs, e.Name())
			}
			continue
		}
		if isIssueFile(e) {
			return true
		}
	}
	for _, sub := range subdirs {
		subEntries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		for _, e := range subEntries {
			if isIssueFile(e) {
				return true
			}
		}
	}
	return false
}

// isMarkdown は表示対象のファイル名か。実測では issue ファイルは 405/405 が .md で、
// .md 以外は audit-log (拡張子なし TSV) と .gitkeep だけだった。
//
// ⚠️ 走査で拾うかの判定には isIssueFile を使うこと (名前だけでは symlink を弾けない)。
func isMarkdown(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md")
}

// isIssueFile は走査で issue として拾うエントリか。判定を DirEntry 受け取りに揃えてあるのは、
// 名前だけの判定 (isMarkdown) を直接使うと symlink チェックを書き忘れられるため。
//
// ⚠️ symlink は必ず弾く: os.DirEntry.IsDir() は symlink に false を返すので、拡張子だけ見ると
// 通常ファイルとして通り、ReadBody の os.ReadFile がリンク先を辿って中身を本文として表示する。
// つまり PR に `issues/999-innocuous.md -> ~/.ssh/id_rsa` を 1 本入れておくだけで、その
// ブランチを checkout した人の画面にリンク先の中身が出る (実機で再現確認済み)。
// symlink 先を追う正当な用途は今のところ無いので、静かに無視する。
func isIssueFile(e os.DirEntry) bool {
	return !e.IsDir() && e.Type()&os.ModeSymlink == 0 && isMarkdown(e.Name())
}
