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
	BeforeSize int64    `json:"before_size"`
	AfterSize  int64    `json:"after_size"`
	Freed      int64    `json:"freed"`
	Trashed    int64    `json:"trashed"` // ゴミ箱へ移した量 (まだ容量は戻っていない)
	Remaining  []string `json:"remaining,omitempty"`
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
	PerEntry time.Duration
	// VerifyTimeout は削除後の再走査 1 回の上限。走査の PerEntry (既定 60 秒) は「初回の全走査」
	// 向けの値で、消えたことの確認には過大なので別に持つ。
	VerifyTimeout time.Duration
	CmdTimeout    time.Duration
	// OnProgress は 1 エントリ終わるごとに呼ばれる (UI の進捗表示用)。nil 可。
	OnProgress func(done, total int, label string)
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
	if opt.PerEntry <= 0 {
		opt.PerEntry = 60 * time.Second
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
		for _, t := range targets {
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
		out := planDelete(ctx, t, opt)
		execEntry(ctx, &out, opt)
		verifyEntry(ctx, &out, opt)
		rep.Entries[i] = out
		// エントリ単位で記録を更新する。ここで落ちても「どこまでやったか」が残る
		// (ゴミ箱の移動先 Dest も、完了まで書かないと失われる)
		if err := hist.write(rep, phaseExecuting); err != nil {
			rep.HistoryError = err.Error()
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
	// ここから先の書き込み失敗は削除を取り消せないので、error ではなく報告に残す
	if err := hist.write(rep, phaseDone); err != nil {
		rep.HistoryError = err.Error()
	}
	return rep, nil
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
	out.BeforeSize = t.Size
	// 🚨 Items のパスが**そのエントリのもの**であることを確かめる。planDelete がここまでに見たのは
	// Reused / FromSnapshot / Status の 3 つのフラグだけで、パスの中身は誰も見ていない。
	// 印を立て忘れた復元経路が 1 本でもあれば、細工した JSON の任意パスが os.RemoveAll に届く
	// (敵対レビュー 2026-09-03 が捏造 Result で ~/Documents/photos の削除を実測)。
	// cli: はパスに触らないので突合しない (対象が SIP 配下で、カタログの Paths にも無い)。
	var allowed map[string]bool
	if method != methodCLI {
		set, err := entryPathSet(ctx, opt, e)
		if err != nil {
			return fail("対象パスを確かめられません: " + err.Error())
		}
		allowed = set
	}
	for _, it := range t.Items {
		out.Items = append(out.Items, planItem(method, argv, it, allowed, opt))
	}
	out.Outcome = OutcomePlanned
	return out
}

// entryPathSet はカタログのエントリが指すパスを展開し直した集合 (validateTarget の正規化まで通す)。
// guard は絞り込むだけで新しいパスを増やさないので、rm / trash の対象はこの集合に必ず属する。
// ⚠️ 集合が空でも error にしない (走査の後に実体が消えた場合は空になりうる)。属さない Item が
// 弾かれれば目的は足りる。
func entryPathSet(ctx context.Context, opt DeleteOptions, e Entry) (map[string]bool, error) {
	env := opt.Env
	for _, tmpl := range e.Paths {
		if strings.Contains(tmpl, "$BREW_PREFIX") && env.BrewPrefix == "" {
			prefix, err := brewPrefix(ctx, opt.Run)
			if err != nil {
				return nil, fmt.Errorf("brew --prefix を取得できず: %w", err)
			}
			env.BrewPrefix = prefix
			break
		}
	}
	set := map[string]bool{}
	for _, tmpl := range e.Paths {
		ps, err := expand(env, tmpl)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			vp, err := validateTarget(env, p)
			if err != nil {
				continue // 走査時にも Failures として弾かれている
			}
			set[vp] = true
		}
	}
	return set, nil
}

// planItem は 1 対象の事前検査。rm / trash はパスの形と実体の同一性を確かめ、cli は識別子だけを見る
// (**cli の対象パスには触らない**。SIP 配下で rm できないものがここに来る)。
func planItem(method string, argv []string, it Item, allowed map[string]bool, opt DeleteOptions) ItemOutcome {
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
	// ⚠️ 実体の有無を**集合の突合より先に**見る。既に消えていると glob も 0 件になるので、
	// 順序が逆だと「既に存在しません」が「このエントリの対象ではありません」に化ける
	fi, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return skip("既に存在しません")
		}
		o.Outcome, o.Reason = OutcomeFailed, "確認できません: "+err.Error()
		return o
	}
	if !allowed[p] {
		o.Outcome, o.Reason = OutcomeFailed, "このエントリの対象ではありません (再スキャンしてください)"
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
// ⚠️ os.RemoveAll は木の途中で失敗してもそこまでの削除を取り消さない。失敗を OutcomeFailed に
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
	if err := root.RemoveAll(base); err != nil {
		o.Outcome, o.Reason = OutcomeIncomplete, "一部だけ消えた可能性があります: "+err.Error()
		return err
	}
	return nil
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
		stdout, stderr, rc, err := runner.WithTimeout(ctx, opt.Run, opt.CmdTimeout, args[0], args[1:]...)
		rec := CommandRecord{Name: args[0], Args: args[1:], RC: rc, Stdout: strings.TrimSpace(stdout), Stderr: strings.TrimSpace(stderr)}
		if err == nil && rc == 0 {
			return rec, OutcomeDeleted // 実際に消えたかは verifyEntry の再走査が決める
		}
		rec.Err = ""
		if err != nil {
			rec.Err = err.Error()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return rec, OutcomeIncomplete
			}
		}
		return rec, OutcomeFailed
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
	}
	e, ok := lookupEntry(opt.Catalog, out.ID)
	if !ok {
		out.Outcome, out.Reason = OutcomeFailed, "再走査できません (カタログにない ID)"
		return
	}
	// ⚠️ Reuse は渡さない (前回値を再利用したら「再走査で確認した」にならない)
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
	out.AfterSize = after.Size
	for _, it := range after.Items {
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
	case touched == 0 && failed > 0:
		// 1 件も触れていない = 何も起きていない
		out.Outcome = OutcomeFailed
		out.Reason = fmt.Sprintf("%d 件を削除できませんでした", failed)
	case touched == 0:
		out.Outcome = OutcomeSkipped
		if out.Reason == "" {
			out.Reason = "触れる対象がありませんでした"
		}
	case partial > 0 || failed > 0 || len(after.Items) > 0:
		// 第 3 の状態: 実行したのに残っている (simctl の非同期削除 / 部分削除がこの形)。
		// 一部でも消えているので「失敗」に畳まない (再試行の可否が変わる)
		out.Outcome = OutcomeIncomplete
		out.Reason = fmt.Sprintf("削除を要求しましたが %d 件が残っています (時間をおいて再スキャンしてください)", len(after.Items))
		if failed+partial > 0 {
			out.Reason = fmt.Sprintf("%d 件が完了しませんでした。%d 件が残っています (時間をおいて再スキャンしてください)", failed+partial, len(after.Items))
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
	// ゴミ箱自体が symlink なら、移動先は ~/.Trash の外に落ちる
	if fi, err := os.Lstat(trashDir); err != nil {
		return "", fmt.Errorf("ゴミ箱を確認できません: %w", err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("ゴミ箱が symlink です (移動しません)")
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
		// ⚠️ RENAME_EXCL は宛先が非空ディレクトリでも EEXIST を返す (実測 2026-09-03)。
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

// openDirNoFollow はディレクトリを fd で開く (最後の要素が symlink なら拒否する)。
func openDirNoFollow(dir string) (int, error) {
	return unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
}

// destructive-op: allow 呼び出し側 (trashMove) が allowDestructive を通してから呼ぶ薄い包み。
// ここで再度通すと、連番のリトライごとに検査が走る (同じ src に対して何度も記録が積まれる)。
func renameExcl(srcDir int, srcBase string, dstDir int, dstBase string) error {
	return unix.RenameatxNp(srcDir, srcBase, dstDir, dstBase, unix.RENAME_EXCL)
}

// allowDestructive は破壊的操作の直前に必ず通る検査点。production では素通りし、
// **テストのハーネス** (main_test.go) がここを差してサンドボックス外への操作を拒否する。
// これが無いと、テストの書き間違い 1 つで実ファイルが消える。
var destructiveHook func(op, path string) error

func allowDestructive(op, path string) error {
	if destructiveHook == nil {
		return nil
	}
	return destructiveHook(op, path)
}

// ---- インベントリ ----

type history struct {
	path string
}

// 記録の相。読み手が「実行前に止まった」「実行中に落ちた」「最後まで行った」を区別できるようにする。
const (
	phasePlanned   = "planned"   // まだ何も触っていない
	phaseExecuting = "executing" // 1 エントリ以上を処理した (ここで止まっていたら途中で落ちた)
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
	// 置き場自体が symlink なら、記録は別の場所に書かれる
	if fi, err := os.Lstat(dir); err != nil {
		return nil, err
	} else if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("記録の置き場が symlink です: %s", dir)
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
		return &history{path: p}, nil
	}
	return nil, errors.New("記録ファイルの名前を確保できません")
}

// discard は確保だけして中身を書けなかったファイルを消す (0 バイトの .json を残すと、
// 次に記録を読む処理が JSON のパースエラーになる)。
//
// destructive-op: allow 消すのは**このプロセスが直前に O_EXCL で作った記録ファイル**だけで、
// ユーザーのデータではない (パスは newHistory が決めた h.path に限られる)。
func (h *history) discard() { _ = os.Remove(h.path) }

type inventory struct {
	Phase   string       `json:"phase"`
	Tool    string       `json:"tool"`
	Written time.Time    `json:"written"`
	Report  DeleteReport `json:"report"`
}

// write は記録を書き直す (planned → executing → done)。
//
// ⚠️ 一時ファイルは **os.CreateTemp** で作る。固定名 (`h.path + ".tmp"`) を os.WriteFile で
// 開くと **symlink を辿る**ので、置き場に書ける立場の相手が `<stamp>.json.tmp` を任意ファイルへの
// symlink として撒いておくだけで、そのファイルが truncate + 上書きされる。しかも rename は
// symlink 自体を差し替えるので、**記録は残ったつもりで実体を持たない** = fail-closed が
// 無音で破れる (敵対レビュー 2026-09-03 が実測)。名前は秒粒度で予測できるので現実的な穴だった。
// newHistory 側が O_EXCL を使っている規律を、この置き場への**全部の書き込み**に通すこと。
func (h *history) write(rep DeleteReport, phase string) error {
	b, err := json.MarshalIndent(inventory{Phase: phase, Tool: "glogx doctor", Written: time.Now(), Report: rep}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(h.path)
	f, err := os.CreateTemp(dir, filepath.Base(h.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// destructive-op: allow 消すのは os.CreateTemp が今作った一時ファイルだけ (rename 成功後は no-op)
	defer func() { _ = os.Remove(tmp) }()
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
