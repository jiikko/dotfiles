package visibility

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// このファイルは、本家が使う os.path と pathlib の規則 (macOS の posix の形) を写す。
// Windows の分岐 (ドライブ・USERPROFILE) は写さない: macOS だけが対象 (dotfiles の CLAUDE.md「対象プラットフォーム」)。
// os.getcwd は $PWD を見ずに実パスを返すので、os.Getwd (PWD が同じ場所を指せばそれを返す) でなく syscall.Getwd を使う。

// 入力のパスを読んでよいか (input_path_problem) の理由の文。
const (
	InputPathOutside = "作業フォルダの外にあるファイルは読みません"
	InputPathHidden  = "名前が点で始まるファイルと、名前が点で始まるフォルダの中のファイルは読みません"
	InputPathNoCwd   = "作業フォルダを読めないため、読んでよいファイルか決められません"
)

// getcwd は os.getcwd()。読めなければ ok が偽。
func getcwd() (string, bool) {
	cwd, err := syscall.Getwd()
	if err != nil {
		return "", false
	}
	// macOS の syscall.Getwd は、消えたフォルダの元のパスを返す。libc の getcwd (Python の os.getcwd) は ENOENT で失敗する。
	if _, err := os.Stat(cwd); err != nil {
		return "", false
	}
	return cwd, true
}

// pyJoin は posixpath.join(a, b)。
func pyJoin(a, b string) string {
	switch {
	case strings.HasPrefix(b, "/") || a == "":
		return b
	case strings.HasSuffix(a, "/"):
		return a + b
	default:
		return a + "/" + b
	}
}

// normpath は posixpath.normpath (先頭が // ちょうどのときだけ 2 つの / を残す)。
func normpath(path string) string {
	if path == "" {
		return "."
	}
	initial := ""
	if strings.HasPrefix(path, "/") {
		initial = "/"
		if strings.HasPrefix(path, "//") && !strings.HasPrefix(path, "///") {
			initial = "//"
		}
	}
	var out []string
	for _, c := range strings.Split(path, "/") {
		if c == "" || c == "." {
			continue
		}
		switch {
		case c != ".." || (initial == "" && len(out) == 0) || (len(out) > 0 && out[len(out)-1] == ".."):
			out = append(out, c)
		case len(out) > 0:
			out = out[:len(out)-1]
		}
	}
	if res := initial + strings.Join(out, "/"); res != "" {
		return res
	}
	return "."
}

// AbsolutePath は absolute_path (os.path.abspath。シンボリックリンクをたどらずに、字面の . と .. と // を整える)。
// 作業ディレクトリを読めないときは、Python の os.getcwd が OSError を投げるのと同じく error を返す。
func AbsolutePath(path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		cwd, ok := getcwd()
		if !ok {
			return "", newError("FileNotFoundError", "[Errno 2] No such file or directory")
		}
		path = pyJoin(cwd, path)
	}
	return normpath(path), nil
}

// pathlib の PurePosixPath の字面 (root と、空と . を除いた部品)。
type purePath struct {
	root  string
	parts []string
}

func parsePure(s string) purePath {
	p := purePath{}
	if strings.HasPrefix(s, "/") {
		p.root = "/"
		if strings.HasPrefix(s, "//") && !strings.HasPrefix(s, "///") {
			p.root = "//"
		}
	}
	for _, c := range strings.Split(s, "/") {
		if c != "" && c != "." {
			p.parts = append(p.parts, c)
		}
	}
	return p
}

func (p purePath) String() string {
	if s := p.root + strings.Join(p.parts, "/"); s != "" {
		return s
	}
	return "."
}

// pureJoin は str(Path(a) / b)。b が絶対なら b だけ。
func pureJoin(a, b string) string {
	pb := parsePure(b)
	if pb.root != "" {
		return pb.String()
	}
	pa := parsePure(a)
	pa.parts = append(append([]string(nil), pa.parts...), pb.parts...)
	return pa.String()
}

// pureParent は str(Path(s).parent)。
func pureParent(s string) string {
	p := parsePure(s)
	if len(p.parts) > 0 {
		p.parts = p.parts[:len(p.parts)-1]
	}
	return p.String()
}

// pureName は Path(s).name。
func pureName(s string) string {
	p := parsePure(s)
	if len(p.parts) == 0 {
		return ""
	}
	return p.parts[len(p.parts)-1]
}

// statMode は os.stat (シンボリックリンクをたどる) の種類。stat できなければ ok が偽。
// path の真偽の問い合わせ (pathExists・pathIsDir・pathIsFile) は、本家の _path_matches と同じく、読めない名前を全部「そうでない」にする。
func statMode(path string) (mode fs.FileMode, ok bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return fi.Mode(), true
}

