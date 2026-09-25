// Package upgrade は pro-con のライブアップグレード: 動いている間にソースが変わったら裏でビルドし、
// 人間の合図 (ctrl+r) で自分自身を新しいバイナリへ入れ替える (syscall.Exec。PID と端末はそのまま)。
//
// 🚨 **ビルドするかの判定とビルドそのものは bin/lib/go_autobuild.zsh (shim) に任せる** (go_autobuild_spawn_if_stale)。
// 入力の指紋・*_test.go の除外・lock・前回の途中死の掃除・失敗の記録と TTL は shim が正本で、Go 側へ写経しない
// (glogx の autobuild.go の spawnAutobuild と同じ方針。自前の近似 = mtime の順序比較は、ビルド中の編集・
// ファイルの削除・未来の mtime を取りこぼし、shim の .autobuild.built ともずれた: 敵対レビュー 2026-09-24)。
// pro-con がするのは「shim に尋ねる」「バイナリが差し替わったかを見る」「切り替える」だけ。
//
// 状態の正本は backend 側 (本番ではファイル。PG は Claude Code の bg session) なので、入れ替えで失うのは
// UI の状態 (タブ・レーン・選択・書きかけの入力) だけ。それも Save / Load で引き継ぐ。
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// BinaryName は shim が src/pro-con/ に置くバイナリの名前。
const BinaryName = "pro-con"

var ErrNoSource = errors.New("ソースのディレクトリから起動していないのでライブアップグレードは無効")

// Source は pro-con のソース (go.mod のあるディレクトリ = バイナリの置き場) と shim。
type Source struct {
	Dir  string
	Exe  string
	Shim string
}

// Detect は実行中のバイナリ exe から、ソースと shim を見つける。exe が「go.mod のあるディレクトリの中の pro-con」で、
// repo の bin/lib/go_autobuild.zsh が辿れるときだけ有効 (shim を経ない起動・別の場所へコピーされたバイナリでは無効)。
func Detect(exe string) (Source, error) {
	exe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// 包むのは ErrNoSource だけ (呼び出し側の契約は「無効」の 1 つ)。EvalSymlinks の失敗は理由の文として添える
		return Source{}, fmt.Errorf("%w: %v", ErrNoSource, err) //nolint:errorlint // 原因まで包むと fs.ErrNotExist 等にも一致し、「無効」以外の分岐を呼び出し側に誘う
	}
	dir := filepath.Dir(exe)
	if filepath.Base(exe) != BinaryName {
		return Source{}, fmt.Errorf("%w: バイナリの名前が %s ではない (%s)", ErrNoSource, BinaryName, exe)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return Source{}, fmt.Errorf("%w: %s に go.mod が無い", ErrNoSource, dir)
	}
	shim := filepath.Join(dir, "..", "..", "bin", "lib", "go_autobuild.zsh")
	if _, err := os.Stat(shim); err != nil {
		return Source{}, fmt.Errorf("%w: %s が無い", ErrNoSource, shim)
	}
	return Source{Dir: dir, Exe: exe, Shim: filepath.Clean(shim)}, nil
}

// Runner は外部コマンドを実行する (テストで差し替える)。成功 (rc=0) なら nil。
type Runner func(ctx context.Context, name string, args ...string) error

// ExecRunner は本物の実行。
func ExecRunner(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	return cmd.Run()
}

// Spawn は shim に「今ビルドが要るか」を尋ね、要るなら裏ビルドを起動させる。起動したら true。
// 判定 (指紋) も backoff (同じ入力で落ちたら再挑戦しない) も多重起動の防止 (lock) も shim の側にある。
// shim の rc=1 は「要らない / backoff 中」で、エラーではない。zsh が無い・timeout など、尋ねること自体ができなかった
// ときだけ err を返す (「要らない」と区別しないと、確認できていないのに最新に見える)。
// パスは位置引数で渡す (スクリプトへ文字列連結しない)。
func (s Source) Spawn(ctx context.Context, run Runner) (bool, error) {
	err := run(ctx, "zsh", "-c", `source "$1"; go_autobuild_spawn_if_stale "$2" "$3"`, "zsh", s.Shim, s.Dir, BinaryName)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1 && ctx.Err() == nil:
		return false, nil
	}
	return false, fmt.Errorf("shim に尋ねられない: %w", err)
}

// Replaced は起動したときのバイナリ start と、今 exe のパスにあるファイルが違うか (shim が差し替えた = 新版がある)。
// 同一性 (inode) と更新時刻・大きさで見る (時刻の前後では比べない)。
func (s Source) Replaced(start os.FileInfo) (bool, error) {
	cur, err := os.Stat(s.Exe)
	if err != nil {
		return false, err
	}
	return !os.SameFile(start, cur) || !cur.ModTime().Equal(start.ModTime()) || cur.Size() != start.Size(), nil
}

// FailedSince は since より後に shim がビルドの失敗を記録したか (.autobuild.failed)。shim がこの記録を消すのは
// ビルドが成功したときだけなので、「記録がある」ではなく「since より後に書かれた」で見る。
func (s Source) FailedSince(since time.Time) bool {
	info, err := os.Stat(filepath.Join(s.Dir, ".autobuild.failed"))
	return err == nil && info.ModTime().After(since)
}

// LogPath は shim のビルドのログ。
func (s Source) LogPath() string { return filepath.Join(s.Dir, ".autobuild.log") }
