package check

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"ja-writing/cmdenv"
	"ja-writing/diff"
	"ja-writing/internal/pycli"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pystr"
	"ja-writing/visibility"
)

// verify の引数と出力 (本家の _verify と、verify の parser)。

const prog = "yomiyasu_check.py"

const verifyProg = prog + " verify"

const verifyUsage = "使い方: yomiyasu_check.py verify --final 最終稿|- [--run-id ID] [--source 原文] [--since ISO8601]\n" +
	"           [--replay] [--timeout 秒] [--state-dir DIR] [--json]\n"

// verifyHelp は verify -h の出力 (本家の argparse が幅 80 で組んだ字。端末の幅で組み直さない)。
//
//go:embed verify_help.txt
var verifyHelp string

var verifySpec = pycli.Spec{
	Values: []string{"--final", "--run-id", "--source", "--original", "--since", "--timeout", "--state-dir"},
	Flags:  []string{"-h", "--help", "--replay", "--json"},
}

// verifyArgs は verify の parser が読んだ値 (指定されなかったものは nil)。
type verifyArgs struct {
	final, runID, source, since, timeout, stateDir *string
	replay, json                                   bool
}

// parseVerify は verify.parse_args。誤りは argparse の文 (required の誤りは、知らない引数の誤りより先に出す)。
func parseVerify(argv []string) (verifyArgs, bool, error) {
	var a verifyArgs
	res, err := verifySpec.Parse(argv)
	var perr pycli.Error
	if err != nil && (!errors.As(err, &perr) || !strings.HasPrefix(string(perr), "unrecognized arguments: ")) {
		return a, false, err
	}
	if res.Help {
		return a, true, nil
	}
	for _, o := range res.Options {
		value := o.Value
		switch o.Name {
		case "--final":
			a.final = &value
		case "--run-id":
			a.runID = &value
		case "--source", "--original":
			a.source = &value
		case "--since":
			a.since = &value
		case "--timeout":
			a.timeout = &value
		case "--state-dir":
			a.stateDir = &value
		case "--replay":
			a.replay = true
		case "--json":
			a.json = true
		}
	}
	if a.final == nil {
		return a, false, pycli.Error("the following arguments are required: --final")
	}
	return a, false, err
}

// Main は subcommand の入口 (本家の _run_cli と main)。今は verify と --version だけを写した。
func Main(_ context.Context, env cmdenv.Env) int {
	args := env.Args
	commands := []string{"run", "verify", "start", "export", "end", "-h", "--help", "--version"}
	if len(args) == 0 || !slices.Contains(commands, args[0]) {
		args = append([]string{"run"}, args...)
	}
	switch {
	case args[0] == "verify":
		code, err := runVerify(env, args[1:])
		var noCwd *NoWorkingDirectoryError
		if errors.As(err, &noCwd) {
			pycli.Eprintln(env, noCwd.Message)
			return cmdenv.ExitUsage
		}
		if errors.Is(err, errReplay) {
			pycli.Eprintln(env, replayNotImplemented)
			return pycli.ExitInternal
		}
		if err != nil {
			return pycli.Finish(env, prog, err)
		}
		return code
	case len(args) == 1 && args[0] == "--version":
		line := fmt.Sprintf("%s %s (record format 1, result format 1, materials format %d)\n", prog, visibility.Version, diff.MaterialsFormatVersion)
		if err := pycli.PrintTo(env, line); err != nil {
			return pycli.Finish(env, prog, pycli.AtExit("stdout", err))
		}
		return 0
	}
	return cmdenv.Unimplemented(env, "check "+args[0])
}

