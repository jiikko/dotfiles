// Package pyos は、パスを受ける・返す os と syscall の関数を、Python の os と同じ字の規則で呼ぶ口。
//
// ja-writing の文字列は本家の str と同じ字 (WTF-8。OS から受けた値は cmdenv が os.fsdecode と同じ規則で読む) で持つ。
// 本家は OS にパスを渡すときに os.fsencode し、OS から受けた名前・リンク先・作業フォルダを os.fsdecode する。
// ここの関数は、渡されたパスを pyfs.Encode してから os・syscall を呼び、受けた名前を pyfs.Decode して返す。
// 誤りの *fs.PathError の Path は渡された字に戻す (読み手は誤りの文に本家と同じ字を出し、標準エラーの口が \udcff の形にする)。
//
// os・syscall のパスの関数を直接呼ばない: 字のまま渡すと、U+DCFF は 0xff でなく WTF-8 の 3 バイト (ED B3 BF) で OS に届き、本家と別の名前を開く。
// 迂回は src/ja-writing/.golangci.local.yml の forbidigo が止める (ja-writing の oracle とテストを除く)。
package pyos

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"ja-writing/internal/pyfs"
)

// encode は path を OS に渡すバイトにする。戻せないサロゲート (U+DC80..U+DCFF でないもの) を持つ字は、本家では os の関数が
// UnicodeEncodeError を投げる。argv・環境変数・cwd は os.fsdecode で読むので、この形は記録の JSON から読んだパスでしか起きない。
// 本家の UnicodeEncodeError は写さず、字の WTF-8 のバイトのまま渡す (この口を入れる前の Go と同じ。README の check の「揃わない範囲」)。
func encode(path string) string {
	if b, err := pyfs.Encode(path); err == nil {
		return b
	}
	return path
}

// restore は err の *fs.PathError の Path を、OS に渡す前の字 path にする。
func restore(err error, path string) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		pe.Path = path
	}
	return err
}

// Stat は os.stat。
func Stat(path string) (fs.FileInfo, error) {
	fi, err := os.Stat(encode(path))
	return fi, restore(err, path)
}

// Lstat は os.lstat。
func Lstat(path string) (fs.FileInfo, error) {
	fi, err := os.Lstat(encode(path))
	return fi, restore(err, path)
}

// Open は open(path, "rb") (os.Open)。
func Open(path string) (*os.File, error) {
	f, err := os.Open(encode(path))
	return f, restore(err, path)
}

// OpenFile は os.open (os.OpenFile)。
func OpenFile(path string, flag int, perm fs.FileMode) (*os.File, error) {
	f, err := os.OpenFile(encode(path), flag, perm)
	return f, restore(err, path)
}

// ReadFile は open(path, "rb").read() (os.ReadFile)。
func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(encode(path))
	return data, restore(err, path)
}

// entry は名前を os.fsdecode した fs.DirEntry。
type entry struct {
	fs.DirEntry
	name string
}

func (e entry) Name() string { return e.name }

// ReadDir は os.scandir (名前で並べた os.ReadDir)。名前は os.fsdecode した字。
func ReadDir(path string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(encode(path))
	for i, e := range entries {
		entries[i] = entry{e, pyfs.Decode(e.Name())}
	}
	return entries, restore(err, path)
}

// Readlink は os.readlink。リンク先は os.fsdecode した字。
func Readlink(path string) (string, error) {
	target, err := os.Readlink(encode(path))
	return pyfs.Decode(target), restore(err, path)
}

// Mkdir は os.mkdir。
func Mkdir(path string, perm fs.FileMode) error {
	return restore(os.Mkdir(encode(path), perm), path)
}

// Chmod は os.chmod。
func Chmod(path string, mode fs.FileMode) error {
	return restore(os.Chmod(encode(path), mode), path)
}

// Remove は os.remove か os.rmdir (os.Remove)。
func Remove(path string) error {
	return restore(os.Remove(encode(path)), path)
}

// Rename は rename(2) (os.rename。syscall.Rename で、誤りは errno のまま)。
func Rename(source, target string) error {
	return syscall.Rename(encode(source), encode(target))
}

// Chflags は os.chflags。
func Chflags(path string, flags int) error {
	return syscall.Chflags(encode(path), flags)
}

// OpenFD は os.open を fd で返す (syscall.Open。誤りは errno のまま)。
func OpenFD(path string, mode int, perm uint32) (int, error) {
	return syscall.Open(encode(path), mode, perm)
}

// Getwd は os.getcwd (syscall.Getwd)。os.fsdecode した字。
func Getwd() (string, error) {
	cwd, err := syscall.Getwd()
	return pyfs.Decode(cwd), err
}

// Decode は OS から受けたパス (F_GETPATH の綴り・パスワードデータベースのホームなど、上の関数を通らないもの) を os.fsdecode した字。
func Decode(name string) string { return pyfs.Decode(name) }

// Link は os.link。誤りの *os.LinkError の Old・New は渡された字に戻す。
func Link(source, target string) error {
	err := os.Link(encode(source), encode(target))
	var le *os.LinkError
	if errors.As(err, &le) {
		le.Old, le.New = source, target
	}
	return err
}
