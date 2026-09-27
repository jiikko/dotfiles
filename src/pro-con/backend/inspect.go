package backend

import (
	"fmt"
	"time"

	"pro-con/diskuse"
	"pro-con/eventlog"
	"pro-con/store"
	"pro-con/wtclean"
)

// 設定画面 (issue 456) の「見る所」と「変える所」の境界。

// Proc は役ごとのプロセスの一覧の 1 行 (pro-con ps と設定画面のプロセスのタブで同じ型)。
type Proc struct {
	Role    string        `json:"role"` // dispatcher / 見張り / PM / 取り込み / PG / テストの係 / 画面 (Session に画面の id)
	PID     int           `json:"pid,omitempty"`
	Age     time.Duration `json:"age,omitempty"` // 起動からの経過 (分からなければ 0)
	State   string        `json:"state"`         // 動いている / 止まっている / 作業中 など
	Card    string        `json:"card,omitempty"`
	Session string        `json:"session,omitempty"`
	Command string        `json:"command,omitempty"`
	// Mismatch はカードと PG の session の食い違い (カードは作業中なのに session が止まっている / カードは完了なのに動いている。issue 497)。
	// 空なら食い違いは無い。設定画面は、止まった PG の行のうち食い違いのあるものだけを出す
	Mismatch string `json:"mismatch,omitempty"`
	// Slot は PG の行が枠を使っているか (issue 557。SlotHeld / SlotIdle / SlotUnknown。止まっていて枠にも数えていない PG と、PG でない行は空)。
	// dispatcher が様子に書いた「枠に数えたカード」を読んで決める (CountedSlots)
	Slot string `json:"slot,omitempty"`
}

// Proc.Slot の値 (issue 557)。
const (
	SlotHeld    = "作業中"    // dispatcher が枠に数えた (turn の途中・入力待ち・起動や再開の結果を確かめている)
	SlotIdle    = "待機中"    // 生きているが枠に数えていない (質問・テストの係の結果・再開の空きを待っている)
	SlotUnknown = "判定できない" // dispatcher が止まっている・古い
)

// CountedSlots は dispatcher が枠に数えたカード (store.DispatcherState.Slots と数えた時刻 at。issue 557)。dispatcher が人に止められている (held)・
// 居ない (gone)・数えたことが無い・数えてから DispatcherStale より経ったなら ok = false (判定できない。止まった dispatcher の最後の数を今の値に見せない)。
// 🚨 古さは Tick ではなく数えた時刻で見る: 一覧を取れない Tick は割り当てを回さず数え直さないので、Tick が新しくても数は古いことがある
func CountedSlots(slots []string, at time.Time, held, gone bool, now time.Time) ([]string, bool) {
	if held || gone || at.IsZero() || now.Sub(at) > DispatcherStale {
		return nil, false
	}
	return slots, true
}

// PGSection は一覧の PG の区分 1 つ (issue 557。pro-con ps と設定画面のプロセスのタブで同じ分け方・同じ見出し)。
type PGSection struct {
	Slot string // SlotHeld / SlotIdle / SlotUnknown / 空 (止まっている)
	Rows []Proc
}

// Title は区分の見出し。limit は今の枠 (DispatcherState.Cap = Snapshot.Limit)。
func (s PGSection) Title(limit int) string {
	switch s.Slot {
	case SlotHeld:
		return fmt.Sprintf("PG 作業中 (枠を使う) %d / 枠 %d", len(s.Rows), limit)
	case SlotIdle:
		return fmt.Sprintf("PG 待機中 (枠を使わない) %d", len(s.Rows))
	case SlotUnknown:
		return fmt.Sprintf("PG %d (枠を使っているか判定できない: dispatcher が止まっている・古い)", len(s.Rows))
	}
	return fmt.Sprintf("PG 止まっている %d", len(s.Rows))
}

// SplitPGs は一覧の行を、PG より前の行 (dispatcher・見張り・PM・取り込み)・PG の区分・後ろの行 (テストの係・画面) に分ける。
// known は枠に数えたカードを読めたか (CountedSlots の ok)。読めたら作業中と待機中の区分を空でも返し (枠と比べる数を 0 でも出す)、
// 読めなければ生きている PG を「判定できない」の 1 区分にまとめる。止まっている PG の区分は行があるときだけ。
func SplitPGs(rows []Proc, known bool) (head []Proc, secs []PGSection, tail []Proc) {
	by := map[string][]Proc{}
	for _, p := range rows {
		switch {
		case p.Role == "PG":
			slot := p.Slot
			if !known && (slot == SlotHeld || slot == SlotIdle) { // 行を読んだ後に dispatcher が止まった (読む時刻の違い)
				slot = SlotUnknown
			}
			by[slot] = append(by[slot], p)
		case len(by) == 0 && len(tail) == 0 && p.Role != "テストの係" && p.Role != "画面":
			head = append(head, p)
		default:
			tail = append(tail, p)
		}
	}
	for _, slot := range []string{SlotHeld, SlotIdle, SlotUnknown, ""} {
		shown := len(by[slot]) > 0
		switch slot {
		case SlotHeld, SlotIdle:
			shown = shown || known
		case SlotUnknown:
			shown = shown || !known
		}
		if shown {
			secs = append(secs, PGSection{Slot: slot, Rows: by[slot]})
		}
	}
	return head, secs, tail
}

