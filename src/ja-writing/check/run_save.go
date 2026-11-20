package check

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"ja-writing/cmdenv"
	"ja-writing/diff"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 記録を書く (本家の _ensure_dir・_touch・_mkdtemp・_remove_temporary・_put_snapshots・_write_reports・_reserve_attempt・_history・_save)。

const attemptTries = 100 // _ATTEMPT_TRIES

// notJudged は _NOT_JUDGED (記録の result.not_judged)。
var notJudged = []string{"meaning_preservation", "reader_comprehension"}

// ensureDir は _ensure_dir (base の下にフォルダを 1 段ずつ作る。作ったフォルダだけ 0o700)。
func ensureDir(base string, parts ...string) (string, error) {
	path := base
	for _, part := range parts {
		path = visibility.PathJoin(path, part)
		if err := pyos.Mkdir(path, 0o700); err != nil {
			if errors.Is(err, fs.ErrExist) {
				if !isDir(path) {
					return "", hiddenError(path, err)
				}
				continue
			}
			return "", err
		}
		if err := pyos.Chmod(path, 0o700); err != nil {
			return "", err
		}
	}
	return path, nil
}

// touch は _touch (空のファイルを作る。すでにあればそのまま)。
func touch(folder, name string) error {
	path := visibility.PathJoin(folder, name)
	file, err := pyos.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return pyos.Chmod(path, 0o600)
}

// tempdirTries は tempfile.TMP_MAX (候補のフォルダ 1 つで、試しのファイルの名前を試す回数)。
const tempdirTries = 20

// gettempdir は tempfile.gettempdir (本家の _mkdtemp が一時フォルダを置く親)。候補を順に、試しのファイルを作って書いて消せるかで試し、
// 最初に通ったフォルダを返す。候補は環境変数 TMPDIR・TEMP・TMP (空は飛ばす)・/tmp・/var/tmp・/usr/tmp・作業ディレクトリ (読めなければ ".")。
// 名前が重なったら同じフォルダで次の名前を試し、ほかの誤り (無い・ファイル・書けない) は次の候補へ移る。どれも通らなければ FileNotFoundError。
// 本家はプロセスの中で 1 度だけ求めて覚えるが、run はプロセスの中で 1 度しか呼ばないので覚えない。
func gettempdir(env cmdenv.Env) (string, error) {
	var candidates []string
	for _, name := range []string{"TMPDIR", "TEMP", "TMP"} {
		if value, _ := env.Lookup(name); value != "" {
			candidates = append(candidates, value)
		}
	}
	candidates = append(candidates, "/tmp", "/var/tmp", "/usr/tmp")
	if cwd, ok := visibility.Getcwd(); ok {
		candidates = append(candidates, cwd)
	} else {
		candidates = append(candidates, ".")
	}
	for _, dir := range candidates {
		if dir != "." {
			absolute, err := visibility.AbsolutePath(dir)
			if err != nil {
				return "", err
			}
			dir = absolute
		}
		if usableTempdir(dir) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("FileNotFoundError: [Errno 2] No usable temporary directory found in %s", pyops.Format(pyjson.Strings(candidates)))
}

// usableTempdir は _get_default_tempdir の 1 つの候補の試し (O_EXCL と O_NOFOLLOW で作り、"blat" を書き、閉じて消せたら真)。
func usableTempdir(dir string) bool {
	for range tempdirTries {
		var random [4]byte
		if _, err := randomRead(random[:]); err != nil {
			return false
		}
		name := visibility.PathJoin(dir, hex.EncodeToString(random[:]))
		file, err := pyos.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return false
		}
		_, werr := file.Write([]byte("blat"))
		cerr := file.Close()
		rerr := pyos.Remove(name)
		return werr == nil && cerr == nil && rerr == nil
	}
	return false
}

