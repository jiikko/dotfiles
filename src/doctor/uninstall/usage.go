package uninstall

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"doctor/internal/homepath"
)

// 「動いていないが、呼ばれることがある」formula を見つけるための材料。スクリプト・hook・設定は formula のコマンドを実行時に呼ぶだけなので、
// lsof にも ps にも出ない (dotfiles の Claude Code の hook が jq を呼ぶ、zshlib が ffprobe を呼ぶ)。
// 見つけても断らず、計画に「使われているかもしれない」と理由を出して人に確かめさせる: コマンドの名前 (git・go) は一般的な語と重なり、
// 文字列の一致だけでは使っているとは言い切れないため。見落としの方を重く見るが、確認の意味が薄れるほどの誤検出は避ける。
//
// 辿り方は 2 通り。🚨 混ぜない:
//   - シェル・tmux の設定 (と、そこから辿る先): source / . / source-file / run-shell の引数の位置に出たパスだけを辿る。本文のどこからでも
//     拾うと、HISTFILE=$HOME/.zsh_history から履歴を読み、過去に打ったパスの先 (キーチェーン・バイナリ) まで読む
//   - Claude Code の settings.json と、そこから呼ぶスクリプト: スクリプトは自分の置き場からの相対や ~ で他を呼ぶので、本文に出る ~ のパスも辿る

// shellConfigFiles はシェルと tmux の設定ファイル。
var shellConfigFiles = []string{".zshrc", ".zshenv", ".zprofile", ".zlogin", ".bashrc", ".bash_profile", ".profile", ".tmux.conf", ".config/tmux/tmux.conf"}

const (
	// entryMaxBytes は名指しで読むファイル (設定・settings.json・そこから辿る先) の上限。設定は大きくなりうる (実機の _tmux.conf は 64KB、
	// nvim の init は 71KB) ので、実質掛けない。🚨 ここを小さくすると Protected の判定が黙って弱くなる
	entryMaxBytes = 8 << 20
	// configMaxBytes は ~/.config の下を総なめに読むときの 1 ファイルの上限
	configMaxBytes = 1 << 20
	// corpusMaxBytes は読む量の合計の上限。超えたら読むのをやめ、一覧の判定不能に出す (symlink の束や巨大なファイルで止まらないため)
	corpusMaxBytes = 32 << 20
	// configPerTool は ~/.config の直下の道具 1 つあたりの上限 (辞書順で前の道具が上限を食い潰さないよう、道具ごとに分ける)
	configPerTool = 300
	configDepth   = 3 // ~/.config/<道具>/<1 段>/<ファイル> まで
	followDepth   = 2 // 入口から辿る段数
)

const pathChars = `[^\s"'\\;|&<>()` + "`" + `]+`

