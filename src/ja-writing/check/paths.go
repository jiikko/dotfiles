package check

import (
	"fmt"
	"os"
	"strings"

	"ja-writing/cmdenv"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// NoWorkingDirectoryError は作業ディレクトリが消えているか読めないとき (本家の _NoWorkingDirectory。入力の誤りとして終了コード 2)。
type NoWorkingDirectoryError struct{ Message string }

func (e *NoWorkingDirectoryError) Error() string { return e.Message }

const stateDirEnv = "YOMIYASU_STATE_DIR"

// Getcwd は _getcwd。読めなければ *NoWorkingDirectoryError。
func Getcwd() (string, error) {
	cwd, ok := visibility.Getcwd()
	if !ok {
		return "", &NoWorkingDirectoryError{Message: fmt.Sprintf(noWorkingDirError, "No such file or directory")}
	}
	return cwd, nil
}

// sameWorkingDirectory は、env.Cwd がプロセスの作業ディレクトリと同じフォルダかを確かめ、違えば誤りを返す (内部エラー)。
// check の部品は相対パスの読み書きと _getcwd をプロセスの作業ディレクトリで行う (OS がそこで解決する) ので、Env にだけ別の作業フォルダを
// 渡されても従えない。黙って片方を使うと、入力を読む場所と記録に書く cwd・状態フォルダの場所が食い違う。
// env.Cwd が空のときと、どちらかを読めない (消えた作業ディレクトリ) ときは確かめない (読めない作業ディレクトリは Getcwd が本家と同じ誤りにする)。
func sameWorkingDirectory(env cmdenv.Env) error {
	if env.Cwd == "" {
		return nil
	}
	given, givenErr := os.Stat(env.Cwd)
	here, hereErr := os.Stat(".")
	readable := givenErr == nil && hereErr == nil
	if !readable || os.SameFile(given, here) {
		return nil
	}
	return fmt.Errorf("作業フォルダ (Env.Cwd) %s が、プロセスの作業フォルダと違います（check はプロセスの作業フォルダでパスを読みます）", env.Cwd)
}

// AbsPath は _abspath (os.path.abspath と同じ。相対パスのときだけ作業ディレクトリを読む)。
func AbsPath(path string) (string, error) {
	if strings.HasPrefix(path, "/") {
		return visibility.Normpath(path), nil
	}
	cwd, err := Getcwd()
	if err != nil {
		return "", err
	}
	return visibility.AbsolutePath(visibility.PathJoin(cwd, path))
}

// DiskPath は _disk_path。パスの比較と by-path のキーに使う綴り。realpath のあと、macOS では開いたファイルの F_GETPATH でディスク上の綴りを取る。
// 大文字と小文字を区別しないファイルシステムで、大文字と小文字や NFC と NFD の違う綴りで同じファイルを渡しても同じ綴りになる。
// macOS では、取った綴りを NFC にそろえ、大文字と小文字を区別しないファイルシステムでは casefold もする。ファイルを消して別の綴りで作り直すと
// (git restore、Finder で作った NFD の名前を NFC で打って作り直す) ディスク上の綴りが変わり、前の回につながらなくなるため。
// path に NUL を含むときの扱いは写していない (本家は ValueError で落ちる。コマンドラインの引数は NUL を持てない)。
func DiskPath(path string) (string, error) {
	real, ok := visibility.Realpath(path)
	if !ok {
		return "", visibilityNoCwd()
	}
	if !isDarwin {
		return real, nil
	}
	probe := diskSpelling(real)
	if probe == "" {
		probe = real
	}
	spelled := pyunicode.Normalize(pyunicode.NFC, probe)
	if CaseInsensitive(probe) {
		return pyunicode.Casefold(spelled), nil
	}
	return spelled, nil
}

// visibilityNoCwd は相対パスの realpath が作業ディレクトリを読めなかったときの誤り (os.getcwd の FileNotFoundError)。
func visibilityNoCwd() error {
	_, err := visibility.AbsolutePath("x")
	return err
}

// CaseInsensitive は _case_insensitive。path (なければ、あるところまでさかのぼったフォルダ) のファイルシステムが大文字と小文字を区別しないか。
// 大文字と小文字を入れ替えた綴りで同じファイルを開けるかで確かめる (Python には問い合わせる関数がない)。
func CaseInsensitive(path string) bool {
	current := path
	for {
		if _, err := os.Lstat(current); err == nil {
			break
		}
		parent := visibility.PathParent(current)
		if parent == current {
			return false
		}
		current = parent
	}
	swapped := pyunicode.Swapcase(current)
	if swapped == current {
		return false
	}
	mine, err := os.Stat(current)
	if err != nil {
		return false
	}
	other, err := os.Stat(swapped)
	if err != nil {
		return false
	}
	return os.SameFile(mine, other)
}

// Place は _place。パスを比べるための文字列 (_disk_path と normcase。posix の normcase は何もしない)。
func Place(path string) (string, error) {
	abs, err := AbsPath(path)
	if err != nil {
		return "", err
	}
	return DiskPath(abs)
}

// SamePath は _same_path。
func SamePath(a, b string) (bool, error) {
	pa, err := Place(a)
	if err != nil {
		return false, err
	}
	pb, err := Place(b)
	if err != nil {
		return false, err
	}
	return pa == pb, nil
}

// SameFile は _same_file。同じファイルか (パスの比較と、あれば samefile。大文字と小文字を区別しないファイルシステムやハードリンクのため)。
func SameFile(a, b string) (bool, error) {
	same, err := SamePath(a, b)
	if err != nil || same {
		return same, err
	}
	fa, err := os.Stat(a)
	if err != nil {
		return false, nil //nolint:nilerr // Path.samefile の OSError は「違うファイル」
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, nil //nolint:nilerr // 同上
	}
	return os.SameFile(fa, fb), nil
}

// HomePath は _home_path。--state-dir と環境変数 YOMIYASU_STATE_DIR の値の頭の ~ を、ホームフォルダに置き換える。
// 置き換えるのは、値が「~」だけのときと、「~/」で始まるときだけ。シェルの引用符の中や、エージェントの設定ファイルの環境変数では ~ が展開されないまま届くため。
// 「~user/x」や「~x」は置き換えない (その名前の利用者が機械にいるかどうかで、場所が変わらないように)。
// ホームが決まらないとき (HOME が空かルートだけ・取れない・「~」で始まる) は、値をそのまま返す (作業ディレクトリからの相対パス)。
// HOME が空かルートだけのときは状態フォルダをファイルシステムのルートに置かないために、expanduser の結果を使わない。
func HomePath(value string, lookup func(string) (string, bool)) string {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value
	}
	if configured, set := lookup("HOME"); set && strings.Trim(pystr.Strip(configured), "/") == "" {
		return value
	}
	expanded, ok := visibility.ExpandUser(lookup)
	if !ok {
		return value
	}
	home := visibility.PathString(expanded)
	result := home + value[1:]
	if !strings.HasPrefix(home, "~") && strings.Trim(pystr.Strip(home), "/") != "" && !strings.HasPrefix(result, "~") {
		return result
	}
	return value
}