func pathExists(path string) bool { _, ok := statMode(path); return ok }

func pathIsDir(path string) bool { m, ok := statMode(path); return ok && m.IsDir() }

func pathIsFile(path string) bool { m, ok := statMode(path); return ok && m.IsRegular() }

// strerror は macOS の strerror(3) の文。Go の syscall.Errno の文は小文字などで、Python の OSError.strerror と違う。
// 表に無い errno は macOS の strerror と同じ "Unknown error: N"。
func strerror(errno syscall.Errno) string {
	switch errno {
	case syscall.EPERM:
		return "Operation not permitted"
	case syscall.ENOENT:
		return "No such file or directory"
	case syscall.EIO:
		return "Input/output error"
	case syscall.ENXIO:
		return "Device not configured"
	case syscall.EBADF:
		return "Bad file descriptor"
	case syscall.ENOMEM:
		return "Cannot allocate memory"
	case syscall.EACCES:
		return "Permission denied"
	case syscall.EBUSY:
		return "Resource busy"
	case syscall.ENOTDIR:
		return "Not a directory"
	case syscall.EISDIR:
		return "Is a directory"
	case syscall.EINVAL:
		return "Invalid argument"
	case syscall.ENFILE:
		return "Too many open files in system"
	case syscall.EMFILE:
		return "Too many open files"
	case syscall.ETXTBSY:
		return "Text file busy"
	case syscall.EAGAIN:
		return "Resource temporarily unavailable"
	case syscall.ENAMETOOLONG:
		return "File name too long"
	case syscall.ELOOP:
		return "Too many levels of symbolic links"
	case syscall.EOPNOTSUPP:
		return "Operation not supported"
	case syscall.ETIMEDOUT:
		return "Operation timed out"
	case syscall.EDEADLK:
		return "Resource deadlock avoided"
	case syscall.EINTR:
		return "Interrupted system call"
	default:
		return "Unknown error: " + errnoString(errno)
	}
}

func errnoString(errno syscall.Errno) string {
	n := int(errno)
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

// errnoOf は err の中の errno。無ければ ok が偽。
func errnoOf(err error) (syscall.Errno, bool) {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno, true
	}
	return 0, false
}

func isAbsentError(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	errno, ok := errnoOf(err)
	return ok && errno == syscall.ENOTDIR
}

// realpath は os.path.realpath(path) (strict でない)。存在しない部分は字面のまま残し、シンボリックリンクの輪は輪の手前で止める。
// 作業ディレクトリを読めないときは ok が偽 (Python は OSError)。
func realpath(filename string) (string, bool) {
	var path string
	if strings.HasPrefix(filename, "/") {
		path = "/"
	} else {
		cwd, ok := getcwd()
		if !ok {
			return "", false
		}
		path = cwd
	}
	// rest は未解決の部品の積み荷 (後ろが先頭)。nil の部品は「リンクの解決が終わった印」で、その下にリンク自身のパスがある。
	type part struct {
		name   string
		marker bool
	}
	parts := strings.Split(strings.TrimRight(filename, "/"), "/")
	rest := make([]part, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		rest = append(rest, part{name: parts[i]})
	}
	partCount := len(rest)
	seen := map[string]*string{}
	for partCount > 0 {
		p := rest[len(rest)-1]
		rest = rest[:len(rest)-1]
		if p.marker {
			link := rest[len(rest)-1].name
			rest = rest[:len(rest)-1]
			resolved := path
			seen[link] = &resolved
			continue
		}
		partCount--
		name := p.name
		if name == "" || name == "." {
			continue
		}
		if name == ".." {
			if i := strings.LastIndex(path, "/"); i >= 0 {
				path = path[:i]
			}
			if path == "" {
				path = "/"
			}
			continue
		}
		newpath := path + "/" + name
		if path == "/" {
			newpath = path + name
		}
		fi, err := os.Lstat(newpath)
		if err != nil {
			path = newpath
			continue
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			path = newpath
			continue
		}
		if resolved, ok := seen[newpath]; ok {
			if resolved != nil {
				path = *resolved
				continue
			}
			path = newpath // 解決の途中でまた同じリンクに戻った (輪)。
			continue
		}
		target, err := os.Readlink(newpath)
		if err != nil {
			path = newpath
			continue
		}
		if strings.HasPrefix(target, "/") {
			path = "/"
		}
		seen[newpath] = nil
		rest = append(rest, part{name: newpath}, part{marker: true})
		targetParts := strings.Split(target, "/")
		for i := len(targetParts) - 1; i >= 0; i-- {
			rest = append(rest, part{name: targetParts[i]})
		}
		partCount += len(targetParts)
	}
	return path, true
}

