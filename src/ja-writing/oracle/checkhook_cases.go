package oracle

import (
	"encoding/json"
	"strings"
)

// CheckHookFile は本家 yomiyasu_hook.py の main を、時刻・実行 ID・time.monotonic を固定して動かした記録 (testdata/golden/ の中)。形は
// check-manage.json と同じ: main の標準出力・標準エラー・終了コードと、状態フォルダの木 (op snapshot)。hook の中の関数 (文から原稿と
// 投稿の本文を取り出す所) と shlex を直接呼んだ戻り値も持つ。Go のテストは同じ呼び出しを check の HookMainWith と中の関数で動かして比べる。
const CheckHookFile = "check-hook.json"

// HookModule は記録の対象の本家の hook の module。
const HookModule = "yomiyasu_hook"

// CheckHookFunctions は記録の対象の関数 (main は hook の main)。
var CheckHookFunctions = []string{"main", "_shell_segments", "_bodies", "_paths", "_invocation", "_bash_command", "split", "quote", "join"}

// hookMain は本家の hook の main を、data を JSON にした標準入力で呼ぶ (data が nil なら標準入力は raw)。
func hookMain(f *fixtureCase, data any, raw string, args ...string) {
	content := raw
	if data != nil {
		// json.Marshal は < と > を \u003c と \u003e にするので、<<ROOT>> が作業フォルダに置き換わらなくなる
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(data); err != nil {
			panic(err)
		}
		content = strings.TrimSuffix(b.String(), "\n")
	}
	f.op(Op{Kind: "stdin", Content: content, Value: "root"})
	f.call(Call{Module: HookModule, Function: "main", Args: []any{Argv(args)}, Digest: true, Keep: true, Capture: true})
}

