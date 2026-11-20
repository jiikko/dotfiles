package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/visibility"
)

// start の登録 (本家の「任意の登録。start を使わない限り armed/ は作らない」の節): _armed_place・_armed_valid・armed_entries・_ArmedLock・
// _armed_write・_armed_log・armed_update・_managed_input・start_run・_armed_for_source・_start_snapshot・_limit_label・_managed_run_count・
// _managed_limit。1078 の hook が呼ぶ口 (ArmedEntries・ArmedUpdate・StartRun) は公開し、誤りは本家の例外の型と文を持つ値で返す
// (ValueError・TimeoutError は *pyops.Exception、UnicodeDecodeError は *visibility.Error、ファイルの誤りは pyos の誤り)。

const (
	armedFormat   = "yomiyasu-armed"
	managedBytes  = 2 * 1024 * 1024 // _MANAGED_BYTES (start の原文の大きさの上限)
	armedLogLimit = 50              // 登録の log に残す数

	// armedStale は、これより古いロックを落ちたプロセスの残りとみなして取り直す秒数 (_ARMED_STALE)。
	armedStale = 10.0
	// armedWaitHook は hook (budget を渡す呼び出し) がロックを待つ上限 (_ARMED_WAIT_HOOK)。
	armedWaitHook = 2.0
	// armedWaitCLI は check の start・run・end がロックを待つ上限 (_ARMED_WAIT_CLI)。残ったロックを取り直せるように armedStale より長い。
	armedWaitCLI = armedStale + 2.0
	// reservationSeconds は中身の無い予約を「ほかの run が検査の途中」とみなす秒数 (_RESERVATION_SECONDS)。
	reservationSeconds = 600.0
)

// armedStatuses は _ARMED_STATUSES。
var armedStatuses = []string{"active", "checked", "unverified", "ended"}

// limitRanges は _DEFAULT_LIMITS の鍵と、指定しないときの値と、start_run が受ける範囲 (鍵の順は本家の dict と同じ)。
var limitRanges = []struct {
	key              string
	given, low, high int64
}{
	{"max_stop_blocks", 2, 0, 5},
	{"max_runs", 3, 1, 10},
	{"deadline_min", 30, 1, 240},
}

// DefaultLimits は start で上限を指定しないときの値 (_DEFAULT_LIMITS の写し。呼ぶたびに新しい dict)。
func DefaultLimits() *pyjson.Object {
	o := pyjson.NewObject()
	for _, r := range limitRanges {
		o.Set(r.key, r.given)
	}
	return o
}

// armedLockPause は、ロックを取れなかったときに取り直す前の待ち。テストは差し替えない (本家と同じく 20 ミリ秒まで)。
var armedLockPause = func(seconds float64) { time.Sleep(time.Duration(seconds * float64(time.Second))) }

// valueError・timeoutException は本家が投げる ValueError・TimeoutError (str(error) は Msg)。
func valueError(msg string) error       { return &pyops.Exception{Type: "ValueError", Msg: msg} }
func timeoutException(msg string) error { return &pyops.Exception{Type: "TimeoutError", Msg: msg} }

// isPyError は err が本家の例外の型 name (と、その子のクラス) の誤りか。UnicodeDecodeError は ValueError の子。
func isPyError(err error, name string) bool {
	var e *pyops.Exception
	if errors.As(err, &e) {
		return e.Type == name || (name == "ValueError" && e.Type == "UnicodeDecodeError")
	}
	var v *visibility.Error
	if errors.As(err, &v) {
		return v.Type == name || (name == "ValueError" && v.Type == visibility.TypeUnicodeDecodeError)
	}
	return false
}

// errorText は str(error) (本家の例外の文。パスつきの errno の誤りは OSError の「[Errno 番号] 理由: 'パス'」)。
func errorText(err error) string {
	if o, ok := osException(err); ok {
		return o.Msg
	}
	return pyErrorMessage(err)
}

// armedPlace は _armed_place (os.path.normcase(os.path.realpath(path))。POSIX の normcase は何もしない)。
func armedPlace(path string) string {
	real, ok := visibility.Realpath(path)
	if !ok {
		panic(failure{visibilityNoCwd()})
	}
	return real
}

// armedFile は状態フォルダの armed/ の下の名前。
func armedFile(state, name string) string {
	return visibility.PathJoin(visibility.PathJoin(state, "armed"), name)
}

