package check

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"ja-writing/cmdenv"
	"ja-writing/internal/pycli"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/internal/pystr"
	"ja-writing/visibility"
)

// start・export・end (本家の _management と、その 3 つの parser)。

//go:embed start_help.txt
var startHelp string

//go:embed export_help.txt
var exportHelp string

//go:embed end_help.txt
var endHelp string

// managementCommand は start・export・end の parser の字 (使い方・-h の出力・受ける引数)。
type managementCommand struct {
	name, usage, help string
	spec              pycli.Spec
}

var managementCommands = map[string]managementCommand{
	"start": {
		name: "start",
		usage: "使い方: yomiyasu_check.py start --source 原文 [--final 最終稿] [--run-id ID] [--session ID] [--host claude-code|codex]\n" +
			"                        [--max-stop-blocks N] [--max-runs N] [--deadline-min N] [--state-dir DIR] [--json]\n" +
			"                        [--words フォルダ | --no-words]\n",
		help: startHelp,
		spec: pycli.Spec{
			Values: []string{"--state-dir", "--run-id", "--source", "--final", "--session", "--host", "--max-stop-blocks", "--max-runs",
				"--deadline-min", "--words"},
			Flags: []string{"-h", "--help", "--json", "--no-words"},
		},
	},
	"export": {
		name:  "export",
		usage: "使い方: yomiyasu_check.py export --final 最終稿 [--run-id ID] [--source 原文] [--state-dir DIR] [--json]\n",
		help:  exportHelp,
		spec:  pycli.Spec{Values: []string{"--state-dir", "--run-id", "--source", "--final"}, Flags: []string{"-h", "--help", "--json"}},
	},
	"end": {
		name:  "end",
		usage: "使い方: yomiyasu_check.py end (--run-id ID | --source 原文) [--state-dir DIR] [--json]\n",
		help:  endHelp,
		spec:  pycli.Spec{Values: []string{"--state-dir", "--run-id", "--source"}, Flags: []string{"-h", "--help", "--json"}},
	},
}

// managementArgs は start・export・end の parser が読んだ値 (指定されなかったものは nil。上限は start の type=int の値)。
type managementArgs struct {
	stateDir, runID, source, final, session, host, words *string
	limits                                               *pyjson.Object
	json, noWords                                        bool
}

// hostChoices は start の --host の choices。
var hostChoices = []string{"claude-code", "codex"}

// parseManagement は sub.parse_args(argv)。誤りは argparse と同じ順に出す: 値を読む途中の誤り (型・choices・同じ組の引数) は出てきた順、
// その後に読み終えたときの誤り (足りない引数)、最後に知らない引数。
func parseManagement(cmd managementCommand, argv []string) (managementArgs, bool, error) {
	a := managementArgs{}
	if cmd.name == "start" {
		a.limits = DefaultLimits()
	}
	res, perr := cmd.spec.Parse(argv)
	var group string // end の --run-id と --source (どちらか 1 つ) のうち、先に出たもの
	for _, o := range res.Options {
		value := o.Value
		switch o.Name {
		case "--state-dir":
			a.stateDir = &value
		case "--run-id", "--source":
			if cmd.name == "end" {
				if group != "" && group != o.Name {
					return a, false, pycli.Error("argument " + o.Name + ": not allowed with argument " + group)
				}
				group = o.Name
			}
			if o.Name == "--run-id" {
				a.runID = &value
			} else {
				a.source = &value
			}
		case "--final":
			a.final = &value
		case "--session":
			a.session = &value
		case "--host":
			if !slices.Contains(hostChoices, value) {
				return a, false, pycli.Error("argument --host: invalid choice: " + pystr.Repr(value) + " (choose from 'claude-code', 'codex')")
			}
			a.host = &value
		case "--max-stop-blocks", "--max-runs", "--deadline-min":
			n, ok := pystr.ParseInt(value)
			if !ok {
				return a, false, pycli.Error("argument " + o.Name + ": invalid int value: " + pystr.Repr(value))
			}
			a.limits.Set(strings.ReplaceAll(strings.TrimPrefix(o.Name, "--"), "-", "_"), pyops.IntValue(n))
		case "--words":
			a.words = &value
		case "--json":
			a.json = true
		case "--no-words":
			a.noWords = true
		}
	}
	var unknown pycli.Error
	if perr != nil && (!errors.As(perr, &unknown) || !strings.HasPrefix(string(unknown), "unrecognized arguments: ")) {
		return a, false, perr
	}
	if res.Help {
		return a, true, nil
	}
	switch {
	case cmd.name == "start" && a.source == nil:
		return a, false, pycli.Error("the following arguments are required: --source")
	case cmd.name == "export" && a.final == nil:
		return a, false, pycli.Error("the following arguments are required: --final")
	case cmd.name == "end" && group == "":
		return a, false, pycli.Error("one of the arguments --run-id --source is required")
	}
	return a, false, perr
}

