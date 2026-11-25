package check

// 本家 yomiyasu_hook.py の _process・_stop・_pretool・_related と、案内の文 (_notice・_reason・語の一覧の引数)。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/internal/pyshlex"
	"ja-writing/visibility"
)

// remaining は _process の remaining (締め切りを過ぎたら TimeoutError('hook_budget'))。
func (h *hookRun) remaining() {
	h.cancelled()
	if h.rt.Monotonic() >= h.budget {
		panic(failure{&hookFault{err: timeoutException("hook_budget"), loc: hookLocBudget}})
	}
}

// cancelled は締め切りで処理が取り消されていれば TimeoutError('hook_budget') を投げる (本家の SIGALRM がどこでも投げるのを、
// ロックを持つ間の繰り返しで写す。経過の時計は読まない: 記録の比べは時計を読む回数に頼る)。
func (h *hookRun) cancelled() {
	if h.ctx != nil && h.ctx.Err() != nil {
		panic(failure{&hookFault{err: timeoutException("hook_budget"), loc: hookLocDeadline}})
	}
}

// hookInLock はロックを持った処理の繰り返しの頭で呼ぶ差し込み口 (テストが、ロックを持ったまま締め切りを迎える場面を作る)。
var hookInLock = func(context.Context) {}

// words は _process の words (案内する run のコマンドに付ける語の一覧の引数。初めて要るときに 1 回だけ決める)。
func (h *hookRun) words() string {
	h.wordsOnce.Do(func() { h.wordsArgs = h.wordsArguments() })
	return h.wordsArgs
}

// wordsArguments は _words_arguments。
func (h *hookRun) wordsArguments() string {
	if h.opts.hasWords && h.opts.noWords {
		h.errorLog(h.states, h.event, &hookFault{err: &pyops.Exception{Type: "WordsOptionError", Msg: "words_and_no_words"}, loc: hookLocNone})
		return ""
	}
	if h.opts.noWords {
		return " --no-words"
	}
	if !h.opts.hasWords {
		return ""
	}
	cwd := h.cwd
	if _, _, err := visibility.ResolveWords(&cwd, h.opts.words, false, h.env.Lookup); err != nil {
		var v *visibility.Error
		if !errors.As(err, &v) || v.Type != visibility.TypeWordsError {
			panic(failure{err})
		}
		h.errorLog(h.states, h.event, err)
		return ""
	}
	folder := hookAbsolute(visibility.PathJoin(h.cwd, HomePath(*h.opts.words, h.env.Lookup)))
	return " --words " + pyshlex.Quote(folder)
}

// wordsNote は _words_note。
func wordsNote(words string) string {
	if words == "" {
		return ""
	}
	return "yomiyasu_check.py run には" + words + " を付けてください。"
}

