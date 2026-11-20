package check

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ja-writing/visibility"
)

// src/atomicfile.Write に寄せない。一時ファイルの名前 (`<名前>.tmp.<乱数>`) が本家の `.tmp-<16進>` と違い、
// WriteProblem が書けなかったフォルダを一時ファイルの名前の形から求める (本家の _write_problem) ので、
// 利用者に見せる文と、残った一時ファイルの名前が本家と変わる。O_NOFOLLOW も付かない。

const (
	temporaryTries = 100
	replaceTries   = 6
	replaceWait    = 50 * time.Millisecond
	temporaryHead  = ".tmp-"
	stateJSON      = "{\"format\": \"yomiyasu-state\", \"format_version\": 1}\n"
)

// 一時ファイルの名前の乱数と、置くときの link。テストが、本物のファイルシステムでは起こせない失敗 (名前が 100 回重なる・
// link を作れないファイルシステム・作ったフォルダに書けない) を差し込むために差し替える。並行に呼ばれる処理からは差し替えない。
var (
	randomRead = rand.Read
	linkFile   = os.Link
)

// namingError は、一時ファイルの名前を temporaryTries 回試して決められなかったときの FileExistsError。
type namingError struct{ Folder string }

func (e *namingError) Error() string { return e.Folder + ": " + e.strerror() }

func (e *namingError) strerror() string {
	return "一時ファイルの名前を100回試して決められません"
}

// streamError は書き込み・閉じるときの失敗。本家の OSError は filename を持たないので、WriteProblem は状態フォルダを示す。
type streamError struct{ Err error }

func (e *streamError) Error() string { return e.Err.Error() }
func (e *streamError) Unwrap() error { return e.Err }

func unpathed(err error) error {
	if err == nil {
		return nil
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return &streamError{Err: pathErr.Err}
	}
	return &streamError{Err: err}
}

func isPermission(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

// fileInfo は stat (follow のとき) か lstat の結果。読めなければ nil (本家の _path_matches と os.path.lexists は読めない名前を「無い」とみなす)。
func fileInfo(path string, follow bool) os.FileInfo {
	stat := os.Lstat
	if follow {
		stat = os.Stat
	}
	info, err := stat(path)
	if err != nil {
		return nil
	}
	return info
}

func isDir(path string) bool {
	info := fileInfo(path, true)
	return info != nil && info.IsDir()
}

// mkstemp は _mkstemp。folder に .tmp-<16進> を O_EXCL・O_NOFOLLOW・0o600 で作って開く。
func mkstemp(folder string) (*os.File, string, error) {
	folder, err := AbsPath(folder)
	if err != nil {
		return nil, "", err
	}
	for range temporaryTries {
		var random [8]byte
		if _, err := randomRead(random[:]); err != nil {
			return nil, "", err
		}
		path := visibility.PathJoin(folder, temporaryHead+hex.EncodeToString(random[:]))
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if err == nil {
			return file, path, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return nil, "", err
	}
	return nil, "", &namingError{Folder: folder}
}

func writeAndClose(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close() // 書けなかった理由を返す。閉じる失敗はその後ろの事情
		return unpathed(err)
	}
	return unpathed(file.Close())
}

// replace は _replace。PermissionError のときは 50 ミリ秒待って 5 回まで入れ替え直す (本家は OS によらず書き直す)。
// os.Rename を使わない。宛先がフォルダのとき rename(2) を呼ばずに EEXIST を返し、本家の os.replace (rename(2) の EISDIR) と理由の文が変わる。
func replace(source, target string) error {
	for attempt := range replaceTries {
		err := syscall.Rename(source, target)
		if err == nil {
			return nil
		}
		if !isPermission(err) || attempt == replaceTries-1 {
			return &os.LinkError{Op: "rename", Old: source, New: target, Err: err}
		}
		time.Sleep(replaceWait) // sleep-ok: 本家の待ち: 入れ替えを試し直す間隔は本家の _REPLACE_WAIT
	}
	return nil
}

// WriteAtomic は _write_atomic。同じフォルダの一時ファイルに書いてから入れ替える (読む側が書きかけを見ない)。失敗したら一時ファイルを消す。
func WriteAtomic(path string, data []byte) error {
	file, temporary, err := mkstemp(visibility.PathParent(path))
	if err != nil {
		return err
	}
	err = writeAndClose(file, data)
	if err == nil {
		err = os.Chmod(temporary, 0o600)
	}
	if err == nil {
		err = replace(temporary, path)
	}
	if err != nil {
		_ = os.Remove(temporary) // 本家も消せなければそのまま元の例外を出す
	}
	return err
}

// hiddenError は _raise_if_hidden。mkdir が EEXIST を出したのにフォルダと確かめられない理由が PermissionError なら、それを返す。
func hiddenError(path string, exists error) error {
	if _, err := os.Lstat(path); err != nil && isPermission(err) {
		return err
	}
	return exists
}

func stillPlaced(path string, info os.FileInfo) bool {
	now, err := os.Lstat(path)
	return err == nil && os.SameFile(now, info)
}

// placeNewFile は _place_new_file。path にまだ何も無いときだけ data を置き、置いたら true。
// 一時ファイルに書いてから link で置き、link を作れないファイルシステムでは O_EXCL で作ってから書く。
func placeNewFile(path string, data []byte) (bool, error) {
	file, temporary, err := mkstemp(visibility.PathParent(path))
	if err != nil {
		return false, err
	}
	err = writeAndClose(file, data)
	if err == nil {
		err = os.Chmod(temporary, 0o600)
	}
	if err != nil {
		_ = os.Remove(temporary) // 本家も消せなければそのまま元の例外を出す
		return false, err
	}
	linkErr := linkFile(temporary, path)
	_ = os.Remove(temporary) // 本家の finally と同じく、消せなくても続ける
	if linkErr == nil {
		return true, nil
	}
	if errors.Is(linkErr, fs.ErrExist) {
		return false, nil
	}
	return placeExclusive(path, data)
}

func placeExclusive(path string, data []byte) (bool, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close() // stat の失敗を返す
		return false, err
	}
	err = writeAndClose(file, data)
	if err == nil {
		err = os.Chmod(path, 0o600)
	}
	if err != nil {
		if stillPlaced(path, info) {
			_ = os.Remove(path) // 本家も消せなければそのまま元の例外を出す
		}
		return false, err
	}
	return true, nil
}