// InputPathProblem は input_path_problem。コマンドラインで渡されたファイルのパスを読んでよいか。読んでよければ ok が真、
// 読まないなら理由の文 (InputPath* のどれか) を返す。ファイルは開かない。
// - は標準入力なので読んでよい。ほかは、シンボリックリンクをたどった本当の場所 (realpath) で比べる。
// 作業フォルダの外か、作業フォルダからの道筋に点で始まる部分があれば読まない。パスに NUL を含むときは読んでよいとし、開くときの誤りに任せる。
// ほかの理由で場所を決められないときは外とみなす。
func InputPathProblem(path string) (reason string, ok bool) {
	if path == "-" {
		return "", true
	}
	cwd, have := getcwd()
	if !have {
		return InputPathNoCwd, false
	}
	base, have := realpath(cwd)
	if !have {
		return InputPathNoCwd, false
	}
	if strings.Contains(path, "\x00") {
		return "", true
	}
	target, have := realpath(path)
	if !have {
		return InputPathOutside, false
	}
	parts, inside := partsInside(base, target)
	if !inside {
		return InputPathOutside, false
	}
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return InputPathHidden, false
		}
	}
	return "", true
}

// commonPath は os.path.commonpath([a, b]) (どちらも絶対パス)。
func commonPath(a, b string) string {
	split := func(p string) []string {
		var out []string
		for _, c := range strings.Split(p, "/") {
			if c != "" && c != "." {
				out = append(out, c)
			}
		}
		return out
	}
	sa, sb := split(a), split(b)
	n := 0
	for n < len(sa) && n < len(sb) && sa[n] == sb[n] {
		n++
	}
	return "/" + strings.Join(sa[:n], "/")
}

// partsInside は _parts_inside。target が作業フォルダ base の中なら base からの道筋の部品 (base そのものなら空)、外なら inside が偽。
// まず字面で比べ、合わないときは target から上のフォルダをたどって base と同じフォルダ (同じ dev と ino) があるかを見る。
// macOS で大文字と小文字だけが違うパスや、日本語の名前の NFC と NFD だけが違うパスは、字面では合わないが同じフォルダなので中とみなす。
func partsInside(base, target string) ([]string, bool) {
	if commonPath(base, target) == base {
		return relPartsBelow(base, target), true
	}
	baseStat, err := os.Stat(base)
	if err != nil {
		return nil, false
	}
	st, ok := baseStat.Sys().(*syscall.Stat_t)
	if !ok || st.Ino == 0 {
		return nil, false
	}
	var parts []string
	current := target
	for {
		if fi, err := os.Stat(current); err == nil && sameFile(fi, baseStat) {
			for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
				parts[i], parts[j] = parts[j], parts[i]
			}
			return parts, true
		}
		parent, name := splitLast(current)
		if name == "" || parent == current {
			return nil, false
		}
		parts = append(parts, name)
		current = parent
	}
}

// relPartsBelow は os.path.relpath(target, base).split("/") (commonpath が base と一致した後なので、base の下の部品だけ。base そのものなら空)。
func relPartsBelow(base, target string) []string {
	baseTail := strings.TrimLeft(normpath(base), "/")
	targetTail := strings.TrimLeft(normpath(target), "/")
	var bl, tl []string
	if baseTail != "" {
		bl = strings.Split(baseTail, "/")
	}
	if targetTail != "" {
		tl = strings.Split(targetTail, "/")
	}
	i := 0
	for i < len(bl) && i < len(tl) && bl[i] == tl[i] {
		i++
	}
	rel := make([]string, 0, len(bl)-i+len(tl)-i)
	for range len(bl) - i {
		rel = append(rel, "..")
	}
	rel = append(rel, tl[i:]...)
	if len(rel) == 0 {
		return nil
	}
	return rel
}

func sameFile(a, b os.FileInfo) bool { return os.SameFile(a, b) }

// splitLast は os.path.split(p) (head と tail。head の末尾の / は、head が / だけのとき以外は落とす)。
func splitLast(p string) (head, tail string) {
	i := strings.LastIndex(p, "/") + 1
	head, tail = p[:i], p[i:]
	if head != "" && head != strings.Repeat("/", len(head)) {
		head = strings.TrimRight(head, "/")
	}
	return head, tail
}