// process は _process (登録・紐付け・イベントごとの処理。標準出力に書く文を返す)。
func (h *hookRun) process(invocation *string) (string, error) {
	session := pyops.Str(h.data.Field("session_id"))
	var registrations []string
	combined := pyjson.NewObject()
	if invocation != nil {
		for _, path := range h.paths(*invocation) {
			h.remaining()
			placed := hookPlace(path)
			exists := false
			for _, state := range h.states {
				for _, e := range must(ArmedEntries(state)) {
					if h.mine(e, session) && placed != nil {
						if p := hookPlace(pyops.Str(pyops.Index(pyops.Index(pyops.Index(e, "targets"), "source"), "path"))); p != nil && *p == *placed {
							exists = true
						}
					}
				}
			}
			if exists {
				continue
			}
			host := h.host
			entry := must(StartRun(h.ctx, h.rt, h.states[0], path, StartOptions{
				Session: &session, Host: &host, By: "hook:" + h.host + ":" + h.event, Cwd: h.cwd, DefaultPlace: !h.stateGiven(),
			}))
			registrations = append(registrations, fmt.Sprintf("yomiyasu の管理: 原稿 %s を登録しました（実行ID %s。上限: 続けさせる2回、run 3回、30分）。"+
				"この原稿を yomiyasu_check.py run の --source に渡すと、記録がこの登録につながります。"+
				"返答を終えるときに、最終稿が検査記録と合うかをフックが確かめます。", hookShown(path, h.cwd), pyops.Format(pyops.Index(entry, "run_id")))+wordsNote(h.words()))
		}
	}
	var bodies []hookBody
	if h.event == "PreToolUse" {
		bodies = hookBodies(h.data, h.cwd)
	}
	for _, state := range h.states {
		h.remaining()
		entries := must(ArmedEntries(state))
		folder := visibility.PathJoin(state, "armed")
		if hookIsDir(folder) {
			valid := map[string]bool{}
			for _, e := range entries {
				valid[pyops.Str(pyops.Index(e, "run_id"))+".json"] = true
			}
			for _, name := range must(listDir(folder)) {
				if strings.HasSuffix(name, ".json") && !valid[name] {
					h.errorLog([]string{state}, h.event, &hookFault{err: valueError("armed_record"), loc: hookLocNone})
					break
				}
			}
		}
		for _, entry := range entries {
			h.remaining()
			if pyops.Equal(pyops.Index(entry, "status"), "ended") {
				continue
			}
			if h.event != "SessionEnd" && h.bindable(entry, session) {
				h.setProcessing(state, pyops.Str(pyops.Index(entry, "run_id")))
				entry = must(ArmedUpdate(h.ctx, h.rt, state, pyops.Str(pyops.Index(entry, "run_id")), func(current *pyjson.Object) (bool, error) {
					if !h.bindable(current, session) {
						return false, nil
					}
					current.Set("session_id", session)
					current.Set("host", h.host)
					current.Set("bound_at", ISO(h.rt.Now()))
					h.log(current, "bound", nil)
					return true, nil
				}, &h.budget))
				h.clearProcessing()
			}
			if !h.mine(entry, session) {
				continue
			}
			runID := pyops.Str(pyops.Index(entry, "run_id"))
			h.setProcessing(state, runID)
			switch {
			case h.event == "SessionEnd":
				must(ArmedUpdate(h.ctx, h.rt, state, runID, func(current *pyjson.Object) (bool, error) {
					if !h.mine(current, session) {
						return false, nil
					}
					current.Set("status", "ended")
					current.Set("status_reason", "session_end")
					h.log(current, "ended:session_end", nil)
					return true, nil
				}, &h.budget))
			case h.event == "Stop":
				response := h.stop(state, entry, session)
				for _, key := range response.Keys() {
					value := response.Field(key)
					if (key == "reason" || key == "systemMessage") && hasKeys(combined, []string{key}) {
						combined.Set(key, pyops.Add(pyops.Add(combined.Field(key), "\n"), value))
					} else {
						combined.Set(key, value)
					}
				}
			case h.event == "PreToolUse" && len(bodies) > 0:
				response := h.pretool(state, entry, bodies, session)
				if response.Len() > 0 {
					old, _ := combined.Field("hookSpecificOutput").(*pyjson.Object)
					next := response.Field("hookSpecificOutput").(*pyjson.Object)
					if old != nil {
						old.Set("permissionDecisionReason", pyops.Add(pyops.Add(old.Field("permissionDecisionReason"), "\n"), next.Field("permissionDecisionReason")))
						if pyops.Equal(next.Field("permissionDecision"), "deny") {
							old.Set("permissionDecision", "deny")
						}
					} else {
						combined = response
					}
				}
			}
			h.clearProcessing()
		}
	}
	h.remaining()
	if len(registrations) > 0 {
		return strings.Join(registrations, "\n"), nil
	}
	if combined.Len() == 0 {
		return "", nil
	}
	return pyjson.Dumps(combined, pyjson.Plain())
}