// writeStateFiles は _write_state_files。.gitignore を置き、置けたときだけ state.json を置く。
// 置けた .gitignore は、state.json を書けなくても消さない。
func writeStateFiles(state string) error {
	placed, err := placeNewFile(visibility.PathJoin(state, ".gitignore"), []byte(GitignoreText))
	if err != nil || !placed {
		return err
	}
	_, err = placeNewFile(visibility.PathJoin(state, "state.json"), []byte(stateJSON))
	return err
}

// addStateFiles は _add_state_files。既定の場所の .yomiyasu/ で、リンクでなく、.gitignore も state.json も無いときだけ 2 つを足す。
func addStateFiles(state string, defaultPlace bool) error {
	if !defaultPlace {
		return nil
	}
	if info, err := os.Lstat(state); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	if fileInfo(visibility.PathJoin(state, ".gitignore"), false) != nil || fileInfo(visibility.PathJoin(state, "state.json"), false) != nil {
		return nil
	}
	return writeStateFiles(state)
}

// mkdirParents は Path(path).mkdir(parents=True, exist_ok=True)。os.MkdirAll にしない: 途中にファイルがあると ENOTDIR を
// 返し (pathlib は FileExistsError)、誤りの名前も途中の部品になって、利用者に見せる文が本家と変わる。
func mkdirParents(path string) error {
	err := os.Mkdir(path, 0o777)
	if errors.Is(err, fs.ErrNotExist) {
		parent := visibility.PathParent(path)
		if parent == path {
			return err
		}
		if err := mkdirParents(parent); err != nil {
			return err
		}
		err = os.Mkdir(path, 0o777)
		if errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err != nil && !isDir(path) {
		return err
	}
	return nil
}

// EnsureState は _ensure_state。状態フォルダを用意する。作ったときは .gitignore と state.json を書き、
// 書けなければ作りかけのフォルダを消す (空でなければ消えない)。すでにあるフォルダは addStateFiles の決まりのときだけ足す。
func EnsureState(state string, defaultPlace bool) error {
	if isDir(state) {
		return addStateFiles(state, defaultPlace)
	}
	if parent := visibility.PathParent(state); parent != "" && !isDir(parent) {
		if err := mkdirParents(parent); err != nil {
			return err
		}
	}
	if err := os.Mkdir(state, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) && isDir(state) {
			return addStateFiles(state, defaultPlace)
		}
		if errors.Is(err, fs.ErrExist) {
			return hiddenError(state, err)
		}
		return err
	}
	err := os.Chmod(state, 0o700)
	if err == nil {
		err = writeStateFiles(state)
	}
	if err != nil {
		_ = os.Remove(state) // 空でなければ消えない (本家の rmdir と同じ)
	}
	return err
}

// WriteProblem は _write_problem。記録を書けなかった理由の文。一時ファイルの名前でなく、書けなかったフォルダを示す。
func WriteProblem(state string, err error) string {
	where := state
	var reason string
	var naming *namingError
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &naming):
		where, reason = naming.Folder, naming.strerror()
	case errors.As(err, &linkErr):
		where = linkErr.Old
	case errors.As(err, &pathErr):
		where = pathErr.Path
	}
	if where == "" {
		where = state
	}
	if strings.HasPrefix(filepath.Base(where), temporaryHead) {
		where = visibility.PathParent(where)
	}
	if reason == "" {
		reason = visibility.OSErrorReason(where, err)
	}
	return where + " に書けません: " + reason
}