// management は _management の入口 (start・export・end)。(終了コード, 誤り)。
func management(ctx context.Context, env cmdenv.Env, rt Runtime, name string, argv []string) (code int, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	cmd := managementCommands[name]
	prog := prog + " " + name
	usage := func(message string) (int, error) { return pycli.UsageTo(env, cmd.usage, prog, message) }
	a, help, perr := parseManagement(cmd, argv)
	if perr != nil {
		return usage(perr.Error())
	}
	if help {
		return 0, pycli.AtExit("stdout", pycli.PrintTo(env, cmd.help))
	}
	if a.stateDir != nil && pystr.Strip(*a.stateDir) == "" {
		return usage("--state-dir に空の値は使えません")
	}
	if a.runID != nil {
		if problem := RunIDProblem(*a.runID); problem != "" {
			return usage(problem)
		}
	}
	if (a.source != nil && *a.source == "-") || (a.final != nil && *a.final == "-") {
		return usage("標準入力（-）は使えません")
	}
	if err := sameWorkingDirectory(env); err != nil {
		return 0, err
	}
	if name == "start" || name == "export" {
		if problem := must(PathProblem(name, []*string{a.source, a.final})); problem != "" {
			return usage(problem)
		}
	}
	var words *visibility.Words
	found := false
	if name == "start" {
		cwd, err := Getcwd()
		if err != nil {
			return 0, err
		}
		var done bool
		var code int
		if words, found, done, code, err = resolveWords(ctx, env, cwd, a.words, a.noWords, usage); done {
			return code, err
		}
	}
	state := must(StateDir(optionOf(a.stateDir), env.Lookup))
	switch name {
	case "start":
		return startCommand(ctx, env, rt, a, state, words, found, usage)
	case "end":
		return endCommand(ctx, env, rt, a, state)
	}
	return exportCommand(ctx, env, a, state, usage)
}