// mkdtemp は _mkdtemp (一時フォルダの下に <prefix><16進> を 0o700 で作り、絶対パスを返す)。
func mkdtemp(rt Runtime, env cmdenv.Env, prefix string) (string, error) {
	parentDir, err := rt.TempDir(env)
	if err != nil {
		return "", err
	}
	parent, err := visibility.AbsolutePath(parentDir)
	if err != nil {
		return "", err
	}
	for range temporaryTries {
		var random [8]byte
		if _, err := randomRead(random[:]); err != nil {
			return "", err
		}
		path := visibility.PathJoin(parent, prefix+hex.EncodeToString(random[:]))
		err := pyos.Mkdir(path, 0o700)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", &namingError{Folder: parent}
}

// removeTemporary は _remove_temporary (shutil.rmtree(path, onexc=again) で消し、それでも残れば標準エラーに 1 行で知らせる)。
func removeTemporary(path string, warn func(string)) {
	if err := removeTree(path); err != nil {
		warn(fmt.Sprintf(temporaryLeftLine, path))
	}
}

// removeTree は shutil.rmtree(path, onexc=again) の写し: 中身を消してからフォルダを消し、消せない・読めないものは again で手当てして
// 1 度だけやり直す。最初の失敗で止まる (本家は again の中の誤りが rmtree の外へ出る)。
// 本家の again は、読めない入れ子のフォルダを開き直す所で os.open の引数が足りず TypeError で落ちる (Python 3.15 で実測)。Go は開き直して続ける
// (check の一時フォルダは check が作った写しだけを持ち、読めないフォルダを作らないので、本番では分かれない)。
// パスで消すので、本家 (fd を使う _rmtree_safe_fd) と違い、歩く途中でフォルダを symlink に差し替えられると先を辿る (README の「揃わない範囲」)。
func removeTree(path string) error { return osTree.remove(path, true) }

// treeOps は removeTree が使うファイルの操作 (テストは、歩く途中で中のものが消える形を作るために包む)。
type treeOps struct {
	lstat   func(string) (fs.FileInfo, error)
	readDir func(string) ([]fs.DirEntry, error)
	delete  func(string) error
}

var osTree = treeOps{lstat: pyos.Lstat, readDir: pyos.ReadDir, delete: pyos.Remove}

// remove は path を中身ごと消す。top は rmtree に渡した根 (本家の orig_entry が None のもの)。
// 本家の _rmtree_safe_fd_step は、根でないものが歩く途中で消えていれば (FileNotFoundError) onexc を呼ばずに次へ進み、根の失敗は
// 消えていても onexc (again) に回す。
func (o treeOps) remove(path string, top bool) error {
	var info fs.FileInfo
	if gone, err := o.step(func(p string) error {
		var err error
		info, err = o.lstat(p)
		return err
	}, path, top); gone || err != nil {
		return err
	}
	if info.IsDir() {
		var entries []fs.DirEntry
		if gone, err := o.step(func(p string) error {
			var err error
			entries, err = o.readDir(p)
			return err
		}, path, top); gone || err != nil {
			return err
		}
		for _, entry := range entries {
			if err := o.remove(visibility.PathJoin(path, entry.Name()), false); err != nil {
				return err
			}
		}
	}
	_, err := o.step(o.delete, path, top)
	return err
}

// step は op(target) を試す。根でない target が既に無ければ (true, nil) で飛ばし、ほかの失敗は again で手当てしてやり直す。
func (o treeOps) step(op func(string) error, target string, top bool) (gone bool, err error) {
	err = op(target)
	if err == nil {
		return false, nil
	}
	if !top && errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	return false, again(op, target)
}

// again は失敗した op(target) を、_remove_temporary の again と同じく、target の親と target の BSD のフラグを外して権限を 0o700 に
// してからやり直す (親を戻すのは、書けない・uchg の親の中のものは、それ自身を戻しても消せないため)。戻せないものはやり直しの誤りで分かる。
func again(op func(string) error, target string) error {
	for _, item := range []string{filepath.Dir(target), target} {
		clearFlags(item)
		_ = pyos.Chmod(item, 0o700)
	}
	return op(target)
}

// putSnapshots は _put_snapshots (入力の写しを folder/<sha256>.bin に置く。同じ中身があれば書かない)。
func putSnapshots(folder string, inputs []*Input) (map[*Input]string, error) {
	paths := map[*Input]string{}
	for _, item := range inputs {
		path := visibility.PathJoin(folder, item.SHA256+".bin")
		current := ""
		if info := fileInfo(path, true); info != nil && info.Mode().IsRegular() {
			current, _ = fileSHA256(path)
		}
		if current != item.SHA256 {
			if err := WriteAtomic(path, item.Data); err != nil {
				return nil, err
			}
		}
		paths[item] = path
	}
	return paths, nil
}

// writeReports は _write_reports (runs/<実行ID>/attempt-<n>.<検査>.json と .txt を書き、記録の checks に名前とハッシュを入れる)。
// 書いたファイルのパスを created に足す。
func writeReports(state string, c *runContext, runID string, attempt attemptNo, created *[]string) error {
	for _, name := range c.required {
		entry := c.entries[name]
		if !pyops.Equal(pyops.Index(entry, "status"), "ran") {
			continue
		}
		var data []byte
		var text string
		if name == "lint" || name == "diff" {
			data, text = c.outputs[name], c.texts[name]
		} else {
			dumped, err := pyjson.Dumps(c.materialsResult, pyjson.Compact())
			if err != nil {
				panic(failure{err})
			}
			data, text = []byte(dumped), materialsText(c)+"\n"
		}
		textData := []byte(text)
		prefix := "runs/" + runID + "/attempt-" + attempt.String() + "." + name
		for _, file := range []struct {
			rel  string
			data []byte
		}{{prefix + ".json", data}, {prefix + ".txt", textData}} {
			path := pathParts(state, file.rel)
			*created = append(*created, path)
			if err := WriteAtomic(path, file.data); err != nil {
				return err
			}
		}
		entry.Set("report", prefix+".json")
		entry.Set("report_sha256", SHA256(data))
		entry.Set("text", prefix+".txt")
		entry.Set("text_sha256", SHA256(textData))
	}
	return nil
}

// attemptExhausted は、回の番号を _ATTEMPT_TRIES 回試して確保できなかったときの OSError (errno の無い例外。文がそのまま理由になる)。
type attemptExhausted struct{ message string }

func (e attemptExhausted) Error() string { return e.message }

// reserveAttempt は _reserve_attempt (回の番号を O_CREAT | O_EXCL で確保する。取れなければ 1 つ増やす)。
func reserveAttempt(runDir string, first attemptNo) (attemptNo, string, error) {
	number := first
	if number.cmp(parseAttemptNo("1")) < 0 {
		number = parseAttemptNo("1")
	}
	for range attemptTries {
		path := visibility.PathJoin(runDir, "attempt-"+number.String()+".json")
		file, err := pyos.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) {
			number = number.next()
			continue
		}
		if err != nil {
			return attemptNo{}, "", err
		}
		if err := file.Close(); err != nil {
			return attemptNo{}, "", err
		}
		if err := pyos.Chmod(path, 0o600); err != nil {
			return attemptNo{}, "", err
		}
		return number, path, nil
	}
	return attemptNo{}, "", attemptExhausted{"回の番号を確保できません（" + runDir + " に attempt-" + number.String() + ".json まで）"}
}

