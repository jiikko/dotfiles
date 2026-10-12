package safefs

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ErrExists は作る・付ける名前の項目が既にあるときの失敗 (上書きはしない)。
var ErrExists = errors.New("同じ名前の項目が既にあります")

// ErrSwapped は呼び出し側が決めた実体 (dev, ino) と今の実体が違うときの失敗。ErrUnidentified は (0, 0) を渡されたとき。
// 文言 (どうすれば直るか) は呼び出し側が付ける (doctor は再スキャン、treefiler は読み直し)。
var (
	ErrSwapped      = errors.New("直前に別の実体へ差し替わりました")
	ErrUnidentified = errors.New("実体を識別できません")
)

// Rename は dir の中の from を to へ名前を変える (同じフォルダの中だけ。移動はしない)。既にある to は上書きしない。
// dev, ino は呼び出し側が「この実体の名前を変える」と決めた時点の from の同一性 (Lstat の値)。
//
// 大文字小文字・Unicode の正規化だけが違う名前 (`readme` → `README`、NFC → NFD) は、大文字小文字を区別しない APFS でも
// RENAME_EXCL がそのまま通る (EEXIST にならない。実測 2026-10-12、macOS の APFS の $TMPDIR)。別の名前の hardlink は
// EEXIST で断る。区別する FS・別の FS で振る舞いが違えば、宛先が既にある扱い (ErrExists) に倒れる。
func Rename(dir, from, to string, dev, ino uint64, guard Guard) error {
	if err := ValidName(from); err != nil {
		return err
	}
	if err := ValidName(to); err != nil {
		return err
	}
	if from == to {
		return nil
	}
	fd, err := openDirNoFollow(dir)
	if err != nil {
		return fmt.Errorf("フォルダを開けません: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()
	src, err := statAt(fd, from)
	if err != nil {
		return fmt.Errorf("名前を変える項目を確認できません: %w", err)
	}
	if dev == 0 && ino == 0 {
		return ErrUnidentified
	}
	if d, i := identity(&src); d != dev || i != ino {
		return ErrSwapped
	}
	if err := check(guard, "rename", filepath.Join(dir, from)); err != nil {
		return err
	}
	if err := check(guard, "rename-dest", filepath.Join(dir, to)); err != nil {
		return err
	}
	err = renameExcl(fd, from, fd, to)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EEXIST):
		return ErrExists
	case errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EINVAL):
		// RENAME_EXCL の効かない FS では、上書きの危険を冒すより変えない
		return errors.New("この場所では上書きしない名前の変更ができません")
	default:
		return err
	}
}

// Create は dir の中に name の空のファイル (isDir なら空のフォルダ) を作る。既にある項目は上書きしない
// (ファイルは O_CREAT|O_EXCL、フォルダは mkdirat。どちらも既にあれば EEXIST で失敗する)。
func Create(dir, name string, isDir bool, guard Guard) error {
	if err := ValidName(name); err != nil {
		return err
	}
	fd, err := openDirNoFollow(dir)
	if err != nil {
		return fmt.Errorf("フォルダを開けません: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()
	if err := check(guard, "create", filepath.Join(dir, name)); err != nil {
		return err
	}
	if isDir {
		err = unix.Mkdirat(fd, name, 0o755)
	} else {
		var f int
		f, err = unix.Openat(fd, name, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
		if err == nil {
			_ = unix.Close(f)
		}
	}
	if errors.Is(err, unix.EEXIST) {
		return ErrExists
	}
	return err
}

// Identity は path の項目そのもの (symlink なら link 自身) の (dev, ino)。Rename / TrashMove に渡す値を取る。
func Identity(path string) (dev, ino uint64, err error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return 0, 0, err
	}
	dev, ino = identity(&st)
	return dev, ino, nil
}

func statAt(fd int, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}
