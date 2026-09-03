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
//   - 解放量は「コマンドが成功したこと」から計算しない。削除後に**再走査**し、実際に消えたことを
//     確認してから数える。消えていなければ「要求したが未完了」を**第 3 の状態**として返す

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
	TrashDir   string
	Now        func() time.Time
	DryRun     bool
	PerEntry   time.Duration
	CmdTimeout time.Duration
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
	for _, t := range targets {
		rep.Entries = append(rep.Entries, planDelete(t, opt))
	}
	if opt.DryRun {
		rep.FinishedAt = opt.Now()
		return rep, nil
	}

	hist, err := newHistory(opt)
	if err != nil {
		return rep, fmt.Errorf("削除の記録を残せないため中止しました: %w", err)
	}
	rep.HistoryPath = hist.path
	if err := hist.write(rep, "planned"); err != nil {
		return rep, fmt.Errorf("削除の記録を残せないため中止しました: %w", err)
	}

	for i := range rep.Entries {
		execEntry(ctx, &rep.Entries[i], opt)
		verifyEntry(ctx, &rep.Entries[i], opt)
		if opt.OnProgress != nil {
			opt.OnProgress(i+1, len(rep.Entries), rep.Entries[i].Label)
		}
	}
	for _, e := range rep.Entries {
		rep.Freed += e.Freed
		rep.Trashed += e.Trashed
	}
	rep.FinishedAt = opt.Now()
	// ここから先の書き込み失敗は削除を取り消せないので、error ではなく報告に残す
	if err := hist.write(rep, "done"); err != nil {
		rep.HistoryError = err.Error()
	}
	return rep, nil
}

