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
