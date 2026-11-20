package check

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"ja-writing/cmdenv"
	"ja-writing/diff"
	"ja-writing/internal/pycli"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pystr"
	"ja-writing/visibility"
)

// run の引数と本体 (本家の run の parser・_run_impl・_resolve_words・_signal_name)。

const runProg = prog + " run"

const runUsage = "使い方: yomiyasu_check.py run --final 最終稿 (--source 原文 | --material 資料) [--material 資料 …]\n" +
	"           [--stance 立場] [--doc-type 種類] [--reader 読者] [--fail-on meaning-risk|warn|error …] [--timeout 秒]\n" +
	"           [--run-id ID | --new-run] [--session ID] [--state-dir DIR] [--no-record] [--fact-gaps] [--json]\n" +
	"           [--words フォルダ | --no-words]\n" +
	"       yomiyasu_check.py run --stdin [上と同じオプション]\n"

// runHelp は run -h の出力 (本家の argparse が幅 80 で組んだ字。端末の幅で組み直さない)。
//
//go:embed run_help.txt
var runHelp string

var runSpec = pycli.Spec{
	Values: []string{"--final", "--source", "--original", "--material", "--materials", "--stance", "--doc-type", "--reader",
		"--fail-on", "--timeout", "--run-id", "--session", "--state-dir", "--words"},
	Flags: []string{"-h", "--help", "--stdin", "--fact-gaps", "--new-run", "--no-record", "--no-words", "--json"},
}

// runArgs は run の parser が読んだ値 (append のオプションは並び、ほかは指定されなければ nil)。
type runArgs struct {
	final, source, materials, failOn                                  []string
	stance, docType, reader, timeout, runID, session, stateDir, words *string
	stdin, factGaps, newRun, noRecord, noWords, json                  bool
}

func parseRun(argv []string) (runArgs, bool, error) {
	var a runArgs
	res, err := runSpec.Parse(argv)
	if err != nil {
		return a, false, err
	}
	if res.Help {
		return a, true, nil
	}
	for _, o := range res.Options {
		value := o.Value
		switch o.Name {
		case "--final":
			a.final = append(a.final, value)
		case "--source", "--original":
			a.source = append(a.source, value)
		case "--material", "--materials":
			a.materials = append(a.materials, value)
		case "--fail-on":
			a.failOn = append(a.failOn, value)
		case "--stance":
			a.stance = &value
		case "--doc-type":
			a.docType = &value
		case "--reader":
			a.reader = &value
		case "--timeout":
			a.timeout = &value
		case "--run-id":
			a.runID = &value
		case "--session":
			a.session = &value
		case "--state-dir":
			a.stateDir = &value
		case "--words":
			a.words = &value
		case "--stdin":
			a.stdin = true
		case "--fact-gaps":
			a.factGaps = true
		case "--new-run":
			a.newRun = true
		case "--no-record":
			a.noRecord = true
		case "--no-words":
			a.noWords = true
		case "--json":
			a.json = true
		}
	}
	return a, false, nil
}

// errStopped は外からのシグナルで止められた run (本家の _Stopped と KeyboardInterrupt)。
type errStopped struct{ signal syscall.Signal }

func (e errStopped) Error() string {
	return "シグナル " + signalName(e.signal) + " で止められました"
}

// signalName は _signal_name (SIGINT は本家の KeyboardInterrupt の文と同じ名前)。
func signalName(sig syscall.Signal) string {
	if name, ok := map[syscall.Signal]string{syscall.SIGINT: "SIGINT", syscall.SIGTERM: "SIGTERM", syscall.SIGHUP: "SIGHUP"}[sig]; ok {
		return name
	}
	return fmt.Sprintf("シグナル%d", int(sig))
}

// stopped は ctx が取り消されていれば、止めたシグナルの errStopped を返す (シグナル以外の取り消しは SIGINT とみなす)。
func stopped(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	if sig, ok := cmdenv.SignalOf(ctx); ok {
		return errStopped{sig}
	}
	return errStopped{syscall.SIGINT}
}

// watchHangup は run の間だけ SIGHUP を受け、ctx を取り消す (nohup などで無視しているときは受けない。本家の _install_stop_handlers)。
// SIGINT と SIGTERM は main が受けている。
func watchHangup(ctx context.Context) (context.Context, context.CancelFunc) {
	if signal.Ignored(syscall.SIGHUP) {
		return ctx, func() {}
	}
	return cmdenv.NotifyContext(ctx, syscall.SIGHUP)
}