// listDir は Path(folder).iterdir() の名前 (順は問わない。any で見るだけ)。
func listDir(folder string) ([]string, error) {
	entries, err := pyos.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

// bindable は _process の紐付けの条件 (まだセッションかホストの決まっていない登録を、このセッションに紐付けてよいか)。
func (h *hookRun) bindable(entry *pyjson.Object, session string) bool {
	sid := pyops.Index(entry, "session_id")
	host := pyops.Index(entry, "host")
	if (sid != nil && !pyops.Equal(sid, session)) || pyops.Equal(pyops.Index(entry, "status"), "ended") || (host != nil && !pyops.Equal(host, h.host)) {
		return false
	}
	if sid != nil && host != nil {
		return false
	}
	if h.expired(entry) {
		return false
	}
	return sid != nil || h.sameProject(entry)
}

// mine は _mine。
func (h *hookRun) mine(entry *pyjson.Object, session string) bool {
	return !pyops.Equal(pyops.Index(entry, "status"), "ended") && pyops.Equal(pyops.Index(entry, "host"), h.host) &&
		pyops.Equal(pyops.Index(entry, "session_id"), session)
}

// expired は _expired (entry["deadline"] < _iso())。
func (h *hookRun) expired(entry *pyjson.Object) bool {
	return pyops.Less(pyops.Index(entry, "deadline"), ISO(h.rt.Now()))
}

// sameProject は _same_project (紐付けていない登録は、登録したプロジェクトの中からだけ紐付ける)。
func (h *hookRun) sameProject(entry *pyjson.Object) bool {
	recorded, ok := pyops.Get(entry, "cwd").(string)
	if !ok || !strings.HasPrefix(recorded, "/") {
		return false
	}
	root := hookPlace(hookProjectRootOr(recorded, recorded))
	here := hookPlace(h.cwd)
	if root == nil || here == nil {
		return false
	}
	return *here == *root || strings.HasPrefix(*here, strings.TrimRight(*root, "/")+"/")
}

// hookShown は _shown (cwd の下なら相対パス、外なら元のパス)。
func hookShown(path, cwd string) string {
	relative, err := relpath(path, cwd)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, "../") {
		return relative
	}
	return path
}

// relpath は os.path.relpath(path, start) (path が空なら ValueError)。
func relpath(path, start string) (string, error) {
	if path == "" {
		return "", valueError("no path specified")
	}
	if start == "" {
		start = "."
	}
	split := func(p string) []string {
		var parts []string
		for _, part := range strings.Split(hookAbsolute(p), "/") {
			if part != "" {
				parts = append(parts, part)
			}
		}
		return parts
	}
	startList, pathList := split(start), split(path)
	i := 0
	for i < len(startList) && i < len(pathList) && startList[i] == pathList[i] {
		i++
	}
	var rel []string
	for range len(startList) - i {
		rel = append(rel, "..")
	}
	rel = append(rel, pathList[i:]...)
	if len(rel) == 0 {
		return ".", nil
	}
	return strings.Join(rel, "/"), nil
}

// limitLabel は hook の _limit_label (check の limitLabel と違い、決まった理由でなければ「続けさせる回数の上限」)。
func hookLimitLabel(entry *pyjson.Object) string {
	reason := pyops.Index(entry, "status_reason")
	pyops.Hashable(reason)
	limits := pyops.Index(entry, "limits")
	switch {
	case pyops.Equal(reason, "deadline"):
		return "期限" + pyops.Format(pyops.Index(limits, "deadline_min")) + "分"
	case pyops.Equal(reason, "run_limit"):
		return "run " + pyops.Format(pyops.Index(limits, "max_runs")) + "回まで"
	}
	return "続けさせる回数の上限"
}

// notice は _notice。
func (h *hookRun) notice(entry *pyjson.Object) string {
	return "yomiyasu: 原稿 " + hookShown(sourcePath(entry), h.cwd) + "（実行ID " + pyops.Format(pyops.Index(entry, "run_id")) + "）は未確認で終わりました（" +
		hookLimitLabel(entry) + "）。最終稿は検査記録と照合できていません。投稿する前に、yomiyasu_check.py verify で確かめてください。"
}

