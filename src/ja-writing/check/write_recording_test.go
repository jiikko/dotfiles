package check

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"unicode/utf8"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pystr"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

// writeCall は状態フォルダへの書き込みと記録の読みの呼び出し。ok が偽なら、この関数たちの名前でない。
func (e *interp) writeCall(c oracle.Call) (out outcome, ok bool) {
	e.t.Helper()
	a := c.Args
	done := func(v pyjson.Value, err error) outcome {
		if err != nil {
			if c.SaveRaised {
				e.save(c, err)
			}
			return pyOSError(err)
		}
		e.save(c, v)
		return outcome{value: v}
	}
	switch c.Function {
	case "_ensure_state":
		return done(nil, EnsureState(e.str(a[0]), a[1].(bool))), true
	case "_write_atomic":
		return done(nil, WriteAtomic(e.str(a[0]), []byte(a[1].(oracle.Bytes)))), true
	case "_place_new_file":
		placed, err := placeNewFile(e.str(a[0]), []byte(a[1].(oracle.Bytes)))
		return done(placed, err), true
	case "_write_problem":
		err, isErr := e.ref(a[1]).(error)
		if !isErr {
			e.t.Fatalf("_write_problem に例外でない値を渡す case: %T", e.ref(a[1]))
		}
		return outcome{value: WriteProblem(e.str(a[0]), err)}, true
	case "_read_record_status":
		status, record, err := ReadRecordStatus(e.str(a[0]))
		if err != nil {
			e.t.Fatalf("記録に入れていない読めない JSON (%s): %v", a[0], err)
		}
		return outcome{value: pair(string(status), recordValue(record))}, true
	case "_read_record":
		record, err := ReadRecord(e.str(a[0]))
		if err != nil {
			e.t.Fatalf("記録に入れていない読めない JSON (%s): %v", a[0], err)
		}
		return outcome{value: recordValue(record)}, true
	}
	return outcome{}, false
}

func recordValue(record *pyjson.Object) pyjson.Value {
	if record == nil {
		return nil
	}
	return record
}

// pyOSError は Go の書き込みの誤りを、本家が投げる OSError の型と str(例外) にする ("[Errno N] 理由: 'ファイル名' -> 'ファイル名2'")。
func pyOSError(err error) outcome {
	var naming *namingError
	if errors.As(err, &naming) {
		return outcome{exc: &oracle.PyException{Type: "FileExistsError", Message: "[Errno 17] " + naming.strerror() + ": " + pystr.Repr(naming.Folder)}}
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return outcome{exc: &oracle.PyException{Type: "?", Message: err.Error()}}
	}
	message := "[Errno " + strconv.Itoa(int(errno)) + "] " + visibility.OSErrorReason("", errno)
	var linkErr *os.LinkError
	var pathErr *fs.PathError
	switch {
	case errors.As(err, &linkErr):
		message += ": " + pystr.Repr(linkErr.Old) + " -> " + pystr.Repr(linkErr.New)
	case errors.As(err, &pathErr):
		message += ": " + pystr.Repr(pathErr.Path)
	}
	return outcome{exc: &oracle.PyException{Type: visibility.OSErrorType(errno), Message: message}}
}

// treeValue は call_function.py の op tree の Go の写し。top から下の [相対パス, 種類, mode, 中身] の並び (名前は昇順に辿る)。
func treeValue(top string) (pyjson.Value, error) {
	var entries []pyjson.Value
	var visit func(path, rel string) error
	visit = func(path, rel string) error {
		info, err := os.Lstat(path)
		if err != nil {
			var errno syscall.Errno
			if !errors.As(err, &errno) {
				return err
			}
			entries = append(entries, []pyjson.Value{rel, "missing", int64(errno), nil})
			return nil
		}
		mode := int64(info.Sys().(*syscall.Stat_t).Mode & 0o7777) // 本家の S_IMODE。Perm() は setgid・sticky を落とす
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entries = append(entries, []pyjson.Value{rel, "link", mode, target})
		case info.IsDir():
			dir, err := os.Open(path)
			var names []string
			if err == nil {
				names, err = dir.Readdirnames(-1)
				_ = dir.Close() // 一覧は読めた
			}
			if err != nil {
				var errno syscall.Errno
				if !errors.As(err, &errno) {
					return err
				}
				entries = append(entries, []pyjson.Value{rel, "dir", mode, int64(errno)})
				return nil
			}
			entries = append(entries, []pyjson.Value{rel, "dir", mode, nil})
			slices.Sort(names)
			for _, name := range names {
				child := name
				if rel != "." {
					child = rel + "/" + name
				}
				if err := visit(filepath.Join(path, name), child); err != nil {
					return err
				}
			}
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !utf8.Valid(data) {
				return errors.New(path + ": UTF-8 でない中身 (op tree は UTF-8 だけを読む)")
			}
			entries = append(entries, []pyjson.Value{rel, "file", mode, string(data)})
		default:
			entries = append(entries, []pyjson.Value{rel, "other", mode, nil})
		}
		return nil
	}
	if err := visit(top, "."); err != nil {
		return nil, err
	}
	return entries, nil
}
