package issues

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
//   - **基準は種類ごとに 1 つだけ**試す。markdown リンクの dest はそのファイルのディレクトリ
//     (markdown の意味論。scripts/issue_done.sh が done/ へ移すとき張り直すのもこの基準)、
//     インラインコードは **その issue ディレクトリの親** (= プロジェクトの root。root/issues なら
//     repo root、root/app1/issues なら root/app1)。repo root に固定すると、root/*/issues を持つ
//     repo で兄弟プロジェクトの同名ファイルを開く。両方の基準を試すと同名の別ファイルを開きうる
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
}

// LinkBase は相対パスの基準 (ResolveLink の doc)。
type LinkBase struct {
	File    string // 本文を読んだ issue ファイル (markdown リンクの基準はこのディレクトリ)
	Project string // インラインコードの基準 (issue ディレクトリの親)
	Repo    string // 相対パスの実体が収まるべき範囲 (repo root)。"" なら相対パスは解決しない
}

// codeLineRe はインラインコードの末尾の行番号 (`path:12` / `path:12:3`)。
var codeLineRe = regexp.MustCompile(`^(.+?):([1-9][0-9]{0,6})(?::[0-9]+)?$`)

// fragLineRe は markdown リンクの fragment の行番号 (GitHub 式の `#L12` / `#L12-L20`)。
var fragLineRe = regexp.MustCompile(`^L([1-9][0-9]{0,6})(?:-L?[0-9]+)?$`)

// ResolveLink は本文のリンク候補を開く対象へ解決する。ok=false はリンクにしない
// (実在しない・パスでない・通常ファイルでない・相対パスの実体が repo の外)。
func ResolveLink(kind markdown.LinkKind, dest string, b LinkBase) (path string, line int, ok bool) {
	p, line, base, ok := linkPath(kind, dest, b)
	if !ok {
		return "", 0, false
	}
	relative := false
	switch {
	case filepath.IsAbs(p):
	case strings.HasPrefix(p, "~/"):
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", 0, false
		}
		p = filepath.Join(home, p[2:])
	case base == "":
		return "", 0, false
	default:
		p, relative = filepath.Join(base, p), true
	}
	if b.Repo == "" {
		return "", 0, false // 外へ出たかを判定できない (fail-closed)
	}
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return "", 0, false
	}
	// 🚨 開く対象は解いた実体にする (書いたパスを nvim に渡すと、判定の後に kernel が別の解き方をする余地が残る)
	real, escapes := realOutsideRepo(p, b.Repo, relative)
	if escapes {
		return "", 0, false
	}
	return real, line, true
}