func sourcePath(entry *pyjson.Object) string {
	return pyops.Str(pyops.Index(pyops.Index(pyops.Index(entry, "targets"), "source"), "path"))
}

// reason は _reason (続けさせるときにエージェントへ渡す文)。
func (h *hookRun) reason(entry *pyjson.Object, final string, v *pyjson.Object) string {
	source := pyshlex.Quote(hookShown(sourcePath(entry), h.cwd))
	final = pyshlex.Quote(hookShown(final, h.cwd))
	prefix := "yomiyasu（実行ID " + pyops.Format(pyops.Index(entry, "run_id")) + "）: "
	tail := "実行できない場合は、完了報告の「実行しなかった検査」に書いてください。"
	var message string
	switch status := v.Field("status"); {
	case pyops.Equal(status, "no_record"):
		message = fmt.Sprintf("登録した原稿 %s の最終稿 %s に、検査記録がありません。yomiyasu_check.py run --source %s --final %s%s "+
			"を実行し、そのあと verify を実行してください。", source, final, source, final, h.words()) + tail
	case pyops.Equal(status, "final_changed"):
		message = "最終稿 " + final + " が、最後に検査した本文と違います（検査のあとに書き換えがありました）。run をもう一度実行し、" +
			"そのあと verify を実行してください。" + wordsNote(h.words()) + tail
	case pyops.Equal(status, "incomplete"):
		var names []string
		for _, name := range []string{"lint", "diff", "materials"} {
			for _, p := range pyops.Iter(v.Field("problems")) {
				code := pyops.Index(p, "code")
				if (pyops.Equal(code, "missing_check") || pyops.Equal(code, "check_target")) && strings.HasPrefix(pyops.Str(pyops.Index(p, "detail")), name) {
					names = append(names, name)
					break
				}
			}
		}
		message = "検査記録に " + strings.Join(names, "、") + " の結果がありません。run をもう一度実行してください。" + wordsNote(h.words()) + tail
	case pyops.Equal(status, "tool_changed"):
		message = "記録のあとで検査スクリプトが変わりました。run をもう一度実行してください。" + wordsNote(h.words()) + tail
	default:
		message = "最終稿 " + final + ": " + pyops.Format(v.Field("message"))
	}
	limits, counters := pyops.Index(entry, "limits"), pyops.Index(entry, "counters")
	left := pyops.Sub(pyops.Index(limits, "max_stop_blocks"), pyops.Index(counters, "stop_blocks"))
	if pyops.Less(left, int64(0)) {
		left = int64(0)
	}
	return prefix + message + "\n（yomiyasu のフックが続けさせるのは、あと " + pyops.Format(left) + " 回までです。）"
}

// finalOf は current['targets']['final'] or check.latest_final_path(state, run_id) (どちらも無ければ nil)。
func finalOf(state string, entry *pyjson.Object) *string {
	if final := pyops.Index(pyops.Index(entry, "targets"), "final"); pyops.Truthy(final) {
		s := pyops.Str(final)
		return &s
	}
	return must(LatestFinalPath(state, pyops.Str(pyops.Index(entry, "run_id"))))
}

