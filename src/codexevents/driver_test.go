package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// codex-run / codex-fanout の -J / -S を、偽の codex で通す (モデルもネットワークも使わない。tests/test_codex_events.py の DriverTests を移した)。
// 偽の codex は引数を calls.txt に 1 行ずつ追記し (run ごとに "--" の行で区切る)、プロンプトの CASE_* に応じて JSONL を出す。
const fakeCodex = `#!/bin/bash
out=""; json=0; prompt=""
prev=""
for a in "$@"; do
  [ "$prev" = "-o" ] && out="$a"
  [ "$a" = "--json" ] && json=1
  case "$a" in CASE_*) prompt="$a" ;; esac
  prev="$a"
done
{ printf '%s\n' "$@"; echo --; } >> calls.txt
if [ "$json" = 0 ]; then printf 'legacy body' > "$out"; echo 'legacy stdout'; exit 0; fi
echo '{"type":"thread.started","thread_id":"fixture-id"}'
[ "$prompt" = CASE_BAD_JSON ] && echo 'not JSON'
[ "$prompt" != CASE_EMPTY ] && echo '{"type":"item.completed","item":{"type":"agent_message","text":"event body"}}'
if [ "$prompt" != CASE_NO_OUTPUT_FILE ]; then
  if [ "$prompt" = CASE_EMPTY ]; then : > "$out"; else printf 'final body' > "$out"; fi
fi
[ "$prompt" != CASE_PARTIAL ] && echo '{"type":"turn.completed","usage":{"input_tokens":7}}'
# 利用上限: 上限の行が events にだけ出る形 (stderr には何も出さない)
if [ "$prompt" = CASE_LIMIT ]; then echo '{"type":"error","message":"You\u2019ve hit your usage limit. Upgrade to Pro or try again at Oct 10th, 2026 7:29 AM."}'; exit 1; fi
# 応答の本文が上限の行を引用しているだけで、失敗は上限ではない (codex-events が本文を log の末尾へ追記する)
if [ "$prompt" = CASE_QUOTED_BODY ]; then printf '実ログ:\nERROR: You’ve hit your usage limit. try again at Oct 10th\n' > "$out"; echo '{"type":"item.completed","item":{"type":"agent_message","text":"実ログ:\nERROR: You\u2019ve hit your usage limit. try again at Oct 10th"}}'; echo '{"type":"turn.failed","error":{"message":"failure"}}'; exit 1; fi
# 本文が上限の行と区切り行まで引用し、本当の原因は stderr の別の ERROR
if [ "$prompt" = CASE_QUOTED_MARK ]; then echo 'ERROR: stream disconnected before completion' >&2; printf 'repro:\nERROR: You’ve hit your usage limit. try again at Oct 10th\n--- Codex JSONL result ---\nend\n' > "$out"; echo '{"type":"turn.failed","error":{"message":"failure"}}'; exit 1; fi
# stderr が上限の行の引用 (プロンプトの echo) と空行で終わり、何も言わずに死ぬ
if [ "$prompt" = CASE_SILENT_DEATH ]; then printf 'user\nこれを直して:\nERROR: You’ve hit your usage limit. try again at Oct 10th\n\n' >&2; exit 137; fi
# 上限の行が stderr にだけ出る (events には出ない)
if [ "$prompt" = CASE_LIMIT_STDERR ]; then echo "ERROR: You've hit your usage limit. try again at Oct 10th" >&2; exit 1; fi
# 上限の文字列はコマンドの出力にあるだけで、失敗は上限ではない
if [ "$prompt" = CASE_QUOTED_FAIL ]; then echo '{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"ERROR: You\u2019ve hit your usage limit."}}'; echo '{"type":"turn.failed","error":{"message":"failure"}}'; exit 9; fi
if [ "$prompt" = CASE_FAIL ]; then echo '{"type":"turn.failed","error":{"message":"failure"}}'; exit 9; fi
exit 0
`

type driver struct {
	t    *testing.T
	root string // 写した bin/ を置く一時ディレクトリ
	env  []string
}

