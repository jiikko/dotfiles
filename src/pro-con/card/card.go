// Package card は pro-con のドメイン (依頼カード・状態・不変条件) を持つ。
// UI にも backend にも依存しない。設計の正本は issues/ の 415 (PM / PG 分離)。
package card

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// State はカンバンの列。カードは常にどれか 1 つに居る (不変条件「依頼は失われない」)。
type State int

// 各列の意味は Meaning が正本 (TUI の ? の表もこれを読む)。
const (
	Requested State = iota
	Planned
	Running
	Waiting
	Review
	Done
)

// Columns はカンバンの左から右の並び。
var Columns = []State{Requested, Planned, Running, Waiting, Review, Done}

// Meaning はその列に居るカードが今どういう状態か (TUI の ? で出すレーンの説明)。
func (s State) Meaning() string {
	switch s {
	case Requested:
		return "受付 PM がカードを作った直後。まだタスクに分けていない。PM は上から分ける"
	case Planned:
		return "タスクに分けてキューに積んだ。PG の空きを待っている。上から起動する (K / J で並べ替え。↻ の再開は先)"
	case Running:
		return "PG が作業している。make test などの占有リソースの順番待ち・利用枠の回復待ちもここ"
	case Waiting:
		return "PG の質問 (まず PM が受け、人に回すかを決める)・権限の確認・落ちて止めた PG。人の番のものは r で回答する"
	case Review:
		return "PG が作業を終えた。PM が diff と実行結果を読んでから完了にする"
	case Done:
		return "終わった (issue で完了・その場で回答・調査のみ・却下・issue 化待ち)。x で片付ける"
	}
	return ""
}