// planDelete は「何を、どうやって消すか」を決めて事前検査する。破壊的操作はしない。
func planDelete(t Result, opt DeleteOptions) EntryOutcome {
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
	if method == methodCLI || method == methodPropose {
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
	for _, it := range t.Items {
		out.Items = append(out.Items, planItem(method, argv, it, opt))
	}
	out.Outcome = OutcomePlanned
	return out
}

// planItem は 1 対象の事前検査。rm / trash はパスの形と実体の同一性を確かめ、cli は識別子だけを見る
// (**cli の対象パスには触らない**。SIP 配下で rm できないものがここに来る)。
func planItem(method string, argv []string, it Item, opt DeleteOptions) ItemOutcome {
	o := ItemOutcome{Path: it.Path, Size: it.Size, Ref: it.Ref, Outcome: OutcomePlanned}
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
	// TOCTOU: 走査時に見たものと同じ実体か。Lstat で辿らない (symlink 自体を見る)
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
	if it.Dev != 0 || it.Ino != 0 {
		if dev != it.Dev || ino != it.Ino {
			return skip("走査時と別の実体に差し替わっています (再スキャンしてください)")
		}
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
			if err := os.RemoveAll(out.Items[i].Path); err != nil {
				out.Items[i].Outcome, out.Items[i].Reason = OutcomeFailed, err.Error()
				continue
			}
			out.Items[i].Outcome = OutcomeDeleted
		}
	case methodTrash:
		for i := range out.Items {
			if out.Items[i].Outcome != OutcomePlanned {
				continue
			}
			dest, err := trashMove(out.Items[i].Path, opt.TrashDir)
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

// execCLI は cli: の経路。`<id>` を含むコマンドは対象ごとに、含まないものはエントリごとに 1 回実行する。
func execCLI(ctx context.Context, out *EntryOutcome, opt DeleteOptions) {
	_, argv, err := parseDeleteVia("cli:" + out.Command)
	if err != nil {
		out.Outcome, out.Reason = OutcomeFailed, err.Error()
		return
	}
	run1 := func(args []string) CommandRecord {
		stdout, stderr, rc, err := runner.WithTimeout(ctx, opt.Run, opt.CmdTimeout, args[0], args[1:]...)
		rec := CommandRecord{Name: args[0], Args: args[1:], RC: rc, Stdout: strings.TrimSpace(stdout), Stderr: strings.TrimSpace(stderr)}
		if err != nil {
			rec.Err = err.Error()
		}
		return rec
	}
	if !cliNeedsRef(argv) {
		rec := run1(argv)
		out.Commands = append(out.Commands, rec)
		if rec.Err != "" || rec.RC != 0 {
			for i := range out.Items {
				if out.Items[i].Outcome == OutcomePlanned {
					out.Items[i].Outcome, out.Items[i].Reason = OutcomeFailed, cmdFailReason(rec)
				}
			}
			return
		}
		for i := range out.Items {
			if out.Items[i].Outcome == OutcomePlanned {
				out.Items[i].Outcome = OutcomeDeleted // 実際に消えたかは verifyEntry の再走査が決める
			}
		}
		return
	}
	for i := range out.Items {
		if out.Items[i].Outcome != OutcomePlanned {
			continue
		}
		args := substituteRef(argv, out.Items[i].Ref)
		rec := run1(args)
		out.Commands = append(out.Commands, rec)
		if rec.Err != "" || rec.RC != 0 {
			out.Items[i].Outcome, out.Items[i].Reason = OutcomeFailed, cmdFailReason(rec)
			continue
		}
		out.Items[i].Outcome = OutcomeDeleted
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
	rep := Scan(ctx, Options{Env: opt.Env, Run: opt.Run, BootTime: opt.BootTime,
		Catalog: []Entry{e}, Concurrency: 1, PerEntry: opt.PerEntry})
	var after Result
	if len(rep.Results) > 0 {
		after = rep.Results[0]
	}
	if after.Status == StatusFailed {
		out.Outcome = OutcomeIncomplete
		out.Reason = "削除後の再走査ができませんでした (消えたか確認できていません): " + after.Reason
		return
	}
	out.AfterSize = after.Size
	for _, it := range after.Items {
		out.Remaining = append(out.Remaining, it.Path)
	}

	var trashed, failed, touched int
	for _, it := range out.Items {
		switch it.Outcome {
		case OutcomeTrashed:
			trashed++
			touched++
			out.Trashed += it.Size
		case OutcomeDeleted:
			touched++
		case OutcomeFailed:
			failed++
		}
	}
	if freed := out.BeforeSize - out.AfterSize; freed > 0 && trashed == 0 {
		out.Freed = freed
	}
	switch {
	case failed > 0:
		out.Outcome = OutcomeFailed
		out.Reason = fmt.Sprintf("%d 件を削除できませんでした", failed)
	case touched == 0:
		out.Outcome = OutcomeSkipped
		if out.Reason == "" {
			out.Reason = "触れる対象がありませんでした"
		}
	case len(after.Items) > 0:
		// 第 3 の状態: 実行したのに残っている (simctl の非同期削除がこの形)
		out.Outcome = OutcomeIncomplete
		out.Reason = fmt.Sprintf("削除を要求しましたが %d 件が残っています (時間をおいて再スキャンしてください)", len(after.Items))
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

// parseDeleteVia は catalog の DeleteVia を解釈する。**sudo は解釈しない** (実行しないため)。
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
		if argv[0] == "sudo" {
			return "", nil, errors.New("sudo はツールが実行しません (コマンドを表示するだけにしてください)")
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

// trashMove は src を trashDir へ移す。**上書きしない**: macOS の renameatx_np(RENAME_EXCL) で
// 「宛先が無いときだけ移す」を原子的に行い、名前が埋まっていたら Finder と同じく連番を足す。
// 素の os.Rename は宛先を黙って上書きするので、以前ゴミ箱に入れた同名を破壊する。
//
// 別ボリュームのときは移さずに失敗する。再帰コピーは途中で失敗すると「半分コピーされた」状態を
// 作り、それこそが復元を難しくするため (ゴミ箱移動の目的は復元手段を残すこと)。
func trashMove(src, trashDir string) (string, error) {
	if trashDir == "" {
		return "", errors.New("ゴミ箱の場所が分かりません")
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return "", fmt.Errorf("ゴミ箱を用意できません: %w", err)
	}
	base := filepath.Base(src)
	for i := 1; i <= 64; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s %d", base, i)
		}
		dst := filepath.Join(trashDir, name)
		err := unix.RenameatxNp(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_EXCL)
		switch {
		case err == nil:
			return dst, nil
		case errors.Is(err, unix.EEXIST), errors.Is(err, unix.ENOTEMPTY):
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

// ---- インベントリ ----

type history struct {
	path string
}

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

type inventory struct {
	Phase   string       `json:"phase"` // planned (削除前) | done (削除後)
	Tool    string       `json:"tool"`
	Written time.Time    `json:"written"`
	Report  DeleteReport `json:"report"`
}

// write は記録を書き直す (planned → done)。tmp + rename で、途中で切れた JSON を残さない。
func (h *history) write(rep DeleteReport, phase string) error {
	b, err := json.MarshalIndent(inventory{Phase: phase, Tool: "glogx doctor", Written: rep.StartedAt, Report: rep}, "", "  ")
	if err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}