// startCommand は _management の start の枝。
func startCommand(ctx context.Context, env cmdenv.Env, rt Runtime, a managementArgs, state string, words *visibility.Words, found bool,
	usage func(string) (int, error)) (int, error) {
	// 読む失敗は入力の誤り。書く失敗は終了コード 3 (状態フォルダに書けないときは、内部エラーでなく案内を出す)
	var err error
	if stop := untilStopped(ctx, func() { _, err = managedInput(*a.source) }); stop != nil {
		return 0, stop
	}
	if err != nil {
		if isPyError(err, "ValueError") || isOSError(err) {
			return usage(errorText(err))
		}
		return 0, err
	}
	session := a.session
	if session == nil {
		if value, ok := env.Lookup("YOMIYASU_SESSION"); ok {
			session = &value
		}
	}
	cwd, err := Getcwd()
	if err != nil {
		return 0, err
	}
	moment := rt.Now()
	entry, err := StartRun(ctx, rt, state, *a.source, StartOptions{FinalPath: a.final, RunID: a.runID, Session: session, Host: a.host,
		By: "start", Limits: a.limits, Cwd: cwd, Moment: &moment, DefaultPlace: !StateDirGiven(optionOf(a.stateDir), env.Lookup)})
	var noCwd *NoWorkingDirectoryError
	var stop errStopped
	switch {
	case err == nil:
	case isPyError(err, "ValueError"):
		return usage(errorText(err))
	case isPyError(err, "TimeoutError"), errors.As(err, &noCwd), errors.As(err, &stop), !isOSError(err):
		// 同じ原稿の登録のロックを待ちきれなかった (書けないのではない) のは、本家と同じく内部エラーにする
		return 0, err
	default:
		note := ""
		if isPermission(err) {
			note = deniedStateNote
		}
		pycli.Eprintln(env, "yomiyasu_check.py start: 原稿を登録できませんでした（"+WriteProblem(state, err)+"）。"+note+
			"--state-dir か YOMIYASU_STATE_DIR で自分が書ける場所を指定してもう一度実行してください。")
		return pycli.ExitInternal, nil
	}
	runID := pyops.Str(entry.Field("run_id"))
	limits := a.limits
	message := fmt.Sprintf("原稿を登録しました（上限: 続けさせる%s回、run %s回、%s分）。この原稿で run と verify を実行すると、記録がこの登録につながります。",
		pyops.Format(limits.Field("max_stop_blocks")), pyops.Format(limits.Field("max_runs")), pyops.Format(limits.Field("deadline_min")))
	target := pyops.Index(pyops.Index(entry, "targets"), "source")
	token := "yomiyasu-check: started via=start run=" + runID + " attempt=- source=sha256:" + pyops.Str(pyops.Index(target, "sha256"))[:12]
	path := armedFile(state, runID+".json")
	// 本家の args.final and entry["targets"]["final"] (省けば None、空の文字列はそのまま)
	var finalPath pyjson.Value
	if a.final != nil {
		finalPath = *a.final
		if *a.final != "" {
			finalPath = pyops.Index(pyops.Index(entry, "targets"), "final")
		}
	}
	source := pyjson.NewObject()
	for _, key := range []string{"path", "sha256", "text_sha256"} {
		source.Set(key, pyops.Index(target, key))
	}
	result := resultHead("start", "started", token, message)
	for _, kv := range []struct {
		k string
		v pyjson.Value
	}{
		{"run_id", runID}, {"armed_path", path},
		{"start_path", visibility.PathJoin(visibility.PathJoin(visibility.PathJoin(state, "runs"), runID), "start.json")},
		{"source", source}, {"final_path", finalPath}, {"limits", limits}, {"deadline", entry.Field("deadline")},
	} {
		result.Set(kv.k, kv.v)
	}
	lines := []string{message, token, "登録: " + path}
	if found {
		// 語の一覧を使ったときだけ (run に付ける場所を示す)
		wordsContext := visibility.WordsContextOf(words).Value()
		result.Set("words", wordsContext)
		lines = append(lines, visibility.WordsLabelOf(wordsContext))
	}
	return 0, printResult(env, a.json, result, lines)
}

// resultHead は結果の形式 1 の先頭の欄 (format・format_version・tool_version・command・status と、状態の行と 1 行目の文。
// status_line と message の順は command ごとに本家と同じにする)。
func resultHead(command, status, token, message string) *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("format", "yomiyasu-check-result")
	o.Set("format_version", int64(1))
	o.Set("tool_version", visibility.Version)
	o.Set("command", command)
	o.Set("status", status)
	if command == "end" {
		o.Set("message", message)
		o.Set("status_line", token)
	} else {
		o.Set("status_line", token)
		o.Set("message", message)
	}
	return o
}

// printResult は --json なら結果、ほかは行をつないで標準出力に書く。
func printResult(env cmdenv.Env, asJSON bool, result *pyjson.Object, lines []string) error {
	if asJSON {
		return pycli.PrintTo(env, must(pyjson.Dumps(result, pyjson.Indent1()))+"\n")
	}
	return pycli.PrintTo(env, strings.Join(lines, "\n")+"\n")
}