func newDriver(t *testing.T) *driver {
	t.Helper()
	repo, err := filepath.Abs("../..")
	must(t, err)
	exe, err := filepath.Abs("codex-events.test-bin")
	must(t, err)
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("TestMain がバイナリを作っていない: %v", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	must(t, os.MkdirAll(filepath.Join(root, "bin", "lib"), 0o755))
	for _, name := range []string{"codex-run", "codex-fanout"} {
		b, err := os.ReadFile(filepath.Join(repo, "bin", name))
		must(t, err)
		must(t, os.WriteFile(filepath.Join(root, "bin", name), b, 0o755))
	}
	// 引数の検査の helper は本物を写す (差し替えると検査の挙動が本物と離れる。issue 775)
	b, err := os.ReadFile(filepath.Join(repo, "bin", "lib", "uint.sh"))
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, "bin", "lib", "uint.sh"), b, 0o644))
	// 解決は差し替える (ビルドの経路は bin/lib/go_tool.sh の側の検査)。時間の上限は別の suite が見るので、引数を渡すだけにする
	must(t, os.WriteFile(filepath.Join(root, "bin", "lib", "runtimeout.sh"), []byte(`runtimeout_resolve() { RUNTIMEOUT="$1/bin/runtimeout"; }`+"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "bin", "lib", "codex_events.sh"), []byte(`codex_events_resolve() { CODEX_EVENTS="`+exe+`"; export CODEX_EVENTS; }`+"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "bin", "runtimeout"), []byte("#!/bin/sh\nshift\nexec \"$@\"\n"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "bin", "codex"), []byte(fakeCodex), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "schema.json"), []byte(`{"type":"object"}`), 0o644))
	return &driver{t: t, root: root, env: append(os.Environ(), "PATH="+filepath.Join(root, "bin")+":"+os.Getenv("PATH"))}
}

func (d *driver) path(rel string) string { return filepath.Join(d.root, rel) }

func (d *driver) read(rel string) string {
	d.t.Helper()
	b, err := os.ReadFile(d.path(rel))
	if err != nil {
		d.t.Fatalf("%s が無い: %v", rel, err)
	}
	return string(b)
}

func (d *driver) exec(name string, args ...string) (int, string) {
	d.t.Helper()
	cmd := exec.Command(d.path("bin/"+name), args...)
	cmd.Dir, cmd.Env = d.root, d.env
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	} else if err != nil {
		d.t.Fatal(err)
	}
	return 0, string(out)
}

// invoke は codex-run を 1 本走らせる (出力先は out/)
func (d *driver) invoke(caseName, mode string, options ...string) (int, string) {
	d.t.Helper()
	must(d.t, os.WriteFile(d.path("prompt.md"), []byte(caseName), 0o644))
	must(d.t, os.RemoveAll(d.path("out")))
	return d.exec("codex-run", append(options, "-o", d.path("out"), mode, d.path("prompt.md"))...)
}

func TestMain(m *testing.M) {
	// driver のテストが codex-fanout に渡すバイナリ (go test の一時ディレクトリは test の外から見えないので、ここに作って消す)
	if out, err := exec.Command("go", "build", "-o", "codex-events.test-bin", ".").CombinedOutput(); err != nil {
		_, _ = os.Stderr.Write(out)
		os.Exit(1)
	}
	rc := m.Run()
	_ = os.Remove("codex-events.test-bin")
	os.Exit(rc)
}

func TestDriverLegacyInvocationStaysCompatible(t *testing.T) {
	d := newDriver(t)
	if rc, out := d.invoke("CASE_OK", "ro"); rc != 0 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	if got := d.read("out/ro.out.md"); got != "legacy body" {
		t.Errorf("out.md = %q", got)
	}
	if _, err := os.Stat(d.path("out/ro.events.jsonl")); err == nil {
		t.Error("-J なしで events.jsonl を作った")
	}
}

func TestDriverJSONRecordsMetadataAndReadableLog(t *testing.T) {
	d := newDriver(t)
	if rc, out := d.invoke("CASE_OK", "ro", "-J"); rc != 0 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	if meta := d.read("out/ro.meta.json"); !strings.Contains(meta, `"complete": true`) || !strings.Contains(meta, `"thread_id": "fixture-id"`) {
		t.Errorf("meta.json = %s", meta)
	}
	if !strings.Contains(d.read("out/ro.log"), "final body") {
		t.Error("log に本文が無い")
	}
}