// field は v[key] を辿る (本家の entry["a"]["b"] で、どこかが dict でないか鍵が無ければ ok が偽 = KeyError か TypeError)。
func field(v pyjson.Value, keys ...string) (pyjson.Value, bool) {
	for _, key := range keys {
		o, ok := v.(*pyjson.Object)
		if !ok || o == nil {
			return nil, false
		}
		if v, ok = o.Get(key); !ok {
			return nil, false
		}
	}
	return v, true
}

// armedValid は _armed_valid (読める登録の形か。どこかの鍵が無い・型が違えば偽)。
func armedValid(entry pyjson.Value, runID string) bool {
	isText := func(keys ...string) bool {
		v, ok := field(entry, keys...)
		_, text := v.(string)
		return ok && text
	}
	isInt := func(keys ...string) bool {
		v, ok := field(entry, keys...)
		return ok && isPyInt(v)
	}
	isTime := func(key string) bool {
		v, ok := field(entry, key)
		text, isStr := v.(string)
		if !ok || !isStr {
			return false
		}
		_, parsed := ParseISO(text)
		return parsed
	}
	format, ok1 := field(entry, "format")
	version, ok2 := field(entry, "format_version")
	id, ok3 := field(entry, "run_id")
	status, ok4 := field(entry, "status")
	final, ok5 := field(entry, "targets", "final")
	log, ok6 := field(entry, "log")
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return false
	}
	_, finalText := final.(string)
	_, isList := log.([]pyjson.Value)
	valid := pyops.Equal(format, armedFormat) && isPyInt(version) && pyops.Equal(version, int64(1)) && pyops.Equal(id, runID) &&
		RunIDProblem(runID) == "" && slices.ContainsFunc(armedStatuses, func(s string) bool { return pyops.Equal(status, s) }) &&
		isTime("created_at") && isTime("deadline") && isText("targets", "source", "path") &&
		isText("targets", "source", "sha256") && isText("targets", "source", "text_sha256") && isText("targets", "source", "snapshot") &&
		(final == nil || finalText) && isInt("counters", "stop_blocks") && isList
	for _, r := range limitRanges {
		valid = valid && isInt("limits", r.key)
	}
	return valid
}

// ArmedEntries は armed_entries (状態フォルダの armed/ にある読める登録を、名前の順に返す。壊れたファイルは飛ばし、フォルダは作らない)。
// armed/ が無ければ空。armed/ を読めない (ファイル・権限) ときは誤り (本家は FileNotFoundError のほかを投げる)。
func ArmedEntries(state string) (entries []*pyjson.Object, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	return armedEntries(state), nil
}

func armedEntries(state string) []*pyjson.Object {
	folder := visibility.PathJoin(state, "armed")
	items, err := pyos.ReadDir(folder)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		panic(failure{err})
	}
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name()
	}
	slices.SortFunc(names, pyStrCompare)
	var entries []*pyjson.Object
	for _, name := range names {
		runID, ok := strings.CutSuffix(name, ".json")
		if !ok || RunIDProblem(runID) != "" {
			continue
		}
		entry := must(ReadRecord(visibility.PathJoin(folder, name)))
		if entry != nil && armedValid(entry, runID) {
			entries = append(entries, entry)
		}
	}
	return entries
}

// armedLock は _ArmedLock (armed/ の下に O_CREAT | O_EXCL でロックのファイルを作る。古いロックは取り直す)。
type armedLock struct {
	path     string
	deadline float64
	identity [2]uint64
}

// newArmedLock は _ArmedLock(state, run_id, budget, source)。budget は Runtime.Monotonic の締め切り (hook。2 秒まで待つ)。nil は check のコマンドで、12 秒まで待つ。
func newArmedLock(rt Runtime, state, runID string, budget *float64, source bool) (*armedLock, error) {
	if RunIDProblem(runID) != "" {
		return nil, valueError("run_id")
	}
	prefix := ""
	if source {
		prefix = ".source-"
	}
	l := &armedLock{path: armedFile(state, prefix+runID+".lock")}
	if budget != nil {
		l.deadline = min(rt.Monotonic()+armedWaitHook, *budget)
	} else {
		l.deadline = rt.Monotonic() + armedWaitCLI
	}
	return l, nil
}