// isOSError は err が本家の OSError にあたる誤り (ファイルの操作の誤り) か。本家の例外の型を持つ誤り (OSError の子のクラスを除く)・
// JSON の誤り・作業フォルダが消えた誤りは違う。
func isOSError(err error) bool {
	var e *pyops.Exception
	var v *visibility.Error
	var j *pyjson.Error
	var noCwd *NoWorkingDirectoryError
	if errors.As(err, &v) {
		return slices.Contains([]string{"OSError", "FileNotFoundError", "PermissionError", "NotADirectoryError", "IsADirectoryError", "FileExistsError"}, v.Type)
	}
	return !errors.As(err, &e) && !errors.As(err, &j) && !errors.As(err, &noCwd)
}

// endCommand は _management の end の枝。
func endCommand(ctx context.Context, env cmdenv.Env, rt Runtime, a managementArgs, state string) (int, error) {
	count := 0
	var entries []*pyjson.Object
	if stop := untilStopped(ctx, func() { entries = armedEntries(state) }); stop != nil {
		return 0, stop
	}
	for _, entry := range entries {
		if pyops.Equal(pyops.Index(entry, "status"), "ended") {
			continue
		}
		if a.runID != nil {
			if !pyops.Equal(pyops.Index(entry, "run_id"), *a.runID) {
				continue
			}
		} else if armedPlace(pyops.Str(entryPath(entry))) != armedPlace(*a.source) {
			continue
		}
		if _, err := ArmedUpdate(ctx, rt, state, pyops.Str(pyops.Index(entry, "run_id")), endChange(rt, "end", "end_command"), nil); err != nil {
			return 0, err
		}
		count++
	}
	message := "登録が見つかりません。"
	if count > 0 {
		message = fmt.Sprintf("登録を終えました（%d件）。この原稿のフックによる照合と投稿の確認は行いません。", count)
	}
	shownID := "-"
	if a.runID != nil && *a.runID != "" {
		shownID = *a.runID
	}
	token := fmt.Sprintf("yomiyasu-check: ended via=end run=%s count=%d", shownID, count)
	result := resultHead("end", "ended", token, message)
	result.Set("run_id", pyjson.OptionalString(a.runID))
	result.Set("count", int64(count))
	code := 0
	if count == 0 {
		code = pycli.ExitHit
	}
	return code, printResult(env, a.json, result, []string{message, token})
}

// exportExtension は書き出す写しの拡張子 (本家の re.fullmatch(r"\.[A-Za-z0-9]{1,10}", extension) に合わなければ .txt)。
var exportExtension = regexp.MustCompile(`^\.[A-Za-z0-9]{1,10}$`)

// pathName は Path(s).name。
func pathName(s string) string {
	whole := visibility.PathString(s)
	if whole == "." {
		return ""
	}
	return whole[strings.LastIndex(whole, "/")+1:]
}

// exportCommand は _management の export の枝 (照合が合えば、最終稿と同じバイト列の写しを exports/ に読み取り専用で書き出す)。
func exportCommand(ctx context.Context, env cmdenv.Env, a managementArgs, state string, usage func(string) (int, error)) (int, error) {
	var final *Target
	var problem string
	var readErr error
	var v verification
	if stop := untilStopped(ctx, func() {
		if final, problem, readErr = ReadTarget(*a.final, nil); readErr != nil || problem != "" {
			return
		}
		v = verify(env.Lookup, state, final, a.runID, nil, a.source)
	}); stop != nil {
		return 0, stop
	}
	switch {
	case readErr != nil:
		return 0, readErr
	case problem != "":
		return usage(problem)
	case v.err != nil:
		return 0, v.err
	case v.problem != "":
		return pycli.Fail(env, v.problem)
	}
	result := v.out.json(final)
	result.Set("command", "export")
	result.Set("export_path", nil)
	result.Set("status_line", strings.Replace(v.out.token, "via=verify", "via=export", 1))
	recordLine := v.out.recordLine
	if v.out.status == "executed" {
		path, err := exportCopy(state, *a.final, v.out, final)
		if err != nil {
			return 0, err
		}
		result.Set("status", "exported")
		result.Set("export_path", path)
		result.Set("status_line", strings.Replace(pyops.Str(result.Field("status_line")), "executed via=", "exported via=", 1))
		result.Set("message", "検査した最終稿と同じ内容の写しを書き出しました。投稿や送信には、この写しのパスを渡してください"+
			"（例 gh pr create --body-file <写し>）。合格の判定ではありません。")
		recordLine = "写し: " + path
	}
	code := pycli.ExitHit
	if pyops.Equal(result.Field("status"), "exported") {
		code = 0
	}
	lines := []string{pyops.Str(result.Field("message")), pyops.Str(result.Field("status_line")), recordLine}
	return code, printResult(env, a.json, result, lines)
}