func TestDriverPartialAndInvalidJSONRejectCLISuccess(t *testing.T) {
	for _, c := range []string{"CASE_PARTIAL", "CASE_BAD_JSON", "CASE_EMPTY"} {
		d := newDriver(t)
		if rc, out := d.invoke(c, "ro", "-J"); rc != 1 {
			t.Errorf("%s: rc=%d %s", c, rc, out)
		}
		if got := strings.TrimSpace(d.read("out/ro.cli.rc")); got != "0" {
			t.Errorf("%s: cli.rc = %s", c, got)
		}
		if got := strings.TrimSpace(d.read("out/ro.rc")); got != "65" {
			t.Errorf("%s: rc = %s (未完了の rc=0 は 65 にする)", c, got)
		}
	}
}

func TestDriverCLIFailureKeepsOriginalExitCode(t *testing.T) {
	d := newDriver(t)
	if rc, _ := d.invoke("CASE_FAIL", "ro", "-J"); rc != 1 {
		t.Errorf("rc=%d", rc)
	}
	if got := strings.TrimSpace(d.read("out/ro.rc")); got != "9" {
		t.Errorf("rc = %s (CLI の rc を保つ)", got)
	}
}

func TestDriverReviewEventFallbackKeepsBodyInLog(t *testing.T) {
	d := newDriver(t)
	if rc, out := d.invoke("CASE_NO_OUTPUT_FILE", "review", "-J"); rc != 0 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	if !strings.Contains(d.read("out/review.log"), "event body") {
		t.Error("log にイベントの本文が無い")
	}
}

func TestDriverSchemaAbsolutePathSurvivesWorkingDirectoryChange(t *testing.T) {
	d := newDriver(t)
	must(t, os.Mkdir(d.path("worktree"), 0o755))
	if rc, out := d.invoke("CASE_OK", "ro", "-J", "-S", "schema.json", "-C", d.path("worktree")); rc != 0 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	args := strings.Split(d.read("worktree/calls.txt"), "\n")
	for i, a := range args {
		if a == "--output-schema" {
			if args[i+1] != d.path("schema.json") {
				t.Errorf("--output-schema %s (want 絶対パス %s)", args[i+1], d.path("schema.json"))
			}
			return
		}
	}
	t.Errorf("--output-schema が渡っていない: %q", args)
}

func TestDriverSchemaChecks(t *testing.T) {
	d := newDriver(t)
	// review では -S を使えない。codex を起こす前に止まる
	if rc, _ := d.invoke("CASE_OK", "review", "-S", d.path("schema.json")); rc != 1 {
		t.Errorf("review の -S で rc=%d", rc)
	}
	// オブジェクトでない schema は codex を起こす前に止まる (python3 の事前確認を codex-events is-object に置き換えた)
	must(t, os.WriteFile(d.path("bad.json"), []byte(`[1]`), 0o644))
	if rc, out := d.invoke("CASE_OK", "ro", "-S", d.path("bad.json")); rc != 1 || !strings.Contains(out, "schema が JSON object でない") {
		t.Errorf("オブジェクトでない schema で rc=%d %s", rc, out)
	}
	if _, err := os.Stat(d.path("calls.txt")); err == nil {
		t.Error("止まるべき呼び出しで codex を起こした")
	}
}

func TestDriverPartialFanoutExcludesIncompleteRunFromMerger(t *testing.T) {
	d := newDriver(t)
	must(t, os.WriteFile(d.path("ok.md"), []byte("CASE_OK"), 0o644))
	must(t, os.WriteFile(d.path("partial.md"), []byte("CASE_PARTIAL"), 0o644))
	must(t, os.WriteFile(d.path("merger.md"), []byte("merge"), 0o644))
	must(t, os.WriteFile(d.path("manifest.tsv"), []byte("ok\tro\tm1\thigh\t"+d.path("ok.md")+"\nbad\tro\tm1\thigh\t"+d.path("partial.md")+"\n"), 0o644))
	if rc, out := d.exec("codex-fanout", "-J", "-m", d.path("merger.md"), d.path("manifest.tsv"), d.path("out")); rc != 2 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	prompt := d.read("out/merger.prompt.md")
	if !strings.Contains(prompt, "bad:rc=65") || strings.Contains(prompt, "bad.out.md") {
		t.Errorf("merger.prompt.md が未完了の run を除いていない: %s", prompt)
	}
	d.read("out/digest.md")
}