// runMain は run の入口 (引数の parse と _run_impl)。(終了コード, 誤り)。
func runMain(ctx context.Context, env cmdenv.Env, rt Runtime, argv []string) (code int, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	ctx, stop := watchHangup(ctx)
	defer stop()
	a, help, perr := parseRun(argv)
	if perr != nil {
		return pycli.UsageTo(env, runUsage, runProg, perr.Error())
	}
	if help {
		return 0, pycli.AtExit("stdout", pycli.PrintTo(env, runHelp))
	}
	return runImpl(ctx, env, rt, a)
}

// runImpl は _run_impl。
func runImpl(ctx context.Context, env cmdenv.Env, rt Runtime, a runArgs) (int, error) {
	usage := func(message string) (int, error) { return pycli.UsageTo(env, runUsage, runProg, message) }
	fail := func(message string) (int, error) {
		pycli.Eprintln(env, message)
		return cmdenv.ExitUsage, nil
	}
	// 1. 引数を確かめる。ここから入力を読み終えるまで、状態フォルダには何も書かない
	if a.stdin && (len(a.final) > 0 || len(a.source) > 0 || len(a.materials) > 0) {
		return usage("--stdin のときは --final、--source、--material を使えません（原文か資料と最終稿は、標準入力の区切りの行で渡します）")
	}
	if !a.stdin {
		switch {
		case len(a.final) == 0:
			return usage("最終稿のファイルを --final で渡してください（ファイルを作らないときは --stdin）")
		case len(a.final) > 1:
			return usage("--final は1つだけ渡してください")
		case len(a.source) > 1:
			return usage("原文（--source）は1つだけ渡してください")
		case len(a.source) == 0 && len(a.materials) == 0:
			return usage("原文（--source）か資料（--material）を渡してください。推敲では書き直す前の原文を --source に、新規作成では資料を --material に渡します")
		}
		dashes := 0
		for _, given := range slices.Concat(a.final, a.source, a.materials) {
			if given == "-" {
				dashes++
			}
		}
		if dashes > 1 {
			return usage("標準入力（-）は1か所だけに使えます")
		}
	}
	if a.runID != nil && a.newRun {
		return usage("--run-id と --new-run は一緒に使えません")
	}
	if a.stateDir != nil && pystr.Strip(*a.stateDir) == "" {
		return usage("--state-dir に空の値は使えません")
	}
	failOn, problem := FailOn(a.failOn)
	if problem != "" {
		return fail(problem)
	}
	docType, err := visibility.NormalizeDocType(a.docType)
	if err != nil {
		return fail(err.Error())
	}
	reader, err := visibility.NormalizeReader(a.reader)
	if err != nil {
		return fail(err.Error())
	}
	timeout, problem := Timeout(a.timeout)
	if problem != "" {
		return fail(problem)
	}
	if a.runID != nil {
		if problem := RunIDProblem(*a.runID); problem != "" {
			return fail(problem)
		}
	}
	cwd, err := Getcwd()
	if err != nil {
		return 0, err
	}
	if a.words != nil && a.noWords {
		return usage("--words と --no-words は一緒に使えません")
	}
	words, found, err := visibility.ResolveWords(&cwd, a.words, a.noWords)
	if err != nil {
		var werr *visibility.Error
		if errors.As(err, &werr) && werr.Type != visibility.TypeWordsError {
			return 0, err
		}
		return fail(err.Error())
	}
	var stance *string
	if a.stance != nil {
		mapped, ok := diff.Stances()[*a.stance]
		if !ok {
			return fail(fmt.Sprintf("--stance に使えない値です: %s。使えるのは %s です。", shown(*a.stance), strings.Join(diff.StanceNames(), "、")))
		}
		stance = &mapped
	}
	if !a.stdin {
		var given []*string
		for _, list := range [][]string{a.final, a.source, a.materials} {
			for i := range list {
				given = append(given, &list[i])
			}
		}
		if problem := must(PathProblem("run", given)); problem != "" {
			return fail(problem)
		}
	}
	if !a.stdin && len(a.source) > 0 && a.source[0] != "-" && a.final[0] != "-" && must(SameFile(a.source[0], a.final[0])) {
		option := ""
		if a.stateDir != nil {
			option = *a.stateDir
		}
		if !a.newRun && !a.noRecord && isDir(must(StateDir(option))+"/armed") {
			panic(failure{errArmed{}})
		}
		return fail(sameFileError)
	}
	in := InputArgs{Stdin: a.stdin, Materials: a.materials}
	if !a.stdin {
		in.Final = a.final[0]
		if len(a.source) > 0 {
			in.Source = &a.source[0]
		}
	}
	loaded, problem, err := ReadInputs(in, env.Stdin)
	if err != nil {
		return 0, err
	}
	if problem != "" {
		return fail(problem)
	}

	c := newRunContext()
	c.source, c.materials, c.final = loaded.Source, loaded.Materials, loaded.Final
	c.docType, c.reader = docType, reader
	if found {
		c.words = visibility.WordsContextOf(words).Value()
		c.flagReplacements = words.FlagReplacements
	}
	if pystr.Strip(c.final.Text) == "" {
		return fail(emptyFinalError)
	}
	finalCompact := ""
	if len(c.materials) > 0 {
		finalCompact = Compact(c.final.Text)
	}
	compacts := make([]string, len(c.materials))
	for i, item := range c.materials {
		compacts[i] = Compact(item.Text)
	}
	for i, item := range c.materials {
		same := item.TextSHA256 == c.final.TextSHA256 || (finalCompact != "" && compacts[i] == finalCompact)
		if same && (c.source == nil || item.Label == nil) {
			return fail(sameAsFinalError)
		}
	}
	c.mode = "compose"
	if c.source != nil {
		c.mode = "rewrite"
	}
	if c.mode == "compose" {
		best, values := SourceLike(compacts, finalCompact, true)
		for i, item := range c.materials {
			value := values[i]
			item.Similarity = &value
		}
		if best != 0 {
			markTreated(c, c.materials[best-1], "similarity")
		}
	}
	c.noRecord = a.noRecord
	c.failOn = failOn
	moment := rt.Now()
	session := a.session
	if session == nil {
		if value, ok := lookupEnv(env, "YOMIYASU_SESSION"); ok {
			session = &value
		}
	}
	host := pyjson.NewObject()
	host.Set("session", pyjson.OptionalString(HostValue(session)))
	var thread *string
	if value, ok := lookupEnv(env, "CODEX_THREAD_ID"); ok {
		thread = &value
	}
	host.Set("codex_thread_id", pyjson.OptionalString(HostValue(thread)))
	host.Set("armed_by", nil)
	tools := ToolVersions(Released{}, a.factGaps)
	options := pyjson.NewObject()
	options.Set("stance", pyjson.OptionalString(stance))
	options.Set("fail_on", pyjson.Strings(failOn))
	options.Set("timeout", timeout)
	options.Set("doc_type", pyjson.OptionalString(docType))
	options.Set("reader", pyjson.OptionalString(reader))
	options.Set("fact_gaps", a.factGaps)
	if c.words != nil {
		options.Set("words", c.words.Clone())
	}

	// 2. 実行 ID と回を決める (読むだけ。別の原文の実行 ID なら、何も書かずに 2)
	state := ""
	if !a.noRecord {
		option := ""
		if a.stateDir != nil {
			option = *a.stateDir
		}
		state = must(StateDir(option))
	}
	var chosen link
	key := pathKey(c.final, c.source, c.materials)
	if state != "" {
		problem := ""
		if a.runID != nil {
			problem = RunIDCaseProblem(state, *a.runID)
		}
		if problem == "" {
			chosen, problem = linkRun(rt, state, a.runID, a.newRun, c.mode, c.source, c.materials, c.final, moment, cwd)
		}
		if problem != "" {
			return fail(problem)
		}
		if !a.newRun {
			mustNotBeArmed(state, c.source)
		}
		option := ""
		if a.stateDir != nil {
			option = *a.stateDir
		}
		if StateDirGiven(option) {
			c.stateDirOption = &state
		}
		c.sourceWasFinal = c.source != nil && wasCheckedFinal(state, c.source)
		if c.mode == "compose" && c.treated == nil {
			if marked := previousTreated(state, chosen.runID, chosen.attempt); marked != nil {
				for _, item := range c.materials {
					if pyops.Equal(pyops.Get(marked, "text_sha256"), item.TextSHA256) {
						markTreated(c, item, "previous_attempt")
						break
					}
				}
			}
		}
	}
	c.required = modeChecks(c.mode, len(c.materials) > 0, c.treated != nil)
	if a.factGaps {
		gaps, err := diff.FactGaps(c.final.Text, docType)
		if err != nil {
			return 0, err
		}
		c.factGaps = gaps
	}

	// 3. 入力の写しを置き、検査はこの写しに対して行う。記録を書かないときは、消す前提の一時フォルダに写しを置き、終わったら消す
	recording := state != ""
	writeProblem, writeDenied := "", false
	inputs := slices.Concat(sourceList(c.source), c.materials, []*Input{c.final})
	var snapshots map[*Input]string
	if recording {
		err := EnsureState(state, !StateDirGiven(optionOf(a.stateDir)))
		if err == nil {
			var folder string
			if folder, err = ensureDir(state, "inputs"); err == nil {
				snapshots, err = putSnapshots(folder, inputs)
			}
		}
		if err != nil {
			recording, writeProblem, writeDenied = false, WriteProblem(state, err), isPermission(err)
			snapshots = nil
		}
	}
	if err := checkInTemporary(ctx, rt, env, c, snapshots, inputs, stance, numberOf(timeout)); err != nil {
		return 0, err
	}

	// 4. 最終稿のファイルを読み直し、検査の途中で変わっていないかを確かめる (標準入力なら省く)
	if c.final.Via == "file" {
		data, err := os.ReadFile(*c.final.Path)
		c.finalChanged = err != nil || SHA256(data) != c.final.SHA256
	}
	c.failOnHits = failOnHits(c)
	if err := stopped(ctx); err != nil {
		return 0, err
	}

	// 5. レポート・記録・索引を書く
	var attempt attemptNo
	recordPath := ""
	fixCount := 0
	var d decision
	if recording {
		var err error
		attempt, recordPath, fixCount, d, err = saveRun(state, c, saveArgs{link: chosen, moment: moment, cwd: cwd, host: host, tools: tools, options: options, pathKey: key})
		if err != nil {
			recording, writeProblem, writeDenied = false, WriteProblem(state, err), isPermission(err)
		}
	}
	if !recording {
		d = decide(c, decideArgs{problem: writeProblem, denied: writeDenied})
	}
	if err := stopped(ctx); err != nil {
		return 0, err
	}
	return printRun(env, c, a, d, recording, savedRun{chosen, attempt, recordPath, fixCount}, writeProblem)
}