// exportCopy は写しを state/exports/<実行ID>/attempt-<回>-<SHA-256 の先頭 12 字><拡張子> に書き、0o444 にする。
// 同じ中身の写しがあれば書き直さない。書いた後の中身が最終稿と違えば消して誤り (本家の OSError("export_hash"))。
func exportCopy(state, given string, out *verifyResult, final *Target) (string, error) {
	name := pathName(given)
	extension := ""
	if strings.Contains(strings.TrimLeft(name, "."), ".") {
		extension = name[strings.LastIndex(name, "."):]
	}
	if !exportExtension.MatchString(extension) {
		extension = ".txt"
	}
	folder, err := ensureDir(state, "exports", out.found.runID)
	if err != nil {
		return "", err
	}
	path := visibility.PathJoin(folder, "attempt-"+out.found.attempt.String()+"-"+final.SHA256[:12]+extension)
	same := false
	if info := fileInfo(path, true); info != nil && info.Mode().IsRegular() {
		current, err := fileSHA256(path)
		if err != nil {
			return "", err
		}
		same = current == final.SHA256
	}
	if !same {
		if err := WriteAtomic(path, final.Data); err != nil {
			return "", err
		}
	}
	if err := pyos.Chmod(path, 0o444); err != nil {
		return "", err
	}
	written, err := fileSHA256(path)
	if err != nil {
		return "", err
	}
	if written != final.SHA256 {
		_ = pyos.Remove(path) // 本家も消してから OSError を出す
		return "", &visibility.Error{Type: "OSError", Message: "export_hash"}
	}
	return path, nil
}

// verification は _verification と、それが呼ぶ _verify(..., _final=final, _return=True) の結果 (出力しない)。
// problem は本家の _verify が _error に出して 2 を返す文。
type verification struct {
	out     *verifyResult
	problem string
	err     error
}

// verify は _verification (状態フォルダ state で、読んだ最終稿 final を照らす。source は原文のパスで、読んで記録の原文と比べる)。
func verify(lookup func(string) (string, bool), state string, final *Target, runID, since, source *string) verification {
	if runID != nil {
		if problem := RunIDProblem(*runID); problem != "" {
			return verification{problem: problem}
		}
	}
	moment, problem := Since(since)
	if problem != "" {
		return verification{problem: problem}
	}
	var sourceTarget *Target
	if source != nil {
		var err error
		if sourceTarget, problem, err = ReadTarget(*source, nil); err != nil || problem != "" {
			return verification{problem: problem, err: err}
		}
	}
	state, err := StateDir(state, lookup)
	if err != nil {
		return verification{err: err}
	}
	if runID != nil && isDir(state) {
		if problem := RunIDCaseProblem(state, *runID); problem != "" {
			return verification{problem: problem}
		}
	}
	return verification{out: verifyOutcome(state, final, sourceTarget, verifyArgs{runID: runID}, moment, nil)}
}

