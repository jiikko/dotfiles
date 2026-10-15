// Package userdirs は道具ごとの設定の置き場を 1 か所で決める (issue 777)。
//
// 置き場は道具ごとに分ける ($XDG_CONFIG_HOME/<道具>、未設定なら ~/.config/<道具>)。前は treefiler と mac-lan-manager が
// ~/.config/glogx の下に、pro-con だけが XDG に従って置いていた。道具は将来 glogx から分離するので glogx の下には置かない
// (2026-10-16 のユーザー決定。XDG に従う・古い置き場のファイルは初回に移す、も同じ決定)。
package userdirs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ConfigBase は設定の置き場の根 ($XDG_CONFIG_HOME、未設定なら <home>/.config)。home を外から渡す道具 (pro-con) のため。
// 🚨 相対パスの XDG_CONFIG_HOME は無視する (XDG の仕様どおり)。受け付けると起動した cwd の下へ設定を移してしまい、cwd を変えて
// 起動すると設定が消えたように見える (issue 777 の敵対的レビュー P2)。
func ConfigBase(home string) string {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" && filepath.IsAbs(base) {
		return base
	}
	return filepath.Join(home, ".config")
}

// ConfigDir は app の設定の置き場 ($XDG_CONFIG_HOME/<app>、未設定なら ~/.config/<app>)。作りはしない。
func ConfigDir(app string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil && !filepath.IsAbs(os.Getenv("XDG_CONFIG_HOME")) {
		return "", err
	}
	return filepath.Join(ConfigBase(home), app), nil
}

// ConfigFile は app の設定ファイル name の置き場。legacy (前の置き場。~ からの相対) が在って新しい置き場に無ければ、1 回だけ移してから返す。
// 移せなかったとき (権限・別の volume など) は前の置き場を返し、前の設定を読み続ける (黙って既定に戻ったように見せない。issue 777 の敵対的レビュー P3)。
func ConfigFile(app, name, legacy string) (string, error) {
	dir, err := ConfigDir(app)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if legacy != "" {
		if home, err := os.UserHomeDir(); err == nil {
			old := filepath.Join(home, legacy)
			if err := MoveIfMissing(old, p); err != nil {
				if _, serr := os.Lstat(old); serr == nil {
					return old, nil
				}
			}
		}
	}
	return p, nil
}

// MoveIfMissing は置き場を変えたときの 1 回きりの引っ越し: newPath に何も無く oldPath にあるときだけ、oldPath を newPath へ移す。
// newPath に既にあれば何もしない (上書きしない。古い方は残る)。oldPath が無ければ何もしない。
// 移すのは rename なので、ファイルの中身・権限はそのまま。newPath の親は 0o700 で作る (設定は本人だけが読む)。
func MoveIfMissing(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return err // 既にある (err == nil) か、確かめられない
	}
	if _, err := os.Lstat(oldPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
		return err
	}
	// 前の置き場が相対 symlink なら、移すと指す先が変わって壊れる。絶対パスの link を新しく作ってから前の link を消す
	if target, err := os.Readlink(oldPath); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(oldPath), target)
		}
		if err := os.Symlink(target, newPath); err != nil {
			return err
		}
		return os.Remove(oldPath)
	}
	return os.Rename(oldPath, newPath)
}