// runVerify は _verify。(終了コード, 誤り)。
func runVerify(env cmdenv.Env, argv []string) (code int, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	a, help, perr := parseVerify(argv)
	if perr != nil {
		return pycli.UsageTo(env, verifyUsage, verifyProg, perr.Error())
	}
	if help {
		return 0, pycli.AtExit("stdout", pycli.PrintTo(env, verifyHelp))
	}
	if a.stateDir != nil && pystr.Strip(*a.stateDir) == "" {
		return pycli.UsageTo(env, verifyUsage, verifyProg, verifyStateDirEmpty)
	}
	if *a.final == "-" && a.source != nil && *a.source == "-" {
		return pycli.UsageTo(env, verifyUsage, verifyProg, verifyStdinTwice)
	}
	fail := func(message string) (int, error) {
		pycli.Eprintln(env, message)
		return cmdenv.ExitUsage, nil
	}
	if a.runID != nil {
		if problem := RunIDProblem(*a.runID); problem != "" {
			return fail(problem)
		}
	}
	since, problem := Since(a.since)
	if problem != "" {
		return fail(problem)
	}
	if _, problem := Timeout(a.timeout); problem != "" {
		return fail(problem)
	}
	if problem := must(PathProblem("verify", []*string{a.final, a.source})); problem != "" {
		return fail(problem)
	}
	var stdin []byte
	if *a.final == "-" || (a.source != nil && *a.source == "-") {
		if stdin, problem = ReadStdinBytes(env.Stdin); problem != "" {
			return fail(problem)
		}
	}
	final, problem, err := ReadTarget(*a.final, stdin)
	if err != nil {
		return 0, err
	}
	if problem != "" {
		return fail(problem)
	}
	var source *Target
	if a.source != nil {
		if source, problem, err = ReadTarget(*a.source, stdin); err != nil {
			return 0, err
		}
		if problem != "" {
			return fail(problem)
		}
	}
	option := ""
	if a.stateDir != nil {
		option = *a.stateDir
	}
	state := must(StateDir(option))
	if a.runID != nil && isDir(state) {
		if problem := RunIDCaseProblem(state, *a.runID); problem != "" {
			return fail(problem)
		}
	}
	out := verifyOutcome(state, final, source, a, since)
	if a.json {
		return out.code(), pycli.PrintTo(env, must(pyjson.Dumps(out.json(final), pyjson.Indent1()))+"\n")
	}
	return out.code(), pycli.PrintTo(env, strings.Join(out.lines(), "\n")+"\n")
}

// verifyResult は _verify の、出力を組む前までの結果。
type verifyResult struct {
	status     string
	line       string
	token      string
	recordLine string
	found      *found
	record     *pyjson.Object // 形と digest を確かめられた記録 (shown)
	required   []string
	docType    pyjson.Value
	reader     pyjson.Value
	meaning    pyjson.Value
	replayed   string
	problems   []problem
	finalCount pyjson.Value
	changed    pyjson.Value
	keptMarks  []pyjson.Value
}

func (o *verifyResult) code() int {
	if o.status == "executed" {
		return 0
	}
	return pycli.ExitHit
}