// acquire は __enter__ (POSIX の枝。Windows の PermissionError の待ち直しは写さない)。締め切りまでに取れなければ TimeoutError("armed_lock")。
// ctx が取り消されたら待つのをやめる (本家はシグナルのハンドラが待ちの途中で例外を投げる)。
func (l *armedLock) acquire(ctx context.Context, rt Runtime) error {
	for rt.Monotonic() < l.deadline {
		if err := stopped(ctx); err != nil {
			return err
		}
		file, err := pyos.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			info, serr := file.Stat()
			cerr := file.Close()
			if serr == nil && cerr == nil {
				st := info.Sys().(*syscall.Stat_t)
				l.identity = [2]uint64{uint64(st.Dev), st.Ino}
				return nil
			}
			_ = pyos.Remove(l.path) // 作った直後に失敗しても、ロックを残さない (本家も消せなければそのまま元の例外を出す)
			return cmpErr(serr, cerr)
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, serr := pyos.Stat(l.path)
		switch {
		case errors.Is(serr, syscall.ENOENT):
			continue
		case serr != nil:
			return serr
		case time.Since(info.ModTime()).Seconds() > armedStale:
			if rerr := pyos.Remove(l.path); rerr != nil && !errors.Is(rerr, syscall.ENOENT) {
				return rerr
			}
			continue
		}
		armedLockPause(min(0.02, max(0, l.deadline-rt.Monotonic())))
	}
	return timeoutException("armed_lock")
}

// cmpErr は先に起きた誤り。
func cmpErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// release は __exit__ (自分が作ったロック (dev, ino が同じ) だけを消す)。
func (l *armedLock) release() error {
	info, err := pyos.Stat(l.path)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	st := info.Sys().(*syscall.Stat_t)
	if [2]uint64{uint64(st.Dev), st.Ino} != l.identity {
		return nil
	}
	if err := pyos.Remove(l.path); err != nil && !errors.Is(err, syscall.ENOENT) {
		return err
	}
	return nil
}

// withArmedLock は with _ArmedLock(...): body。body の誤りと panic の後もロックを外す。外すときの誤りは body の誤りより先に返す
// (本家の __exit__ が投げた例外が、with の中の例外に代わって出るのと同じ)。
func withArmedLock(ctx context.Context, rt Runtime, state, runID string, budget *float64, source bool, body func() error) (err error) {
	lock, err := newArmedLock(rt, state, runID, budget, source)
	if err != nil {
		return err
	}
	if err := lock.acquire(ctx, rt); err != nil {
		return err
	}
	defer func() {
		if rerr := lock.release(); rerr != nil {
			err = rerr
		}
	}()
	return body()
}

// armedWrite は _armed_write (log を最後の 50 件にして、armed/<実行ID>.json に書く)。
func armedWrite(state string, entry *pyjson.Object) error {
	trimLog(entry)
	data := must(pyjson.Dumps(entry, pyjson.Indent1()))
	return WriteAtomic(armedFile(state, pyops.Str(pyops.Index(entry, "run_id"))+".json"), []byte(data+"\n"))
}

func trimLog(entry *pyjson.Object) {
	log := pyops.Index(entry, "log")
	entry.Set("log", pyops.Slice(log, max(0, pyops.Len(log)-armedLogLimit), -1))
}

// armedLog は _armed_log (log に 1 件足し、最後の 50 件にする)。
func armedLog(rt Runtime, entry *pyjson.Object, event, result string, ms int) {
	item := pyjson.NewObject()
	item.Set("at", ISO(rt.Now()))
	item.Set("event", event)
	item.Set("result", result)
	item.Set("ms", int64(ms))
	log, ok := pyops.Index(entry, "log").([]pyjson.Value)
	if !ok {
		pyops.Raise("AttributeError", "'%s' object has no attribute 'append'", pyops.TypeName(pyops.Index(entry, "log")))
	}
	entry.Set("log", append(slices.Clone(log), item))
	trimLog(entry)
}

// ArmedChange は ArmedUpdate が登録を読み直した後に呼ぶ変更 (本家の change)。entry を書き換えてよく、write が偽なら書かない (本家の change が False を返したとき)。
type ArmedChange func(entry *pyjson.Object) (write bool, err error)