// hookEvent はホストのイベントの JSON (cwd は作業フォルダの下の rel)。
func hookEvent(f *fixtureCase, event, session, rel string, extra map[string]any) map[string]any {
	data := map[string]any{"session_id": session, "hook_event_name": event, "cwd": f.dir(rel)}
	if rel == "" {
		data["cwd"] = f.dir()
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// hookSubmit は作業フォルダで UserPromptSubmit の prompt を本家の hook の main に渡す。
func hookSubmit(f *fixtureCase, session, prompt string, args ...string) {
	hookMain(f, hookEvent(f, "UserPromptSubmit", session, "", map[string]any{"prompt": prompt}), "", args...)
}

// hookFn は hook の中の関数を直接呼ぶ (bytes と tuple は objects で JSON にする)。
func hookFn(f *fixtureCase, fn string, args ...any) {
	f.call(Call{Module: HookModule, Function: fn, Args: args, Digest: true, Keep: true, Objects: true})
}

// CheckHookCases は check-hook.json の case。
func CheckHookCases() []VisCase {
	cases := []VisCase{hookArgsCase(), hookStopCase(), hookCheckedCase(), hookPretoolCase(), hookBindCase(), hookWordsCase(), hookCodexCase(), hookBudgetCase(), hookStatesCase(), hookPartsCase()}
	for i := range cases {
		scopeRefs(&cases[i])
	}
	return cases
}

// hookArgsCase は引数と入力の誤り (どれも何も出さずに 0。--print-config の誤りだけ 2) と、登録が無いときの素通り。
func hookArgsCase() VisCase {
	f := newFixtureCase("hook-args")
	f.write("src.md", vSource)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	stop := hookEvent(f, "Stop", "s1", "", nil)
	for _, args := range [][]string{
		{"--version"}, {"-h"}, {"--help", "--host", "codex"}, {"--print-config"}, {"--print-config", "x"}, {"--host"}, {"--host", "x"},
		{"--host", "claude-code", "--nope"}, {"--host", "claude-code", "--state-dir"}, {},
	} {
		hookMain(f, stop, "", args...)
	}
	for _, raw := range []string{"", "nope", "[]", `{"session_id": 5}`, `{"session_id": " \u3000"}`, "\ufeff{}", `{"session_id": "s", "hook_event_name": "Nope", "cwd": "/"}`} {
		hookMain(f, nil, raw, "--host", "claude-code")
	}
	hookMain(f, hookEvent(f, "PostToolUse", "s1", "", nil), "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "UserPromptExpansion", "s1", "", nil), "", "--host", "codex")
	hookMain(f, map[string]any{"session_id": "s1", "hook_event_name": "Stop", "cwd": "rel"}, "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "Stop", "s1", "missing", nil), "", "--host", "claude-code")
	hookMain(f, stop, "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "", map[string]any{"prompt": "src.md を直して"}), "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "", map[string]any{"prompt": "/yomiyasu"}), "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "", map[string]any{"prompt": "/yomiyasux src.md"}), "", "--host", "claude-code")
	snapshot(f)
	return f.done()
}

// hookStopCase は登録 → 原文を書き換えた Stop (検査記録が無いので続けさせる) → 上限で未確認 → 知らせた後は黙る。
func hookStopCase() VisCase {
	f := newFixtureCase("hook-stop")
	f.write("src.md", vSource).write("notes.md", vOther)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	hookSubmit(f, "s1", "  /yomiyasu  missing.md と notes.md と src.md を直して", "--host", "claude-code")
	snapshot(f)
	hookSubmit(f, "s1", "/yomiyasu notes.md", "--host", "claude-code")
	hookSubmit(f, "s1", "/yomiyasu:yomiyasu\tnotes.md", "--host", "claude-code")
	stop := func(active any) {
		extra := map[string]any{}
		if active != nil {
			extra["stop_hook_active"] = active
		}
		hookMain(f, hookEvent(f, "Stop", "s1", "", extra), "", "--host", "claude-code")
	}
	stop(nil)
	hookMain(f, hookEvent(f, "Stop", "other", "", nil), "", "--host", "claude-code")
	f.write("notes.md", vFinal)
	stop(nil)
	snapshot(f)
	stop(true)
	stop("yes")
	stop(false)
	snapshot(f)
	hookMain(f, hookEvent(f, "SessionEnd", "s1", "", nil), "", "--host", "claude-code")
	stop(nil)
	snapshot(f)
	return f.done()
}

// hookCheckedCase は run と verify で検査した最終稿の Stop (checked)・最終稿が消えた・原文が消えた・期限切れ。
func hookCheckedCase() VisCase {
	f := newFixtureCase("hook-checked")
	f.write("src.md", vSource).write("final.md", vFinal).write("src2.md", vSourceB).write("final2.md", vOther)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	stop := func() { hookMain(f, hookEvent(f, "Stop", "s1", "", nil), "", "--host", "claude-code") }
	hookMain(f, hookEvent(f, "UserPromptExpansion", "s1", "", map[string]any{"command_name": "yomiyasu", "command_args": "src.md"}), "", "--host", "claude-code")
	run(f, "--source", "src.md", "--final", "final.md")
	stop()
	snapshot(f)
	f.write("final.md", vFinal2)
	stop()
	stop()
	snapshot(f)
	hookMain(f, hookEvent(f, "UserPromptExpansion", "s1", "", map[string]any{"command_name": "yomiyasu:yomiyasu", "command_args": "src2.md"}), "", "--host", "claude-code")
	run(f, "--source", "src2.md", "--final", "final2.md")
	f.op(Op{Kind: "remove", Path: f.dir("final2.md")})
	stop()
	f.op(Op{Kind: "remove", Path: f.dir("src2.md")})
	clock(f, "2026-01-01T01:00:00Z")
	stop()
	snapshot(f)
	return f.done()
}

// hookPretoolCase は投稿の照合: gh の --body・--body-file・api の -f・release の --notes-file・置換を含む文 (取り出せない)・mcp の本文。
func hookPretoolCase() VisCase {
	f := newFixtureCase("hook-pretool")
	f.write("src.md", vSource).write("final.md", vFinal).write("unrelated.md", vOther)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "", map[string]any{"prompt": "/yomiyasu src.md"}), "", "--host", "claude-code")
	run(f, "--source", "src.md", "--final", "final.md")
	bash := func(command any) {
		hookMain(f, hookEvent(f, "PreToolUse", "s1", "", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": command}}), "", "--host", "claude-code")
	}
	bash("gh pr create --title t --body-file final.md")
	bash("gh pr create --body-file unrelated.md")
	bash("gh pr create --body '" + vSource + "'")
	bash("gh issue comment 1 --body=\"" + vFinal + "\"")
	bash("cd x && gh issue create -b 'x' --body-file src.md; echo done")
	bash("gh api repos/o/r/issues -f title=t -f body=@final.md -F 'body=@src.md'")
	bash("gh api repos/o/r/issues --input src.md")
	bash("gh release create v1 --notes-file final.md")
	bash("gh pr create --body \"$(cat src.md)\"")
	bash("cat <<EOF | gh pr create --body-file -\n" + vSource + "EOF\n")
	bash("/opt/homebrew/bin/gh pr review --body-file final.md")
	bash("git commit -m src.md")
	bash([]any{"bash", "-lc", "gh pr create --body-file src.md"})
	bash([]any{"gh", "pr", "create", "--body-file", "src.md"})
	bash("gh pr create --body 'unclosed")
	mcp := func(input map[string]any) {
		hookMain(f, hookEvent(f, "PreToolUse", "s1", "", map[string]any{"tool_name": "mcp__github__create_issue", "tool_input": input}), "", "--host", "claude-code")
	}
	mcp(map[string]any{"title": "t", "body": vFinal})
	mcp(map[string]any{"body": vSource, "nested": map[string]any{"text": vSource, "deeper": map[string]any{"message": vSource}}})
	mcp(map[string]any{"content": 5, "notes": vOther})
	hookMain(f, hookEvent(f, "PreToolUse", "s1", "", map[string]any{"tool_name": "Edit", "tool_input": map[string]any{"body": vSource}}), "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "PreToolUse", "s1", "", map[string]any{"tool_name": "Bash", "tool_input": "x"}), "", "--host", "claude-code")
	snapshot(f)
	return f.done()
}

// hookBindCase は start で作ったセッションの無い登録の紐付け (同じプロジェクトの中だけ)・別のセッション・終えた登録・壊れた登録。
func hookBindCase() VisCase {
	f := newFixtureCase("hook-bind")
	f.write("src.md", vSource).write("sub/x.md", vOther).mkdir("outside")
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	start(f, "--source", "src.md", "--run-id", "b1")
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "sub", map[string]any{"prompt": "x"}), "", "--host", "claude-code", "--state-dir", "..")
	snapshot(f)
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s2", "", map[string]any{"prompt": "x"}), "", "--host", "claude-code")
	start(f, "--source", "src.md", "--run-id", "b2")
	hookMain(f, hookEvent(f, "SessionEnd", "s1", "", nil), "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "Stop", "s3", "outside", nil), "", "--host", "claude-code", "--state-dir", "../.yomiyasu")
	f.write(".yomiyasu/armed/junk.json", "{}")
	hookMain(f, hookEvent(f, "Stop", "s4", "", nil), "", "--host", "codex")
	snapshot(f)
	return f.done()
}

// hookWordsCase は案内する run のコマンドに付ける語の一覧の引数 (--words・--no-words・両方・無い一覧・読めない一覧)。
// 上限を大きくした登録で、どの変種でも続けさせる (案内の文を作るときだけ一覧を読む)。
func hookWordsCase() VisCase {
	f := newFixtureCase("hook-words")
	f.write("src.md", vSource).write("words/allow.txt", "語\n").write("words/flag.txt", "").write("bad/allow.txt", "").mkdir("bad/flag.txt")
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "", map[string]any{"prompt": "/yomiyasu src.md"}), "", "--host", "claude-code", "--words", "words")
	writeArmed(f, "w1", "src.md", vSource, map[string]any{"host": "claude-code", "session_id": "s1",
		"limits": map[string]any{"max_stop_blocks": 5, "max_runs": 3, "deadline_min": 30}})
	f.write("src.md", vFinal)
	for _, args := range [][]string{{"--no-words"}, {"--words", "words"}, {"--words", "missing"}, {"--words", "bad"}, {"--words", "words", "--no-words"}} {
		hookMain(f, hookEvent(f, "Stop", "s1", "", nil), "", append([]string{"--host", "claude-code"}, args...)...)
	}
	snapshot(f)
	return f.done()
}

// hookCodexCase は codex のホスト ($yomiyasu の語・PostToolUse・取り出せない本文は deny)。
func hookCodexCase() VisCase {
	f := newFixtureCase("hook-codex")
	f.write("src.md", vSource)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	hookSubmit(f, "c1", "$yomiyasux src.md", "--host", "codex")
	hookSubmit(f, "c1", "  src.md を $yomiyasu で", "--host", "codex")
	hookMain(f, hookEvent(f, "PreToolUse", "c1", "", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr create --body \"$(cat src.md)\""}}), "", "--host", "codex")
	hookMain(f, hookEvent(f, "PostToolUse", "c1", "", map[string]any{"tool_name": "Bash"}), "", "--host", "codex")
	hookMain(f, hookEvent(f, "Stop", "c1", "", nil), "", "--host", "codex")
	snapshot(f)
	return f.done()
}