// verifyOutcome は _verify の 2 (状態フォルダ)・3 (記録を探して確かめる)・4 (出力の値) まで。
func verifyOutcome(state string, final *Target, source *Target, a verifyArgs, since *time.Time) *verifyResult {
	o := &verifyResult{replayed: "skipped"}
	var runIDs []string
	var reason string
	var expected *string
	var st verifyState
	trusted := false
	text, _, decodeErr := visibility.DecodeSourceBytes(final.Data)
	empty := len(final.Data) == 0 || (final.TextSHA256 != nil && decodeErr == nil && blank(text))
	switch {
	case empty:
		st.name = "no_record"
		o.problems = append(o.problems, problem{"empty_final", "最終稿が空です"})
	case !isDir(state):
		st.name = "no_record"
		o.problems = append(o.problems, problem{"no_state_dir", "状態フォルダがありません: " + state})
	default:
		r := find(state, final, a.runID, source)
		switch r.kind {
		case "no_record":
			st.name = "no_record"
			o.problems = append(o.problems, problem{"no_record", "この最終稿の記録が見つかりません"})
			if recordedAsInput(state, final) {
				o.problems = append(o.problems, problem{"is_recorded_source", "この本文は記録で原文か資料として使われています"})
			}
		case "ambiguous":
			st.name, runIDs, reason = "ambiguous", r.runIDs, r.reason
			o.problems = append(o.problems, problem{"ambiguous", map[string]string{
				"materials": "資料の集まりの違う実行", "source": "原文の違う実行", "place": "このパスで最後に検査した記録と原文の違う実行",
			}[reason] + ": " + strings.Join(runIDs, ", ")})
		case "final_changed", "wrong_file":
			st.name, o.found = r.kind, r.item
			record := o.found.final()
			if r.kind == "final_changed" {
				expected = expectedToken(record, final)
			}
			shown := "（ハッシュなし）"
			if t := expectedToken(record, final); t != nil {
				shown = *t
			}
			o.problems = append(o.problems, problem{r.kind, "記録 " + o.found.path + " の最終稿は " + shown})
			if r.kind == "final_changed" {
				if onlyLineEndings(state, record, final) {
					o.problems = append(o.problems, problem{"only_line_endings_differ", "両方の改行を LF にそろえると同じです"})
				} else if final.TextSHA256 != nil && pyops.Equal(pyops.Get(record, "text_sha256"), *final.TextSHA256) {
					o.problems = append(o.problems, problem{"only_line_endings_differ", "本文（text_sha256）は記録と同じです"})
				}
			}
		default:
			o.found = r.item
			var found []problem
			st, found, o.replayed, trusted = verifyRecord(state, o.found, final, source, since, a.replay, Released{})
			o.problems = append(o.problems, found...)
			if trusted && final.Via == "file" {
				recorded := pyops.Get(pyops.Index(pyops.Index(o.found.record, "inputs"), "final"), "path")
				if recorded != nil && !must(SamePath(pyops.Str(recorded), *final.Path)) {
					o.problems = append(o.problems, problem{"final_path_differs", "記録の最終稿のパス: " + pyops.Str(recorded)})
				}
			}
		}
	}
	codes := map[string]bool{}
	for _, p := range o.problems {
		codes[p.code] = true
	}
	if trusted {
		o.record = o.found.record
		statuses := map[string]pyjson.Value{}
		for _, entry := range pyops.Iter(pyops.Index(o.record, "checks")) {
			statuses[pyops.Str(pyops.Index(entry, "name"))] = pyops.Index(entry, "status")
		}
		allRan := true
		for _, name := range requiredFor(o.record, nil) {
			if (name == "diff" || name == "materials") && !pyops.Equal(statuses[name], "ran") {
				allRan = false
			}
		}
		if allRan {
			o.meaning = pyops.Index(pyops.Index(o.record, "result"), "meaning_risk_count")
		}
	}
	switch {
	case st.name == "stopped":
		var order []string
		groups := map[string][]string{}
		var named []string
		for _, s := range st.stopped {
			why := failedNames[s[1]]
			if _, ok := groups[why]; !ok {
				order = append(order, why)
			}
			groups[why] = append(groups[why], label(s[0]))
			named = append(named, label(s[0])+"（"+why+"）")
		}
		var parts []string
		for _, why := range order {
			parts = append(parts, strings.Join(groups[why], "、")+" は"+why)
		}
		o.status, o.line = "incomplete", fmt.Sprintf(verifyPartialLine, strings.Join(parts, "、"), strings.Join(named, "、"))
	case st.name == "incomplete":
		labels := make([]string, len(st.names))
		for i, name := range st.names {
			labels[i] = label(name)
		}
		o.status, o.line = "incomplete", fmt.Sprintf(verifyLines["incomplete"], strings.Join(labels, "、"))
	case st.name == "executed":
		o.status = "executed"
		labels := []string{}
		for _, name := range requiredFor(o.record, nil) {
			labels = append(labels, label(name))
		}
		o.line = fmt.Sprintf(verifyLines["executed"], strings.Join(labels, "、"))
		if codes["newer_attempt_differs"] {
			o.line += newerAttemptNote
		}
	case st.name == "no_record" && codes["empty_final"]:
		o.status, o.line = st.name, verifyEmptyLine
	case st.name == "ambiguous" && reason == "materials":
		o.status, o.line = st.name, ambiguousMaterials
	case st.name == "ambiguous" && reason == "place":
		o.status, o.line = st.name, ambiguousPlaceLine
	case st.name == "tool_changed":
		o.status, o.line = st.name, toolChangedLine(o.problems)
	default:
		o.status, o.line = st.name, verifyLines[st.name]
		if st.name == "final_changed" && codes["only_line_endings_differ"] {
			if onlyLineEndings(state, o.found.final(), final) {
				o.line += lineEndingsNote
			} else {
				o.line += textSameVerifyNote
			}
		}
		if st.name == "no_record" && codes["is_recorded_source"] {
			o.line += recordedSourceNote
		}
	}
	if o.record != nil {
		o.required = requiredFor(o.record, nil)
		o.docType, o.reader = recordContext(o.record)
	}
	var replayField *string
	if o.status == "executed" || o.status == "not_reproducible" {
		replayField = &o.replayed
	}
	status := Status{State: o.status, Via: "verify", Final: final.Token(), Expected: expected, Replay: replayField}
	if o.found != nil {
		status.RunID, status.Attempt = o.found.runID, o.found.attempt
	}
	if o.record != nil {
		mode := pyops.Str(pyops.Index(o.record, "mode"))
		checks := strings.Join(o.required, ",")
		status.Mode, status.Checks = &mode, &checks
		status.DocType, status.Reader = optionalText(o.docType), optionalText(o.reader)
	}
	status.MeaningRisk = optionalText(o.meaning)
	o.token = StatusLine(status)
	switch {
	case o.found != nil:
		o.recordLine = "記録: " + o.found.path
	case len(runIDs) > 0:
		o.recordLine = "記録: 複数あります（実行ID " + strings.Join(runIDs, "、") + "）"
	case codes["empty_final"]:
		o.recordLine = verifyEmptyRecord
	default:
		o.recordLine = fmt.Sprintf(noRecordLine, state)
	}
	o.finalCount = recordSummaryCount(o.record, "lint", "candidate_count")
	o.changed = recordSummaryCount(o.record, "diff", "hunk_count")
	if o.record != nil && o.status == "executed" {
		o.keptMarks = recordKeptMarks(o.record)
	}
	return o
}

