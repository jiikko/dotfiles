// Package subproc は外部プロセス実行の安全弁を 1 箇所に集める。
//
// glogx (main / issues) と ratelimit (usage / main) が外部コマンドを起動する。glogx の main は他を
// import する側なので、共有したい値や規律を main に置くと下位パッケージから呼べず、**値だけを写す**
// 運用になる (termsafe / widthenv が独立パッケージになっているのと同じ理由)。ratelimit を glogx の外へ
// 出したときに、同じ理由で subproc も独立 module (src/subproc) にした。実際 issues/discover.go は
// 猶予の値を写せないまま WaitDelay を張り忘れており、repo で唯一の抜けになっていた (issue 105)。
package subproc

import (
	"context"
	"os/exec"
	"time"
)

// WaitDelay は ctx キャンセル/プロセス終了後に子孫が I/O パイプを握り続けていても Wait() を
// 確実に戻すための猶予 (Go 1.20+ の Cmd.WaitDelay)。exec.CommandContext は ctx キャンセルで
// **直接の子だけ**を kill するため、子が親 stdout の write 端を継承した孫プロセスを残すと、
// 直接の子を kill しても Wait() はその孫がパイプを閉じるまでブロックしうる。WaitDelay を
// 設けると、キャンセル/終了からこの時間でパイプを強制クローズして Wait を返す。
// プロセスが正常終了して自分でパイプを閉じる通常ケースには影響しない安全弁
// (呼び出し側の timeout に対して十分小さく、かつ正当な出力の取りこぼしが起きない程度に確保)。
//
// 🚨 出力を取る実行 (Output / CombinedOutput / Stdout に io.Writer を張る形) では必須。
// これらは os.Pipe と copy goroutine を作るので、上の「孫がパイプを握る」条件にそのまま当たる。
// パイプを持たない Run() は /dev/null 直結なので事情が違う。
const WaitDelay = 2 * time.Second

// GitOpTimeout は対話中に非同期発行されるローカル git 実行の上限。ローカル操作としては
// 十分寛大な値で、ネットワークマウント・.git ロック競合・hook の stdin 待ちでハングした git が
// goroutine ごと残り続けるのを防ぐ (issue 029 P2)。
//
// main と issues の両方が git を起動するのでここに置く (以前は main 側にだけあり、issues は
// 「import できないので値だけ揃える」と写していた。issue 105/106)。
const GitOpTimeout = 30 * time.Second

// CommandContext は exec.CommandContext に WaitDelay を張って返す。
//
// 🚨 新しい外部コマンド実行はこれを使うこと。素の exec.CommandContext を呼ぶと、WaitDelay を
// 張るのが「書く人が覚えているか」に依存し、静かに抜ける (issue 105 がその実例。13 箇所中
// 1 箇所だけが抜けていて、手元でも CI でも誰も気づかなかった)。
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = WaitDelay
	return cmd
}

// GitArgs は git の引数の前に、開いた repo の設定からコマンドを走らせないための設定を付ける。git を起動するときは
// 必ずこれを通す (tests/scripts/test_git_calls_hardened.sh が素の起動を止める)。
//
// 🚨 `.git/config` の `core.fsmonitor=<cmd>` は status (と diff・blame) のたびにそのコマンドを走らせる。tarball・zip で
// 配られた「repo」を glogx / treefiler で開くだけで、キー操作なしに走った (issue 692。git 2.55.0 で実測)。
// 差分を出す呼び出し (diff・show・log -p) は呼び出し側で --no-textconv --no-ext-diff も付ける (textconv と外部 diff は
// 設定では一律に止められない)。
//
// 止めないもの (脅威の範囲の外): filter.<名前>.clean / smudge は、内容が変わったファイルがあると status でも走るが、
// git-lfs など正当な用途があり、ユーザーがシェルで git status を打っても同じように走る。
//
// 足したもの (どれも開いた repo の設定から走る。敵対的レビューで再現 2026-10-09):
//   - log.showSignature=false: `log.showSignature=true` と `gpg.program=<cmd>` で、署名つきの commit を出す log / show が gpg.program を走らせた
//     (glogx は repo を開いただけで走った。`.git` を普通のディレクトリとして置けば git clone でも届く)。署名の表示は glogx も pro-con も使っていない
//   - diff.submodule=short: `diff.submodule=diff` だと submodule の中の diff は --no-textconv / --no-ext-diff を受け継がず、submodule の textconv が走った
//   - --no-optional-locks: status・diff が index を書き直すと、`core.hooksPath` の post-index-change hook が走った
//
// 止めないもの (続き): merge.<名前>.driver は merge-tree で走る (pro-con の見張りが自分の管理する repo に打つだけ)。
// 🚨 safe.bareRepository=explicit は付けない: 埋め込んだ bare repo (`.git` の中身を普通のディレクトリとして置いた形) の設定を拾わなくなるが、
// -c は GIT_CONFIG_PARAMETERS で hook にも引き継がれ、`git -C <bare>` を打つ pre-push hook と、bare repo の中で起動した glogx・
// bare + worktree の repo を指す pro-con が rc=128 で壊れた (レビューで再現 2026-10-09)。埋め込みの形は記録に留める (issue 692)
// push / pull / commit の hook・credential helper・sshCommand はキーを押す操作で、シェルの git と同じ。
var gitHarden = []string{
	"-c", "core.fsmonitor=false",
	"-c", "log.showSignature=false",
	"-c", "diff.submodule=short",
	"--no-optional-locks",
}

func GitArgs(args ...string) []string {
	return append(append(make([]string, 0, len(gitHarden)+len(args)), gitHarden...), args...)
}

// GitCommand は WaitDelay を張り、GitArgs を通した git を返す。
func GitCommand(ctx context.Context, args ...string) *exec.Cmd {
	return CommandContext(ctx, "git", GitArgs(args...)...)
}