// hookBudgetCase は締め切り: 経過の時計を読むたびに 1 秒進め、_process の remaining が TimeoutError('hook_budget') を投げる
// (hook-errors.log に残す)。登録を処理している途中で締め切りを過ぎたときは、その登録の log に timeout を残す。
func hookBudgetCase() VisCase {
	f := newFixtureCase("hook-budget")
	f.write("src.md", vSource).write("src2.md", vSourceB)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "", map[string]any{"prompt": "/yomiyasu src.md"}), "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "", map[string]any{"prompt": "/yomiyasu src2.md"}), "", "--host", "claude-code")
	f.op(Op{Kind: "monotonic", Value: "step"})
	hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", "", map[string]any{"prompt": "x"}), "", "--host", "claude-code")
	hookMain(f, hookEvent(f, "SessionEnd", "s1", "", nil), "", "--host", "claude-code")
	f.write("src.md", vFinal)
	hookMain(f, hookEvent(f, "Stop", "s1", "", nil), "", "--host", "claude-code")
	f.op(Op{Kind: "monotonic", Value: ""})
	snapshot(f)
	return f.done()
}

// hookStatesCase は状態フォルダの候補: YOMIYASU_STATE_DIR・空白だけと ${ の値・CLAUDE_PROJECT_DIR・.git のある上のフォルダ。
func hookStatesCase() VisCase {
	f := newFixtureCase("hook-states")
	f.write("proj/src.md", vSource).mkdir("proj/.git").mkdir("proj/deep/er").write("other/x.md", vOther)
	chdir(f)
	clock(f, "2026-01-01T00:00:00Z")
	submit := func(rel, args string, opts ...string) {
		hookMain(f, hookEvent(f, "UserPromptSubmit", "s1", rel, map[string]any{"prompt": "/yomiyasu " + args}), "", append([]string{"--host", "claude-code"}, opts...)...)
	}
	submit("proj/deep/er", "../../src.md")
	setenv(f, "YOMIYASU_STATE_DIR", "  ")
	setenv(f, "CLAUDE_PROJECT_DIR", "${x}")
	submit("other", "x.md")
	setenv(f, "YOMIYASU_STATE_DIR", f.dir("envstate"))
	setenv(f, "CLAUDE_PROJECT_DIR", f.dir("proj"))
	hookMain(f, hookEvent(f, "Stop", "s1", "other", nil), "", "--host", "claude-code")
	submit("other", "x.md")
	submit("other", "x.md", "--state-dir", "given")
	f.op(Op{Kind: "restoreenv"})
	snapshot(f)
	return f.done()
}