func (s State) Label() string {
	switch s {
	case Requested:
		return "依頼"
	case Planned:
		return "分解済み"
	case Running:
		return "作業中"
	case Waiting:
		return "質問待ち"
	case Review:
		return "レビュー"
	case Done:
		return "完了"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

// Ending は issue に紐づかないカードの終わり方 (要件 10)。issue を持つカードは EndNone のままでよい。
type Ending int

const (
	EndNone         Ending = iota
	EndAnswered            // その場で回答した
	EndResearchOnly        // 調べて終わった
	EndRejected            // 却下した
	EndPendingIssue        // issue 化待ち (放置されると行方不明になる典型なので UI で目立たせる)
)

func (e Ending) Label() string {
	switch e {
	case EndNone:
		return ""
	case EndAnswered:
		return "回答済み"
	case EndResearchOnly:
		return "調査のみ"
	case EndRejected:
		return "却下"
	case EndPendingIssue:
		return "issue 化待ち"
	}
	return fmt.Sprintf("Ending(%d)", int(e))
}

// WaitKind は Running / Waiting のカードが何を待っているか。正当な待ちは watchdog の停滞判定から外す (要件 13)。
type WaitKind int

const (
	WaitNone       WaitKind = iota
	WaitQuestion            // PG が質問した (Waiting 列)
	WaitPermission          // 権限プロンプトで止まった (Waiting 列。claude agents --json の waitingFor)
	WaitResource            // 占有リソースの順番待ち (要件 12。Running 列のまま)
	WaitQuota               // 利用枠の回復待ち (Running 列のまま)
	WaitCrashed             // PG が短い間に何度も落ちたので dispatcher が止めた (Waiting 列。人間が回答すると同じ session を再開する。426 の決定 4)
)

// NeedsAnswer は人間か PM の操作が要る待ちか (= Waiting 列に置く待ちか)。
func (w WaitKind) NeedsAnswer() bool {
	switch w {
	case WaitQuestion, WaitPermission, WaitCrashed:
		return true
	case WaitNone, WaitResource, WaitQuota:
		return false
	}
	return false
}

type Wait struct {
	Kind     WaitKind
	Resource string // WaitResource のときのリソース名 (device / xcode 等)
	Position int    // WaitResource のときの列の順番 (1 始まり)
	Question string // WaitQuestion / WaitPermission のときの質問文。WaitCrashed のときは止めた理由
}

// IssueRef は repo + 番号で issue を指す (パスで持たない。done への移動や改番で切れないように)。
type IssueRef struct {
	Repo   string
	Number int
	Status string // open / next / done (表示用。正本は issue ファイルの位置)
}

func (r IssueRef) String() string { return fmt.Sprintf("%s#%03d", r.Repo, r.Number) }

// OrderKind は追加オーダーの仕分け (要件 15)。
type OrderKind int

const (
	OrderAppend   OrderKind = iota // 追記: 同じ範囲の小さな追加。作業中の PG へ届ける
	OrderRedirect                  // 方針変更: PG を止めて指示を差し替える
	OrderSeparate                  // 別件: 新しいカードにする
)

func (k OrderKind) Label() string {
	switch k {
	case OrderAppend:
		return "追記"
	case OrderRedirect:
		return "方針変更"
	case OrderSeparate:
		return "別件"
	}
	return fmt.Sprintf("OrderKind(%d)", int(k))
}

type Order struct {
	Kind      OrderKind
	Text      string
	At        time.Time
	Delivered bool // PG へ届いたか (不変条件: 未達かどうかがカードで見える)
}

// Pending は PG へまだ届けていない追加オーダー (追記・方針変更。別件は新しいカードになるので積まない)。
func (c Card) Pending() []Order {
	var out []Order
	for _, o := range c.Orders {
		if !o.Delivered {
			out = append(out, o)
		}
	}
	return out
}

// Btw は PG を止めずに聞いた「今どうなってる?」1 件 (415 要件 9)。答えは PG とは別のプロセスが出す (PG の文脈を汚さない)。
type Btw struct {
	Question string
	At       time.Time
	Answer   string    `json:",omitempty"`
	Answered time.Time `json:",omitzero"` // zero なら未回答 (dispatcher が答える)
}

// Exec は今実行しているコマンド (make test / 実機 E2E 等)。ゼロ値は「何も実行していない」。
// 本物のモードでは、PG が `pro-con card run` で頼んだコマンドを dispatcher (テストの係) が実行している間だけ入る (426 の決定 5)。
type Exec struct {
	Command  string
	Resource string        // 占有しているリソース (device / xcode 等)。占有しないコマンドは空
	Since    time.Time     // 実行を始めた時刻
	Expected time.Duration // 見込みの所要 (前回の実測)。0 なら不明
	// RunID は dispatcher (テストの係) の実行ごとの印。実行の bash の引数 ($0) に `pro-con-run:<RunID>` として載る。
	// dispatcher が死んで残った実行を、次の dispatcher がこの印で見つけて止める (実行を始める前に記録する)
	RunID string `json:",omitempty"`
}

func (e Exec) Active() bool { return e.Command != "" }

// AwaitsRun はテストの係に頼んだコマンドの結果を待っているか (頼みが列にある / 実行中)。
func (c Card) AwaitsRun() bool { return c.Run != "" || c.Exec.Active() }

// HoldsPGSlot は作業中のカードが PG の枠 (--limit) を使っているか (issue 455)。枠は turn の途中の PG だけを数える:
// テストの係の結果を待つ PG は turn を終えて idle で、トークンを使わない (重い処理はテストの係が 1 本ずつ回す) ので数えない。
// idle と確かめられないうち (頼んだ直後で turn の途中 / 一覧に居ない) は数える。結果が届くと分解済みへ戻り、枠の空きを待って再開する (再開が先)。
// idle は、このカードの PG が一覧で idle と出ているか。
func HoldsPGSlot(c Card, idle bool) bool {
	return c.State == Running && (!c.AwaitsRun() || !idle)
}

// DropRun はテストの係への頼みと実行中の記録を取り下げる。作業中の列を離れるときは必ず呼ぶ
// (残すと、後で届いた結果や「結果が無い」が、別の理由で再開した PG を止めて再開し直す)。
func (c *Card) DropRun() {
	c.Run, c.RunAt, c.RunCwd, c.Exec, c.RunRetried = "", time.Time{}, "", Exec{}, false
	if c.Wait.Kind == WaitResource {
		c.Wait = Wait{}
	}
}

// StallThreshold は watchdog が「進捗なし」を停滞とみなすまでの時間。コマンドの実行中は見込みの 2 倍と base の
// 長い方 (長いテストを停滞と誤判定しない)。それ以外は base。
func StallThreshold(c Card, base time.Duration) time.Duration {
	if c.Exec.Active() {
		return max(base, 2*c.Exec.Expected)
	}
	return base
}

type Event struct {
	At     time.Time
	Text   string
	Screen string `json:",omitempty"` // 打った画面 (画面から受付の箱に置いた依頼だけ。「a1b2c3 join review」。issue 481)
}

// AttachKind は添付の種類 (人間がどう見るかを決める。issue 453)。
type AttachKind string

const (
	AttachImage AttachKind = "画像"   // open で Preview に渡す
	AttachText  AttachKind = "文字"   // 詳細の中にそのまま出す (tmux の capture-pane -e の色つきの文字など)
	AttachFile  AttachKind = "ファイル" // それ以外。open に渡す
)

// Attachment は PG がカードに付けた証拠 1 件 (作った画面の見た目・コマンドの出力。issue 453)。
// ファイルは dispatcher が状態の置き場の attachments/<カード>/ へ移したもの (Path は絶対パス)。カードが記録から外れたら dispatcher が消す
type Attachment struct {
	Path string
	Name string // PG が付けたときの元のファイル名
	Note string `json:",omitempty"`
	Kind AttachKind
	Size int64
	At   time.Time // PG が付けた時刻
}

type Card struct {
	ID       string
	ParentID string // 1 つの依頼を分けたとき / 別件の追加オーダーの元
	Title    string
	Request  string // 依頼の原文 (人間が書いたまま)
	Prompt   string // PM に渡した指示の全文 (スコープの前置き + 原文)。TUI から出した依頼だけが持つ
	Repo     string
	Owner    string // 受付 PM / PM-A / PG-2 / 人間
	Session  string // 担当 PG の session id (claude --bg の id)
	State    State
	Since    time.Time // 今の State に入った時刻
	// Rank は人が入れ替えたレーンの中の並び (issue 470。rank.go)。Since が変わる (列を移る) と効かなくなる
	Rank    Rank `json:",omitzero"`
	Wait    Wait
	Stalled bool // watchdog が停滞と判定した
	Exec    Exec // 今実行しているコマンド (作業中の列のまま。列は担当が変わるときだけ移る)
	Issues  []IssueRef
	Ending  Ending
	Orders  []Order
	Btws    []Btw `json:",omitempty"`
	History []Event
	// Attachments は PG が `pro-con card attach` で付けた添付 (付けた順。issue 453)
	Attachments []Attachment `json:",omitempty"`
	Log         []string     // PG の出力の末尾 (本番は transcript から読む)
	// Doing は PG が今走らせているもの、DoingAt は dispatcher がそれを集めた時刻 (issue 473)。読む側 (画面・card show) が store.DoingFile から足す。
	// 🚨 記録 (cards.json) には書かない: 書き手は dispatcher の Apply だけで、数秒で古くなる様子を記録の差分に混ぜない
	Doing   []Doing   `json:"-"`
	DoingAt time.Time `json:"-"`
	// Progress は作業の進捗 (commit・未 commit・issue の進捗節)、ProgressAt は dispatcher がそれを集めた時刻 (issue 469。store.ProgressFile)。
	// Conflicts は見張りが見た取り込みの衝突の文、ConflictsAt は見張りが見た時刻 (store.ConflictsFile)。どれも Doing と同じく記録には書かない
	Progress    *Progress `json:"-"`
	ProgressAt  time.Time `json:"-"`
	Conflicts   []string  `json:"-"`
	ConflictsAt time.Time `json:"-"`
	// LastRun はテストの係が最後に返した結果 (issue 469。結果は Resume で PG に渡して消えるので、詳細に残す分)
	LastRun *RunRecord `json:",omitempty"`
	// Resume は次に PG を再開するときに渡す文 (質問への回答)。dispatcher が渡したら空にする (本物のモードだけ。426 の決定 2)
	Resume string `json:",omitempty"`
	// Launching は dispatcher が PG の起動・再開を始めて、結果をまだ確かめていない印 ("起動" / "再開")。起動の前に記録へ書く
	// (claude が「失敗」と返しても session が立っていることがあり、dispatcher が途中で落ちることもある。次の Tick が一覧で確かめる)
	Launching string `json:",omitempty"`
	// LaunchedAt は dispatcher が最後に起動・再開を始めた時刻。これより前に始まった session は、このカードの PG として取り込まない
	LaunchedAt time.Time `json:",omitzero"`
	// Rejects は claude が起動・再開を受け付けなかった (rc≠0 がすぐ返った) のが続いた回数。起動・再開が済んだか、人の番へ回したら 0 に戻す (462)
	Rejects int `json:",omitempty"`
	// Crashes は PG のプロセスが落ちて Claude Code が自動で再開した時刻 (transcript の再開の文の時刻)。dispatcher が数えて、
	// 短い間に上限を超えたら止める
	Crashes []time.Time `json:",omitempty"`
	// CrashesFrom は Crashes を数え始める時刻 (これより前の回数は数えない)。起動・再開で進めるが、一覧から消えた PG の再開では進めない
	// (進めると、消える → 再開を繰り返す PG の回数が毎回 0 に戻る)。空なら LaunchedAt から数える
	CrashesFrom time.Time `json:",omitzero"`
	// StopWanted は落ちた回数が上限に達したが、まだ止められていない印 (止められるまで毎 Tick 試す。時間の窓を過ぎても諦めない)
	StopWanted bool `json:",omitempty"`
	// DeadSince は dispatcher が、このカードの PG の session が一覧に無い / pid 無し (落ちて自動の再開を待っている) のを最初に見た時刻。
	// 生きているのを見たら外す。止める・再開する前の待ち (restartWait) はここから数える
	DeadSince time.Time `json:",omitzero"`
	// StopAfterClose はカードを閉じた (close) が、dispatcher がまだ PG の session を止め終えていない印 (close の適用と同時に付く。
	// dispatcher が落ちても次の Tick で止める)。止まったのを確かめたか、closeStopWait を過ぎて諦めたら外す
	StopAfterClose bool `json:",omitempty"`
	// StopSent は StopAfterClose / DeleteAt の間に、dispatcher が PG の session へ止める要求を出した印 (止まったのを後の Tick で見たとき、
	// 「止めた」と「既に止まっていた」を取り違えない)。印と一緒に外す。🚨 記録の名前は 447 のときのまま (動いている記録を読めるように)
	StopSent bool `json:"CloseStopSent,omitempty"`
	// DeleteAt は削除の依頼を受けた時刻 (issue 451。依頼の列のカードは印を付けずにすぐ消す)。付いている間、dispatcher は PG を起動・再開せず、
	// PG の session が止まったのを確かめてからカードを記録から外す。止められずに諦めたら外す (カードは残る)。DeleteBy は依頼した人
	DeleteAt time.Time `json:",omitzero"`
	DeleteBy string    `json:",omitempty"`
	// Stopped は pro-con の終了で dispatcher が PG を止めた印。次の再開は、落ちた PG の自動の再開を待たずに (止めずに) 行う。起動・再開で外す
	Stopped bool `json:",omitempty"`
	// Revived は、落ちた・消えた PG を人の判断なしに再開へ回した印 (458 の消えた PG・483 の再起動の復旧)。再開 (settle) で落ちた回数の
	// 数え始め (CrashesFrom) を今に戻さない (戻すと、落ち続ける PG を上限に届かないまま再開し続ける)。再開と人の回答待ちへ送るときに外す
	Revived bool `json:",omitempty"`
	// Run は PG が `pro-con card run` で頼んだ、まだ結果を返していないコマンド (シェルの 1 行)。RunAt は頼んだ時刻 (順番の鍵)。
	// dispatcher が順番に実行し、結果を持たせて PG を再開したら空にする
	Run   string    `json:",omitempty"`
	RunAt time.Time `json:",omitzero"`
	// RunCwd は頼んだ側 (`pro-con card run` を打ったシェル) の作業ディレクトリ。dispatcher はそのカードの PG の worktree と一致するときだけ実行する
	// (別のカードの名前で頼まれた実行を、そのカードの worktree で走らせない)
	RunCwd string `json:",omitempty"`
	// RunRetried は、実行の途中で dispatcher が止まった Run を 1 度頼み直した印 (issue 483)。また中断したら頼み直さずに rc=-1 を返す
	// (実行そのものがマシンか dispatcher を落としている疑い)。Run を外すときに外す
	RunRetried bool `json:",omitempty"`
	// Archived は完了のレーンから片付けた (x・完了から 24 時間の自動)。ボードには出さない。dispatcher が記録から書庫へ移す (store.Archive)
	Archived bool
	// FromRequest はこのカードを作った受付の箱の依頼 (add) の ID。`pro-con card add` が、置いた依頼から振られたカード ID を引く (issue 442)
	FromRequest string `json:",omitempty"`
	// After はこのカードより先に完了させるカード (PM が `card plan --after` で付ける。issue 468)。dispatcher はこれらが完了するまで起動しない。
	// 同じ判断・不変条件を変えるカードを並べない (並べると、合わせた結果が片方のテストでしか守られない)
	After []string `json:",omitempty"`
	// LastProgress は「実質的に進んだ」最後の時刻 (watchdog が見る。活動ではなく進捗)
	LastProgress time.Time
}

// Deleting は削除の依頼を受けて、PG を止めるのを待っているか。
func (c Card) Deleting() bool { return !c.DeleteAt.IsZero() }

// Children は id を親に持つカードの ID (親を消すと子が親を失う = Check の違反)。
func Children(cards []Card, id string) []string {
	var out []string
	for _, c := range cards {
		if c.ParentID == id {
			out = append(out, c.ID)
		}
	}
	return out
}

// Resumes は同じ session の再開を待っているか (回答・差し戻し・テストの結果・未達の追加オーダーを持つ。dispatcher はこれを
// 新しい起動より先にする。画面は分解済みのレーンで印を出す)。
func (c Card) Resumes() bool { return (c.Resume != "" || len(c.Pending()) > 0) && c.Session != "" }

// ResumesFirst は dispatcher がレーンの並びより先に起動するカードか (再開の初回。削除中は起動しない)。画面の ↻ の印も同じ判定を使う。
// 🚨 印の残った再試行 (Launching) は先にしない: 失敗し続ける再開が毎回先頭に並び、1 本の枠を永久に占めて他のカードを起動させなくなる
func (c Card) ResumesFirst() bool { return c.Resumes() && c.Launching == "" && !c.Deleting() }

// handoffMark は PM が PG の質問を人に回したときの履歴の文の印 (HandoffText が書き、HandedOff が読む)。
const handoffMark = " が人に回した: "

// HandoffText は質問を人に回したときに履歴へ残す文 (理由は原文のまま)。
func HandoffText(by, why string) string { return by + handoffMark + why }

// Turn はそのカードを今先へ進めるのが誰か (issue 452。画面の目印・`pro-con card list` が読む。判定はここだけに置く)。
type Turn int

const (
	TurnNone       Turn = iota // 誰の番でもない (完了・片付けた・削除中)
	TurnPG                     // PG かテストの係が動いている / PG の空き・前のカードを待っている
	TurnPM                     // PM (依頼を分ける・PG の質問に答えるか人に回す)
	TurnIntegrator             // 取り込みの係 (レビュー。487)
	TurnHuman                  // 人間が操作しないと進まない
)

func (t Turn) Label() string {
	switch t {
	case TurnNone:
		return ""
	case TurnPG:
		return "PG"
	case TurnPM:
		return "PM"
	case TurnIntegrator:
		return "取り込みの係"
	case TurnHuman:
		return "人"
	}
	return fmt.Sprintf("Turn(%d)", int(t))
}

// Roles は dispatcher が起こさない役 (設定 pm / integrator = "off")。起こさない役の番は人の番になる。ゼロ値は「どちらも起こす」。
type Roles struct {
	PMOff         bool `json:"pm_off,omitempty"`
	IntegratorOff bool `json:"integrator_off,omitempty"`
}

// Turn は今誰の番か。人の番は、権限の確認 (PM には答えられない)・落ち続けて止めた PG・PM が人に回した質問とレビュー・
// 起こさない役 (r) の仕事 (依頼・質問・レビュー)。
func (c Card) Turn(r Roles) Turn {
	if c.Archived || c.Deleting() {
		return TurnNone
	}
	switch c.State {
	case Requested:
		if r.PMOff {
			return TurnHuman
		}
		return TurnPM
	case Planned, Running:
		return TurnPG
	case Waiting:
		if c.Wait.Kind != WaitQuestion || c.HandedOff() || r.PMOff { // 権限の確認・落ちて止めた (Check は Waiting を NeedsAnswer の待ちに限る)
			return TurnHuman
		}
		return TurnPM
	case Review:
		if c.HandedOff() || r.IntegratorOff {
			return TurnHuman
		}
		return TurnIntegrator
	case Done:
	}
	return TurnNone
}

// HandedOff は今の質問を人に回したか (質問待ちに入った後の履歴に HandoffText がある)。
func (c Card) HandedOff() bool {
	if (c.State != Waiting || c.Wait.Kind != WaitQuestion) && c.State != Review { // レビュー待ちは取り込みの係が人に回す (487)
		return false
	}
	for _, e := range c.History {
		if !e.At.Before(c.Since) && strings.Contains(e.Text, handoffMark) {
			return true
		}
	}
	return false
}

// Answerable は回答を受け付けるか (質問待ちの列に居る)。backend の回答・TUI の r・案内の色がこれを見る。
func (c Card) Answerable() bool { return c.State == Waiting }

// Violation は不変条件の破れ。UI は件数を出し、0 件でないことを隠さない。
type Violation struct {
	CardID string
	Reason string
}

// Check は 415 の不変条件のうち、カードの集合だけで判定できるものを見る。
func Check(cards []Card) []Violation {
	var out []Violation
	ids := make(map[string]bool, len(cards))
	for _, c := range cards {
		if ids[c.ID] {
			out = append(out, Violation{c.ID, "カード ID が重複している"})
		}
		ids[c.ID] = true
	}
	for _, c := range cards {
		if c.ParentID != "" && !ids[c.ParentID] {
			out = append(out, Violation{c.ID, "親カード " + c.ParentID + " が存在しない"})
		}
		if c.State == Done && len(c.Issues) == 0 && c.Ending == EndNone {
			out = append(out, Violation{c.ID, "完了したが、紐づく issue も終わり方も無い"})
		}
		if c.State == Waiting && !c.Wait.Kind.NeedsAnswer() {
			out = append(out, Violation{c.ID, "質問待ちの列に居るが、回答の要る待ちではない"})
		}
		if c.State != Waiting && c.Wait.Kind.NeedsAnswer() {
			out = append(out, Violation{c.ID, "回答の要る待ちなのに質問待ちの列に居ない"})
		}
		if c.Archived && c.State != Done {
			out = append(out, Violation{c.ID, "片付けたが完了していない (ボードから見えないまま動いている)"})
		}
	}
	return append(out, afterViolations(cards)...)
}

// SessionName は PG の session と worktree の名前 (claude --bg -w <name> -n <name>)。
func SessionName(c Card) string { return "pc-" + strings.ToLower(c.ID) }

// WorktreePath は claude --bg -w <name> が作る PG の worktree (427 の 3f で実測。dispatcher と見張り = package monitor が使う)。
// repo の場所が分からなければ空 (呼び出し側が空を弾く)。
func WorktreePath(repoPath string, c Card) string {
	if repoPath == "" {
		return ""
	}
	return filepath.Join(repoPath, ".claude", "worktrees", SessionName(c))
}
