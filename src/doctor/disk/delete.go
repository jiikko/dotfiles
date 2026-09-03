package disk

// 削除経路 (issues/148 の段階 ④)。**このファイルだけが破壊的操作を持つ**。
//
// 🚨 不変条件 (issues/148 の「④ (削除) の不変条件」節が正本):
//
//   - 削除対象は「**今回の走査**で validateTarget を通った Result」だけ。Reused / FromSnapshot の
//     印が立っている行は拒否する (doctor-snapshot.json と doctor-disk.json は一般ユーザー権限で
//     書き換えられるので、そこに書かれたパスが削除対象になってはいけない)
//   - 削除の作法 (rm / trash / cli / propose) と Risk は、渡された Result の Entry ではなく
//     **コンパイル済みカタログ**から ID で引き直す (Entry も保存経路を通ってくるため)
//   - `deleteVia: cli:` の対象を rm しない。パスに触るのは rm / trash だけ
//   - risk: confirm はゴミ箱へ移動する (復元手段をユーザーに残す)。rm には落とさない
//   - sudo を実行しない
//   - 削除する対象は、そのエントリの Paths を展開し直した集合に**属していること**を要求する
//     (Result の Items だけを信じない。細工した Result で任意パスを消せないようにする)
//   - 解放量は「コマンドが成功したこと」から計算しない。削除後に**再走査**し、実際に消えたことを
//     確認してから数える。消えていなければ「要求したが未完了」を**第 3 の状態**として返す
//   - 破壊的操作は必ず allowDestructive を通す (テストのハーネスがここでサンドボックス外を止める)

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"doctor/cachedir"
	"doctor/runner"

	"golang.org/x/sys/unix"
)

// DeletePhase は 1 エントリの処理段階 (進捗表示用。結末ではない)。
type DeletePhase string

const (
	PhaseScanning  DeletePhase = "scanning"  // 削除の前に走査し直している
	PhaseDeleting  DeletePhase = "deleting"  // 実際に消している
	PhaseVerifying DeletePhase = "verifying" // 消えたことを確かめている
)

// Outcome は 1 エントリ / 1 対象の結末。**成功と失敗の 2 値にしない**: 「実行したが対象が残っている」
// (非同期削除の simctl が実際にこれを返す) を第 3 の状態として持つ。
type Outcome string

const (
	OutcomePlanned    Outcome = "planned"    // dry-run。まだ何もしていない
	OutcomeDeleted    Outcome = "deleted"    // 再走査で消えたことを確認した
	OutcomeTrashed    Outcome = "trashed"    // ゴミ箱へ移動した (空にするまで容量は戻らない)
	OutcomeProposed   Outcome = "proposed"   // 実行していない (コマンドを提示するだけ)
	OutcomeIncomplete Outcome = "incomplete" // 実行したが対象が残っている
	OutcomeSkipped    Outcome = "skipped"    // 触らなかった (既に無い / 走査時と別の実体 / 対象なし)
	OutcomeFailed     Outcome = "failed"     // 実行できなかった
)

// 削除の作法。カタログの DeleteVia を解釈した結果。
const (
	methodRM      = "rm"
	methodTrash   = "trash"
	methodCLI     = "cli"
	methodPropose = "propose"
)

// ItemOutcome は 1 対象パスの結末。
type ItemOutcome struct {
	Path    string  `json:"path"`
	Size    int64   `json:"size"`
	Outcome Outcome `json:"outcome"`
	Reason  string  `json:"reason,omitempty"`
	Dest    string  `json:"dest,omitempty"` // ゴミ箱の移動先
	// Staged は rm の直前に付けた予測不能名 (`.glogx-delete-<hex>`)。改名と RemoveAll の
	// あいだにプロセスが死ぬと、この名前の残骸が親ディレクトリに残る。**名前を記録に残さないと
	// 後から掃除も追跡もできない** (issue 236 の P3-3)。削除まで進めば残骸は無いが、
	// 記録は「この名前を使った」ことを示す (phase が executing で止まっていたら候補)
	Staged string `json:"staged,omitempty"`
	// Ref は cli: の `<id>` に入れる識別子 (simctl のランタイム識別子)。走査時の Item から取る。
	Ref string `json:"ref,omitempty"`
	// dev / ino は走査時の実体。破壊的操作の直前に取り直した値と突き合わせる (TOCTOU)。
	// 記録には出さない (読み手の役に立たず、値だけが古くなる)。
	dev, ino uint64
}

// CommandRecord は cli: 経路で実行した 1 コマンドの記録。stdout / stderr / rc を**分けて**残す
// (混ぜると判定材料がどの stream だったか確定できない。simctl は rc=24 + stderr のみを返す)。
type CommandRecord struct {
	Name   string   `json:"name"`
	Args   []string `json:"args"`
	RC     int      `json:"rc"`
	Stdout string   `json:"stdout,omitempty"`
	Stderr string   `json:"stderr,omitempty"`
	Err    string   `json:"err,omitempty"` // 起動できなかった / タイムアウト
}

// EntryOutcome は 1 エントリの削除結果。
type EntryOutcome struct {
	ID       string          `json:"id"`
	Label    string          `json:"label"`
	Method   string          `json:"method"`
	Command  string          `json:"command,omitempty"` // propose / cli の提示文字列
	Outcome  Outcome         `json:"outcome"`
	Reason   string          `json:"reason,omitempty"`
	Items    []ItemOutcome   `json:"items"`
	Commands []CommandRecord `json:"commands,omitempty"`
	// BeforeSize は走査時の占有量、AfterSize は削除後の再走査の値。Freed は**引き算の実測**で、
	// 「コマンドが成功したから消えたはず」では数えない。
	// 🚨 どちらも**このエントリで触ろうとした対象**に閉じた量で、エントリ全体の占有量ではない
	// (ディレクトリ単位で一部だけ選べるため。issue 232)。エントリ全体を選んだときは一致する。
	// 記録 (JSON) を読む側もこの意味で読むこと。
	BeforeSize int64    `json:"before_size"`
	AfterSize  int64    `json:"after_size"`
	Freed      int64    `json:"freed"`
	Trashed    int64    `json:"trashed"` // ゴミ箱へ移した量 (まだ容量は戻っていない)
	Remaining  []string `json:"remaining,omitempty"`
}

// CommandLines は「このエントリで実際に実行するコマンド」を組み立てた形。
// cli: 以外は空 (rm / trash はツールが直接消すので外部コマンドを起こさない)。
//
// 🚨 置換の規則をここに置くのは、**UI 側で組み直させない**ため (同じ判定が 2 実装になると
// 「確認画面に出したコマンド」と「実際に実行するコマンド」が食い違う)。
func (e EntryOutcome) CommandLines() []string {
	if e.Method != methodCLI || e.Command == "" {
		return nil
	}
	_, argv, err := parseDeleteVia("cli:" + e.Command)
	if err != nil {
		return nil
	}
	if !cliNeedsRef(argv) {
		return []string{strings.Join(argv, " ")}
	}
	out := make([]string, 0, len(e.Items))
	for _, it := range e.Items {
		if validateRef(it.Ref) != nil {
			continue
		}
		out = append(out, strings.Join(substituteRef(argv, it.Ref), " "))
	}
	return out
}