// stateDirFromEnv は環境変数で決めた状態フォルダの値 (空白だけと、展開されなかった ${…} を含む値は無いものとして "" を返す)。
func stateDirFromEnv(lookup func(string) (string, bool)) string {
	value, set := lookup(stateDirEnv)
	if !set || pystr.Strip(value) == "" || strings.Contains(value, "${") {
		return ""
	}
	return value
}

// StateDirFor は state_dir_for (run・verify・フックで同じ規則)。ここではフォルダを作らない。
//
//  1. option (--state-dir)。2. 環境変数 YOMIYASU_STATE_DIR。3. cwd から上へたどり、.git (フォルダでもファイルでもよい) がある最初のフォルダの .yomiyasu/。
//  4. cwd の .yomiyasu/。
//
// 1 と 2 の値の頭の ~ は HomePath の決まりでホームフォルダに置き換える。環境変数が空白だけのときと、展開されなかった ${…} を含むときは、ないものとして 3 と 4 に進む
// (作業フォルダに「${CLAUDE_PROJECT_DIR}」のようなフォルダを作らない)。
func StateDirFor(cwd, option string, lookup func(string) (string, bool)) (string, error) {
	selected := option
	if selected == "" {
		selected = stateDirFromEnv(lookup)
	}
	if selected != "" {
		return visibility.AbsolutePath(visibility.PathJoin(cwd, HomePath(selected, lookup)))
	}
	cwd, err := visibility.AbsolutePath(cwd)
	if err != nil {
		return "", err
	}
	current := cwd
	for {
		if visibility.PathExists(visibility.PathJoin(current, ".git")) {
			return visibility.PathJoin(current, ".yomiyasu"), nil
		}
		parent := visibility.PathParent(current)
		if parent == current {
			return visibility.PathJoin(cwd, ".yomiyasu"), nil
		}
		current = parent
	}
}

// StateDir は _state_dir。絶対パスで指定したときは、作業ディレクトリが消えていても通る。
func StateDir(option string, lookup func(string) (string, bool)) (string, error) {
	selected := option
	if selected == "" {
		selected = stateDirFromEnv(lookup)
	}
	if selected != "" {
		selected = HomePath(selected, lookup)
		if strings.HasPrefix(selected, "/") {
			return visibility.Normpath(selected), nil
		}
	}
	cwd, err := Getcwd()
	if err != nil {
		return "", err
	}
	return StateDirFor(cwd, option, lookup)
}

// StateDirGiven は _state_dir_given。状態フォルダを --state-dir か環境変数 YOMIYASU_STATE_DIR で決めたか。
func StateDirGiven(option string, lookup func(string) (string, bool)) bool {
	return option != "" || stateDirFromEnv(lookup) != ""
}