// history は _history (同じ実行の前の回から、previous・fix_count・前回と同じ最終稿か)。
func history(state, runID string, attempt attemptNo, final *Input) (pyjson.Value, int, bool) {
	var texts []string
	var previous pyjson.Value
	for _, number := range attemptNumbers(state + "/runs/" + runID) {
		if number.cmp(attempt) >= 0 {
			continue
		}
		record := must(ReadRecord(attemptFile(state, runID, number)))
		var recorded pyjson.Value
		if record != nil {
			recorded = pyops.Get(pyops.Or(pyops.Get(record, "inputs"), pyjson.NewObject()), "final")
		}
		o, ok := recorded.(*pyjson.Object)
		if !ok {
			continue
		}
		p := pyjson.NewObject()
		p.Set("attempt", number.value())
		p.Set("final_sha256", pyops.Get(o, "sha256"))
		previous = p
		if result, ok := pyops.Get(record, "result").(*pyjson.Object); ok && pyops.Equal(pyops.Get(result, "status"), "partial") {
			continue
		}
		texts = append(texts, pyops.Format(pyops.Get(o, "text_sha256")))
	}
	same := len(texts) > 0 && texts[len(texts)-1] == final.TextSHA256
	texts = append(texts, final.TextSHA256)
	fixCount := 0
	for i := 1; i < len(texts); i++ {
		if texts[i] != texts[i-1] {
			fixCount++
		}
	}
	return previous, fixCount, same
}

// saveArgs は _save が記録に入れる値。
type saveArgs struct {
	link    link
	moment  time.Time
	cwd     string
	host    *pyjson.Object
	tools   *pyjson.Object
	options *pyjson.Object
	pathKey string
}

// pythonInfo は記録の python の欄 (Go の実際の値。本家の verify は型だけを見る)。
func pythonInfo() *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("version", runtime.Version())
	o.Set("implementation", "Go ja-writing "+visibility.Version)
	o.Set("unicode_version", pyunicode.UnicodeVersion)
	return o
}

