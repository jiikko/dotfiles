package issues

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"tuikit/markdown"
)

// 本文中のファイルパスを開けるファイルへ解決する (viewer のジャンプモード。本文 pager の Tab)。
//
// 🚨 2026-07-31 にこの機能を保留した理由 (docs/issues-viewer-spec.md) への答えがこのファイルの形:
// 本文に書かれたパスは repo root から実在するものが半分以下で、基準が repo root / issue の
// ディレクトリ / 別の場所で揺れる。そこで
//   - **実在する通常ファイルに解決できたものだけ**をリンクにする (外れるものは画面で強調もしない)
//   - **基準は順序つきで決める**。markdown リンクの dest は、まずそのファイルのディレクトリ
//     (markdown の意味論。scripts/issue_done.sh が done/ へ移すとき張り直すのもこの基準)、そこに
//     無ければ **その issue ディレクトリの親** (= プロジェクトの root)。後者は `[x](issues/epic/…)` のように
//     repo root 起点でリンクを書く repo が実在するため (ubiregi-server。2026-09-28 ユーザー要望)。
//     両方に実在するときは前者を優先し、どちらを開くかはヘッダーの 2 行目 (解決した先) で見せる。
//     インラインコードはプロジェクトの root だけ (root/issues なら repo root、root/app1/issues なら
//     root/app1)。repo root に固定すると、root/*/issues を持つ repo で兄弟プロジェクトの同名ファイルを開く
//   - **相対パスで書かれたものは、symlink を解いた実体が repo の中に在ること**を要求する (realOutsideRepo)。
//     PR で `docs/x.md -> ~/.ssh/id_ed25519` の symlink と本文の `docs/x.md` を足されると、画面には
//     無害な名前だけが出たまま repo 外を開かされる (discover.go の hasMarkdown が symlink を拒否して
//     いるのと同じ脅威)。絶対パスと `~/` でも、**symlink の鎖が一度でも repo の中を通るなら**同じ要求を
//     当てる (`~/dotfiles/docs/x.md` と書けば同じ symlink を通れてしまい、表示も repo 相対に畳まれる)。
//     repo に一度も触れない絶対パスは書いた人の指定を信じて実体を問わない
// 基準を足したくなったら、上の「別ファイルを開く」を先に解くこと。

// FileLink は開けるファイルへ解決できたリンク 1 出現。
type FileLink struct {
	Index int // markdown.Link の添字 (同じ幅で整形した結果の中でだけ有効。Link の doc)
	Kind  markdown.LinkKind
	Dest  string // 本文に書かれた値 (外部由来。表示するなら無害化する)
	Path  string // 開く対象の絶対パス
	Line  int    // 行番号の指定 (`foo.go:12` / `foo.md#L12`)。0 = なし
	Top   int    // 最初のセグメントの表示行 (選択をスクロールで見せるため)
	Dir   bool   // ディレクトリ (nvim -R がファイラーとして開く)
}

// LinkBase は相対パスの基準 (ResolveLink の doc)。
type LinkBase struct {
	File    string // 本文を読んだ issue ファイル (markdown リンクの基準はこのディレクトリ)
	Project string // インラインコードの基準 (issue ディレクトリの親)
	// Repos は「repo の中」とみなす範囲 = 同じ repo の全 checkout (WorktreeRoots)。空なら解決しない。
	// 🚨 1 つの root にしない: PR で入れた symlink は全 checkout に現れるので、worktree から本体の
	// `~/dotfiles/...` を絶対パスで開くと、本体側の symlink を「repo の外」と見て素通りする (5 周目で実測)。
	// 🚨 別の場所に clone した同じ repo は列挙できないので検出しない (脅威モデルの外として受容)。
	Repos []string
}

// braceRe は波括弧の展開 1 組 (`x.{sql,tsv}`)。入れ子と 2 組以上は展開しない (シェルの全機能は要らない。
// 本文で実際に使われているのは 1 組だけ)。
var braceRe = regexp.MustCompile(`^([^{}]*)\{([^{}]*,[^{}]*)\}([^{}]*)$`)

// ExpandBraces は波括弧 1 組を展開する (`a.{x,y}` → `a.x`, `a.y`)。展開しないものはそのまま 1 つ返す。
func ExpandBraces(s string) []string {
	m := braceRe.FindStringSubmatch(s)
	if m == nil {
		return []string{s}
	}
	parts := strings.Split(m[2], ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, m[1]+p+m[3])
	}
	return out
}

// codeLineRe はインラインコードの末尾の行番号 (`path:12` / `path:12:3`)。
var codeLineRe = regexp.MustCompile(`^(.+?):([1-9][0-9]{0,6})(?::[0-9]+)?$`)

// fragLineRe は markdown リンクの fragment の行番号 (GitHub 式の `#L12` / `#L12-L20`)。
var fragLineRe = regexp.MustCompile(`^L([1-9][0-9]{0,6})(?:-L?[0-9]+)?$`)