// stop は _stop (返答の終わりに、最終稿が検査記録と合うかを確かめる)。
func (h *hookRun) stop(state string, entry *pyjson.Object, session string) *pyjson.Object {
	response := pyjson.NewObject()
	active := pyops.GetOr(h.data, "stop_hook_active", false)
	must(ArmedUpdate(h.ctx, h.rt, state, pyops.Str(pyops.Index(entry, "run_id")), func(current *pyjson.Object) (bool, error) {
		if !h.mine(current, session) {
			return false, nil
		}
		if pyops.Equal(pyops.Index(current, "status"), "unverified") {
			if pyops.Truthy(pyops.Index(current, "notified")) {
				return false, nil
			}
			response.Set("systemMessage", h.notice(current))
			current.Set("notified", true)
			h.log(current, pyops.Str(pyops.Add("unverified:", pyops.Index(current, "status_reason"))), active)
			return true, nil
		}
		final := finalOf(state, current)
		source := pyops.Index(pyops.Index(current, "targets"), "source")
		if final == nil {
			path := pyops.Str(pyops.Index(source, "path"))
			if !hookIsFile(path) {
				h.log(current, "source_missing", nil)
				return true, nil
			}
			data := must(pyos.ReadFile(path))
			sum := sha256.Sum256(data)
			if pyops.Equal(hex.EncodeToString(sum[:]), pyops.Index(source, "sha256")) {
				return false, nil
			}
			final = &path
		}
		if !hookIsFile(*final) {
			h.log(current, "final_missing", nil)
			return true, nil
		}
		v := must(VerifyQuick(h.quietEnv(), "yomiyasu_hook.py", state, final, nil, strPtr(pyops.Str(pyops.Index(current, "run_id"))), strPtr(pyops.Str(pyops.Index(current, "created_at")))))
		if pyops.Equal(v.Field("status"), "executed") {
			current.Set("status", "checked")
			current.Set("status_reason", nil)
			verified := pyjson.NewObject()
			for _, key := range []string{"attempt", "final_sha256", "text_sha256"} {
				verified.Set(key, pyops.Index(v, key))
			}
			verified.Set("at", ISO(h.rt.Now()))
			current.Set("verified", verified)
			h.log(current, "checked", active)
			return true, nil
		}
		current.Set("status", "active")
		reason := ""
		if h.expired(current) {
			reason = "deadline"
		} else if pyops.GreaterEq(pyops.Index(pyops.Index(current, "counters"), "stop_blocks"), pyops.Index(pyops.Index(current, "limits"), "max_stop_blocks")) {
			reason = "stop_limit"
		}
		if reason != "" {
			current.Set("status", "unverified")
			current.Set("status_reason", reason)
			if !pyops.Truthy(pyops.Index(current, "notified")) {
				response.Set("systemMessage", h.notice(current))
				current.Set("notified", true)
			}
			h.log(current, "unverified:"+reason, active)
			return true, nil
		}
		counters := pyops.Index(current, "counters").(*pyjson.Object)
		counters.Set("stop_blocks", pyops.Add(counters.Field("stop_blocks"), int64(1)))
		response.Set("decision", "block")
		response.Set("reason", h.reason(current, *final, v))
		h.log(current, pyops.Str(pyops.Add("block:", v.Field("status"))), active)
		return true, nil
	}, &h.budget))
	return response
}

func strPtr(s string) *string { return &s }

// quietEnv は check の口に渡す Env (本家は check の標準出力と標準エラーを捨てる _SilentStream で包む)。
func (h *hookRun) quietEnv() cmdenv.Env {
	env := h.env
	env.Stdout, env.Stderr = nil, nil
	return env
}

// related は _related (投稿の本文・ファイル・コマンドが、登録した原稿に関係するか)。
func (h *hookRun) related(state string, entry *pyjson.Object, b hookBody) bool {
	runID := pyops.Str(pyops.Index(entry, "run_id"))
	known, paths := mustKnown(RunKnownTexts(state, runID))
	source := pyops.Index(pyops.Index(entry, "targets"), "source")
	paths[pyops.Str(pyops.Index(source, "path"))] = true
	known[pyops.Str(pyops.Index(source, "text_sha256"))] = true
	if final := finalOf(state, entry); final != nil {
		paths[*final] = true
	}
	var placed *string
	if b.path != nil {
		placed = hookPlace(*b.path)
	}
	if placed != nil {
		exports := hookPlace(visibility.PathJoin(state, "exports"))
		for p := range paths {
			if q := hookPlace(p); q != nil && *q == *placed {
				return true
			}
		}
		if exports != nil && strings.HasPrefix(*placed, *exports+"/") {
			return true
		}
	}
	if b.body == nil {
		for p := range paths {
			rel := must(relpath(p, h.cwd))
			for _, value := range []string{p, rel, visibility.PathName(p)} {
				if value != "" && strings.Contains(b.command, value) {
					return true
				}
			}
		}
		return false
	}
	for p := range paths {
		data, err := hookReadText(p)
		if err != nil {
			continue
		}
		if digest, err := TextSHA256Of(data); err == nil {
			known[digest] = true
		}
	}
	digest, err := TextSHA256Of(b.body)
	return err == nil && known[digest]
}

