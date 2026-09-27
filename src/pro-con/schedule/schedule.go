// Package schedule は dispatcher が決まった時刻に回す予定の表 (issue 550)。
//
// 🚨 表 (Jobs) は、dispatcher が起こす argv と、画面・pro-con config に出す字面の両方の唯一の出典。
// 字面を別の文字列で持たない (持つと、表示と実際に呼ぶものがずれても誰も気づかない)。
package schedule

import (
	"fmt"
	"strings"
	"time"
)

// ResultPrefix は予定のコマンドが stdout の最後に出す結果の 1 行の頭 (「結果: worktree 消した 3・… (rc=0)」)。
// dispatcher はこの行をそのまま記録と画面に出す (出力の文面を dispatcher 側で数え直さない)。
const ResultPrefix = "結果: "

// ResultLine は結果の 1 行 (末尾に自分の終了コードを入れる: dispatcher が終わりを待てなかったときも、行から成否が分かる)。
func ResultLine(body string, rc int) string {
	return fmt.Sprintf("%s%s (rc=%d)", ResultPrefix, body, rc)
}

// ResultRC は s の中の結果の行 (ResultLine) から終了コードを読む。無ければ ok = false。
func ResultRC(s string) (rc int, ok bool) {
	i := strings.LastIndex(s, ResultPrefix)
	if i < 0 {
		return 0, false
	}
	j := strings.LastIndex(s[i:], " (rc=")
	if j < 0 {
		return 0, false
	}
	_, err := fmt.Sscanf(s[i+j:], " (rc=%d)", &rc)
	return rc, err == nil
}

// ExitLocked は予定のコマンドが「別の実行が lock を持っているので何もしなかった」ときの終了コード (失敗の 1 と分ける)。
const ExitLocked = 3

// RCUnknown は dispatcher が子の終わりを待てなかった (抜けた・入れ替わった) ので rc が分からない、の記録の値 (起こせなかった -1 と分ける)。
const RCUnknown = -2

// LockedAlarm は ExitLocked が何回続いたら失敗として出すか (1 回なら人の手の実行と重なっただけ)。
const LockedAlarm = 2

// Job は予定の 1 行: 毎日 Hour:Min (ローカル時刻) に `pro-con <Args...>` を子として回すか、Internal の処理を dispatcher の中で回す。
type Job struct {
	Name string   // 記録と出力の鍵 (schedule.json・schedule/<Name>.out / .err)。中で回す行は、どの処理を呼ぶかもこれで決まる
	Hour int      // 0〜23
	Min  int      // 0〜59
	Args []string // pro-con に渡す引数 (argv[1:])。中で回す行は持たない
	// Internal は子を起こさずに dispatcher が中で回す処理の説明 (画面の字面)。書き手を dispatcher 1 つに保つ処理 (426) に使う:
	// 子に書庫を書かせると書き手が 2 つになり、子から受付の箱で頼むと、箱の「適用した」が消し終えたことを意味しない (issue 497)
	Internal string
	// Lock はコマンドが走っている間 flock で持つファイル (状態の置き場の下)。dispatcher が待てなかった実行 (抜けた・入れ替わった) の
	// 終わりを、次の dispatcher がこの lock が空いたことで知る
	Lock string
}

// WorktreeCleanLock は pro-con worktree clean --yes が持つ lock (dispatcher の予定と人の手の実行を重ねない)。
const WorktreeCleanLock = "worktree-clean.lock"

// CardPurge は完了から 1 週間たったカードを書庫から消す、dispatcher の中で回す予定の名前 (dispatcher/forget.go の purge。issue 497)。
const CardPurge = "card-purge"

// Jobs は dispatcher が回す予定 (同じ Tick に回すのは 1 つだけなので、並びの順に回る)。
var Jobs = []Job{
	// 書庫のカードを消す (session・worktree は消さない。片付けの印を残し、04:00 の worktree clean がそれを読んで片付ける)
	{Name: CardPurge, Hour: 3, Min: 30, Internal: "完了から 1 週間たったカードを書庫から消す"},
	// 閉じたカードの PG の worktree・ブランチ・session を片付ける (issue 492)。消す判定は worktree clean が 1 個ずつ取り直してする
	{Name: "worktree-clean", Hour: 4, Min: 0, Args: []string{"worktree", "clean", "--yes"}, Lock: WorktreeCleanLock},
}

// Command は画面に出す字面 (dispatcher が起こすものと同じ引数。中で回す行は、そう分かる印を付けた説明)。
func (j Job) Command() string {
	if j.Internal != "" {
		return "(dispatcher の中) " + j.Internal
	}
	return "pro-con " + strings.Join(j.Args, " ")
}

// When は「いつ」の字面。
func (j Job) When() string { return fmt.Sprintf("毎日 %02d:%02d", j.Hour, j.Min) }

// Slot は now 以前で最も新しい予定の時刻。
func (j Job) Slot(now time.Time) time.Time {
	s := j.at(now)
	if s.After(now) {
		s = j.at(now.AddDate(0, 0, -1))
	}
	return s
}

// Next は now より後で最も早い予定の時刻。
func (j Job) Next(now time.Time) time.Time {
	s := j.at(now)
	if !s.After(now) {
		s = j.at(now.AddDate(0, 0, 1))
	}
	return s
}

// Due は、最後に始めた時刻が lastStart のとき、now に回すべきか。止まっていた間に何回ぶん過ぎていても 1 回だけ真になる
// (始めた時刻を記録すれば、同じ枠では偽に戻る)。記録が無い (zero) なら真。
// lastStart が now より後 (時計が戻った) なら真 (偽にすると、戻った分の日数だけ黙って回らない)。
func (j Job) Due(lastStart, now time.Time) bool {
	return lastStart.Before(j.Slot(now)) || lastStart.After(now)
}

// at は t の日付の予定の時刻 (t の location で)。
func (j Job) at(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, j.Hour, j.Min, 0, 0, t.Location())
}
