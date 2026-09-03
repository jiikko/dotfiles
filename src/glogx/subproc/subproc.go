// Package subproc は外部プロセス実行の安全弁を 1 箇所に集める。
//
// main / issues / usage の 3 パッケージが外部コマンドを起動する。main は他 2 つを import する
// 側なので、共有したい値や規律を main に置くと下位パッケージから呼べず、**値だけを写す**運用に
// なる (termsafe / widthenv が独立パッケージになっているのと同じ理由)。実際 issues/discover.go は
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
