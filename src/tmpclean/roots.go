package tmpclean

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"safefs"
)

// stopError は前提の検査で止めたこと (判定不能・承認の不一致・想定外の場所)。判定不能を「生きていない」「無い」に丸めずに、
// 呼び出し側へ止まった理由として返す。終了コードは「1 件も移していなければ 2、移した後なら 4」(Run が決める)。
type stopError struct{ msg string }

func (e *stopError) Error() string { return e.msg }

func stopf(format string, a ...any) error { return &stopError{msg: fmt.Sprintf(format, a...)} }

func isStop(err error) bool {
	var s *stopError
	return errors.As(err, &s)
}

// roots は動かしてよい場所の根。tmpdir が空なら $TMPDIR は扱わない。
type roots struct {
	session string
	tmpdir  string
}

var tmpDirShape = regexp.MustCompile(`^/private/var/folders/[^/]+/[^/]+/T$`)

// resolveRoots は根を決めて検査する。needTmpdir は tmpdir の種類を扱うか (扱うのに決められなければ止める。
// 黙って飛ばすと、その種類の候補が全部「候補でない」で残り、成功に見える)。allowMissingSession は数えるだけのときに
// scratchpad の根が無いのを「0 件」として通すか (無いと r.session は空。Claude Code を使っていない機械で $TMPDIR まで見えなくしない)。
func resolveRoots(d deps, needTmpdir, allowMissingSession bool) (roots, error) {
	var r roots
	if d.strictRoots && d.sessionRoot != fmt.Sprintf("/private/tmp/claude-%d", os.Getuid()) {
		return r, stopf("scratchpad の根が想定外: %s", d.sessionRoot)
	}
	if _, err := os.Lstat(d.sessionRoot); !allowMissingSession || !errors.Is(err, fs.ErrNotExist) {
		if err := realDir(d.sessionRoot); err != nil {
			return r, stopf("scratchpad の根を使えない: %v", err)
		}
		r.session = d.sessionRoot
	}
	if d.tmpDirEnv == "" {
		if needTmpdir {
			return r, stopf("TMPDIR が空なので tmpdir は扱えない")
		}
		return r, nil
	}
	real, err := filepath.EvalSymlinks(d.tmpDirEnv)
	if err != nil {
		if needTmpdir {
			return r, stopf("TMPDIR を実パスにできない: %v", err)
		}
		return r, nil
	}
	real = filepath.Clean(real)
	if d.strictRoots && !tmpDirShape.MatchString(real) {
		if needTmpdir {
			return r, stopf("TMPDIR が /private/var/folders/*/*/T の形でない: %s", real)
		}
		return r, nil
	}
	if err := realDir(real); err != nil {
		if needTmpdir {
			return r, stopf("TMPDIR を使えない: %v", err)
		}
		return r, nil
	}
	r.tmpdir = real
	return r, nil
}

// realDir は p が実体の dir で、経路のどこにも symlink が無いこと (/var /tmp /etc は先に畳む) を確かめる。
func realDir(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("dir でない: %s", p)
	}
	return safefs.NoSymlinkInPath(p)
}
