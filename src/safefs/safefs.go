// Package safefs はファイルを動かす・作る操作のうち、「上書きしない」「経路を差し替えられない」を守る部品を 1 か所に置く
// (doctor の削除エンジンと treefiler のファイル操作が共有する。issue 722)。macOS 専用 (renameatx_np)。
//
// 🚨 どの操作も、syscall の直前に呼び出し側の Guard を通す。Guard が nil なら何もせずに失敗する (fail-closed)。
// Guard は呼び出し側が持つ検査点で、production では素通りし、テストのハーネスがそこを差して一時ディレクトリの外を
// **実行前に**拒否する (`~/.claude/rules/sandbox-real-destructive-test-apis.md`)。safefs 自身はハーネスを持たない
// (doctor は disk/main_test.go の 4 段構え、それ以外は package sandbox を使う)。
package safefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Guard は破壊的・作成の操作の直前に呼ばれる検査点。op は操作の名前 (拒否の記録からどちら側で止まったかを読めるように分ける)、
// path はその操作が触るパス。error を返すと操作は行われない。
type Guard func(op, path string) error

// ProductionGuard は production の既定の Guard: 素通りする。ただし **テストのバイナリの中では拒否する**
// (テストがハーネスを差し忘れたまま本物の操作を呼ぶと、実データの場所へファイルが動く。差し忘れを「動かない」側に倒す)。
// 🚨 判定は実行ファイル名なので完全ではない (`go run` で書いた自作ハーネスは検出できない)。目的は差し忘れを止めることだけ。
func ProductionGuard(op, path string) error {
	if runningUnderTest {
		return fmt.Errorf("テスト中に %s が要求されましたが、ハーネスが設定されていません: %s", op, path)
	}
	return nil
}

var runningUnderTest = strings.HasSuffix(os.Args[0], ".test") || strings.Contains(os.Args[0], "/_test/")

func check(g Guard, op, path string) error {
	if g == nil {
		return fmt.Errorf("検査点が渡されていないので %s をしません: %s", op, path)
	}
	return g(op, path)
}

// systemLinkPrefixes は macOS がルート直下に置く symlink。/var → /private/var 等。
// 「経路の途中に symlink があれば拒否」の規律にこれが引っかかると TMPDIR (/var/folders/...) が
// 全部拒否されるので、この 3 つだけは先に実体へ書き換える (他の symlink は拒否のまま)。
var systemLinkPrefixes = map[string]string{"/var": "/private/var", "/tmp": "/private/tmp", "/etc": "/private/etc"}

// NormalizeSystemLinks は p の先頭の macOS 既定の symlink (/var /tmp /etc) を実体へ書き換える。
func NormalizeSystemLinks(p string) string {
	for from, to := range systemLinkPrefixes {
		if p == from || strings.HasPrefix(p, from+"/") {
			return to + p[len(from):]
		}
	}
	return p
}

// NoSymlinkInPath は経路の全要素が symlink でないことを確かめる (/var /tmp /etc は先に畳む)。
// 最終要素だけを見る Lstat / O_NOFOLLOW では足りない (HOME 自体が symlink の構成で実測)。
func NoSymlinkInPath(p string) error {
	for dir := NormalizeSystemLinks(filepath.Clean(p)); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("経路の途中に symlink がある: %s", dir)
		}
	}
	return nil
}

// openDirNoFollow はディレクトリを fd で開く (最後の要素が symlink なら拒否する)。
func openDirNoFollow(dir string) (int, error) {
	return unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
}

// renameExcl は「宛先が無いときだけ移す」rename。呼び出し側が Guard を通してから呼ぶ
// (ここで通すと、連番のリトライごとに検査が走って同じ記録が何度も積まれる)。
func renameExcl(srcDir int, srcBase string, dstDir int, dstBase string) error {
	return unix.RenameatxNp(srcDir, srcBase, dstDir, dstBase, unix.RENAME_EXCL)
}

// ValidName は 1 つのフォルダの中の名前として使えるか (操作がそのフォルダの外へ出ないこと)。
// 空・`.`・`..`・`/` を含む・NUL を含む・255 バイト超を断る。
func ValidName(name string) error {
	switch {
	case name == "":
		return errors.New("名前が空です")
	case name == "." || name == "..":
		return fmt.Errorf("%q は名前に使えません", name)
	case strings.ContainsRune(name, '/'):
		return errors.New("名前に / は使えません (同じフォルダの中だけで操作します)")
	case strings.ContainsRune(name, 0):
		return errors.New("名前に NUL は使えません")
	case len(name) > 255:
		return errors.New("名前が長すぎます (255 バイトまで)")
	}
	return nil
}

// ID は項目そのもの (lstat) の実体 (dev, inode)。呼び出し側が「この実体を操作する」と決めた時点の値を Rename / TrashMove へ渡す
// (2 つの uint64 で渡すと取り違えても型が通る。issue 734)。ゼロ値は「識別できない」で、操作は ErrUnidentified で断る。
type ID struct{ Dev, Ino uint64 }

func identity(st *unix.Stat_t) ID { return ID{Dev: uint64(st.Dev), Ino: st.Ino} }

// ResolveUnder は root の中のフォルダ dir (root からの絶対パス) の実体のパス。root だけは symlink を辿り、root から dir までの途中は
// 1 段ずつ Lstat して symlink とフォルダでないものを断る。🚨 dir を丸ごと EvalSymlinks しない: 呼び出し側が持っているパスを確かめた後で
// 途中のフォルダが外を指す symlink に差し替わると、root の外を操作することになる (treefiler の issue 722 の敵対レビューで再現)。
// 残る窓: ここで確かめてから Rename / Create / TrashMove が親を開くまでの間の差し替え (それらは最後の要素だけを O_NOFOLLOW で開く)。
func ResolveUnder(root, dir string) (string, error) {
	p, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", errors.New("root の外です")
	}
	if rel == "." {
		return p, nil
	}
	for part := range strings.SplitSeq(rel, "/") {
		p = filepath.Join(p, part)
		fi, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		if !fi.IsDir() { // symlink は Lstat で IsDir にならない
			return "", fmt.Errorf("経路の途中がフォルダでなくなりました (読み直してください): %s", part)
		}
	}
	return p, nil
}