// managedLimit は _managed_limit (登録の run が上限に当たっていないかを、登録のロックの中で確かめる)。当たっていなければ回を確保して
// chosen.reserved に入れ、偽を返す。当たっていれば登録を unverified にして、検査を実行しなかったことを出し、真を返す。
func managedLimit(ctx context.Context, env cmdenv.Env, rt Runtime, state string, managed *pyjson.Object, c *runContext, a runArgs,
	chosen *link) (bool, error) {
	check := func(current *pyjson.Object) (bool, error) {
		var reason pyjson.Value
		if pyops.Equal(pyops.Index(current, "status"), "unverified") {
			reason = pyops.Index(current, "status_reason")
		}
		if pyops.Equal(pyops.Index(current, "status"), "ended") {
			return false, nil
		}
		deadline, _ := ParseISO(pyops.Str(pyops.Index(current, "deadline")))
		runID := pyops.Str(pyops.Index(current, "run_id"))
		if reason == nil && rt.Now().After(deadline) {
			reason = "deadline"
		}
		runDir := visibility.PathJoin(visibility.PathJoin(state, "runs"), runID)
		if reason == nil && pyops.GreaterEq(int64(managedRunCount(runDir)), pyops.Index(pyops.Index(current, "limits"), "max_runs")) {
			reason = "run_limit"
		}
		if reason == nil {
			folder, err := ensureDir(state, "runs", runID)
			if err != nil {
				return false, err
			}
			number, path, err := reserveAttempt(folder, chosen.attempt)
			if err != nil {
				return false, err
			}
			chosen.reserved = &reservedAttempt{number, path}
			return false, nil
		}
		current.Set("status", "unverified")
		current.Set("status_reason", reason)
		armedLog(rt, current, "run", concatText("unverified:", reason), 0)
		return true, nil
	}
	entry, err := ArmedUpdate(ctx, rt, state, pyops.Str(pyops.Index(managed, "run_id")), check, nil)
	if err != nil {
		return false, err
	}
	if !pyops.Equal(pyops.Index(entry, "status"), "unverified") {
		return false, nil
	}
	message := "この原稿の管理の上限を超えたので、検査を実行しませんでした（" + limitLabel(entry) + "）。本文はもう変えず、" +
		"完了報告の「実行しなかった検査」に「最終稿の照合（管理の上限）」と書いてください。" +
		"利用者から新しい依頼があれば、start で登録し直せます。"
	runID := pyops.Str(pyops.Index(entry, "run_id"))
	token := StatusLine(Status{State: "unverified", Via: "run", RunID: runID, Final: c.final.Token(), Reason: optionalText(pyops.Index(entry, "status_reason"))})
	path := armedFile(state, runID+".json")
	result := resultHead("run", "unverified", token, message)
	for _, kv := range []struct {
		k string
		v pyjson.Value
	}{
		{"run_id", runID}, {"attempt", nil}, {"record_path", nil}, {"reason", pyops.Index(entry, "status_reason")},
		{"armed_path", path}, {"next", "report_unverified"}, {"fix_count", nil}, {"mode", c.mode}, {"context", resultContext(c)},
		{"fail_on_hit", false}, {"meaning_risk_count", nil}, {"fail_on_meaning_risk_count", nil}, {"lint", nil}, {"diff", nil},
		{"materials", nil}, {"stiffness_check", nil}, {"tone_check", nil}, {"hedges_check", nil}, {"immutable_check", nil},
		{"politeness_check", nil}, {"fact_gaps", nil},
	} {
		result.Set(kv.k, kv.v)
	}
	return true, printResult(env, a.json, result, []string{message, token, "登録: " + path})
}

// TextSHA256Of は text_sha256_of (バイト列を decode_source で読んだ本文の text_sha256)。UTF-8 として読めなければ UnicodeDecodeError。
func TextSHA256Of(data []byte) (string, error) {
	text, _, err := visibility.DecodeSourceBytes(data)
	if err != nil {
		return "", err
	}
	return TextSHA256(text), nil
}

