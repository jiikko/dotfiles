package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"dupscan/internal/report"
)

// 実際に出した report / gate の json の鍵が、-outputs の一覧の型の欄に全部ある (型を辿る処理と出力の形が食い違わない)。
func TestOutputsListCoversTheKeysOfRealJSON(t *testing.T) {
	other := strings.ReplaceAll(helperOther, "struct D", "struct E")
	root := repoWith(t, map[string]string{
		"Sources/A.swift": allowedGroup(helperA, "2 intentional 分けておく"), "Sources/B.swift": helperB,
		"Sources/D.swift": helperOther, "Sources/E.swift": other,
	})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/C.swift", helperC)
	gitRun(t, root, "rm", "-q", "Sources/E.swift")

	_, out, errs := runReport(t, "-format", "json", "-min-nodes", "12", root)
	keys := checkJSONAgainstOutputs(t, "report", out, errs)
	rc, out, errs := runGateCmd(t, "-format", "json", "-min-nodes", "12", "-base", "HEAD", root)
	if rc != 1 {
		t.Fatalf("前提: gate は新しい写し C を違反にする: rc=%d err=%q", rc, errs)
	}
	keys += checkJSONAgainstOutputs(t, "gate", out, errs)
	for _, cmd := range []string{"report", "gate"} {
		checkVerboseOnlyLinesAreListed(t, cmd, root)
	}
	for _, want := range []string{"GroupJSON.exclude", "ViolationJSON.exclude", "ResolvedJSON.remain", "GateSummary.stale_exclusions", "UnitRef.key"} {
		if !strings.Contains(keys, want+"\n") {
			t.Errorf("前提: fixture の json に %s が出る: %q", want, keys)
		}
	}
}

// checkVerboseOnlyLinesAreListed は、-v を付けたときにだけ出る行が、-outputs の -v の行の見出しのどれかで始まるかを見る
// (表を通らずに書いた行は、書き方に依らずここで見える。fixture に出る行の範囲で)。
func checkVerboseOnlyLinesAreListed(t *testing.T, command, root string) {
	t.Helper()
	args := []string{"-min-nodes", "12", "-limit", "0", root}
	if command == "gate" {
		args = append([]string{"-base", "HEAD"}, args...)
	}
	_, plain, _ := runReport(t, append(commandArgs(command), args...)...)
	_, verbose, _ := runReport(t, append(append(commandArgs(command), "-v"), args...)...)
	o, _ := report.OutputsFor(command)
	inPlain := map[string]bool{}
	for _, l := range strings.Split(plain, "\n") {
		inPlain[l] = true
	}
	extra := 0
	for _, l := range strings.Split(verbose, "\n") {
		if l == "" || inPlain[l] || strings.HasPrefix(l, "dupscan") {
			continue
		}
		extra++
		listed := false
		for _, v := range o.Verbose {
			listed = listed || strings.HasPrefix(l, v.Prefix)
		}
		if !listed {
			t.Errorf("%s -v にだけ出る行 %q の見出しが -outputs の一覧に無い", command, l)
		}
	}
	if extra < 3 {
		t.Fatalf("前提: %s の fixture で -v にだけ出る行がある (%d 行): %q", command, extra, verbose)
	}
}

// commandArgs は run に渡す subcommand の語 (report は既定なので無し)。
func commandArgs(command string) []string {
	if command == "report" {
		return nil
	}
	return []string{command}
}

// checkJSONAgainstOutputs は out の json の鍵を、command の一覧の型の欄と照らし、見た「型.欄」を 1 行ずつ返す。
func checkJSONAgainstOutputs(t *testing.T, command, out, errs string) string {
	t.Helper()
	o, ok := report.OutputsFor(command)
	if !ok {
		t.Fatalf("-outputs に %s が無い", command)
	}
	fields := map[string]map[string]string{}
	for _, ty := range o.Types {
		fields[ty.Name] = map[string]string{}
		for _, f := range ty.Fields {
			fields[ty.Name][f.Name] = strings.TrimPrefix(f.Type, "[]")
		}
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%s の json を読めない: %v out=%q err=%q", command, err, out, errs)
	}
	var seen strings.Builder
	var walk func(v any, typ, path string)
	walk = func(v any, typ, path string) {
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				walk(e, typ, path+"[]")
			}
		case map[string]any:
			for k, child := range v {
				ft, ok := fields[typ][k]
				if !ok {
					t.Errorf("%s: %s の鍵 %q が -outputs の %s の欄に無い", command, path, k, typ)
					continue
				}
				seen.WriteString(typ + "." + k + "\n")
				if _, nested := fields[ft]; nested {
					walk(child, ft, path+"."+k)
				}
			}
		}
	}
	walk(v, o.Root, command)
	return seen.String()
}

func TestCapabilitiesOutputsDescribesEveryListedCommand(t *testing.T) {
	registered := []string{}
	for _, spec := range commandSpecs() {
		registered = append(registered, spec.Name)
	}
	for _, cmd := range report.OutputCommands() {
		if !slices.Contains(registered, cmd) {
			t.Errorf("-outputs の %s は登録された subcommand ではない", cmd)
		}
		for _, format := range []string{"text", "json"} {
			var out, errb bytes.Buffer
			if rc := runCapabilities([]string{"-outputs", cmd, "-format", format}, &out, &errb); rc != 0 || errb.Len() != 0 {
				t.Errorf("-outputs %s -format %s: rc=%d err=%q", cmd, format, rc, errb.String())
			}
			if out.Len() > capabilitiesMaxBytes/2 {
				t.Errorf("-outputs %s -format %s は上限の半分 (%d B) を超えた: %d B", cmd, format, capabilitiesMaxBytes/2, out.Len())
			}
			if !strings.Contains(out.String(), "exclude") || !strings.Contains(out.String(), "除外の 1 行") {
				t.Errorf("-outputs %s に除外の 1 行の欄と -v の行が出る: %q", cmd, out.String())
			}
		}
	}
	for _, bad := range [][]string{{"-outputs", "nope"}, {"-outputs="}} {
		var out, errb bytes.Buffer
		if rc := runCapabilities(bad, &out, &errb); rc != 2 || out.Len() != 0 || !strings.Contains(errb.String(), "gate / report") {
			t.Errorf("%v は rc=2 と引ける名前 (能力表を黙って出さない): rc=%d out=%q err=%q", bad, rc, out.String(), errb.String())
		}
	}
}
