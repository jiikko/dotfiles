package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// gateRecord は record の最後の 1 行。
func gateRecord(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ls := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	var r map[string]any
	if err := json.Unmarshal([]byte(ls[len(ls)-1]), &r); err != nil {
		t.Fatalf("%v: %q", err, ls[len(ls)-1])
	}
	return r
}

func reasons(r map[string]any) []string {
	var out []string
	for _, x := range r["reasons"].([]any) {
		out = append(out, x.(string))
	}
	return out
}

// 記録は最終の終了結果を持つ。failed (重複の違反) の意味は変えず、exit_code と reasons で新しい除外・strict の判定不能を分ける。
func TestGateRecordHasFinalOutcome(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    []string
		rc      int
		reasons []string
		failed  bool
	}{
		{"clean", func(*testing.T, string) {}, nil, 0, nil, false},
		{"duplicates", func(t *testing.T, root string) { writeFile(t, root, "Sources/B.swift", helperB) }, nil, 1, []string{"duplicates"}, true},
		{"new exclusion only", func(t *testing.T, root string) {
			writeFile(t, root, ".dupscan.json", `{"version": 1, "exclude": [{"paths": ["Gen/**"], "kind": "false-positive", "reason": "生成コード"}]}`)
		}, []string{"-fail-new-exclusions"}, 1, []string{"new_exclusions"}, false},
		{"strict and duplicates", func(t *testing.T, root string) {
			writeFile(t, root, "Sources/B.swift", helperB)
			writeFile(t, root, "Sources/Broken.swift", "func broken(")
		}, []string{"-strict-analysis"}, 2, []string{"strict_incomplete", "duplicates"}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := committedRepo(t)
			tt.setup(t, root)
			rec := filepath.Join(t.TempDir(), "gate.jsonl")
			rc, out, errs := runGateCmd(t, append(append([]string{"-record", rec, "-base", "HEAD"}, tt.args...), root)...)
			if rc != tt.rc {
				t.Fatalf("rc=%d want %d out=%q stderr=%q", rc, tt.rc, out, errs)
			}
			r := gateRecord(t, rec)
			if r["exit_code"] != float64(tt.rc) || !slices.Equal(reasons(r), tt.reasons) || r["failed"] != tt.failed {
				t.Errorf("exit_code=%v reasons=%v failed=%v", r["exit_code"], r["reasons"], r["failed"])
			}
			if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(r["head_commit"].(string)) {
				t.Errorf("head_commit は完全な SHA: %v", r["head_commit"])
			}
			cond := r["conditions"].(map[string]any)
			if cond["base"] != "HEAD" || cond["levels"] != "L1,L2" || cond["min_nodes"] != float64(20) {
				t.Errorf("再現条件: %v", cond)
			}
			if tt.name == "new exclusion only" {
				el := r["new_exclusions"].([]any)[0].(map[string]any)
				if el["kind"] != "false-positive" || el["reason"] != "生成コード" {
					t.Errorf("新しく足された除外の種別と理由: %v", el)
				}
			}
		})
	}
}

// 判定の前に止まった run も、-record の先が読めていれば段階とエラーを残す (GateSummary の欄は無い)。
func TestGateRecordKeepsEarlyError(t *testing.T) {
	root := committedRepo(t)
	rec := filepath.Join(t.TempDir(), "gate.jsonl")
	if rc, _, _ := runGateCmd(t, "-record", rec, "-base", "no-such-ref", root); rc != 2 {
		t.Fatalf("rc=%d", rc)
	}
	r := gateRecord(t, rec)
	if r["exit_code"] != float64(2) || r["phase"] != "base" || r["error"] == "" || !slices.Equal(reasons(r), []string{"error"}) {
		t.Errorf("段階とエラーと、空でない reasons: %v", r)
	}
	if hc, _ := r["head_commit"].(string); len(hc) != 40 {
		t.Errorf("head を決めた後に止まった run にも head_commit を残す: %v", r["head_commit"])
	}
	if _, ok := r["failed"]; ok {
		t.Errorf("判定に届かなかった run に failed を出さない (成功に読ませない): %v", r)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

// 出力に失敗した run は rc=2 で、記録にも output_error として残る (記録を出力の前に書くと rc=0 の行が残る)。
func TestGateRecordAfterOutputFailure(t *testing.T) {
	root := committedRepo(t)
	rec := filepath.Join(t.TempDir(), "gate.jsonl")
	var errb bytes.Buffer
	if rc := run([]string{"gate", "-record", rec, "-base", "HEAD", root}, failingWriter{}, &errb); rc != 2 {
		t.Fatalf("rc=%d stderr=%q", rc, errb.String())
	}
	r := gateRecord(t, rec)
	if r["exit_code"] != float64(2) || !slices.Equal(reasons(r), []string{"output_error"}) || r["failed"] != false {
		t.Errorf("exit_code=%v reasons=%v failed=%v", r["exit_code"], r["reasons"], r["failed"])
	}
}

// 知らない形式は判定の前に断る (判定の欄と phase を同じ行に混ぜない)。
func TestGateRecordRejectsUnknownFormatBeforeJudging(t *testing.T) {
	root := committedRepo(t)
	writeFile(t, root, "Sources/B.swift", helperB)
	rec := filepath.Join(t.TempDir(), "gate.jsonl")
	if rc, _, _ := runGateCmd(t, "-record", rec, "-format", "bogus", "-base", "HEAD", root); rc != 2 {
		t.Fatalf("rc=%d", rc)
	}
	r := gateRecord(t, rec)
	if _, ok := r["failed"]; ok || r["phase"] != "args" {
		t.Errorf("判定の前に止める: %v", r)
	}
}

// 記録を書けないときは元の判定を stderr に残す。
func TestGateRecordFailureKeepsOriginalRC(t *testing.T) {
	root := committedRepo(t)
	writeFile(t, root, "Sources/B.swift", helperB)
	rc, _, errs := runGateCmd(t, "-record", filepath.Join(t.TempDir(), "no", "such", "dir.jsonl"), "-base", "HEAD", root)
	if rc != 2 || !strings.Contains(errs, "判定は rc=1") {
		t.Errorf("rc=%d stderr=%q", rc, errs)
	}
}

// 読み手の閉じたパイプへ出すと、SIGPIPE で落ちず rc=2 で記録が残る (記録は出力の後に書くので、落ちると行が無くなる)。
func TestGateRecordSurvivesClosedPipe(t *testing.T) {
	root := committedRepo(t)
	writeFile(t, root, "Sources/B.swift", helperB)
	rec := filepath.Join(t.TempDir(), "gate.jsonl")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	cmd := exec.Command(os.Args[0], "gate", "-record", rec, "-base", "HEAD", root)
	cmd.Env = append(os.Environ(), "DUPSCAN_TEST_RUN_MAIN=1")
	cmd.Stdout = w
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err = cmd.Run()
	w.Close()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("SIGPIPE で落ちず rc=2: err=%v stderr=%q", err, errb.String())
	}
	got := gateRecord(t, rec)
	if !slices.Equal(reasons(got), []string{"output_error", "duplicates"}) || got["exit_code"] != float64(2) {
		t.Errorf("output_error と duplicates が併存する: %v", got["reasons"])
	}
}