// -J の run は上限の行が events にだけ出ることがある。log に無くても台帳の cause で上限と分かる (issue 762)
func TestDriverJSONUsageLimitOnlyInEventsIsReported(t *testing.T) {
	d := newDriver(t)
	rc, out := d.invoke("CASE_LIMIT", "ro", "-J")
	if rc != 1 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	if strings.Contains(d.read("out/ro.log"), "usage limit") {
		t.Fatal("前提が崩れた: 上限の行が log にも出ている (events だけを見る経路を検査できない)")
	}
	if !strings.Contains(d.read("out/runs.tsv"), "\tusage_limit\n") || !strings.Contains(out, "利用上限") {
		t.Errorf("events にだけ出た上限を拾っていない: runs.tsv=%q out=%s", d.read("out/runs.tsv"), out)
	}
}

// events の中でも上限と見るのは error / turn.failed のイベントだけ。コマンドの出力に同じ文字列があるだけの失敗は上限にしない (issue 762)
func TestDriverJSONUsageLimitTextInCommandOutputIsNotReported(t *testing.T) {
	d := newDriver(t)
	rc, out := d.invoke("CASE_QUOTED_FAIL", "ro", "-J")
	if rc != 1 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	if !strings.Contains(d.read("out/ro.events.jsonl"), "hit your usage limit") {
		t.Fatal("前提が崩れた: events に上限の文字列が無い")
	}
	if strings.Contains(d.read("out/runs.tsv"), "usage_limit") || strings.Contains(out, "利用上限") {
		t.Errorf("コマンドの出力にある文字列を上限と読んだ: runs.tsv=%q out=%s", d.read("out/runs.tsv"), out)
	}
}

// -J の log には応答の本文が追記される。本文が上限の行を引用しているだけの失敗は上限にせず、stderr にだけ出た本物の上限は拾う (issue 762)
func TestDriverJSONUsageLimitIgnoresQuotedBodyButReadsStderr(t *testing.T) {
	d := newDriver(t)
	rc, out := d.invoke("CASE_QUOTED_BODY", "ro", "-J")
	if rc != 1 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	if !strings.Contains(d.read("out/ro.log"), "hit your usage limit") {
		t.Fatal("前提が崩れた: 本文が log に追記されていない")
	}
	if strings.Contains(d.read("out/runs.tsv"), "usage_limit") {
		t.Errorf("本文の引用を上限と読んだ: %q", d.read("out/runs.tsv"))
	}
	d = newDriver(t)
	if rc, out := d.invoke("CASE_LIMIT_STDERR", "ro", "-J"); rc != 1 || !strings.Contains(d.read("out/runs.tsv"), "\tusage_limit\n") {
		t.Errorf("stderr にだけ出た上限を拾っていない: rc=%d runs.tsv=%q out=%s", rc, d.read("out/runs.tsv"), out)
	}
}

// -J の log の本文が上限の行と区切り行まで引用していても、何も言わずに死んだ run でも、上限にしない (issue 762 の 4 周目)
func TestDriverJSONUsageLimitIgnoresAppendedBodyAndSilentDeath(t *testing.T) {
	for _, c := range []string{"CASE_QUOTED_MARK", "CASE_SILENT_DEATH"} {
		d := newDriver(t)
		rc, out := d.invoke(c, "ro", "-J")
		if rc != 1 {
			t.Fatalf("%s: rc=%d %s", c, rc, out)
		}
		if !strings.Contains(d.read("out/ro.log"), "hit your usage limit") {
			t.Fatalf("%s: 前提が崩れた: log に上限の行の引用が無い", c)
		}
		if strings.Contains(d.read("out/runs.tsv"), "usage_limit") {
			t.Errorf("%s: 上限でない失敗を上限と読んだ: %q", c, d.read("out/runs.tsv"))
		}
	}
}