// ResolveLink は本文のリンク候補を開く対象へ解決する。ok=false はリンクにしない
// (実在しない・パスでない・通常ファイルでもディレクトリでもない・相対パスの実体が repo の外)。
// 相対パスは linkPath が返す基準を順に試し、最初に解決できたものを採る。
func ResolveLink(kind markdown.LinkKind, dest string, b LinkBase) (path string, line int, ok bool) {
	p, line, bases, ok := linkPath(kind, dest, b)
	if !ok {
		return "", 0, false
	}
	if len(bases) == 0 || filepath.IsAbs(p) || strings.HasPrefix(p, "~/") {
		bases = []string{""}
	}
	for _, base := range bases {
		real, ok := resolveFrom(p, base, b)
		if !ok {
			continue
		}
		// ディレクトリは `/` を含む書き方 (`census/raw/`) のときだけ止まり先にする。単語 1 つのコードスパン
		// (`config` / `client`) が同名のディレクトリに当たって光ると、本文の大半の語がリンクに見える
		// (ubiregi-server 188 で実測: `config` `client` が当たった)
		if fi, err := os.Stat(real); err == nil && fi.IsDir() && !dirMention(p) {
			continue
		}
		return real, line, true
	}
	return "", 0, false
}

// dirMention はディレクトリを止まり先にしてよい書き方か: **相対パスで、名前を含み、`/` で区切られたもの**
// (`census/raw/` / `src/`)。単語 1 つ (`config`)、区切り記号だけ (`/` `./` `~/`)、絶対パス (`/tmp`) は外す。
// 本文には `/` が区切り記号の意味で 246 件、`/tmp` が 16 件あり (dotfiles の issues/ と docs/。2026-09-28 の敵対
// レビューで実測)、Tab の止まり先が無意味なリンクで埋まる。
func dirMention(p string) bool {
	if filepath.IsAbs(p) || strings.HasPrefix(p, "~") {
		return false
	}
	c := filepath.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return false
	}
	return strings.Contains(p, "/")
}

// resolveFrom は 1 つの基準で p を解決する (base="" は絶対パス / `~/` 専用)。
func resolveFrom(p, base string, b LinkBase) (string, bool) {
	relative := false
	switch {
	case filepath.IsAbs(p):
	case strings.HasPrefix(p, "~/"):
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", false
		}
		p = filepath.Join(home, p[2:])
	case base == "":
		return "", false
	default:
		p, relative = filepath.Join(base, p), true
	}
	if len(b.Repos) == 0 {
		return "", false // 外へ出たかを判定できない (fail-closed)
	}
	// ディレクトリも止まり先にする (nvim -R がファイラーとして開く。`census/raw/` のような言及が実在する)
	if fi, err := os.Stat(p); err != nil || !(fi.Mode().IsRegular() || fi.IsDir()) {
		return "", false
	}
	// 🚨 開く対象は解いた実体にする (書いたパスを nvim に渡すと、判定の後に kernel が別の解き方をする余地が残る)
	real, escapes := realOutsideRepo(p, b.Repos, relative)
	if escapes {
		return "", false
	}
	return real, true
}

// realOutsideRepo は p を開いたときの実体と、それが repo の外へ出てはいけないのに出ているかを返す。
//
// 規則は効果で決める: **repo の中に置かれた symlink を 1 つでも通ったら** (相対パスで書いたものは
// 常に通った扱い)、最終の実体も repo の中であることを要求する。PR で入れられる symlink は repo の中の
// ものだけなので、そこを通る鎖は「書いた人が名指しした場所」ではない (`~/.claude/rules/x.md` や
// `~/.claude/skills/forge/…` は dotfiles へ向く per-file / ディレクトリの link なので、repo の外に見えて
// repo の中の symlink を通りうる)。repo の中の symlink を 1 つも通らない絶対パスは書いた人の指定を信じる。
//
// 🚨 「どの symlink を通ったか」は近似で選ばない。1〜3 周目の敵対レビューで、最後の成分だけ見る /
// 書かれた prefix だけ見る / Readlink を Clean して辿る、のいずれも迂回された (途中のディレクトリの
// symlink、symlink 越しの `..`、repo 外のディレクトリ link から repo 内のディレクトリ link へ入る鎖)。
// ここでは kernel と同じく成分ごとに解決し (resolveObserving)、出会った symlink を全部数える。
// 解決の終点は filepath.EvalSymlinks の答えと突き合わせ、食い違えば判定できない扱いにする (自前の
// 解決が本物と違うまま判定しない)。
// repo の中かどうかは字面で比べず、実体の祖先を repo と os.SameFile (同じ inode) で比べる (APFS は
// 大文字小文字と Unicode 正規化を区別しないので、字面の前方一致は `~/DOTFILES/...` で迂回された)。
// 判定できない (解決の失敗・鎖が長すぎる・本物と食い違う) ときは出る扱い (fail-closed)。
func realOutsideRepo(p string, repos []string, relative bool) (real string, escapes bool) {
	rfis := make([]os.FileInfo, 0, len(repos))
	for _, r := range repos {
		if fi, err := os.Stat(r); err == nil {
			rfis = append(rfis, fi)
		}
	}
	if len(rfis) == 0 {
		return "", true
	}
	// inRepo は実体のパス (symlink を含まない) d が repo (のどれかの checkout) そのものかその中か
	inRepo := func(d string) bool {
		for ; ; d = filepath.Dir(d) {
			if fi, err := os.Stat(d); err == nil && slices.ContainsFunc(rfis, func(r os.FileInfo) bool { return os.SameFile(fi, r) }) {
				return true
			}
			if d == filepath.Dir(d) {
				return false
			}
		}
	}
	real, touched, ok := resolveObserving(p, inRepo)
	if !ok {
		return "", true
	}
	// 🚨 EvalSymlinks は正解役として完全ではない: 空文字の target (`ln -s ""`) を自前の resolver と同じく
	// 親ディレクトリとして解く (kernel は ENOENT。5 周目で実測)。ResolveLink の手前の os.Stat が落とすので
	// 実害は無いが、その Stat を外すならここも見直すこと
	if want, err := filepath.EvalSymlinks(p); err != nil || want != real {
		return "", true
	}
	return real, (relative || touched) && !inRepo(real)
}