var (
	// sourceArgRe は source / . / source-file / run-shell の引数 (絶対パスも)。
	sourceArgRe = regexp.MustCompile(`(?:^|[\s;&|({])(?:source|\.|source-file(?:\s+-[qnFv]+)*|run-shell(?:\s+-[bC]+)*)\s+["']?(` + pathChars + `)`)
	// homePathRe は本文のどこかに出る ~ / $HOME / 自分の置き場で始まるパス (スクリプトの辿り方でだけ使う)。
	homePathRe = regexp.MustCompile(`(?:~|\$HOME|\$\{HOME\}|\$\{0:A:h\}|\$\{BASH_SOURCE%/\*\}|\$\(dirname "?\$0"?\)|\$\(cd "?\$\(dirname "?\$0"?\)"? && pwd\))/` + pathChars)
	// commandRe は settings.json の "command": "..." の先頭の語 (絶対パスで書いた hook を辿る)。
	commandRe  = regexp.MustCompile(`"command"\s*:\s*"([^"\s]+)`)
	defaultRe  = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*:-([^}]+)\}`)
	wordSplitR = regexp.MustCompile(`[^A-Za-z0-9_.+@-]+`)
	selfDirRe  = regexp.MustCompile(`^(?:\$\{0:A:h\}|\$\{BASH_SOURCE%/\*\}|\$\(dirname "?\$0"?\)|\$\(cd "?\$\(dirname "?\$0"?\)"? && pwd\))/`)
)

// corpusFile は探す範囲の 1 ファイル。words は中身を語に分けたもの (コマンドの名前と突き合わせる)。
type corpusFile struct {
	path  string
	text  string
	shell bool // シェル・tmux の設定から辿ったもの (Protected の判定に使う)
	words map[string]bool
}

// expandPath は ~ / $HOME / ${HOME} を home に、自分の置き場の書き方を dir に直す。
func expandPath(home, dir, p string) string {
	for _, pre := range []string{"~/", "$HOME/", "${HOME}/"} {
		if rest, ok := strings.CutPrefix(p, pre); ok {
			return filepath.Join(home, rest)
		}
	}
	if m := selfDirRe.FindString(p); m != "" {
		return filepath.Join(dir, p[len(m):])
	}
	return p
}

// corpus は読んだファイルの集まり。同じ実体 (dev, ino) を 2 度読まない。
type corpus struct {
	home      string
	files     []corpusFile
	seen      map[[2]uint64]bool
	total     int64
	truncated bool
}

// skipRead は読まない置き場と名前: ~/Library (キーチェーン・アプリのデータ)、履歴、補完のキャッシュ、zsh のコンパイル済み。
func (c *corpus) skipRead(p string) bool {
	if strings.HasPrefix(p, filepath.Join(c.home, "Library")+"/") {
		return true
	}
	name := filepath.Base(p)
	return strings.HasSuffix(name, "_history") || strings.HasSuffix(name, ".zwc") || strings.HasPrefix(name, ".zcompdump")
}

// read は p を読み、読めたら files に足して中身を返す。limit を超えるもの・普通のファイルでないもの・NUL を含むもの (テキストでない) は読まない。
func (c *corpus) read(p string, limit int64, shell bool) (string, bool) {
	p = filepath.Clean(p)
	if c.skipRead(p) || c.truncated {
		return "", false
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > limit {
		return "", false
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		id := [2]uint64{uint64(st.Dev), st.Ino} //nolint:gosec // dev は負にならない
		if c.seen[id] {
			return "", false
		}
		c.seen[id] = true
	}
	if c.total+fi.Size() > corpusMaxBytes {
		c.truncated = true
		return "", false
	}
	data, err := os.ReadFile(p)
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	c.total += int64(len(data))
	c.files = append(c.files, corpusFile{path: p, text: string(data), shell: shell})
	return string(data), true
}

// followShell はシェル・tmux の設定を読み、source / . / source-file / run-shell の引数に出たパスを depth 段まで辿る。
func (c *corpus) followShell(p string, depth int) {
	text, ok := c.read(p, entryMaxBytes, true)
	if !ok || depth >= followDepth {
		return
	}
	for _, m := range sourceArgRe.FindAllStringSubmatch(defaultRe.ReplaceAllString(text, "$1"), -1) {
		c.followShell(expandPath(c.home, filepath.Dir(p), m[1]), depth+1)
	}
}

// followScript は settings.json・hook のスクリプトを読み、source の引数・command の先頭・本文の ~ のパスを depth 段まで辿る。
func (c *corpus) followScript(p string, depth int) {
	text, ok := c.read(p, entryMaxBytes, false)
	if !ok || depth >= followDepth {
		return
	}
	text = defaultRe.ReplaceAllString(text, "$1")
	var next []string
	for _, m := range sourceArgRe.FindAllStringSubmatch(text, -1) {
		next = append(next, m[1])
	}
	for _, m := range commandRe.FindAllStringSubmatch(text, -1) {
		next = append(next, m[1])
	}
	next = append(next, homePathRe.FindAllString(text, -1)...)
	for _, n := range next {
		c.followScript(expandPath(c.home, filepath.Dir(p), n), depth+1)
	}
}

// usage は formula の使われ方を探す範囲。1 回作って、Protected (shellText) と UsedBy (files) の両方に使う。
type usage struct {
	files     []corpusFile
	shellText string
	truncated bool
}

// buildUsage はシェル・tmux の設定 (と辿る先)・Claude Code の設定とそこから呼ぶスクリプト (と辿る先)・~/.config の下を読む。
func buildUsage(home string) usage {
	c := &corpus{home: home, seen: map[[2]uint64]bool{}}
	for _, f := range shellConfigFiles {
		c.followShell(filepath.Join(home, f), 0)
	}
	c.followScript(filepath.Join(home, ".claude", "settings.json"), 0)
	c.walkConfig(filepath.Join(home, ".config"))
	var b strings.Builder
	for i := range c.files {
		c.files[i].words = words(c.files[i].text)
		if c.files[i].shell {
			b.WriteString(c.files[i].text)
			b.WriteByte('\n')
		}
	}
	return usage{files: c.files, shellText: b.String(), truncated: c.truncated}
}

// walkConfig は ~/.config の直下の道具ごとに、configDepth 段まで・configPerTool 件まで読む。直下の symlink の dir も辿る
// (dotfiles では ~/.config/nvim が dotfiles への symlink)。
func (c *corpus) walkConfig(root string) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		top := filepath.Join(root, e.Name())
		real, err := filepath.EvalSymlinks(top)
		if err != nil {
			continue
		}
		if st, err := os.Stat(real); err != nil || !st.IsDir() {
			c.read(top, configMaxBytes, false)
			continue
		}
		n := 0
		_ = filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // 読めない dir は飛ばす (探す範囲が狭くなるだけで、消す判定は人の確認に回る)
			}
			rel, _ := filepath.Rel(real, p)
			if d.IsDir() {
				if rel != "." && (strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator))+1 >= configDepth) {
					return filepath.SkipDir
				}
				return nil
			}
			if n >= configPerTool || c.truncated {
				return filepath.SkipAll
			}
			n++
			c.read(p, configMaxBytes, false)
			return nil
		})
	}
}

// words は中身を語に分ける。先頭の - と末尾の . を落とした形も入れる (`${JQ:-jq}` の -jq、文末の nvim. を拾う)。先頭の . は落とさない
// (落とすと .git・*.go から git・go に当たり、確認の意味が薄れる)。
func words(text string) map[string]bool {
	m := map[string]bool{}
	for _, w := range wordSplitR.Split(text, -1) {
		if w == "" {
			continue
		}
		m[w] = true
		if t := strings.TrimRight(strings.TrimLeft(w, "-"), "."); t != "" {
			m[t] = true
		}
	}
	return m
}

// formulaCommands は formula が入れたコマンドの名前 (Cellar/<name>/<版>/bin と sbin の中身)。
func formulaCommands(path string) []string {
	var cmds []string
	for _, sub := range []string{"bin", "sbin"} {
		ents, _ := filepath.Glob(filepath.Join(path, "*", sub, "*"))
		for _, e := range ents {
			if name := filepath.Base(e); !slices.Contains(cmds, name) {
				cmds = append(cmds, name)
			}
		}
	}
	return cmds
}

// brewPathMentions は brew の置き場の形で formula の名前を含むか (bin を持たない formula = zsh のプラグイン・*-completion のため)。
// 素の "/<名前>/" にしない: /go/・/git/・/vim/ のような汎用の dir に広く当たる。
func brewPathMentions(text, base string) bool {
	for _, pre := range []string{"Cellar/", "opt/", "share/", "plugins/"} {
		if strings.Contains(text, pre+base+"/") {
			return true
		}
	}
	return false
}

// usedBy は formula のコマンドの名前が語として出てくるか、brew の置き場の形で名前が出てくるファイルを最大 3 つ返す (~ で始まる形)。
func usedBy(home, name string, files []corpusFile, cmds []string) []string {
	base := filepath.Base(name)
	var out []string
	for _, f := range files {
		if slices.ContainsFunc(cmds, func(c string) bool { return f.words[c] }) || brewPathMentions(f.text, base) {
			out = append(out, homepath.Tilde(home, f.path))
			if len(out) == 3 {
				break
			}
		}
	}
	return out
}
