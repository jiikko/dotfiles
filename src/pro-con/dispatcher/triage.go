package dispatcher

// テストの係の失敗の要約と一次判定 (426 の決定 5 の要約役を広げた。issue 475)。失敗したときだけ haiku を 1 回起こす (起動の回数は増やさない)。
// 材料は dispatcher が集めて prompt に入れる (haiku に tools を渡さない = 読むだけ・書かないは構造で決まる):
//   - ログの末尾
//   - そのカードの変更 (PG の worktree の、取り込む先との分かれ目から作業ツリーまでの diff --stat と、未追跡のファイルの数。commit 前の変更も含む)
//   - 同じ repo の同じコマンドの最近の結果 (dispatcher のメモリだけ。実際に走って rc が出たものだけ。落ちたら次の dispatcher は空から始める)
// 🚨 判定は証拠ではない (「見込み」)。PG に渡す結果の文に要約と並べるだけで、dispatcher は判定を見て動きを変えない。
// 枠: 実行を始めるとき (Tick の中) に capacity が 0 (95% 以上 = 新しく起動・再開しない) なら、その実行は要約しない。

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SummaryInput は失敗の要約の材料。
type SummaryInput struct {
	Tail   string // ログの末尾
	Diff   string // そのカードの変更 (取れなければ空)
	Recent string // 同じコマンドの最近の結果 (無ければ空)
}

// runRecord はテストの係が実際に走らせた 1 本の結果 (rc が出たもの)。repo はカードの repo (別の repo の同じコマンドと混ぜない)。
type runRecord struct {
	cardID, repo, command string
	rc                    int
	at                    time.Time
}

// keepRuns は覚えておく結果の数 / recentShown は材料に並べる同じコマンドの結果の数。
const (
	keepRuns    = 50
	recentShown = 5
)

// remember は実際に走った 1 本の結果を覚える (Tick の中から呼ぶ)。rc が出ていない (実行できなかった・途中で止めた) ものと、
// rc がコマンドのものか分からない (lockman の 122 / 125 = err 付き) ものは覚えない (「前から落ちる」の材料にしない)。
func (d *Dispatcher) remember(job *runJob, r runResult, now time.Time) {
	if r.rc < 0 || r.err != nil {
		return
	}
	d.recent = append(d.recent, runRecord{cardID: job.cardID, repo: job.repo, command: job.command, rc: r.rc, at: now})
	if len(d.recent) > keepRuns {
		d.recent = d.recent[len(d.recent)-keepRuns:]
	}
}

// recentRuns は同じ repo の同じコマンドの最近の結果を、新しい順に材料の文にする (Tick の中から呼ぶ)。
func (d *Dispatcher) recentRuns(cardID, repo, command string) string {
	var b strings.Builder
	n := 0
	for i := len(d.recent) - 1; i >= 0 && n < recentShown; i-- {
		r := d.recent[i]
		if r.command != command || r.repo != repo {
			continue
		}
		who := r.cardID
		if r.cardID == cardID {
			who += " (このカードの前回)"
		}
		fmt.Fprintf(&b, "- %s %s: rc=%d\n", r.at.Format("01-02 15:04"), who, r.rc)
		n++
	}
	return b.String()
}

// diffTimeout は変更の材料を集める git 1 回の上限 / maxStatLines は材料に並べる変更のファイルの数。
const (
	diffTimeout  = 20 * time.Second
	maxStatLines = 30
)

// changeStat は dir (PG の worktree かその下) の変更の材料。取り込む先 (origin/HEAD → origin/master → origin/main) との分かれ目から作業ツリーまで。
// 取れなければ空 (要約は材料なしで続ける)。🚨 index も ref も書かない読み取りだけ
func changeStat(ctx context.Context, dir string) string {
	var base string
	for _, ref := range []string{"origin/HEAD", "origin/master", "origin/main"} {
		if out, err := gitOut(ctx, dir, "merge-base", "HEAD", ref); err == nil && out != "" {
			base = out
			break
		}
	}
	if base == "" {
		return ""
	}
	stat, err := gitOut(ctx, dir, "diff", "--stat", base)
	if err != nil {
		return ""
	}
	if lines := strings.Split(stat, "\n"); len(lines) > maxStatLines+1 { // 大きな変更で prompt を膨らませない (最後の行は合計)
		stat = strings.Join(lines[:maxStatLines], "\n") + fmt.Sprintf("\n(ほか %d ファイル)\n", len(lines)-1-maxStatLines) + lines[len(lines)-1]
	}
	untracked := 0
	if st, err := gitOut(ctx, dir, "status", "--porcelain", "--untracked-files=normal"); err == nil {
		for _, l := range strings.Split(st, "\n") {
			if strings.HasPrefix(l, "??") {
				untracked++
			}
		}
	}
	if untracked > 0 {
		stat += fmt.Sprintf("\n未追跡のファイル: %d 個", untracked)
	}
	return strings.TrimSpace(stat)
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitRaw(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

// gitRaw は出力を削らずに返す (git status の先頭の空白が意味を持つ)。
func gitRaw(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, diffTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-optional-locks"}, args...)...) // status が index を書き直さない
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// summaryPrompt は要約と一次判定の prompt。
func summaryPrompt(in SummaryInput) string {
	var b strings.Builder
	b.WriteString("次はテストかビルドのコマンドが失敗したときの材料です。\n")
	b.WriteString("1 行目に、失敗の見込みを次の 4 つから 1 つだけ書く: 「判定: この変更のせい」「判定: 負荷・環境で落ちた見込み」「判定: 前から落ちる見込み」「判定: 判定できない」。\n")
	b.WriteString("「この変更のせい」は落ちた場所が下の変更のファイルに当たるとき、「前から落ちる」は同じコマンドがほかのカードでも同じように落ちているとき、")
	b.WriteString("「負荷・環境」はタイムアウト・資源の不足・外の要因がログに出ているとき。材料に根拠が無ければ「判定できない」にする。\n")
	b.WriteString("2 行目から、何が失敗したか (落ちたテスト名・エラーの場所と内容) を日本語で 5 行以内に要約する。材料に書かれていないことは書かず、質問もしないこと。\n\n")
	b.WriteString("## このカードの変更 (取り込む先との分かれ目から作業ツリーまでの git diff --stat)\n")
	b.WriteString(orNothing(in.Diff) + "\n\n")
	b.WriteString("## 同じコマンドの最近の結果 (新しい順)\n")
	b.WriteString(orNothing(strings.TrimSpace(in.Recent)) + "\n\n")
	b.WriteString("## ログの末尾\n")
	b.WriteString(in.Tail)
	return b.String()
}

func orNothing(s string) string {
	if s == "" {
		return "(なし)"
	}
	return s
}

// summarizeTimeout は要約の上限。
const summarizeTimeout = 3 * time.Minute

// HaikuSummarize は失敗の要約と一次判定を haiku に作らせる。settings は HaikuSettings。
func HaikuSummarize(claude, dir, settings string) func(ctx context.Context, in SummaryInput) (string, error) {
	return func(ctx context.Context, in SummaryInput) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, summarizeTimeout)
		defer cancel()
		return haiku(ctx, claude, dir, settings, summaryPrompt(in))
	}
}
