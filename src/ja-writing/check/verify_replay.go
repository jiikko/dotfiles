package check

import (
	"context"
	"errors"
	"slices"
	"sync"

	"ja-writing/cmdenv"
	"ja-writing/diff"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/lint"
	"ja-writing/visibility"
)

// verify --replay の作り直し (本家の _replay・_ReplayError・_Material)。

// replayError は _ReplayError (子を起動できない・待てない・シグナルで止まった。記録の食い違いではないので、verify は状態を決めずに rc 3 で終える)。
type replayError struct{ detail string }

func (e replayError) Error() string { return e.detail }

// replayer は verify --replay の作り直しに要る値と、動いている子。
// 作り直しは入力と記録の読みと同じ goroutine (untilStopped) で動く。止められたとき untilStopped は読みの終わりを待たないので、
// runVerify が stop で子を止めてから戻る (止めずに戻ると、プロセスが終わった後も子が締め切りまで動き続ける。本家は except BaseException で子を止めてから終わる)。
type replayer struct {
	ctx     context.Context
	rt      Runtime
	env     cmdenv.Env
	timeout float64

	mu      sync.Mutex
	stopped bool
	running []*children
}

// begin は子の組を覚える。既に止めていれば、その組を起動させない。
func (r *replayer) begin(c *children) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = append(r.running, c)
	if r.stopped {
		c.markStopped()
	}
}

// stop は動いている子を止め、これから起こす子も起こさない。
func (r *replayer) stop() {
	r.mu.Lock()
	r.stopped = true
	running := slices.Clone(r.running)
	r.mu.Unlock()
	for _, c := range running {
		c.stop()
	}
}

// replayText は写しの本文 (_replay の text_of: 写しを開いて decode_source)。読めないときは本家の例外と同じ型と文の誤り。
func replayText(folder string, item pyjson.Value) (string, error) {
	data, err := pyos.ReadFile(folder + "/" + snapshotName(item))
	if err != nil {
		if e, ok := osException(err); ok {
			return "", e
		}
		return "", err
	}
	text, _, err := visibility.DecodeSourceBytes(data)
	return text, err
}

// snapshotName は子に渡す写しの名前 (写しのフォルダを作業フォルダにする。run の _run_checks と同じ)。
func snapshotName(item pyjson.Value) string {
	return pyops.Str(pyops.Index(item, "sha256")) + ".bin"
}

// pyErrorMessage は本家の str(error) (型の名前を付けない例外の文)。
func pyErrorMessage(err error) string {
	var v *visibility.Error
	if errors.As(err, &v) {
		return v.Message
	}
	var e *pyops.Exception
	if errors.As(err, &e) {
		return e.Msg
	}
	return err.Error()
}