// markTreated は資料を原文とみなす。
func markTreated(c *runContext, item *Input, by string) {
	c.treated = item
	item.TreatedAsSource = true
	item.TreatedBy = &by
}

func sourceList(source *Input) []*Input {
	if source == nil {
		return nil
	}
	return []*Input{source}
}

func optionOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// lookupEnv は os.environ.get(name) (無ければ偽)。cmdenv.Env の Getenv は空と無いを分けないので、本家と同じく空の値は HostValue が捨てる。
func lookupEnv(env cmdenv.Env, name string) (string, bool) {
	value := env.Getenv(name)
	return value, value != ""
}

// numberOf は --timeout の値 (int か float) の秒数。
func numberOf(v pyjson.Value) float64 {
	n, _ := pyops.Number(v)
	return n
}

// checkInTemporary は写しが無ければ一時フォルダに置いてから検査を動かし、一時フォルダを消す (本家の _run_impl の 3 の try と finally)。
func checkInTemporary(ctx context.Context, rt Runtime, env cmdenv.Env, c *runContext, snapshots map[*Input]string, inputs []*Input, stance *string, timeout float64) error {
	if snapshots == nil {
		temporary, err := mkdtemp(rt, "yomiyasu-check-")
		if err != nil {
			return err
		}
		defer removeTemporary(temporary, func(message string) { pycli.Eprintln(env, message) })
		if snapshots, err = putSnapshots(temporary, inputs); err != nil {
			return err
		}
	}
	if err := runChecks(ctx, rt, c, snapshots, stance, timeout); err != nil {
		if stop := stopped(ctx); stop != nil {
			return stop
		}
		return err
	}
	return nil
}