// saveRun は _save (回を確保し、レポート・記録・索引を書く)。(回, 記録のパス, 修正の回数, 決めたこと)。
// 書けなければ誤り (確保した回のファイルと、この回のレポートと索引は消す。内部エラーの panic でも消す)。
func saveRun(state string, c *runContext, a saveArgs) (attempt attemptNo, recordPath string, fixCount int, d decision, err error) {
	runID := a.link.runID
	runDir, err := ensureDir(state, "runs", runID)
	if err != nil {
		return attemptNo{}, "", 0, decision{}, err
	}
	if a.link.reserved != nil {
		attempt, recordPath = a.link.reserved.number, a.link.reserved.path
	} else if attempt, recordPath, err = reserveAttempt(runDir, a.link.attempt); err != nil {
		return attemptNo{}, "", 0, decision{}, err
	}
	// 消すファイルは名前つきの戻り値でなく別の変数に持つ (誤りで返すときの return は recordPath を空にしてから defer を走らせる)
	created := []string{recordPath}
	committed := false
	defer func() {
		if !committed {
			for _, path := range created {
				_ = pyos.Remove(path) // 本家も消せないファイルはそのまま残す
			}
		}
	}()
	previous, fixCount, same := history(state, runID, attempt, c.final)
	d = decide(c, decideArgs{recorded: true, attempt: attempt, runID: runID, fixCount: fixCount, sameAsPrevious: same})
	if err := writeReports(state, c, runID, attempt, &created); err != nil {
		return attemptNo{}, "", 0, decision{}, err
	}
	var summary pyjson.Value
	if c.materialsResult != nil {
		var classes []pyjson.Value
		for _, item := range pyops.Iter(pyops.GetOr(c.materialsResult, "items", []pyjson.Value{})) {
			classes = append(classes, pyops.Get(item, "classification"))
		}
		s := pyjson.NewObject()
		for _, key := range []string{"from_materials", "not_in_sources", "dropped_certainty"} {
			s.Set(key, countValue(classes, key))
		}
		summary = s
	}
	inputs := pyjson.NewObject()
	var source pyjson.Value
	if c.source != nil {
		source = must(c.source.Record())
	}
	inputs.Set("source", source)
	materials := make([]pyjson.Value, len(c.materials))
	for i, item := range c.materials {
		materials[i] = must(item.Record())
	}
	inputs.Set("materials", materials)
	inputs.Set("final", must(c.final.Record()))
	checks := make([]pyjson.Value, len(c.required))
	for i, name := range c.required {
		checks[i] = c.entries[name]
	}
	var factGaps pyjson.Value
	if c.factGaps != nil {
		factGaps = c.factGaps
	}
	failOnRisks, ferr := diff.FailOnCount(c.risks)
	if ferr != nil {
		raiseFrom(ferr)
	}
	result := pyjson.NewObject()
	for _, kv := range []struct {
		k string
		v pyjson.Value
	}{
		{"status", d.status}, {"token", d.token}, {"next", d.next}, {"fail_on_hit", len(c.failOnHits) > 0},
		{"meaning_risk_count", int64(len(c.risks))}, {"fail_on_meaning_risk_count", int64(failOnRisks)},
		{"final_unchanged_during_run", !c.finalChanged}, {"not_judged", pyjson.Strings(notJudged)},
	} {
		result.Set(kv.k, kv.v)
	}
	record := pyjson.NewObject()
	for _, kv := range []struct {
		k string
		v pyjson.Value
	}{
		{"format", "yomiyasu-check-record"}, {"format_version", int64(1)}, {"run_id", runID}, {"attempt", attempt.value()},
		{"fix_count", int64(fixCount)}, {"previous", previous}, {"created_at", ISO(a.moment)}, {"mode", c.mode}, {"cwd", a.cwd},
		{"host", a.host}, {"skill", SkillInfo()}, {"tools", a.tools}, {"python", pythonInfo()}, {"inputs", inputs},
		{"options", a.options}, {"required_checks", pyjson.Strings(c.required)}, {"checks", checks},
		{"materials_summary", summary}, {"fact_gaps", factGaps}, {"result", result},
	} {
		record.Set(kv.k, kv.v)
	}
	record.Set("record_digest", must(RecordDigest(record)))
	dumped := must(pyjson.Dumps(record, pyjson.Indent1()))
	if err := WriteAtomic(recordPath, []byte(dumped+"\n")); err != nil {
		return attemptNo{}, "", 0, decision{}, err
	}
	name := runID + "." + attempt.String()
	for _, index := range [][3]string{
		{"by-final", c.final.SHA256, name}, {"by-text", c.final.TextSHA256, name}, {"by-path", a.pathKey, Stamp(a.moment) + "." + name},
	} {
		folder, err := ensureDir(state, "index", index[0], index[1])
		if err != nil {
			return attemptNo{}, "", 0, decision{}, err
		}
		created = append(created, visibility.PathJoin(folder, index[2]))
		if err := touch(folder, index[2]); err != nil {
			return attemptNo{}, "", 0, decision{}, err
		}
	}
	committed = true
	return attempt, recordPath, fixCount, d, nil
}