// run は _replay。写しに対して記録と同じ引数で lint と diff を子で動かし、照合はプロセスの中で作り直して、レポート (.json と .txt) の
// ハッシュを記録と比べる。[(問題のコード, 詳しく)] と replay の値を返す。子を動かせなかったときは failure{replayError} を投げる。
func (r *replayer) run(state string, record *pyjson.Object, files ToolFiles) ([]problem, string) {
	inputs := pyops.Index(record, "inputs")
	entries := map[string]pyjson.Value{}
	for _, entry := range pyops.Iter(pyops.Index(record, "checks")) {
		if pyops.Equal(pyops.Index(entry, "status"), "ran") {
			name := pyops.Index(entry, "name")
			pyops.Hashable(name)
			entries[pyops.Str(name)] = entry
		}
	}
	has := func(name string) bool { _, ok := entries[name]; return ok }
	folder := must(visibility.AbsolutePath(visibility.PathJoin(state, "inputs")))

	// 記録と同じ種類と読者と語の一覧を付ける (付けないと lint のレポートが合わない)。一覧を使わなかった記録は --no-words
	docType, reader := recordContext(record)
	words, _ := recordWords(record).(*pyjson.Object)
	args := append(contextArguments(optionalText(docType), optionalText(reader)), wordsArguments(words)...)
	exe, err := r.rt.Executable()
	if err != nil {
		panic(failure{err})
	}
	var commands []childCommand
	if has("lint") {
		commands = append(commands, lintCommand("lint", exe, args, snapshotName(pyops.Index(inputs, "final"))))
	}
	if has("diff") {
		var materials []string
		for _, item := range pyops.Iter(pyops.Or(pyops.Get(inputs, "materials"), []pyjson.Value{})) {
			materials = append(materials, snapshotName(item))
		}
		// 新規作成で資料を原文とみなした記録は、その資料の写しと比べる (形の検査を通った記録では必ずある)
		source := diffSourceItem(record)
		commands = append(commands, diffCommand(exe, optionalText(pyops.Get(pyops.Index(record, "options"), "stance")), materials, args,
			snapshotName(source), snapshotName(pyops.Index(inputs, "final"))))
	}

	var problems []problem
	type rebuiltMaterials struct {
		result *pyjson.Object
		texts  []string
	}
	var rebuilt *rebuiltMaterials
	group := newChildren(r.rt, r.timeout, folder, r.rt.ChildEnv(r.env))
	r.begin(group)
	results, err := group.run(r.ctx, commands, func() {
		if !has("materials") {
			return
		}
		// 照合の原文は run と同じ (推敲では原文、新規作成では原文とみなした資料)
		result, texts, err := func() (*pyjson.Object, []string, error) {
			var original *string
			if item := diffSourceItem(record); item != nil {
				text, err := replayText(folder, item)
				if err != nil {
					return nil, nil, err
				}
				original = &text
			}
			var texts []string
			var labels []*string
			for _, item := range pyops.Iter(pyops.Index(inputs, "materials")) {
				text, err := replayText(folder, item)
				if err != nil {
					return nil, nil, err
				}
				texts = append(texts, text)
				labels = append(labels, optionalText(pyops.Get(item, "label")))
			}
			final, err := replayText(folder, pyops.Index(inputs, "final"))
			if err != nil {
				return nil, nil, err
			}
			result, err := diff.Ground(final, texts, original, labels)
			return result, texts, err
		}()
		if err != nil {
			problems = append(problems, problem{"replay_error", "materials: " + pyErrorText(err)})
			return
		}
		rebuilt = &rebuiltMaterials{result, texts}
	})
	if err != nil {
		panic(failure{err})
	}
	for _, cmd := range commands {
		res := results[cmd.name]
		if res.status != "error" {
			continue
		}
		code, isInt := res.code.(int64)
		switch {
		case res.code == nil:
			panic(failure{replayError{cmd.name + ": " + pyops.Str(res.err)}})
		case isInt && code < 0:
			panic(failure{replayError{cmd.name + ": " + "シグナル" + pyops.Format(-code) + "で止まりました"}})
		}
	}

	options := pyops.Index(record, "options")
	if pyops.Truthy(pyops.Get(options, "fact_gaps")) {
		// 一覧は通常の verify でも比べる (変われば tool_changed で、ここには来ない)。候補が偶然同じでも、別の一覧による再現を一致とはしない
		key := "references/lists/" + FactGapsList + ".txt"
		if !pyops.Equal(ListSHA256(ToolVersions(files, true), key), ListSHA256(pyops.Index(record, "tools").(*pyjson.Object), key)) {
			problems = append(problems, problem{"replay_mismatch", key})
		}
		gaps, err := func() (*pyjson.Object, error) {
			text, err := replayText(folder, pyops.Index(inputs, "final"))
			if err != nil {
				return nil, err
			}
			return diff.FactGaps(text, optionalText(pyops.Get(options, "doc_type")))
		}()
		switch {
		case err != nil:
			problems = append(problems, problem{"replay_error", "fact_gaps: " + pyErrorMessage(err)})
		case !pyops.Equal(gaps, pyops.Get(record, "fact_gaps")):
			problems = append(problems, problem{"replay_mismatch", "fact_gaps"})
		}
	}

	var timedOut []string
	parsed := map[string]*pyjson.Object{}
	versions := map[string]int{"lint": lint.LintFormatVersion, "diff": diff.DiffFormatVersion}
	for _, name := range []string{"lint", "diff"} {
		res, ok := results[name]
		if !ok {
			continue
		}
		if res.status == "timeout" {
			timedOut = append(timedOut, name)
			continue
		}
		if res.status != "ran" {
			problems = append(problems, problem{"replay_error", name + ": " + pyops.Str(res.err)})
			continue
		}
		if !pyops.Equal(SHA256(res.stdout), pyops.Index(entries[name], "report_sha256")) {
			problems = append(problems, problem{"replay_mismatch", name + ".json"})
		}
		value, why := parseReport(res.stdout, name, versions[name])
		if value == nil {
			problems = append(problems, problem{"replay_error", name + ": " + why})
			continue
		}
		text, err := reportText(name, value)
		if err != nil {
			problems = append(problems, problem{"replay_error", name + ": 人が読む形を作れません（" + pyErrorText(err) + "）"})
			continue
		}
		parsed[name] = value
		if !pyops.Equal(SHA256([]byte(text)), pyops.Index(entries[name], "text_sha256")) {
			problems = append(problems, problem{"replay_mismatch", name + ".txt"})
		}
	}
	if rebuilt != nil {
		data := must(pyjson.Dumps(rebuilt.result, pyjson.Compact()))
		if !pyops.Equal(SHA256([]byte(data)), pyops.Index(entries["materials"], "report_sha256")) {
			problems = append(problems, problem{"replay_mismatch", "materials.json"})
		}
		if _, diffParsed := parsed["diff"]; !has("diff") || diffParsed {
			// 照合の .txt は diff の「元の文にない語」を使うので、diff を作り直せたときだけ比べる
			c := newRunContext()
			c.materials = make([]*Input, len(rebuilt.texts))
			c.diffResult = parsed["diff"]
			c.materialsResult = rebuilt.result
			if !pyops.Equal(SHA256([]byte(materialsText(c)+"\n")), pyops.Index(entries["materials"], "text_sha256")) {
				problems = append(problems, problem{"replay_mismatch", "materials.txt"})
			}
		}
	}
	for _, name := range timedOut {
		problems = append(problems, problem{"replay_timeout", name})
	}
	if slices.ContainsFunc(problems, func(p problem) bool { return p.code == "replay_mismatch" || p.code == "replay_error" }) {
		return problems, "mismatch"
	}
	if len(timedOut) > 0 {
		return problems, "timeout"
	}
	return problems, "ok"
}

// reportText は子の JSON から人が読む形 (.txt の中身。run と verify --replay) を作る (lint は render_report、diff は report。末尾に改行)。本家が例外になる JSON は error。
func reportText(name string, value *pyjson.Object) (text string, err error) {
	defer pyops.Catch(&err)
	if name == "lint" {
		text, err = lint.RenderReport(value)
	} else {
		text, err = diff.Report(value)
	}
	return text + "\n", err
}