// hookPartsCase は hook の中の関数と shlex を直接呼ぶ (文の切り方・本文の取り出し・原稿の探し方)。
func hookPartsCase() VisCase {
	f := newFixtureCase("hook-parts")
	f.write("a.md", "本文\n").write("b c.txt", "本文\n").write("bad.md", "\xff").write("dir.md/x", "")
	chdir(f)
	for _, s := range []string{
		"", "a", "a b", "a 'b c' \"d e\"", `a\ b`, `"a\"b"`, `'a\'`, `"a\x"`, "a'b'c", "'' \"\"", "'unclosed", `trail\`,
		"a;b|c&&d", "a #b", "é ß ñ", "a\tb\nc\rd", "gh pr create --body=\"x y\"", "a\u3000b",
	} {
		f.call(Call{Module: "shlex", Function: "split", Args: []any{s}, Digest: true, Keep: true})
	}
	for _, s := range []string{"", "a", "a b", "it's", "é", "a@%+=:,./-_", "~x", "$x"} {
		f.call(Call{Module: "shlex", Function: "quote", Args: []any{s}, Digest: true, Keep: true})
	}
	f.call(Call{Module: "shlex", Function: "join", Args: []any{[]string{"a", "b c", ""}}, Digest: true, Keep: true})
	for _, c := range []string{
		"a; b | c && d || e\nf", "echo 'a;b' \"c|d\" `e;f` $(g; h) \"$(i; j)\" k", "a \\; b", "cat <<EOF\nx; y\nEOF\nz",
		"cat <<-'E'\n\tbody\n\tE\nnext", "cat <<EOF\nnever closed", "a <<EOF b <<END\n1\nEOF\n2\nEND\nc", "x $(a $(b) c); d",
		"a&&b", "a & b", "\"open; x", "cat << \"Q\"\nq\nQ\r\nr",
	} {
		hookFn(f, "_shell_segments", c)
	}
	for _, v := range []any{"x", []any{"bash", "-c", "y"}, []any{"a", "b c"}, []any{"a", 1}, 5, nil} {
		hookFn(f, "_bash_command", v)
	}
	for _, a := range []string{
		"a.md", "@a.md と b c.txt", "'b c.txt' a.md", "「a.md」を直す", "missing.md a.md", "bad.md a.md", "dir.md a.md", "x.mdx", "a.markdown.md",
		"'unclosed a.md", "../a.md", "a.md a.md",
	} {
		hookFn(f, "_paths", a, f.dir())
	}
	for _, d := range []map[string]any{
		{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr create --body x -b=y --body-file a.md --body-file - --body-file"}},
		{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr edit 1 --body-file bad.md; gh release edit v --notes n -n=m --notes-file a.md"}},
		{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh api x -f body=a -f title=b --raw-field text=c -F notes=@a.md -F body=plain --field body= -f novalue"}},
		{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr list; gh repo view; gh pr"}},
		{"tool_name": "Bash", "tool_input": map[string]any{"command": "(gh pr create --body x)&& gh.exe issue create --body y"}},
		{"tool_name": "mcp__x", "tool_input": map[string]any{"body": "a", "z": map[string]any{"text": "b", "y": map[string]any{"message": "c", "w": map[string]any{"notes": "d"}}}}},
		{"tool_name": "Write", "tool_input": map[string]any{}},
		{"tool_name": 5},
	} {
		hookFn(f, "_bodies", d, f.dir())
	}
	for _, c := range []struct {
		data        map[string]any
		host, event string
	}{
		{map[string]any{"prompt": " /yomiyasu a.md"}, "claude-code", "UserPromptSubmit"},
		{map[string]any{"prompt": "/yomiyasu\u3000a.md"}, "claude-code", "UserPromptSubmit"},
		{map[string]any{"prompt": "/yomiyasu:yomiyasu\na"}, "claude-code", "UserPromptSubmit"},
		{map[string]any{"prompt": "x $yomiyasu:yomiyasu y"}, "codex", "UserPromptSubmit"},
		{map[string]any{"prompt": 5}, "codex", "UserPromptSubmit"},
		{map[string]any{"command_name": "yomiyasu", "command_args": 5}, "claude-code", "UserPromptExpansion"},
		{map[string]any{"command_name": "other", "command_args": "a"}, "claude-code", "UserPromptExpansion"},
		{map[string]any{"command_name": "yomiyasu", "command_args": "a"}, "codex", "UserPromptExpansion"},
	} {
		hookFn(f, "_invocation", c.data, c.host, c.event)
	}
	return f.done()
}