// resolveObserving は絶対パス p を成分ごとに解決する (filepath.EvalSymlinks と同じ解き方: symlink を
// 解いてから次の成分へ進み、`..` は解いた後の親へ戻る)。touched は、置かれている場所 (実体のディレクトリ)
// が inRepo を満たす symlink を 1 つでも通ったか。
func resolveObserving(p string, inRepo func(string) bool) (real string, touched, ok bool) {
	if !filepath.IsAbs(p) {
		return "", false, false
	}
	rest := strings.Split(p, "/")
	dest := "/"
	links := 0
	for len(rest) > 0 {
		c := rest[0]
		rest = rest[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			dest = filepath.Dir(dest)
			continue
		}
		next := filepath.Join(dest, c)
		fi, err := os.Lstat(next)
		if err != nil {
			return "", false, false
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			dest = next
			continue
		}
		if links++; links > 255 {
			return "", false, false
		}
		if inRepo(dest) { // symlink は dest (実体のディレクトリ) に置かれている
			touched = true
		}
		t, err := os.Readlink(next)
		if err != nil {
			return "", false, false
		}
		if filepath.IsAbs(t) {
			dest = "/"
		}
		rest = append(strings.Split(t, "/"), rest...)
	}
	return dest, touched, true
}

// linkPath は dest からパス部分・行番号・相対パスの基準を取り出す (ファイルシステムは見ない)。
func linkPath(kind markdown.LinkKind, dest string, b LinkBase) (p string, line int, bases []string, ok bool) {
	dest = strings.TrimSpace(dest)
	// 🚨 制御文字を含むものはパスとして扱わない。画面 (ヘッダー) に出す値でもあり、nvim の引数でもある
	if dest == "" || strings.ContainsFunc(dest, unicode.IsControl) {
		return "", 0, nil, false
	}
	switch kind {
	case markdown.LinkDest:
		// `<path>` の山括弧 (中は空白を含んでよい) と `[x](path "title")` の title を外す
		if rest, ok := strings.CutPrefix(dest, "<"); ok {
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return "", 0, nil, false
			}
			// 閉じた後ろは空白 (title) か終わりだけ。`<a>b` は CommonMark ではリンクにならない形
			if after := rest[end+1:]; after != "" && after[0] != ' ' && after[0] != '\t' {
				return "", 0, nil, false
			}
			dest = rest[:end]
		} else if i := strings.IndexAny(dest, " \t"); i >= 0 {
			dest = dest[:i]
		}
		if strings.HasPrefix(dest, "#") || strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") {
			return "", 0, nil, false
		}
		if i := strings.IndexByte(dest, '#'); i >= 0 {
			if m := fragLineRe.FindStringSubmatch(dest[i+1:]); m != nil {
				line, _ = strconv.Atoi(m[1])
			}
			dest = dest[:i]
		}
		if i := strings.IndexByte(dest, '?'); i >= 0 {
			dest = dest[:i]
		}
		if u, err := url.PathUnescape(dest); err == nil {
			dest = u
		}
		// 🚨 解いた後にもう一度見る (`%1b` / `%0a` は上の判定を素通りして制御文字に戻る)
		if strings.ContainsFunc(dest, unicode.IsControl) {
			return "", 0, nil, false
		}
		if b.File == "" {
			return "", 0, nil, false
		}
		bases = []string{filepath.Dir(b.File)}
		if b.Project != "" && b.Project != bases[0] {
			bases = append(bases, b.Project)
		}
		return dest, line, bases, dest != ""
	case markdown.LinkCode:
		if strings.ContainsFunc(dest, unicode.IsSpace) {
			return "", 0, nil, false // コマンドや文 (`git log -1`) はパスでない
		}
		if m := codeLineRe.FindStringSubmatch(dest); m != nil {
			dest = m[1]
			line, _ = strconv.Atoi(m[2])
		}
		if b.Project == "" {
			return dest, line, nil, true
		}
		return dest, line, []string{b.Project}, true
	default:
		return "", 0, nil, false
	}
}
