// Package testtmp は「テストが起こした一時ディレクトリを、中断されても回収する」ための
// 共通ヘルパー (issue 305)。
//
// なぜ必要か: `TestMain` の `MkdirTemp` → `m.Run()` → `RemoveAll` という形は、
// **`m.Run()` から戻らない終了 (SIGKILL / panic / -timeout の SIGQUIT) では末尾に到達しない**。
// 実測 2026-09-09: コンパイル済みのテストバイナリを実行中に SIGKILL すると
// `glogx-test-cache*` が 1 件残った (同 2026-09-06 の監査では 40 件溜まっていた)。
// trap / defer は中断で走らないので、**回収の責任は「次回の起動時の掃除」が持つ**。
// 参照実装は scripts/with_fresh_worktree.sh:sweep_stale (同じ「自分の prefix かつ pid が
// 生きていないものだけ消す」形)。
//
// 🚨 **掃除は破壊的操作なので、母集合を自分が作った 1 ディレクトリに閉じている**
// (`adversarial-review-own-safeguards.md` §0-A: 作らずに済む構造を先に問う)。
// TMPDIR 全体を prefix で走査する形は採らない — TMPDIR は env で差し替え可能で、テストは
// 日常的に差し替えるので、`TMPDIR=/tmp` で走らせた瞬間に「他人のものを prefix で拾う」形になる。
// ここが見るのは **`$TMPDIR/dotfiles-go-test` の直下だけ**で、そこは自分が作ったもの以外入らない。
//
// 🚨 **プロセスは決して kill しない**。pid は「この dir はまだ使われているか」の判定にしか
// 使わず、kill するのはディレクトリだけ。pid 再利用の窓を踏んでも、被害は
// 「まだ生きている run の dir を消さずに残す」側 (fail-safe) にしか出ない。
package testtmp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// rootName は掃除の母集合。ここより外は絶対に触らない。
const rootName = "dotfiles-go-test"

// Setup は掃除つきの一時ディレクトリを作り、後始末の関数を返す。
//
// prefix は呼び出し元を見分けるための名前 (例 "glogx-cache")。返す dir は
// `$TMPDIR/dotfiles-go-test/<prefix>.<pid>.<連番>` の形で、pid を名前に持つのは
// 「まだ走っているテストの dir を消さない」ためだけ。
//
// 掃除した件数は第 2 戻り値。**0 件を成功の証拠にしない**ため、呼び出し元が報告できるようにする
// (verify-execution-not-just-exit-code.md)。
func Setup(prefix string) (dir string, swept int, cleanup func(), err error) {
	if prefix == "" || strings.ContainsAny(prefix, `/\.`) {
		return "", 0, nil, fmt.Errorf("testtmp: prefix が不正 (空 / パス区切り / ドットを含む): %q", prefix)
	}
	root := filepath.Join(os.TempDir(), rootName)
	// 0o700: 他ユーザーに触らせない。TMPDIR が per-user でない環境 (TMPDIR=/tmp) でも同じ。
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", 0, nil, fmt.Errorf("testtmp: 母集合の dir を作れない: %w", err)
	}
	swept = sweep(root)

	dir, err = os.MkdirTemp(root, fmt.Sprintf("%s.%d.", prefix, os.Getpid()))
	if err != nil {
		return "", swept, nil, fmt.Errorf("testtmp: 一時 dir を作れない: %w", err)
	}
	return dir, swept, func() { _ = os.RemoveAll(dir) }, nil
}

// sweep は root 直下の「pid が生きていない」エントリを消し、消した件数を返す。
//
// 🚨 消してよい条件を全部満たしたものだけを消す:
//   - root の**直下**であること (再帰しない。深く潜る形は作らない)
//   - **symlink でない**こと (`Lstat` で判定。symlink を辿ると root の外を消しうる)
//   - **自分と同じ uid** が所有していること
//   - 名前が `<prefix>.<pid>.<suffix>` の形で、pid が数字であること
//   - その pid が**自分ではなく、かつ生きていない**こと
//
// 判定できなかったものは**消さない**side へ倒す (残骸が残るのは衛生の問題だが、
// 生きている run の dir を消すとテストが不可解に落ちる)。
func sweep(root string) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	me := os.Getpid()
	n := 0
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		fi, err := os.Lstat(path)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			continue // symlink とファイルは触らない (辿ると root の外へ出る)
		}
		if !ownedByMe(fi) {
			continue // 所有者が違う / 判定できないものは触らない
		}
		pid, ok := pidOf(e.Name())
		if !ok || pid == me || alive(pid) {
			continue
		}
		if os.RemoveAll(path) == nil {
			n++
		}
	}
	return n
}

// ownedByMe は自分の uid が所有しているかを見る。**判定できなければ false** (消さない側)。
//
// 🚨 純関数に切り出してあるのは**テストできる形にするため**。所有者が違う dir は root でないと
// 作れないので、実ファイルで異常系を作れない (`refuse-low-value-coverage.md` の「困難×高価値は
// テスタブルへ直してから書く」)。ここが崩れると、TMPDIR を共有する環境で他人の dir を消しうる。
func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(st.Uid) == os.Getuid()
}

// pidOf は `<prefix>.<pid>.<suffix>` の pid を返す。形が違えば ok=false (= 消さない)。
func pidOf(name string) (int, bool) {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return 0, false
	}
	pid, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// alive は pid のプロセスが生きているかを見る。**シグナルは送らない** (0 は存在確認)。
// 判定できないときは「生きている」に倒す (消さない側が安全)。
//
// 🚨 **`os.FindProcess` + `Process.Signal` を使わない**。あちらは reap 済みの pid に対して
// `os: process already finished` を返すことがあり、文言で判定すると**死んでいるものを
// 「生きている」と読む** (実測 2026-09-09: この形で掃除が 1 件も動かず、テストが skip して
// 変異検証まで汚染した)。`syscall.Kill` の errno を直接見れば曖昧さが無い。
//   - ESRCH  … 存在しない = 死んでいる
//   - EPERM  … 存在するが自分のものではない = 生きている (触らない)
//   - その他 … 判定不能なので「生きている」に倒す
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	return true
}
