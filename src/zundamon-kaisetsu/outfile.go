package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// partPath は出力先の隣に一意な一時ファイルを作って名前を返す (同じ dir なので rename で置き換えられる。
// 同じ出力先へ同時に build しても取り合わない)。
func partPath(out string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+".*.part")
	if err != nil {
		return "", err
	}
	return f.Name(), f.Close()
}

// fileUmask は起動時の umask (main が読む。テストでは 022 とみなす)。
var fileUmask = os.FileMode(0o022)

// checkReplaceable は dst を一時ファイルからの rename で置き換えられるか (既存ならディレクトリでなく、書き込める) を見る。
// replaceKeepingMode と、build の最初の確認 (cmdBuild) が同じ判定を使う
func checkReplaceable(dst string) error {
	st, err := os.Stat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // まだ無い (置き換えではなく作る)
	}
	if err != nil {
		return err // 自分を指す symlink (ELOOP)・途中が dir でない・読めない、を「まだ無い」と読まない
	}
	if st.IsDir() {
		return fmt.Errorf("ディレクトリがある")
	}
	if err := syscall.Access(dst, 2 /* W_OK */); err != nil {
		return fmt.Errorf("書き込めない既存のファイル: %w", err)
	}
	return nil
}

// replaceKeepingMode は tmp を dst へ rename する。dst が既にあればそのパーミッションを引き継ぎ、無ければ 0666 から umask を
// 引いたもの (Python の write_text と同じ)。読み取り専用の既存のファイルは置き換えない (Python は PermissionError で止まる)。
// dst は呼び出し側で symlink を解決しておく (rename はリンクそのものを置き換えるので、リンク先に書く Python 版と違ってしまう)。
// 既存のファイルのハードリンクは切れる (rename は別の inode にする。出力物にハードリンクを張る運用は想定しない)。
func replaceKeepingMode(tmp, dst string) error {
	mode := 0o666 &^ fileUmask
	if err := checkReplaceable(dst); err != nil {
		return err
	}
	if st, err := os.Stat(dst); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// writeAtomic は書きかけのファイルをキャッシュとして拾わないよう、一時名に書いてから rename する。
//
// 合成のキャッシュは Python 版と同じく「一時ファイルに書いて rename」の意味にする: 既存のファイルが読み取り専用でも置き換え、
// パーミッションは常に 0666 から umask を引いたもの (既存のものを引き継がない)。出力 (writeOutput) は Python 版の write_text の
// 意味 (既存のパーミッションを保ち、書き込めないものは置き換えない) なので分ける (敵対的レビュー 4 周目 P2-1)。
func writeAtomic(path string, data []byte) error {
	tmp, err := partPath(path)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o666&^fileUmask); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeOutput は出力先の隣の一意な一時ファイルに書いてから置き換える (中断・失敗で前回の正常な出力を壊さず、一時ファイルも残さない)。
func writeOutput(dst string, data []byte) error {
	tmp, err := partPath(dst)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return replaceKeepingMode(tmp, dst)
}