// printRun は _run_impl の 6 (出力と終了コード)。
// savedRun は記録を書いた回 (実行 ID・回・記録のパス・修正の回数)。
type savedRun struct {
	link       link
	attempt    attemptNo
	recordPath string
	fixCount   int
}

func printRun(env cmdenv.Env, c *runContext, a runArgs, d decision, recording bool, saved savedRun, writeProblem string) (int, error) {
	complete := c.risksComplete()
	counts := candidateCounts(c)
	changedSections, changed := changedPlaces(c, recording)
	if d.status == "partial" {
		pycli.Eprintln(env, prog+": "+d.line)
		for _, name := range c.entryOrder {
			entry := c.entries[name]
			if pyops.Equal(pyops.Index(entry, "status"), "ran") {
				continue
			}
			pycli.Eprintln(env, name+": "+pyops.Format(pyops.Or(pyops.Index(entry, "error"), pyops.Index(entry, "status"))))
			if s := pyops.Str(pyops.Index(entry, "stderr")); s != "" {
				pycli.Eprintln(env, strings.TrimRight(s, "\n"))
			}
		}
	}
	var text string
	if a.json {
		text = must(pyjson.Dumps(runJSON(c, d, recording, saved, complete, counts, changed), pyjson.Indent1())) + "\n"
	} else {
		recordLine := "記録: " + saved.recordPath
		if !recording {
			why := writeProblem
			if c.noRecord {
				why = "--no-record"
			}
			recordLine = "記録: なし（" + why + "）"
		}
		all := append(changedSections, sections(c)...)
		note := disclaimer
		if !recording {
			note = unrecordedDisclaimer
		}
		var wordsLines []string
		if c.words != nil {
			wordsLines = []string{visibility.WordsLabelOf(c.words)}
		}
		head := slices.Concat([]string{d.line, d.token, summaryLine(c)}, headLines(c), []string{note, recordLine}, wordsLines,
			detectorLines(c), candidateLines(c, counts))
		text = strings.Join(head, "\n") + "\n\n"
		if len(all) > 0 {
			text += strings.Join(all, "\n\n") + "\n\n"
		}
		text += d.line + "\n" + d.token + "\n"
	}
	code := 0
	switch {
	case d.status == "partial":
		code = pycli.ExitInternal
	case len(c.failOnHits) > 0:
		code = pycli.ExitHit
	}
	return code, pycli.PrintTo(env, text)
}