// realOutsideRepo は p を開いたときの実体と、それが repo の外へ出てはいけないのに出ているかを返す。
//   - 実体は filepath.EvalSymlinks (成分ごとに symlink を解き、`..` も解いた後に当てる = kernel と同じ解き方)。
//     🚨 自前で Readlink を辿って Clean しない: ディレクトリの symlink (`repo/d -> /outside` を通る
//     `d/x`) と、readlink 先の `..` (`l -> e/../x` で e が symlink) で、判定した場所と開く場所が食い違う
//     (敵対レビュー 3 周目で実測)
//   - 「repo を通ったか」は、symlink の鎖の**各段**について、書かれたパスの**各 prefix** の実体が repo の中かで
//     見る (最後の成分だけ見ると、途中のディレクトリの symlink と repo 外から repo 内の symlink へ入る鎖を落とす)
//   - 通ったなら (相対パスは常に通った扱い) 実体も repo の中を要求する。PR で入れられるのは repo の中の
//     symlink だけなので、そこを通る鎖は「書いた人が名指しした場所」ではない (`~/.claude/rules/x.md` は
//     dotfiles では per-file link なので、repo の外に見えて repo の中の symlink を通る)
// repo の中かどうかは**字面で比べない** (APFS は大文字小文字と Unicode 正規化を区別しないので `~/DOTFILES/...`
// で前方一致をすり抜けられた)。実体の祖先を repo と os.SameFile (同じ inode) で比べる。
// 判定できない (解決の失敗・鎖が長すぎる) ときは出る扱い (fail-closed)。
func realOutsideRepo(p, repo string, relative bool) (real string, escapes bool) {
	rfi, err := os.Stat(repo)
	if err != nil {
		return "", true
	}
	// inRepo は q の実体が repo そのものか repo の中か
	inRepo := func(q string) bool {
		r, err := filepath.EvalSymlinks(q)
		if err != nil {
			return false
		}
		for d := r; ; d = filepath.Dir(d) {
			if fi, err := os.Stat(d); err == nil && os.SameFile(fi, rfi) {
				return true
			}
			if d == filepath.Dir(d) {
				return false
			}
		}
	}
	real, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", true
	}
	touched := relative
	cur := p
	for hop := 0; !touched; hop++ {
		if hop >= 40 {
			return "", true
		}
		// 各 prefix (書かれた形のまま。Clean すると symlink 越しの `..` を字面で潰す)
		for i := 1; i <= len(cur); i++ {
			if (i == len(cur) || cur[i] == '/') && inRepo(cur[:i]) {
				touched = true
				break
			}
		}
		// 鎖の次の段: 親の実体の下で、最後の成分が symlink なら readlink する
		j := strings.LastIndexByte(cur, '/')
		if j < 0 {
			break
		}
		dir, err := filepath.EvalSymlinks(cur[:max(j, 1)])
		if err != nil {
			return "", true
		}
		full := dir + "/" + cur[j+1:]
		li, err := os.Lstat(full)
		if err != nil {
			return "", true
		}
		if li.Mode()&os.ModeSymlink == 0 {
			break
		}
		t, err := os.Readlink(full)
		if err != nil {
			return "", true
		}
		if !filepath.IsAbs(t) {
			t = dir + "/" + t
		}
		cur = t
	}
	return real, touched && !inRepo(real)
}

// linkPath は dest からパス部分・行番号・相対パスの基準を取り出す (ファイルシステムは見ない)。
func linkPath(kind markdown.LinkKind, dest string, b LinkBase) (p string, line int, base string, ok bool) {
	dest = strings.TrimSpace(dest)
	// 🚨 制御文字を含むものはパスとして扱わない。画面 (ヘッダー) に出す値でもあり、nvim の引数でもある
	if dest == "" || strings.ContainsFunc(dest, unicode.IsControl) {
		return "", 0, "", false
	}
	switch kind {
	case markdown.LinkDest:
		// `<path>` の山括弧 (中は空白を含んでよい) と `[x](path "title")` の title を外す
		if rest, ok := strings.CutPrefix(dest, "<"); ok {
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return "", 0, "", false
			}
			// 閉じた後ろは空白 (title) か終わりだけ。`<a>b` は CommonMark ではリンクにならない形
			if after := rest[end+1:]; after != "" && after[0] != ' ' && after[0] != '\t' {
				return "", 0, "", false
			}
			dest = rest[:end]
		} else if i := strings.IndexAny(dest, " \t"); i >= 0 {
			dest = dest[:i]
		}
		if strings.HasPrefix(dest, "#") || strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") {
			return "", 0, "", false
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
			return "", 0, "", false
		}
		if b.File == "" {
			return "", 0, "", false
		}
		return dest, line, filepath.Dir(b.File), dest != ""
	case markdown.LinkCode:
		if strings.ContainsFunc(dest, unicode.IsSpace) {
			return "", 0, "", false // コマンドや文 (`git log -1`) はパスでない
		}
		if m := codeLineRe.FindStringSubmatch(dest); m != nil {
			dest = m[1]
			line, _ = strconv.Atoi(m[2])
		}
		return dest, line, b.Project, true
	default:
		return "", 0, "", false
	}
}
