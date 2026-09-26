package backend

import (
	"time"

	"pro-con/diskuse"
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
	LimitFrom string // dispatcher が使っている上限の出どころ ("設定" / "起動の引数"。1 度も回っていなければ空)
	Pending   int    // 受付の箱で適用を待っている設定の依頼の数
	Err       string // settings.json を読めない理由 (dispatcher は --limit で動く)
}

// 変える所の名前 (SetConfig.Key。store.SettingLimit / SettingPM と同じ語)。
const (
	ConfigLimit = "limit"
	ConfigPM    = "pm"
)

// SetConfig は設定を変える依頼 (pro-con config set と同じ。受付の箱に置き、dispatcher の次の Tick から効く)。
type SetConfig struct {
	Key   string
	Value string
}

// OpConfig は設定を変える操作 (設定画面の ← →)。
const OpConfig Op = "config"

func (SetConfig) isCommand() {}