// optionalText は状態の行の値 (None は出さない。ほかは str())。
func optionalText(v pyjson.Value) *string {
	if v == nil {
		return nil
	}
	s := pyops.Format(v)
	return &s
}

// toolChangedLine は _tool_changed_line (tool_changed の 1 行目)。
func toolChangedLine(problems []problem) string {
	var changed []string
	for _, p := range problems {
		if p.code == "tool_changed" {
			changed = append(changed, p.detail)
		}
	}
	var lists []string
	for _, key := range append(listKeys(), "references/lists/"+FactGapsList+".txt") {
		if slices.Contains(changed, key) {
			lists = append(lists, key)
		}
	}
	switch {
	case len(lists) == 0:
		return verifyLines["tool_changed"]
	case len(lists) == len(changed):
		return fmt.Sprintf(listChangedLine, strings.Join(lists, "、"))
	}
	return fmt.Sprintf(scriptAndListChangedLine, strings.Join(lists, "、"))
}

// recordSummaryCount は _record_summary_count (記録の検査の要約の件数。int でなければ nil)。
func recordSummaryCount(record *pyjson.Object, name, key string) pyjson.Value {
	if record == nil {
		return nil
	}
	for _, entry := range pyops.Iter(pyops.Index(record, "checks")) {
		if pyops.Equal(pyops.Index(entry, "name"), name) {
			value := pyops.Get(pyops.Or(pyops.Get(entry, "summary"), pyops.EmptyDict()), key)
			if isPyInt(value) {
				return value
			}
			return nil
		}
	}
	return nil
}