// EventLog は設定画面のログのタブが読む backend (任意。持たない backend はタブに「読めない」と出す。issue 512)。
// Events は前に呼んだ後に足された出来事を返す (最初の 1 回は全部)。出どころは pro-con log と同じ (eventlog.Follower)。
// 🚨 読むだけ (--view でも使う)。画面は描くたびに呼ばず、変化の知らせ (Notifier) で裏で呼ぶ。
type EventLog interface {
	Events() ([]eventlog.Event, error)
}

// ProcStopped は Proc.State の「止まっている」(設定画面はこの行を 1 行に畳む)。
const ProcStopped = "止まっている"

// Inspector は設定画面の見る所を読む backend (任意。持たない backend は見る所を出さない)。
// 🚨 どちらも読むだけ (--view でも使う。状態の置き場・受付の箱・lock・presence の印に何も書かない)。
// 🚨 重い (Procs は ps を 1 回、DiskUsage は worktree を全部歩いて数秒) ので、画面は描くたびに呼ばず、開いたときと測り直しのキーで裏で呼ぶ。
type Inspector interface {
	// Procs は pro-con が起動したプロセスを役ごとに返す (pro-con ps と同じ出どころ)。読めなかったものがあれば、読めた分と err を返す。
	Procs() ([]Proc, error)
	// DiskUsage は pro-con が作った物のディスクの使用量と内訳を測る (pro-con du と同じ)。
	DiskUsage() (diskuse.Usage, error)
}

// Config は変える所の今の値 (Snapshot に載せる。dispatcher が書いた settings.json と様子から)。
type Config struct {
	Limit     int    // 設定の PG の枠 (0 なら設定なし = dispatcher の --limit か既定)
	PMs       int    // 設定の PM の数 (0 なら設定なし)
	UsageOff  bool   // 利用枠で PG を絞らない設定 (既定は絞る)
	LimitFrom string // dispatcher が使っている上限の出どころ ("設定" / "起動の引数"。1 度も回っていなければ空)
	Pending   int    // 受付の箱で適用を待っている設定の依頼の数
	Err       string // settings.json を読めない理由 (dispatcher は --limit で動く)
	// Review は設定の敵対的レビューの担い手 (空なら設定なし)。ReviewNow / ReviewFrom は dispatcher が使っている担い手とその出どころ
	// (設定なしなら config.toml か既定。1 度も回っていなければ空)。Codex / CodexErr は PG に渡す codex の実体と、解けなかった理由 (514)
	Review, ReviewNow, ReviewFrom string
	Codex, CodexErr               string
	// ScheduleOff は予定 (issue 550) を回さない設定 (既定は回す)
	ScheduleOff bool
}

// 変える所の名前 (SetConfig.Key)。
const (
	ConfigLimit  = store.SettingLimit
	ConfigPM     = store.SettingPM
	ConfigUsage  = store.SettingUsage // チェックボックス (1 = 枠で絞る / 0 = 絞らない。SetConfig の Value は on / off)
	ConfigReview = store.SettingReview
	// ConfigSchedule は予定を回すか (チェックボックス。SetConfig の Value は on / off。issue 550)
	ConfigSchedule = store.SettingSchedule
)

// ReviewModes は review に置ける値 (先頭が既定)。
var ReviewModes = store.ReviewModes

// SetConfig は設定を変える依頼 (pro-con config set と同じ。受付の箱に置き、dispatcher の次の Tick から効く)。
type SetConfig struct {
	Key   string
	Value string
}

// OpConfig は設定を変える操作 (設定画面の ← →)。
const OpConfig Op = "config"

func (SetConfig) isCommand() {}

// WorktreeReader は設定画面のディスクのタブが読む「自動の片付けが残した worktree と理由」(pro-con worktree clean の一覧と同じ
// wtclean.Scan。issue 553)。任意 (持たない backend はディスクのタブに「読めない」と出す)。
// 🚨 読むだけ (--view でも使う)。重い (worktree ごとの git・claude agents・lsof) ので、ディスクを測るときに裏で呼ぶ。
type WorktreeReader interface {
	Worktrees() ([]wtclean.Verdict, error)
}

// OpWorktree は残した worktree を人が消す・残す操作 (設定画面のディスクのタブの D / L。見ているだけの画面は受けない)。
const OpWorktree Op = "worktree"

// WorktreeDecider は、人が決める worktree (wtclean.Verdict.Ask) を人が「消す」(remove。wtclean.Discard) か「残す」(wtclean.Hold)
// に決める口 (issue 553)。🚨 git の worktree とブランチを動かすので、見ているだけの画面 (--view) は型の上で持たない。
// 数秒かかるので画面は裏で呼ぶ。pro-con worktree clean --yes (dispatcher の予定) とは worktree-clean.lock で重ねない。
type WorktreeDecider interface {
	DecideWorktree(v wtclean.Verdict, remove bool) (wtclean.Result, error)
}