// ArmedUpdate は armed_update (ロックの中で登録を読み直し、change を当てて書く)。budget は Runtime.Monotonic の締め切り (hook。nil は check のコマンド)。
// 読めない登録は ValueError("armed_record")、ロックを取れなければ TimeoutError("armed_lock")、change の後に締め切りを過ぎていれば
// 書かずに TimeoutError("armed_budget")。読み直した登録 (change を当てた後) を返す。
func ArmedUpdate(ctx context.Context, rt Runtime, state, runID string, change ArmedChange, budget *float64) (entry *pyjson.Object, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	err = withArmedLock(ctx, rt, state, runID, budget, false, func() error {
		record := must(ReadRecord(armedFile(state, runID+".json")))
		if record == nil || !armedValid(record, runID) {
			return valueError("armed_record")
		}
		write, err := change(record)
		if err != nil {
			return err
		}
		if write {
			if budget != nil && rt.Monotonic() >= *budget {
				return timeoutException("armed_budget")
			}
			if err := armedWrite(state, record); err != nil {
				return err
			}
		}
		entry = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// managedInput は _managed_input (通常のファイルの原文を 2 MiB まで読む。標準入力は使えない)。
func managedInput(path string) (*Input, error) {
	if info := fileInfo(path, true); path == "-" || info == nil || !info.Mode().IsRegular() {
		return nil, valueError("通常の原文ファイルを渡してください（標準入力は使えません）。")
	}
	data, err := readLimited(path, managedBytes+1)
	if err != nil {
		return nil, err
	}
	if len(data) > managedBytes {
		return nil, valueError("原文は2 MiBまでです。")
	}
	abs, err := visibility.AbsolutePath(path)
	if err != nil {
		return nil, err
	}
	return newInput("source", 0, &abs, data)
}

// readLimited は open(path, 'rb').read(limit)。
func readLimited(path string, limit int64) ([]byte, error) {
	file, err := pyos.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, limit))
}

// StartOptions は StartRun の省ける引数 (本家の start_run のキーワード引数)。
type StartOptions struct {
	FinalPath, RunID, Session, Host *string
	// By は登録した道 (start か hook:<ホスト>:<イベント>)。空なら "start"。
	By string
	// Limits は上限 (max_stop_blocks・max_runs・deadline_min)。nil なら DefaultLimits。
	Limits *pyjson.Object
	// Cwd は原文と最終稿の相対パスを読む作業フォルダ。空なら Getcwd。
	Cwd string
	// Moment は登録の時刻。nil なら Runtime.Now。
	Moment *time.Time
	// DefaultPlace は EnsureState の defaultPlace (状態フォルダを --state-dir と環境変数で決めず、決まった規則で決めたときに真)。
	DefaultPlace bool
}

// StartRun は start_run (入力の確かめを全部済ませてから、写し・start の記録・登録を書く)。書いた登録を返す。
// 入力の誤りは ValueError (と UnicodeDecodeError)、ロックを取れなければ TimeoutError("armed_lock")、書けなければファイルの誤り。
func StartRun(ctx context.Context, rt Runtime, state, sourcePath string, o StartOptions) (entry *pyjson.Object, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	started := rt.Monotonic()
	cwd := o.Cwd
	if cwd == "" {
		if cwd, err = Getcwd(); err != nil {
			return nil, err
		}
	}
	moment := rt.Now()
	if o.Moment != nil {
		moment = *o.Moment
	}
	limits := DefaultLimits()
	if o.Limits != nil {
		limits = o.Limits.Clone()
	}
	for _, r := range limitRanges {
		v, _ := limits.Get(r.key)
		if !isPyInt(v) || !pyops.GreaterEq(v, r.low) || !pyops.GreaterEq(r.high, v) {
			return nil, valueError(fmt.Sprintf("%s は%d〜%dです。", r.key, r.low, r.high))
		}
	}
	given := "-"
	if sourcePath != "-" {
		given = visibility.PathJoin(cwd, sourcePath)
	}
	source, err := managedInput(given)
	if err != nil {
		return nil, err
	}
	var final *string
	if o.FinalPath != nil {
		if *o.FinalPath == "-" {
			return nil, valueError("--final に標準入力は使えません。")
		}
		abs, err := visibility.AbsolutePath(visibility.PathJoin(cwd, *o.FinalPath))
		if err != nil {
			return nil, err
		}
		final = &abs
	}
	runID := ""
	if o.RunID != nil {
		runID = *o.RunID
	}
	if runID == "" {
		runID = newRunID(rt, state, moment)
	}
	if problem := cmp2(RunIDProblem(runID), func() string { return RunIDCaseProblem(state, runID) }); problem != "" {
		return nil, valueError(problem)
	}
	taken := func() bool {
		return fileInfo(visibility.PathJoin(visibility.PathJoin(state, "runs"), runID), false) != nil ||
			fileInfo(armedFile(state, runID+".json"), false) != nil
	}
	if taken() {
		return nil, valueError("すでにある実行IDです。")
	}
	session := HostValue(o.Session)
	entry = newArmedEntry(source, final, runID, moment, cwd, session, o, limits)
	record, err := startRecord(source, runID, moment, cwd)
	if err != nil {
		return nil, err
	}
	if err := EnsureState(state, o.DefaultPlace); err != nil {
		return nil, err
	}
	if _, err := ensureDir(state, "armed"); err != nil {
		return nil, err
	}
	// 同じ原文の start を直列にする (鍵は原文の実際の場所の SHA-256)
	sourceLock := SHA256([]byte(armedPlace(*source.Path)))
	err = withArmedLock(ctx, rt, state, sourceLock, nil, true, func() error {
		return withArmedLock(ctx, rt, state, runID, nil, false, func() error {
			if taken() {
				return valueError("すでにある実行IDです。")
			}
			return startWrite(ctx, rt, state, source, entry, record, runID, moment, started)
		})
	})
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// cmp2 は a か、a が空なら b()。
func cmp2(a string, b func() string) string {
	if a != "" {
		return a
	}
	return b()
}

// newArmedEntry は start_run の登録の dict (鍵の順は本家と同じ)。
func newArmedEntry(source *Input, final *string, runID string, moment time.Time, cwd string, session *string, o StartOptions, limits *pyjson.Object) *pyjson.Object {
	by := o.By
	if by == "" {
		by = "start"
	}
	var bound pyjson.Value
	if session != nil && o.Host != nil && *o.Host != "" {
		bound = ISO(moment)
	}
	sourceRecord := must(source.Record())
	targetSource := pyjson.NewObject()
	for _, key := range []string{"path", "sha256", "text_sha256", "snapshot"} {
		targetSource.Set(key, sourceRecord.Field(key))
	}
	targets := pyjson.NewObject()
	targets.Set("source", targetSource)
	targets.Set("final", pyjson.OptionalString(final))
	counters := pyjson.NewObject()
	counters.Set("stop_blocks", int64(0))
	minutes, _ := pyops.Number(limits.Field("deadline_min"))
	entry := pyjson.NewObject()
	for _, kv := range []struct {
		k string
		v pyjson.Value
	}{
		{"format", armedFormat}, {"format_version", int64(1)}, {"run_id", runID}, {"created_at", ISO(moment)},
		{"deadline", ISO(moment.Add(time.Duration(minutes) * time.Minute))}, {"by", by}, {"host", pyjson.OptionalString(o.Host)},
		{"session_id", pyjson.OptionalString(session)}, {"bound_at", bound}, {"cwd", cwd}, {"targets", targets}, {"limits", limits},
		{"counters", counters}, {"status", "active"}, {"status_reason", nil}, {"verified", nil}, {"notified", false},
		{"log", []pyjson.Value{}},
	} {
		entry.Set(kv.k, kv.v)
	}
	return entry
}

// startRecord は runs/<実行ID>/start.json の記録 (record_digest つき)。
func startRecord(source *Input, runID string, moment time.Time, cwd string) (*pyjson.Object, error) {
	sourceRecord, err := source.Record()
	if err != nil {
		return nil, err
	}
	inputs := pyjson.NewObject()
	inputs.Set("source", sourceRecord)
	inputs.Set("materials", []pyjson.Value{})
	record := pyjson.NewObject()
	for _, kv := range []struct {
		k string
		v pyjson.Value
	}{
		{"format", "yomiyasu-check-start"}, {"format_version", int64(1)}, {"run_id", runID}, {"created_at", ISO(moment)},
		{"cwd", cwd}, {"mode", "rewrite"}, {"inputs", inputs},
	} {
		record.Set(kv.k, kv.v)
	}
	digest, err := RecordDigest(record)
	if err != nil {
		return nil, err
	}
	record.Set("record_digest", digest)
	return record, nil
}

// startWrite は start_run のロックの中 (実行のフォルダ・写し・start.json・index/starts を書き、同じ原文の前の登録を終え、登録を書く)。
func startWrite(ctx context.Context, rt Runtime, state string, source *Input, entry, record *pyjson.Object, runID string, moment time.Time, started float64) error {
	folder, err := ensureDir(state, "runs")
	if err != nil {
		return err
	}
	runDir := visibility.PathJoin(folder, runID)
	if err := pyos.Mkdir(runDir, 0o700); err != nil {
		return err
	}
	inputs, err := ensureDir(state, "inputs")
	if err != nil {
		return err
	}
	if _, err := putSnapshots(inputs, []*Input{source}); err != nil {
		return err
	}
	if err := WriteAtomic(visibility.PathJoin(runDir, "start.json"), []byte(must(pyjson.Dumps(record, pyjson.Indent1()))+"\n")); err != nil {
		return err
	}
	starts, err := ensureDir(state, "index", "starts", SHA256([]byte(must(Place(*source.Path)))))
	if err != nil {
		return err
	}
	if err := touch(starts, Stamp(moment)+"."+runID); err != nil {
		return err
	}
	place := armedPlace(*source.Path)
	for _, old := range armedEntries(state) {
		if pyops.Equal(pyops.Index(old, "status"), "ended") || armedPlace(pyops.Str(entryPath(old))) != place {
			continue
		}
		if _, err := ArmedUpdate(ctx, rt, state, pyops.Str(pyops.Index(old, "run_id")), endChange(rt, "start", "superseded"), nil); err != nil {
			return err
		}
	}
	armedLog(rt, entry, "start", "started", int((rt.Monotonic()-started)*1000))
	return armedWrite(state, entry)
}

// endChange は登録を終える change (start の end_old と end の finish。もう終えた登録は書かない)。log の結果は "ended:<理由>"。
func endChange(rt Runtime, event, reason string) ArmedChange {
	return func(current *pyjson.Object) (bool, error) {
		if pyops.Equal(pyops.Index(current, "status"), "ended") {
			return false, nil
		}
		current.Set("status", "ended")
		current.Set("status_reason", reason)
		armedLog(rt, current, event, "ended:"+reason, 0)
		return true, nil
	}
}

// entryPath は登録の targets.source.path。
func entryPath(entry pyjson.Value) pyjson.Value {
	return pyops.Index(pyops.Index(pyops.Index(entry, "targets"), "source"), "path")
}

// armedForSource は _armed_for_source (原文の登録を 1 つに決められれば返す。runID があればその実行 ID、無ければ原文の場所か中身で探す)。
func armedForSource(state string, source *Input, runID *string, newRun bool) *pyjson.Object {
	if newRun || source == nil || state == "" || !isDir(visibility.PathJoin(state, "armed")) {
		return nil
	}
	var candidates []*pyjson.Object
	for _, entry := range armedEntries(state) {
		if pyops.Equal(pyops.Index(entry, "status"), "ended") {
			continue
		}
		var matches bool
		if runID != nil {
			matches = pyops.Equal(pyops.Index(entry, "run_id"), *runID)
		} else {
			target := pyops.Index(pyops.Index(entry, "targets"), "source")
			matches = (source.Path != nil && *source.Path != "" && armedPlace(*source.Path) == armedPlace(pyops.Str(pyops.Index(target, "path")))) ||
				pyops.Equal(pyops.Index(target, "sha256"), source.SHA256)
		}
		if matches {
			candidates = append(candidates, entry)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return nil
}

// isHex64 は re.fullmatch(r"[0-9a-f]{64}", v)。str でなければ本家と同じ TypeError。
func isHex64(v pyjson.Value) bool {
	s, ok := v.(string)
	if !ok {
		pyops.Raise("TypeError", "expected string or bytes-like object, got '%s'", pyops.TypeName(v))
	}
	return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == ""
}

// samePathValue は _same_path(value, path) (value が str でなければ本家の os.path と同じく TypeError)。
func samePathValue(value pyjson.Value, path string) bool {
	s, ok := value.(string)
	if !ok {
		pyops.Raise("TypeError", "expected str, bytes or os.PathLike object, not %s", pyops.TypeName(value))
	}
	return must(SamePath(s, path))
}

// startSnapshot は _start_snapshot (同じファイルを直す run の原文を、start の記録と写しから取る)。(実行 ID, 原文) か ("", nil)。
// runID が nil なら、index/starts/ の原文の場所の鍵から新しい順に、6 時間以内に同じ作業フォルダで登録したものを探す。
func startSnapshot(rt Runtime, state, sourcePath string, runID *string, cwd *string) (string, *Input) {
	var ids []string
	if runID != nil && *runID != "" {
		ids = []string{*runID}
	} else {
		key := SHA256([]byte(must(Place(sourcePath))))
		names, _ := listNames(visibility.PathJoin(visibility.PathJoin(visibility.PathJoin(state, "index"), "starts"), key))
		slices.SortFunc(names, func(a, b string) int { return pyStrCompare(b, a) })
		for _, name := range names {
			if _, id, ok := strings.Cut(name, "."); ok {
				ids = append(ids, id)
			}
		}
	}
	given := runID != nil && *runID != ""
	for _, candidate := range ids {
		if RunIDProblem(candidate) != "" {
			continue
		}
		record := must(ReadRecord(visibility.PathJoin(visibility.PathJoin(visibility.PathJoin(state, "runs"), candidate), "start.json")))
		if record == nil || record.Len() == 0 || !pyops.Equal(pyops.Get(record, "format"), "yomiyasu-check-start") ||
			!pyops.Equal(pyops.Get(record, "record_digest"), must(RecordDigest(record))) {
			continue
		}
		createdText, _ := pyops.Get(record, "created_at").(string)
		created, ok := ParseISO(createdText)
		if !given && (!ok || rt.Now().Sub(created).Seconds() > linkSeconds || cwd == nil || !samePathValue(pyops.GetOr(record, "cwd", ""), *cwd)) {
			continue
		}
		item, isDict := pyops.Get(pyops.GetOr(record, "inputs", pyops.EmptyDict()), "source").(*pyjson.Object)
		if !isDict || !samePathValue(pyops.GetOr(item, "path", ""), sourcePath) {
			continue
		}
		digest := pyops.GetOr(item, "sha256", "")
		if !isHex64(digest) {
			continue
		}
		data, err := readLimited(visibility.PathJoin(visibility.PathJoin(state, "inputs"), digest.(string)+".bin"), managedBytes+1)
		if err != nil || SHA256(data) != digest {
			continue
		}
		abs := must(AbsPath(sourcePath))
		return candidate, must(newInput("source", 0, &abs, data))
	}
	return "", nil
}

// limitLabel は _limit_label (上限で止めた理由の短い名前)。
func limitLabel(entry *pyjson.Object) string {
	limits := pyops.Index(entry, "limits")
	table := map[string]string{
		"deadline":  "期限" + pyops.Format(pyops.Index(limits, "deadline_min")) + "分",
		"run_limit": "run " + pyops.Format(pyops.Index(limits, "max_runs")) + "回まで",
	}
	return pyops.Str(pyops.Lookup(table, pyops.Index(entry, "status_reason"), "続けさせる回数の上限"))
}

// managedRunCount は _managed_run_count (登録の run の回数。書き終えた記録と、10 分以内の中身の無い予約を数える)。
// 時刻は壁時計 (本家の time.time()。ファイルの mtime と比べる)。
func managedRunCount(runDir string) int {
	count := 0
	for _, number := range attemptNumbers(runDir) {
		info, err := pyos.Stat(visibility.PathJoin(runDir, "attempt-"+number.String()+".json"))
		if err != nil {
			continue
		}
		if info.Size() > 0 || time.Since(info.ModTime()).Seconds() < reservationSeconds {
			count++
		}
	}
	return count
}

// reservedAttempt は登録の run が上限の判定と同じロックの中で確保した回 (本家の args._armed_reservation と link["reserved"])。
type reservedAttempt struct {
	number attemptNo
	path   string
}

// concatText は "<head>" + value (value が str でなければ本家と同じ TypeError)。
func concatText(head string, value pyjson.Value) string {
	s, ok := value.(string)
	if !ok {
		pyops.Raise("TypeError", "can only concatenate str (not \"%s\") to str", pyops.TypeName(value))
	}
	return head + s
}