func mustKnown(texts, paths map[string]bool, err error) (map[string]bool, map[string]bool) {
	if err != nil {
		panic(failure{err})
	}
	return texts, paths
}

// pretool は _pretool (登録した原稿に関係する投稿の本文を、検査記録と照らす)。
func (h *hookRun) pretool(state string, entry *pyjson.Object, bodies []hookBody, session string) *pyjson.Object {
	response := pyjson.NewObject()
	must(ArmedUpdate(h.ctx, h.rt, state, pyops.Str(pyops.Index(entry, "run_id")), func(current *pyjson.Object) (bool, error) {
		if !h.mine(current, session) {
			return false, nil
		}
		runID := pyops.Str(pyops.Index(current, "run_id"))
		since := strPtr(pyops.Str(pyops.Index(current, "created_at")))
		for _, b := range bodies {
			hookInLock(h.ctx)
			h.cancelled()
			if !h.related(state, current, b) {
				continue
			}
			var status, decision, reason string
			if b.body == nil {
				status = "unextractable"
				decision = "deny"
				if h.host == "claude-code" {
					decision = "ask"
				}
				reason = "yomiyasu（実行ID " + runID + "）: 投稿の本文を取り出せないため、登録した原稿 " + hookShown(sourcePath(current), h.cwd) +
					" の検査記録と照らせません。export の写しを --body-file に渡してください。"
			} else {
				v := must(VerifyQuick(h.quietEnv(), "yomiyasu_hook.py", state, nil, b.body, &runID, since))
				if pyops.Equal(v.Field("status"), "executed") {
					continue
				}
				var placed *string
				if b.path != nil {
					placed = hookPlace(*b.path)
				}
				if placed != nil {
					finals := []pyjson.Value{pyops.Index(pyops.Index(current, "targets"), "final"), pyjson.OptionalString(must(LatestFinalPath(state, runID)))}
					matched := false
					for _, final := range finals {
						if pyops.Truthy(final) {
							if q := hookPlace(pyops.Str(final)); q != nil && *q == *placed {
								matched = true
								break
							}
						}
					}
					if matched {
						v = must(VerifyQuick(h.quietEnv(), "yomiyasu_hook.py", state, b.path, b.body, &runID, since))
						if pyops.Equal(v.Field("status"), "executed") {
							continue
						}
					}
				}
				status, decision = pyops.Str(v.Field("status")), "deny"
				final := sourcePath(current)
				if f := finalOf(state, current); f != nil {
					final = *f
				}
				reason = "yomiyasu（実行ID " + runID + "）: 投稿しようとしている本文は、登録した原稿 " + hookShown(sourcePath(current), h.cwd) +
					" の検査記録と照合できていません（" + status + "）。yomiyasu_check.py run と verify を実行し、yomiyasu_check.py export --final " +
					hookShown(final, h.cwd) + " で書き出した写しを --body-file に渡してください。照合できないときは、投稿せずに未確認として利用者に伝えてください。"
				reason += wordsNote(h.words())
			}
			out := pyjson.NewObject()
			out.Set("hookEventName", "PreToolUse")
			out.Set("permissionDecision", decision)
			out.Set("permissionDecisionReason", reason)
			response.Set("hookSpecificOutput", out)
			h.log(current, decision+":"+status, nil, "PreToolUse")
			return true, nil
		}
		return false, nil
	}, &h.budget))
	return response
}