// runJSON は run の --json (結果の形式 1)。
func runJSON(c *runContext, d decision, recording bool, saved savedRun, complete bool, counts, changed *pyjson.Object) *pyjson.Object {
	var diffResult pyjson.Value
	if c.diffResult != nil {
		r := c.diffResult.Clone()
		r.Set("meaning_risks", diffRisksValue(c.diffRisks))
		diffResult = r
	}
	var runID, attemptValue, path, fixCount pyjson.Value
	if recording {
		runID, attemptValue, path, fixCount = saved.link.runID, saved.attempt.value(), saved.recordPath, int64(saved.fixCount)
	}
	var meaning, failOnMeaning pyjson.Value
	if complete {
		meaning = int64(len(c.risks))
		n, err := diff.FailOnCount(c.risks)
		if err != nil {
			raiseFrom(err)
		}
		failOnMeaning = int64(n)
	}
	var lintValue, materials, gaps, kept, changedValue pyjson.Value
	if c.lintResult != nil {
		lintValue, kept = c.lintResult, c.keptMarks
	}
	if c.materialsResult != nil {
		materials = c.materialsResult
	}
	if c.factGaps != nil {
		gaps = c.factGaps
	}
	if changed != nil {
		changedValue = changed
	}
	out := pyjson.NewObject()
	for _, kv := range []struct {
		k string
		v pyjson.Value
	}{
		{"format", "yomiyasu-check-result"}, {"format_version", int64(1)}, {"tool_version", visibility.Version},
		{"command", "run"}, {"status", d.status}, {"status_line", d.token}, {"message", d.line}, {"next", d.next},
		{"run_id", runID}, {"attempt", attemptValue}, {"fix_count", fixCount}, {"mode", c.mode}, {"context", resultContext(c)},
		{"record_path", path}, {"fail_on_hit", len(c.failOnHits) > 0}, {"meaning_risk_count", meaning},
		{"fail_on_meaning_risk_count", failOnMeaning},
		{"stiffness_check", detectorValue(c, "stiffness")}, {"tone_check", detectorValue(c, "tone")},
		{"hedges_check", detectorValue(c, "hedges")}, {"immutable_check", detectorValue(c, "immutable")},
		{"politeness_check", detectorValue(c, "politeness")},
		{"lint", lintValue}, {"diff", diffResult}, {"materials", materials}, {"candidate_counts", counts},
		{"changed_places", changedValue}, {"fact_gaps", gaps}, {"kept_marks", kept},
	} {
		out.Set(kv.k, kv.v)
	}
	return out
}

func diffRisksValue(risks []pyjson.Value) pyjson.Value {
	if risks == nil {
		return nil
	}
	return risks
}