// verifiedContentsLine は _verified_contents_line (executed の 4 行目)。
func verifiedContentsLine(finalCount, changedCount pyjson.Value, required []string) string {
	final := "不明"
	if finalCount != nil {
		final = pyops.Format(finalCount) + "件"
	}
	line := "記録の中身: 見直し候補 最終稿 " + final
	if slices.Contains(required, "diff") {
		changed := "不明"
		if changedCount != nil {
			changed = pyops.Format(changedCount) + "か所"
		}
		return line + "、直した箇所 " + changed + "（原文の件数と元と後は run の出力にあります）"
	}
	return line + "（新規作成）"
}

// lines は人が読む出力 (1 行目に次の行動、2 行目に状態の行、3 行目に記録のパス。executed だけ 4 行目に件数)。
func (o *verifyResult) lines() []string {
	lines := []string{o.line, o.token, o.recordLine}
	if o.status == "executed" {
		lines = append(lines, verifiedContentsLine(o.finalCount, o.changed, o.required))
		if len(o.keptMarks) > 0 {
			lines = append(lines, fmt.Sprintf(keptMarkVerify, keptMarksText(o.keptMarks)))
		}
	}
	return lines
}

// json は --json の出力 (結果の形式 1)。
func (o *verifyResult) json(final *Target) *pyjson.Object {
	shown := o.record != nil
	var runID, attempt, recordPath, recordedSHA, mode, context, required, checks pyjson.Value
	if o.found != nil {
		runID, attempt, recordPath = o.found.runID, int64(o.found.attempt), o.found.path
		if record := o.found.final(); record != nil {
			if s, ok := pyops.Get(record, "sha256").(string); ok {
				recordedSHA = s
			}
		}
	}
	if shown {
		mode = pyops.Index(o.record, "mode")
		c := pyjson.NewObject()
		c.Set("doc_type", o.docType)
		c.Set("reader", o.reader)
		context = c
		required = pyjson.Strings(o.required)
		statuses := map[string]pyjson.Value{}
		for _, entry := range pyops.Iter(pyops.Index(o.record, "checks")) {
			statuses[pyops.Str(pyops.Index(entry, "name"))] = pyops.Index(entry, "status")
		}
		m := pyjson.NewObject()
		for _, name := range o.required {
			status, ok := statuses[name]
			if !ok {
				status = "missing"
			}
			m.Set(name, status)
		}
		checks = m
	}
	problems := make([]pyjson.Value, len(o.problems))
	for i, p := range o.problems {
		item := pyjson.NewObject()
		item.Set("code", p.code)
		item.Set("detail", p.detail)
		problems[i] = item
	}
	var kept pyjson.Value
	if o.keptMarks != nil {
		kept = o.keptMarks
	}
	out := pyjson.NewObject()
	for _, kv := range []struct {
		key   string
		value pyjson.Value
	}{
		{"format", "yomiyasu-check-result"}, {"format_version", int64(1)}, {"tool_version", visibility.Version},
		{"command", "verify"}, {"status", o.status}, {"status_line", o.token}, {"message", o.line},
		{"run_id", runID}, {"attempt", attempt}, {"record_path", recordPath}, {"final", final.JSON()},
		{"record_final_sha256", recordedSHA}, {"mode", mode}, {"context", context},
		{"required_checks", required}, {"checks", checks}, {"meaning_risk_count", o.meaning}, {"replay", o.replayed},
		{"problems", problems}, {"final_candidate_count", o.finalCount}, {"changed_place_count", o.changed},
		{"kept_marks", kept},
	} {
		out.Set(kv.key, kv.value)
	}
	return out
}