// RunKnownTexts は run_known_texts (実行の start.json と各回の記録にある、原文と最終稿の本文の text_sha256 と、最終稿のパス)。
// 実行 ID の形が違えば ValueError("run_id")。
func RunKnownTexts(state, runID string) (texts, paths map[string]bool, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	if RunIDProblem(runID) != "" {
		return nil, nil, valueError("run_id")
	}
	texts, paths = map[string]bool{}, map[string]bool{}
	folder := visibility.PathJoin(visibility.PathJoin(state, "runs"), runID)
	names := []string{"start.json"}
	for _, n := range attemptNumbers(folder) {
		names = append(names, "attempt-"+n.String()+".json")
	}
	for _, name := range names {
		record := must(ReadRecord(visibility.PathJoin(folder, name)))
		if record == nil {
			continue
		}
		inputs, ok := pyops.Get(record, "inputs").(*pyjson.Object)
		if !ok {
			continue
		}
		for _, role := range []string{"source", "final"} {
			item, ok := pyops.Get(inputs, role).(*pyjson.Object)
			if !ok {
				continue
			}
			if digest, ok := pyops.Get(item, "text_sha256").(string); ok {
				texts[digest] = true
			}
			if path, ok := pyops.Get(item, "path").(string); ok && role == "final" {
				paths[path] = true
			}
		}
	}
	return texts, paths, nil
}

// LatestFinalPath は latest_final_path (実行の読める最新の記録の最終稿のパス。無ければ nil)。実行 ID の形が違えば ValueError("run_id")。
func LatestFinalPath(state, runID string) (path *string, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	if RunIDProblem(runID) != "" {
		return nil, valueError("run_id")
	}
	record := latestRecord(state, runID)
	if record == nil {
		return nil, nil
	}
	item, ok := pyops.Get(pyops.GetOr(record, "inputs", pyops.EmptyDict()), "final").(*pyjson.Object)
	if !ok {
		return nil, nil
	}
	if s, ok := pyops.Get(item, "path").(string); ok {
		return &s, nil
	}
	return nil, nil
}

// VerifyQuick は verify_quick (verify と同じ照らし方で、出力しない。lint と diff は動かさない)。最終稿は finalBytes か、無ければ finalPath を読む。
// 返す dict の鍵は status・attempt・final_sha256・text_sha256・problems・record_path・message (本家と同じ順)。
// verify が入力の誤りで 2 を返す形は ValueError("verify の入力が不正です（終了コード2）")。lookup は状態フォルダを決める環境変数を読む。
func VerifyQuick(lookup func(string) (string, bool), state string, finalPath *string, finalBytes []byte, runID, since *string) (result *pyjson.Object, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	if finalBytes == nil {
		if finalPath == nil {
			return nil, valueError("final_path or final_bytes is required")
		}
		if finalBytes, err = pyos.ReadFile(*finalPath); err != nil {
			return nil, err
		}
	}
	var path *string
	if finalPath != nil {
		abs := must(AbsPath(*finalPath))
		path = &abs
	}
	target := newTarget(path, finalBytes)
	if target.Via == "stdin" && target.TextSHA256 == nil {
		// 本家は状態の行の final= を作る所 (_Target.token) の assert で止まる (本文のハッシュが無い標準入力の最終稿)
		return nil, &pyops.Exception{Type: "AssertionError", Msg: ""}
	}
	if pystr.Strip(state) == "" {
		// 本家は parser.error で SystemExit(2) を投げる
		return nil, &pyops.Exception{Type: "SystemExit", Msg: "2"}
	}
	v := verify(lookup, state, target, runID, since, nil)
	switch {
	case v.err != nil:
		return nil, v.err
	case v.problem != "":
		return nil, valueError("verify の入力が不正です（終了コード2）")
	}
	verified := v.out.json(target)
	result = pyjson.NewObject()
	result.Set("status", v.out.status)
	result.Set("attempt", verified.Field("attempt"))
	result.Set("final_sha256", target.SHA256)
	result.Set("text_sha256", pyjson.OptionalString(target.TextSHA256))
	result.Set("problems", verified.Field("problems"))
	result.Set("record_path", verified.Field("record_path"))
	result.Set("message", v.out.line)
	return result, nil
}