// DeleteReport は 1 回の削除操作の全体。
type DeleteReport struct {
	Entries      []EntryOutcome `json:"entries"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   time.Time      `json:"finished_at"`
	Freed        int64          `json:"freed"`
	Trashed      int64          `json:"trashed"`
	HistoryPath  string         `json:"-"`
	HistoryError string         `json:"-"` // 記録の**後半**が書けなかった (削除自体は実行済み)
	DryRun       bool           `json:"dry_run"`
}

// DeleteOptions は Delete の入力。テストは Env / Run / Catalog / HistoryDir / TrashDir / Now を差す。
type DeleteOptions struct {
	Env      Env
	Run      runner.Runner
	BootTime func() (time.Time, error)
	Catalog  []Entry // 削除の作法を引く正本。既定はコンパイル済みカタログ
	// HistoryDir は削除インベントリの置き場。既定は cachedir.Base()/doctor-history。
	HistoryDir string
	// TrashDir は risk: confirm の移動先。既定は $HOME/.Trash。
	TrashDir string
	Now      func() time.Time
	DryRun   bool
	// ScanTimeout は削除の**前**にやり直す走査 1 回の上限、VerifyTimeout は削除**後**の確認の上限。
	// どちらも「初回の全走査」向けの 60 秒より短くてよい (対象が 1 エントリに絞られているため)。
	ScanTimeout   time.Duration
	VerifyTimeout time.Duration
	CmdTimeout    time.Duration
	// OnProgress は 1 エントリ終わるごとに呼ばれる (UI の進捗表示用)。nil 可。
	// 🚨 **Delete を呼んだ goroutine から同期に呼ばれる** (Scan.OnResult のように別 goroutine から
	// 並行に呼ばれるのではない)。bubbletea なら model を直接触らず Msg に載せること。
	OnProgress func(done, total int, label string)
	// OnCommand は cli: の 1 コマンドが終わるたびに呼ばれる (実行中の画面へ流すため)。
	// stdout / stderr / 終了コードを**分けたまま**渡す。OnProgress と同じ goroutine から同期に呼ばれる。
	OnCommand func(CommandRecord)
	// OnPhase は「今このエントリの何をしているか」を流す。plan の走査は重いエントリで数秒かかり、
	// **DryRun (確認プロンプトを出すための下見) でも通る**ので、口が無いと UI が無言で固まる。
	// OnProgress と同じ goroutine から同期に呼ばれる。nil 可。
	OnPhase func(i, total int, label string, phase DeletePhase)
}

func withDeleteDefaults(opt DeleteOptions) DeleteOptions {
	if opt.Catalog == nil {
		opt.Catalog = catalog
	}
	if opt.Run == nil {
		opt.Run = runner.Exec
	}
	if opt.BootTime == nil {
		opt.BootTime = bootTime
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.TrashDir == "" && opt.Env.Home != "" {
		opt.TrashDir = filepath.Join(opt.Env.Home, ".Trash")
	}
	if opt.ScanTimeout <= 0 {
		opt.ScanTimeout = 60 * time.Second
	}
	if opt.VerifyTimeout <= 0 {
		opt.VerifyTimeout = 30 * time.Second
	}
	if opt.CmdTimeout <= 0 {
		opt.CmdTimeout = 5 * time.Minute // go clean -modcache / brew cleanup は分単位かかる
	}
	return opt
}

// DefaultHistoryDir は削除インベントリの既定の置き場。
func DefaultHistoryDir() (string, error) {
	base, err := cachedir.Base()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "doctor-history"), nil
}

// Delete は選ばれた Result を削除する。DryRun なら事前検査だけを行い、何も壊さない。
//
// 記録 (インベントリ) を残せないときは**削除しない** (fail-closed)。何を消したかの記録が
// 残らない削除は、後から「何が無くなったのか」を誰も再構成できない。
func Delete(ctx context.Context, targets []Result, opt DeleteOptions) (DeleteReport, error) {
	opt = withDeleteDefaults(opt)
	rep := DeleteReport{StartedAt: opt.Now(), DryRun: opt.DryRun}
	// 同じエントリを 2 回渡されても 1 回しか処理しない (二重に渡すと解放量が二重計上される)
	targets = dedupeTargets(targets)
	if opt.DryRun {
		for i, t := range targets {
			opt.phase(i, len(targets), t.Entry.Label, PhaseScanning)
			rep.Entries = append(rep.Entries, planDelete(ctx, t, opt))
		}
		rep.FinishedAt = opt.Now()
		return rep, nil
	}

	hist, err := newHistory(opt)
	if err != nil {
		return rep, fmt.Errorf("削除の記録を残せないため中止しました: %w", err)
	}
	rep.HistoryPath = hist.path
	// 「これから何を触るつもりか」を先に残す。ここから先はいつ落ちても、記録が
	// 「触るつもりだった対象」を持っている状態になる
	for _, t := range targets {
		rep.Entries = append(rep.Entries, intent(t))
	}
	// aborted は「記録を残せなくなって残りを触らずに止めた」印 (phase の決定に使う。issue 236)
	aborted := false
	if err := hist.write(rep, phasePlanned); err != nil {
		hist.discard()
		return rep, fmt.Errorf("削除の記録を残せないため中止しました: %w", err)
	}

	for i, t := range targets {
		// 🚨 破壊的操作の前に必ず中断を見る。os.RemoveAll / trashMove は ctx を取らないので、
		// ここで見ないと「中止したのに残り全部が消える」になる (敵対レビュー 2026-09-03 で実測)
		if ctx.Err() != nil {
			rep.Entries[i].Outcome = OutcomeSkipped
			rep.Entries[i].Reason = "中断されました (このエントリは触っていません)"
			continue
		}
		// plan (実体の同一性検査) と exec を**隣接**させる。全件を先に plan すると、
		// 最後のエントリは検査から実行までの間に再走査 (最大 PerEntry) を何度も挟むことになり、
		// TOCTOU の窓が分単位で開く (敵対レビュー 2026-09-03 で親ディレクトリ差し替えを実測)
		opt.phase(i, len(targets), t.Entry.Label, PhaseScanning)
		out := planDelete(ctx, t, opt)
		opt.phase(i, len(targets), out.Label, PhaseDeleting)
		execEntry(ctx, &out, opt)
		opt.phase(i, len(targets), out.Label, PhaseVerifying)
		verifyEntry(ctx, &out, opt)
		rep.Entries[i] = out
		// エントリ単位で記録を更新する。ここで落ちても「どこまでやったか」が残る
		// (ゴミ箱の移動先 Dest も、完了まで書かないと失われる)。
		// 🚨 書けなくなったら**残りを触らずに止める**。fail-closed は最初の 1 回だけでなく
		// 途中でも成り立たせる (ディスク掃除ツールが走るのは、まさにディスクが逼迫した状況)
		if err := hist.write(rep, phaseExecuting); err != nil {
			rep.HistoryError = err.Error()
			for j := i + 1; j < len(rep.Entries); j++ {
				rep.Entries[j].Outcome = OutcomeSkipped
				rep.Entries[j].Reason = "記録を残せなくなったため中止しました (このエントリは触っていません)"
			}
			aborted = true
			break
		}
		if opt.OnProgress != nil {
			opt.OnProgress(i+1, len(rep.Entries), out.Label)
		}
	}
	for _, e := range rep.Entries {
		rep.Freed += e.Freed
		rep.Trashed += e.Trashed
	}
	rep.FinishedAt = opt.Now()
	// 中断で終わった run に done (= 最後まで行った) と書かない
	//
	// 🚨 **`ctx.Err()` だけを見ない** (issue 236 の P3-1)。記録が書けなくなって残りを
	// Skipped にした run は ctx が生きているので、**書込失敗が一時的なら** (ENOSPC →
	// 削除で空きが戻る、はディスク掃除ツールでは現実的) 最後の write が成功して
	// 記録が「最後まで行った」と言う。中断した事実は変数で持ち回る。
	phase := phaseDone
	if ctx.Err() != nil || aborted {
		phase = phaseAborted
	}
	// ここから先の書き込み失敗は削除を取り消せないので、error ではなく報告に残す
	if err := hist.write(rep, phase); err != nil {
		rep.HistoryError = err.Error()
	}
	return rep, nil
}

func (o DeleteOptions) phase(i, total int, label string, p DeletePhase) {
	if o.OnPhase != nil {
		o.OnPhase(i, total, label, p)
	}
}

// intent は「これから触るつもりの対象」。planDelete より前に記録へ残すためのもので、
// 検査は何もしていない (Outcome は planned)。
func intent(t Result) EntryOutcome {
	out := EntryOutcome{ID: t.Entry.ID, Label: t.Entry.Label, Outcome: OutcomePlanned}
	for _, it := range t.Items {
		out.Items = append(out.Items, ItemOutcome{Path: it.Path, Size: it.Size, Ref: it.Ref, Outcome: OutcomePlanned})
	}
	return out
}

// dedupeTargets は同じ ID を 1 件に畳む (2 回渡すと同じ量を 2 回 freed に数える)。
func dedupeTargets(targets []Result) []Result {
	seen := map[string]bool{}
	out := make([]Result, 0, len(targets))
	for _, t := range targets {
		if seen[t.Entry.ID] {
			continue
		}
		seen[t.Entry.ID] = true
		out = append(out, t)
	}
	return out
}

// HasFailures は 1 件でも失敗 / 未完了があるか。err == nil だけを見る呼び出し元が
// 「全部消えた」と読み違えないための口。
func (r DeleteReport) HasFailures() bool {
	for _, e := range r.Entries {
		if e.Outcome == OutcomeFailed || e.Outcome == OutcomeIncomplete {
			return true
		}
	}
	return false
}

// planDelete は「何を、どうやって消すか」を決めて事前検査する。破壊的操作はしない。
func planDelete(ctx context.Context, t Result, opt DeleteOptions) EntryOutcome {
	out := EntryOutcome{ID: t.Entry.ID, Label: t.Entry.Label}
	fail := func(reason string) EntryOutcome {
		out.Outcome, out.Reason = OutcomeFailed, reason
		return out
	}
	// 🚨 走査していない結果を削除しない。Reused (計測値の再利用) と FromSnapshot (画面ごと復元) は
	// 別の印なので**両方**見る。どちらも「そのパスが今も消してよい形か」を確かめていない
	if t.Reused {
		return fail("前回の計測値を再利用した行です (r で再スキャンしてから削除してください)")
	}
	if t.FromSnapshot {
		return fail("保存された結果から復元した行です (r で再スキャンしてから削除してください)")
	}
	if t.Status != StatusOK {
		return fail(fmt.Sprintf("走査できていないエントリは削除しません (status=%s)", t.Status))
	}
	// 作法と危険度は**カタログから引き直す**。Result.Entry は保存経路を通ってくる
	e, ok := lookupEntry(opt.Catalog, t.Entry.ID)
	if !ok {
		return fail("カタログにない ID です: " + t.Entry.ID)
	}
	out.Label = e.Label
	method, argv, err := parseDeleteVia(e.DeleteVia)
	if err != nil {
		return fail(err.Error())
	}
	out.Method = method
	if method == methodCLI {
		out.Command = strings.TrimPrefix(e.DeleteVia, "cli:")
	}
	// risk: confirm はユーザーのファイルでありうる。ゴミ箱以外の経路に落とさない
	if e.Risk == RiskConfirm && method != methodTrash {
		return fail(fmt.Sprintf("risk: confirm はゴミ箱移動でなければ削除しません (deleteVia=%s)", e.DeleteVia))
	}
	if method == methodPropose {
		out.Outcome, out.Reason = OutcomeProposed, "コマンドを提示するだけで、ツールは実行しません"
		return out
	}
	if len(t.Items) == 0 {
		out.Outcome, out.Reason = OutcomeSkipped, "対象がありません"
		return out
	}
	// 🚨 **削除の直前に走査し直し、その結果だけを対象にする。**
	//
	// 渡された Result の Items / Size は「画面に出すための申告値」で、測り直した値ではない。
	// glob だけを突き合わせても足りない: guard (Chrome 起動中 / mtime が起動時刻より古い /
	// 孤児判定) こそが「そのパスは今消してよいか」を決めているので、guard を通さない突合は
	// 「エントリの glob の内側」しか守らない (敵対レビュー 2026-09-03 が実測: Chrome 起動中に
	// ライブなテンポラリを消せた / 走査後に mtime が更新された対象を警告なしで消せた)。
	// 走査を通せば集合の作り方が 1 つになり (走査側と削除側で 2 実装しない)、サイズと
	// (dev, ino) も実測値になる。
	//
	// 🚨 代償は時間。重いエントリ (DerivedData で実測 5.7 秒) は削除の前後で 2 回走査する。
	// 破壊的操作の正しさを申告値に預けないための費用として受け入れる。
	fresh := Scan(ctx, Options{Env: opt.Env, Run: opt.Run, BootTime: opt.BootTime,
		Catalog: []Entry{e}, Concurrency: 1, PerEntry: opt.ScanTimeout})
	var cur Result
	if len(fresh.Results) > 0 {
		cur = fresh.Results[0]
	}
	switch {
	case cur.Status == StatusBlocked:
		out.Outcome, out.Reason = OutcomeSkipped, "いまは対象外です: "+cur.Reason
		return out
	case cur.Status != StatusOK || fresh.Partial:
		return fail("削除の前に走査し直せませんでした: " + cur.Reason)
	}
	index := map[string]Item{}
	for _, it := range cur.Items {
		index[itemKey(method, it)] = it
	}
	for _, want := range t.Items {
		it, ok := index[itemKey(method, want)]
		if !ok {
			out.Items = append(out.Items, unmatchedItem(opt, want))
			continue
		}
		out.Items = append(out.Items, planItem(method, argv, it, opt))
		out.BeforeSize += it.Size // 申告値ではなく測り直した値
	}
	// 🚨 **触る対象が 1 件も無いなら Planned にしない** (issue 231)。rm / trash / cli の ref 版は
	// exec が Item 単位で非 Planned を弾くので実害が出ないが、cli の非 ref 版
	// (`go clean -modcache` / `brew cleanup`) は Item を 1 つも見ずにコマンドを 1 回実行する。
	// その後 verifyEntry は touched == 0 を見て「触れる対象がありませんでした」と書くので、
	// **実行したのに「何も起きていない」と記録が断言する**形になっていた。
	// 前提は 4 経路で共通なので、判定を execCLI に足さずここを出典にする。
	//
	// 🚨 このゲートは「cli: エントリの Item 集合 ≡ コマンドの効果」を前提にしている。
	// 現行の 2 件は満たす (`brew-cleanup-residue` の Item は `brew cleanup --dry-run` の出力そのもの、
	// `go-modcache` は GOMODCACHE の世代ディレクトリ)。**Paths がコマンドの効果の真部分集合**に
	// なる cli: エントリを足すと、Item が全部消えただけでコマンドを止めてしまう。
	// 実装では強制できないので、カタログに cli: を足すときのレビューの責務とする。
	planned, failed := 0, 0
	for _, it := range out.Items {
		switch it.Outcome {
		case OutcomePlanned:
			planned++
		case OutcomeFailed:
			failed++
		case OutcomeDeleted, OutcomeTrashed, OutcomeIncomplete, OutcomeSkipped, OutcomeProposed:
		}
	}
	if planned == 0 {
		out.Outcome, out.Reason = untouchedOutcome(failed)
		return out
	}
	out.Outcome = OutcomePlanned
	return out
}

// untouchedOutcome は「触った Item が 1 件も無い」ときの結末語。plan の時点 (issue 231) と
// exec 後の verify が**同じ語**を使うための出典 (2 箇所で別の語を作らない)。
func untouchedOutcome(failed int) (Outcome, string) {
	if failed > 0 {
		return OutcomeFailed, fmt.Sprintf("%d 件を削除できませんでした", failed)
	}
	return OutcomeSkipped, "触れる対象がありませんでした"
}

// itemKey は「同じ対象か」を突き合わせる鍵。cli: で識別子を持つもの (simctl のランタイム) は
// パスでなく識別子で照合する (パスは SIP 配下で、消す手がかりにならない)。
func itemKey(method string, it Item) string {
	if method == methodCLI && it.Ref != "" {
		return "ref:" + it.Ref
	}
	return "path:" + filepath.Clean(it.Path)
}

// unmatchedItem は「走査し直したら候補に無かった」対象の結末。既に消えていた場合と、
// そもそも候補でない (細工された / guard が今は除外している) 場合を分ける。
func unmatchedItem(opt DeleteOptions, want Item) ItemOutcome {
	o := ItemOutcome{Path: want.Path, Size: want.Size, Ref: want.Ref}
	if p, err := validateTarget(opt.Env, want.Path); err == nil {
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			o.Outcome, o.Reason = OutcomeSkipped, "既に存在しません"
			return o
		}
	}
	o.Outcome = OutcomeFailed
	o.Reason = "いまは削除の候補ではありません (再スキャンしてください)"
	return o
}

// planItem は 1 対象の事前検査。rm / trash はパスの形と実体の同一性を確かめ、cli は識別子だけを見る
// (**cli の対象パスには触らない**。SIP 配下で rm できないものがここに来る)。
func planItem(method string, argv []string, it Item, opt DeleteOptions) ItemOutcome {
	o := ItemOutcome{Path: it.Path, Size: it.Size, Ref: it.Ref, Outcome: OutcomePlanned, dev: it.Dev, ino: it.Ino}
	skip := func(reason string) ItemOutcome {
		o.Outcome, o.Reason = OutcomeSkipped, reason
		return o
	}
	if method == methodCLI {
		if !cliNeedsRef(argv) {
			return o // コマンドはエントリ単位で 1 回だけ実行する
		}
		if err := validateRef(it.Ref); err != nil {
			o.Outcome, o.Reason = OutcomeFailed, err.Error()
			return o
		}
		return o
	}
	p, err := validateTarget(opt.Env, it.Path)
	if err != nil {
		o.Outcome, o.Reason = OutcomeFailed, "対象パスを拒否: "+err.Error()
		return o
	}
	o.Path = p
	// TOCTOU: 走査時に見たものと同じ実体か。Lstat で辿らない (symlink 自体を見る)。
	// 🚨 実体の有無を**集合の突合より先に**見る。既に消えていると glob も 0 件になるので、
	// 順序が逆だと「既に存在しません」が「このエントリの対象ではありません」に化ける
	fi, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return skip("既に存在しません")
		}
		o.Outcome, o.Reason = OutcomeFailed, "確認できません: "+err.Error()
		return o
	}
	dev, ino, ok := statIdentity(fi)
	if !ok {
		o.Outcome, o.Reason = OutcomeFailed, "実体を識別できません"
		return o
	}
	// 走査時の記録が無い (Dev も Ino も 0) なら照合できない。**触らない側へ倒す**:
	// 「識別できないときは素通り」にすると、守りが自分で自分を無効化する条件を持つことになる
	// (duSize は必ず両方を入れるので、正当な経路でここには来ない)
	if it.Dev == 0 && it.Ino == 0 {
		return skip("走査時の実体を識別できません (再スキャンしてください)")
	}
	if dev != it.Dev || ino != it.Ino {
		return skip("走査時と別の実体に差し替わっています (再スキャンしてください)")
	}
	return o
}

// execEntry は planDelete が通した対象を実際に消す。
func execEntry(ctx context.Context, out *EntryOutcome, opt DeleteOptions) {
	if out.Outcome != OutcomePlanned {
		return // failed / skipped / proposed は何もしない
	}
	switch out.Method {
	case methodRM:
		for i := range out.Items {
			if out.Items[i].Outcome != OutcomePlanned {
				continue
			}
			if ctx.Err() != nil {
				out.Items[i].Outcome, out.Items[i].Reason = OutcomeSkipped, "中断されました"
				continue
			}
			if err := removeItem(&out.Items[i]); err != nil {
				continue // 結末は removeItem が入れている
			}
			out.Items[i].Outcome = OutcomeDeleted
		}
	case methodTrash:
		for i := range out.Items {
			if out.Items[i].Outcome != OutcomePlanned {
				continue
			}
			if ctx.Err() != nil {
				out.Items[i].Outcome, out.Items[i].Reason = OutcomeSkipped, "中断されました"
				continue
			}
			dest, err := trashMove(out.Items[i].Path, opt.TrashDir, out.Items[i].dev, out.Items[i].ino)
			if err != nil {
				out.Items[i].Outcome, out.Items[i].Reason = OutcomeFailed, err.Error()
				continue
			}
			out.Items[i].Outcome, out.Items[i].Dest = OutcomeTrashed, dest
		}
	case methodCLI:
		execCLI(ctx, out, opt)
	}
}

// removeItem は 1 対象を消す。**親ディレクトリを fd で掴んでから**同一性を確かめ、同じ fd 経由で
// 消す (os.Root)。パスで再解決すると、検査と削除のあいだに親ディレクトリを差し替えられて
// 別の木が消える (敵対レビュー 2026-09-03 が実測)。
//
// 🚨 os.RemoveAll は木の途中で失敗してもそこまでの削除を取り消さない。失敗を OutcomeFailed に
// すると「何も消えていない」と読まれるので、OutcomeIncomplete (一部消えている) にする。
func removeItem(o *ItemOutcome) error {
	root, err := os.OpenRoot(filepath.Dir(o.Path))
	if err != nil {
		o.Outcome, o.Reason = OutcomeFailed, "親ディレクトリを開けません: "+err.Error()
		return err
	}
	defer func() { _ = root.Close() }()
	base := filepath.Base(o.Path)
	fi, err := root.Lstat(base)
	if err != nil {
		if os.IsNotExist(err) {
			o.Outcome, o.Reason = OutcomeSkipped, "既に存在しません"
			return err
		}
		o.Outcome, o.Reason = OutcomeFailed, "確認できません: "+err.Error()
		return err
	}
	dev, ino, ok := statIdentity(fi)
	if !ok || dev != o.dev || ino != o.ino {
		o.Outcome, o.Reason = OutcomeSkipped, "直前に別の実体へ差し替わりました (再スキャンしてください)"
		return errIdentityChanged
	}
	if err := allowDestructive("remove", o.Path); err != nil {
		o.Outcome, o.Reason = OutcomeFailed, err.Error()
		return err
	}
	// 🚨 消す前に**予測できない名前へ改名する**。Lstat と RemoveAll は base を 2 回名前解決するので、
	// そのあいだに同じ親ディレクトリ内で rename を当てられると、検査していない木が消える
	// (敵対レビュー 2026-09-03 の実測: 4464 試行中 121 回 = 2.7% で成立した)。改名してしまえば、
	// 相手は自分が置けない名前を狙うことになる。
	// 🚨 改名と削除のあいだにプロセスが死ぬと、この名前の残骸が親ディレクトリに残る
	// (対象はキャッシュの中なので実害は小さいが、次の走査の glob には出ないことがある)。
	// **使った名前を o.Staged に残す** (issue 236 の P3-3): 記録が phase: executing で
	// 止まっていたら、その run の Staged が残骸の候補になる。名前が無いと追跡できない。
	staged, err := stagingName()
	if err != nil {
		o.Outcome, o.Reason = OutcomeFailed, "削除の準備ができません: "+err.Error()
		return err
	}
	o.Staged = staged
	if err := root.Rename(base, staged); err != nil {
		o.Outcome, o.Reason = OutcomeFailed, "削除の準備ができません: "+err.Error()
		return err
	}
	// 改名したものが本当に検査した実体か (改名の直前に差し替えられていたら、ここで初めて分かる)
	if fi, err := root.Lstat(staged); err != nil {
		o.Outcome, o.Reason = OutcomeFailed, "確認できません: "+err.Error()
		return err
	} else if d, i, ok := statIdentity(fi); !ok || d != o.dev || i != o.ino {
		_ = root.Rename(staged, base) // 掴んだものが違うので戻す
		o.Outcome, o.Reason = OutcomeSkipped, "直前に別の実体へ差し替わりました (再スキャンしてください)"
		return errIdentityChanged
	}
	if err := root.RemoveAll(staged); err != nil {
		o.Outcome, o.Reason = OutcomeIncomplete, "一部だけ消えた可能性があります: "+err.Error()
		return err
	}
	return nil
}

// stagingName は削除の直前に付ける一時名。相手が先回りして置けないよう乱数から作る。
func stagingName() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return ".glogx-delete-" + hex.EncodeToString(b[:]), nil
}

var errIdentityChanged = errors.New("走査時と別の実体")

// execCLI は cli: の経路。`<id>` を含むコマンドは対象ごとに、含まないものはエントリごとに 1 回実行する。
func execCLI(ctx context.Context, out *EntryOutcome, opt DeleteOptions) {
	_, argv, err := parseDeleteVia("cli:" + out.Command)
	if err != nil {
		out.Outcome, out.Reason = OutcomeFailed, err.Error()
		return
	}
	// 中断 / タイムアウトで落ちたコマンドは「実行できなかった」ではない。go clean -modcache や
	// brew cleanup は途中まで消しているので、failed (何も起きていない) に畳まず incomplete にする。
	run1 := func(args []string) (CommandRecord, Outcome) {
		// 外部コマンドも破壊的操作。ここを通さないと、Run を差し忘れた fixture が
		// 実カタログの `go clean -modcache` / `brew cleanup` を本当に実行する
		if err := allowDestructive("cli", strings.Join(args, " ")); err != nil {
			return CommandRecord{Name: args[0], Args: args[1:], RC: -1, Err: err.Error()}, OutcomeFailed
		}
		stdout, stderr, rc, err := runner.WithTimeout(ctx, opt.Run, opt.CmdTimeout, args[0], args[1:]...)
		rec := CommandRecord{Name: args[0], Args: args[1:], RC: rc, Stdout: strings.TrimSpace(stdout), Stderr: strings.TrimSpace(stderr)}
		res := OutcomeDeleted // 実際に消えたかは verifyEntry の再走査が決める
		switch {
		case err == nil && rc == 0:
		case err != nil:
			rec.Err = err.Error()
			res = OutcomeFailed
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				res = OutcomeIncomplete // 中断・タイムアウトは途中まで消している可能性がある
			}
		default:
			res = OutcomeFailed
		}
		if opt.OnCommand != nil {
			opt.OnCommand(rec)
		}
		return rec, res
	}
	if !cliNeedsRef(argv) {
		rec, res := run1(argv)
		out.Commands = append(out.Commands, rec)
		for i := range out.Items {
			if out.Items[i].Outcome != OutcomePlanned {
				continue
			}
			out.Items[i].Outcome = res
			if res != OutcomeDeleted {
				out.Items[i].Reason = cmdFailReason(rec)
			}
		}
		return
	}
	for i := range out.Items {
		if out.Items[i].Outcome != OutcomePlanned {
			continue
		}
		if ctx.Err() != nil {
			out.Items[i].Outcome, out.Items[i].Reason = OutcomeSkipped, "中断されました"
			continue
		}
		rec, res := run1(substituteRef(argv, out.Items[i].Ref))
		out.Commands = append(out.Commands, rec)
		out.Items[i].Outcome = res
		if res != OutcomeDeleted {
			out.Items[i].Reason = cmdFailReason(rec)
		}
	}
}

func cmdFailReason(rec CommandRecord) string {
	if rec.Err != "" {
		return "コマンドを実行できません: " + rec.Err
	}
	msg := rec.Stderr
	if msg == "" {
		msg = rec.Stdout
	}
	if msg == "" {
		msg = "(出力なし)"
	}
	return fmt.Sprintf("exit %d: %s", rec.RC, msg)
}

// verifyEntry は削除後に**再走査**して、実際に消えたことを確認する。
// 「コマンドが成功した」を解放量の根拠にしない (simctl は rc=0 でも非同期で残す)。
func verifyEntry(ctx context.Context, out *EntryOutcome, opt DeleteOptions) {
	switch out.Outcome {
	case OutcomeFailed, OutcomeSkipped, OutcomeProposed:
		return
	case OutcomePlanned, OutcomeDeleted, OutcomeTrashed, OutcomeIncomplete:
		// 再走査して実際に消えたかを確かめる (下へ進む)
	}
	e, ok := lookupEntry(opt.Catalog, out.ID)
	if !ok {
		out.Outcome, out.Reason = OutcomeFailed, "再走査できません (カタログにない ID)"
		return
	}
	// 🚨 Reuse は渡さない (前回値を再利用したら「再走査で確認した」にならない)
	rep := Scan(ctx, Options{Env: opt.Env, Run: opt.Run, BootTime: opt.BootTime,
		Catalog: []Entry{e}, Concurrency: 1, PerEntry: opt.VerifyTimeout})
	var after Result
	if len(rep.Results) > 0 {
		after = rep.Results[0]
	}
	// 🚨 ok 以外は「消えた」の根拠にならない。blocked (guard で対象外になった) は Items が nil /
	// Size 0 で返るので、素通りさせると「消えた + 全額 freed」に化ける (敵対レビュー 2026-09-03 が
	// cli: + guard の組み合わせで実測)。blocked は「消えた」でも「残っている」でもない第 3 の値
	if after.Status != StatusOK {
		out.Outcome = OutcomeIncomplete
		out.Reason = "削除後の再走査で確認できませんでした (消えたか分かりません): " + after.Reason
		return
	}
	if rep.Partial {
		out.Outcome = OutcomeIncomplete
		out.Reason = "削除後の再走査が途中で終わりました (消えたか分かりません)"
		return
	}
	// 🚨 **数える範囲を「触ろうとした対象」に閉じる** (issue 232)。after はエントリ全体の
	// 再走査なので、部分選択 (Enter で開いて Space でディレクトリを選ぶ正規の経路) では
	// 残した兄弟がそのまま after に載る。BeforeSize は渡された Item のぶんしか足していないので、
	// 閉じないと引き算の両辺でスコープが違い、①兄弟が残っているだけで
	// 「実行したのに残っている」= incomplete、②Freed = BeforeSize - AfterSize が
	// 兄弟のほうが大きいと負に落ちて 0、小さいと「選んだ分 - 兄弟」という別物になる。
	// エントリ全体を渡した場合は集合が一致するので、値は今までと変わらない。
	touchedKeys := make(map[string]struct{}, len(out.Items))
	for _, it := range out.Items {
		touchedKeys[itemKey(out.Method, Item{Path: it.Path, Ref: it.Ref})] = struct{}{}
	}
	for _, it := range after.Items {
		if _, ok := touchedKeys[itemKey(out.Method, it)]; !ok {
			continue // 触っていない兄弟。自分の成否の材料にしない
		}
		// 🚨 既知の非対称: touchedKeys は unmatchedItem 由来 (BeforeSize に入っていない) も含む。
		// その対象が残ると AfterSize にだけ載り、Freed を**過小**に見せる。旧実装 (after.Size) でも
		// 同じ向きにずれており、安全側 (解放量を多く申告しない) なのでこのまま。直すなら
		// 「BeforeSize に寄与した Item」の印を plan 側で持つ必要がある
		out.AfterSize += it.Size
		out.Remaining = append(out.Remaining, it.Path)
	}

	var trashed, failed, partial, touched int
	var touchedSize int64
	for _, it := range out.Items {
		switch it.Outcome {
		case OutcomeTrashed:
			trashed++
			touched++
			touchedSize += it.Size
			out.Trashed += it.Size
		case OutcomeDeleted:
			touched++
			touchedSize += it.Size
		case OutcomeIncomplete:
			partial++
			touched++
			touchedSize += it.Size
		case OutcomeFailed:
			failed++
		case OutcomePlanned, OutcomeProposed, OutcomeSkipped:
			// 触っていない (解放量にも失敗にも数えない)
		}
	}
	// 🚨 解放量は **①再走査の引き算** と **②実際に触った Item の合計** の小さい方。
	// ①だけだと、飛ばした Item (既に消えていた / 差し替わっていた) のぶんまで自分の手柄になる
	// (BeforeSize は走査時の全 Item の合計なので、他人が消した量が混ざる)。
	// ②だけだと「コマンドが成功したから消えたはず」になり、このファイルの不変条件に反する。
	// 両方を要求すると「自分が触って、かつ実際に減った」ぶんだけが残る。
	if freed := min(out.BeforeSize-out.AfterSize, touchedSize); freed > 0 && trashed == 0 {
		out.Freed = freed
	}
	switch {
	case touched == 0:
		// 1 件も触れていない = 何も起きていない。語は untouchedOutcome が出典 (plan 側と揃える)
		// 🚨 ここへ来る out.Outcome は Planned / Deleted / Trashed / Incomplete のいずれかで、
		// その経路は Reason を書かない (planDelete の Reason 設定はすべて非 Planned で return し、
		// execCLI の parse 失敗は Failed で早期 return する)。したがって上書きの条件は要らない
		out.Outcome, out.Reason = untouchedOutcome(failed)
	case partial > 0 || failed > 0 || len(out.Remaining) > 0:
		// 第 3 の状態: 実行したのに残っている (simctl の非同期削除 / 部分削除がこの形)。
		// 一部でも消えているので「失敗」に畳まない (再試行の可否が変わる)。
		// 🚨 残存は out.Remaining (触った対象に閉じた集合) で数える。after.Items 全部で数えると
		// 部分選択のたびに incomplete になる (issue 232)
		out.Outcome = OutcomeIncomplete
		out.Reason = fmt.Sprintf("削除を要求しましたが %d 件が残っています (時間をおいて再スキャンしてください)", len(out.Remaining))
		if failed+partial > 0 {
			out.Reason = fmt.Sprintf("%d 件が完了しませんでした。%d 件が残っています (時間をおいて再スキャンしてください)", failed+partial, len(out.Remaining))
		}
	case trashed > 0:
		out.Outcome = OutcomeTrashed
		out.Reason = "ゴミ箱へ移動しました (空にするまで容量は戻りません)"
	default:
		out.Outcome = OutcomeDeleted
	}
}

// statIdentity は Lstat の結果から (dev, ino) を取る。走査時に記録した値との照合に使う。
func statIdentity(fi os.FileInfo) (dev, ino uint64, ok bool) {
	st, ok2 := fi.Sys().(*syscall.Stat_t)
	if !ok2 {
		return 0, 0, false
	}
	return uint64(st.Dev), st.Ino, true
}

// ---- 作法の解釈 ----

func lookupEntry(cat []Entry, id string) (Entry, bool) {
	for _, e := range cat {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// parseDeleteVia は catalog の DeleteVia を解釈する。
//
// 脅威モデル: カタログはコンパイル時定数なので、ここは**攻撃者**ではなく「カタログを足す人の
// 不注意」を止めるゲート。したがって `sudo` の検出は素の綴りと basename だけを見る。
// **検出しない形**: `env sudo …` / PATH に置かれた sudo の shim / `xargs sudo`。
// これらはカタログ追加時のレビューの責務とし、ここでは追わない (字句で全部塞ごうとすると
// 迂回が無限に出る。~/.claude/rules/adversarial-review-own-safeguards.md の stopping rule)。
func parseDeleteVia(s string) (method string, argv []string, err error) {
	switch {
	case s == methodRM:
		return methodRM, nil, nil
	case s == methodTrash:
		return methodTrash, nil, nil
	case s == methodPropose:
		return methodPropose, nil, nil
	case strings.HasPrefix(s, "cli:"):
		argv = strings.Fields(strings.TrimPrefix(s, "cli:"))
		if len(argv) == 0 {
			return "", nil, errors.New("cli: のコマンドが空です")
		}
		if argv[0] == "sudo" || filepath.Base(argv[0]) == "sudo" {
			return "", nil, errors.New("sudo はツールが実行しません (コマンドを表示するだけにしてください)")
		}
		// `--runtime=<id>` のように埋め込まれた形は、cliNeedsRef がフィールド完全一致でしか
		// 見ないためリテラルの `<id>` がそのまま渡る。カタログの書き方の誤りとして落とす
		for _, a := range argv {
			if a != refPlaceholder && strings.Contains(a, refPlaceholder) {
				return "", nil, fmt.Errorf("%s は独立した引数にしてください: %q", refPlaceholder, a)
			}
		}
		return methodCLI, argv, nil
	}
	return "", nil, fmt.Errorf("削除の作法が不明です: %q", s)
}

const refPlaceholder = "<id>"

func cliNeedsRef(argv []string) bool {
	for _, a := range argv {
		if a == refPlaceholder {
			return true
		}
	}
	return false
}

func substituteRef(argv []string, ref string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a == refPlaceholder {
			out[i] = ref
			continue
		}
		out[i] = a
	}
	return out
}

// validateRef は cli: のコマンド引数になる識別子を絞る。argv 渡しなのでシェルの injection は無いが、
// `-f` のような**引数の injection** は起きる (先頭のハイフンを禁止する理由)。
func validateRef(ref string) error {
	if ref == "" {
		return errors.New("識別子がありません (再スキャンしてください)")
	}
	if len(ref) > 128 {
		return errors.New("識別子が長すぎます")
	}
	for i, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == ':':
		case r == '-' && i > 0:
		default:
			return fmt.Errorf("識別子に使えない文字があります: %q", ref)
		}
	}
	return nil
}

// ---- ゴミ箱 ----

// trashMove は src を trashDir へ移す。
//
// 3 つのことを同時に守る:
//   - **上書きしない**。macOS の renameatx_np(RENAME_EXCL) で「宛先が無いときだけ移す」を
//     原子的に行う。素の os.Rename は宛先を黙って上書きし、以前ゴミ箱に入れた同名を破壊する。
//     名前が埋まっていたら Finder と同じく連番を足す
//   - **経路を差し替えられない**。src の親と移動先を fd で掴み、その fd 経由で同一性を確かめてから
//     rename する (パスで再解決すると、検査と実行のあいだに親を差し替えられる)
//   - **半端なコピーを作らない**。別ボリューム (EXDEV) や RENAME_EXCL の効かない FS では移さずに
//     失敗する。再帰コピーは途中で失敗すると復元をかえって難しくする (ゴミ箱移動の目的は復元手段)
func trashMove(src, trashDir string, dev, ino uint64) (string, error) {
	if trashDir == "" {
		return "", errors.New("ゴミ箱の場所が分かりません")
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return "", fmt.Errorf("ゴミ箱を用意できません: %w", err)
	}
	// ゴミ箱の**経路のどこか**に symlink があると、移動先は ~/.Trash の外に落ちる
	// (最終要素だけを見る Lstat / O_NOFOLLOW では足りない。HOME 自体が symlink の構成で実測)
	if err := noSymlinkInPath(trashDir); err != nil {
		return "", fmt.Errorf("ゴミ箱を使えません: %w", err)
	}
	srcDir, err := openDirNoFollow(filepath.Dir(src))
	if err != nil {
		return "", fmt.Errorf("移動元の親を開けません: %w", err)
	}
	defer func() { _ = unix.Close(srcDir) }()
	dstDir, err := openDirNoFollow(trashDir)
	if err != nil {
		return "", fmt.Errorf("ゴミ箱を開けません: %w", err)
	}
	defer func() { _ = unix.Close(dstDir) }()

	base := filepath.Base(src)
	var st unix.Stat_t
	if err := unix.Fstatat(srcDir, base, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", fmt.Errorf("移動元を確認できません: %w", err)
	}
	// 🚨 **production からは到達しない**が、意図して残している (issue 236 の P3-5)。
	// planItem が先に statIdentity で同じ検査をして skip するので、ここへ (0,0) は来ない。
	// それでも外さないのは、この関数が**破壊的操作の直前の最後の照合**だから:
	// 外すと下の `uint64(st.Dev) != dev || st.Ino != ino` が (0,0) と実体を比較することになり、
	// 「識別できないまま移動する」経路が開く。呼び出し元が 1 つ増えた瞬間に穴になる形なので、
	// 冗長さと引き換えに残す (list-masked-failure-modes-before-removing-guard.md)。
	// ⚠️ 到達しないのでテストで固定できていない = 「段」として数えないこと。
	if dev == 0 && ino == 0 {
		return "", errors.New("走査時の実体を識別できません (再スキャンしてください)")
	}
	if uint64(st.Dev) != dev || st.Ino != ino {
		return "", errors.New("直前に別の実体へ差し替わりました (再スキャンしてください)")
	}
	if err := allowDestructive("trash", src); err != nil {
		return "", err
	}
	for i := 1; i <= 64; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s %d", base, i)
		}
		// 🚨 RENAME_EXCL は宛先が非空ディレクトリでも EEXIST を返す (実測 2026-09-03)。
		// ENOTEMPTY は来ないので見ない
		err := renameExcl(srcDir, base, dstDir, name)
		switch {
		case err == nil:
			return filepath.Join(trashDir, name), nil
		case errors.Is(err, unix.EEXIST):
			continue
		case errors.Is(err, unix.EXDEV):
			return "", errors.New("ゴミ箱と別ボリュームのため移動できません (手で移動してください)")
		case errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EINVAL):
			// RENAME_EXCL の効かない FS では、上書きの危険を冒すより移動しない
			return "", errors.New("この場所ではゴミ箱への安全な移動ができません (手で移動してください)")
		default:
			return "", err
		}
	}
	return "", fmt.Errorf("ゴミ箱に同名が多すぎます: %s", base)
}

// noSymlinkInPath は経路の全要素が symlink でないことを確かめる (validateTarget と同じ規律)。
func noSymlinkInPath(p string) error {
	// /var → /private/var 等は macOS の既定の link なので先に畳む (validateTarget と同じ)
	for dir := normalizeSystemLinks(filepath.Clean(p)); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
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

// renameExcl は trashMove 専用の薄い包み。呼び出し側が allowDestructive を通してから呼ぶ
// (ここで再度通すと、連番のリトライごとに検査が走って同じ src の記録が何度も積まれる)。
func renameExcl(srcDir int, srcBase string, dstDir int, dstBase string) error {
	return unix.RenameatxNp(srcDir, srcBase, dstDir, dstBase, unix.RENAME_EXCL) // destructive-op: allow trashMove が通す
}

// allowDestructive は破壊的操作の直前に必ず通る検査点。production では素通りし、
// **テストのハーネス** (main_test.go) がここを差してサンドボックス外への操作を拒否する。
// これが無いと、テストの書き間違い 1 つで実ファイルが消える。
var destructiveHook func(op, path string) error

// runningUnderTest は「テストバイナリとして走っている」の判定。testing を production の
// import に持ち込まないための実行ファイル名の検査 (go test が作るバイナリは .test で終わる)。
// 🚨 完全ではない (`go run` で書いた自作ハーネスは検出できない)。目的は
// **他パッケージのテストが hook を差し忘れたまま Delete を呼ぶ**のを止めることだけ。
var runningUnderTest = strings.HasSuffix(os.Args[0], ".test") || strings.Contains(os.Args[0], "/_test/")

func allowDestructive(op, path string) error {
	if destructiveHook != nil {
		return destructiveHook(op, path)
	}
	// テスト中に hook が無いのは「ハーネスを差し忘れた」= 実データを消しうる状態。
	// disk 以外のパッケージ (glogx など) が Delete を呼ぶテストを書いた瞬間にここへ来る
	if runningUnderTest {
		return fmt.Errorf("テスト中に破壊的操作 (%s) が要求されましたが、ハーネスが設定されていません: %s", op, path)
	}
	return nil
}

// ---- インベントリ ----

type history struct {
	path string
	now  func() time.Time
}

// 記録の相。読み手が「実行前に止まった」「実行中に落ちた」「最後まで行った」を区別できるようにする。
const (
	phasePlanned   = "planned"   // まだ何も触っていない
	phaseExecuting = "executing" // 1 エントリ以上を処理した (ここで止まっていたら途中で落ちた)
	phaseAborted   = "aborted"   // 中断された (最後まで行っていない)
	phaseDone      = "done"
)

// newHistory は記録ファイルの名前を**先に確保**する (O_EXCL)。同じ秒に 2 回消しても衝突しない。
func newHistory(opt DeleteOptions) (*history, error) {
	dir := opt.HistoryDir
	if dir == "" {
		d, err := DefaultHistoryDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// 置き場が symlink なら記録は別の場所に書かれる。
	// 🚨 **最終要素だけでなく経路全体を見る** (issue 236 の P3-2)。以前は os.Lstat(dir) で
	// dir 自身しか検査しておらず、ゴミ箱側 (noSymlinkInPath) と非対称だった。
	// 途中のディレクトリが symlink なら、置き場ごと別の場所へ差し替えられる。
	if err := noSymlinkInPath(dir); err != nil {
		return nil, fmt.Errorf("記録の置き場を検査できません (%s): %w", dir, err)
	}
	stamp := opt.Now().Format("20060102-150405")
	for i := 1; i <= 64; i++ {
		name := stamp + ".json"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.json", stamp, i)
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, err
		}
		_ = f.Close()
		return &history{path: p, now: opt.Now}, nil
	}
	return nil, errors.New("記録ファイルの名前を確保できません")
}

// discard は確保だけして中身を書けなかったファイルを消す (0 バイトの .json を残すと、
// 次に記録を読む処理が JSON のパースエラーになる)。
func (h *history) at() time.Time {
	if h.now == nil {
		return time.Now()
	}
	return h.now()
}

func (h *history) discard() {
	if err := allowDestructive("history-remove", h.path); err != nil {
		return
	}
	_ = os.Remove(h.path)
}

type inventory struct {
	Phase   string       `json:"phase"`
	Tool    string       `json:"tool"`
	Written time.Time    `json:"written"`
	Report  DeleteReport `json:"report"`
}

// write は記録を書き直す (planned → executing → done)。
//
// 🚨 一時ファイルは **os.CreateTemp** で作る。固定名 (`h.path + ".tmp"`) を os.WriteFile で
// 開くと **symlink を辿る**ので、置き場に書ける立場の相手が `<stamp>.json.tmp` を任意ファイルへの
// symlink として撒いておくだけで、そのファイルが truncate + 上書きされる。しかも rename は
// symlink 自体を差し替えるので、**記録は残ったつもりで実体を持たない** = fail-closed が
// 無音で破れる (敵対レビュー 2026-09-03 が実測)。名前は秒粒度で予測できるので現実的な穴だった。
// newHistory 側が O_EXCL を使っている規律を、この置き場への**全部の書き込み**に通すこと。
func (h *history) write(rep DeleteReport, phase string) error {
	// 置き場は呼び出し側が決められる (opt.HistoryDir)。実ファイルを上書きしうるので検査点を通す
	if err := allowDestructive("history-write", h.path); err != nil {
		return err
	}
	b, err := json.MarshalIndent(inventory{Phase: phase, Tool: "glogx doctor", Written: h.at(), Report: rep}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(h.path)
	f, err := os.CreateTemp(dir, filepath.Base(h.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // destructive-op: allow CreateTemp が今作った一時ファイルだけ
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}
