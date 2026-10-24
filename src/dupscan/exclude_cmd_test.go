package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const helperC = `struct C {
    func otherLocation(_ q: String) -> StorageLocation {
        StorageLocation(connectionID: ConnectionID(rawValue: "conn"), path: ObjectPath(rawValue: q))
    }
}
`

func runReport(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	rc := run(args, &out, &errb)
	return rc, out.String(), errb.String()
}

// groupRule は report の json の最初の組に付く「除外の 1 行」に kind と reason を埋めた除外。
func groupRule(t *testing.T, root string) map[string]any {
	t.Helper()
	rc, out, errs := runReport(t, "-format", "json", root)
	var s struct{ Groups []struct{ Exclude string } }
	if err := json.Unmarshal([]byte(out), &s); rc != 0 || err != nil || len(s.Groups) != 1 || s.Groups[0].Exclude == "" {
		t.Fatalf("組 1 つと除外の 1 行を期待: rc=%d err=%v out=%s stderr=%s", rc, err, out, errs)
	}
	var r map[string]any
	if err := json.Unmarshal([]byte(s.Groups[0].Exclude), &r); err != nil {
		t.Fatal(err)
	}
	r["kind"], r["reason"] = "intentional", "分けておく"
	return r
}

func writeConfig(t *testing.T, root string, rules ...any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"version": 1, "exclude": rules})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".dupscan.json", string(b))
}

func TestGroupExclusionHidesGroupAndSurvivesLineShift(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	writeConfig(t, root, groupRule(t, root))
	if rc, out, errs := runReport(t, root); rc != 0 || !strings.Contains(out, "重複なし") || strings.Count(out, "\n") != 1 {
		t.Errorf("除外した組が出た: rc=%d out=%q err=%q", rc, out, errs)
	}
	if rc, out, _ := runReport(t, "-no-exclude", root); rc != 0 || !strings.Contains(out, "組 1") {
		t.Errorf("-no-exclude で組が出ない: rc=%d out=%q", rc, out)
	}
	writeFile(t, root, "Sources/A.swift", "// 行をずらす\n\n\n"+helperA)
	if _, out, _ := runReport(t, root); !strings.Contains(out, "重複なし") {
		t.Errorf("行がずれて除外が外れた: %q", out)
	}
}

func TestGroupExclusionDoesNotHideNewMember(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	writeConfig(t, root, groupRule(t, root))
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/C.swift", helperC)
	if _, out, _ := runReport(t, root); !strings.Contains(out, "Sources/C.swift") {
		t.Errorf("除外した組に増えた新しいメンバーが報告に出ない: %q", out)
	}
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "新しい: Sources/C.swift") || strings.Contains(out, "新しい: Sources/A.swift") {
		t.Errorf("新しいメンバー C だけを違反に期待: rc=%d out=%q err=%q", rc, out, errs)
	}
}

func TestRenamedMemberMakesExclusionStale(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	writeConfig(t, root, groupRule(t, root))
	writeFile(t, root, "Sources/B.swift", strings.Replace(helperB, "makeLocation", "renamedLocation", 1))
	rc, out, _ := runReport(t, "-v", root)
	if rc != 0 || !strings.Contains(out, "効いていない除外 1") || !strings.Contains(out, "効いていない除外: ") {
		t.Errorf("改名で効かなくなった除外が出ない: rc=%d out=%q", rc, out)
	}
}

func TestDeletedCopyRecreatedIsNotCovered(t *testing.T) {
	// 除外した写し B を寄せて消した後、同じ鍵・同じ本体の B を書き直した: 除外は先回りで覆わない。
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	writeConfig(t, root, groupRule(t, root))
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "B を含む")
	gitRun(t, root, "rm", "-q", "Sources/B.swift")
	gitRun(t, root, "commit", "-q", "-m", "B を寄せた")
	writeFile(t, root, "Sources/B.swift", helperB)
	if rc, out, errs := runGateCmd(t, "-base", "HEAD", root); rc != 1 || !strings.Contains(out, "新しい: Sources/B.swift") {
		t.Errorf("消えた写しの釘の場所に置いた写しが通った: rc=%d out=%q err=%q", rc, out, errs)
	}
}

func TestNewExclusionsAreListedAndCanFail(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/B.swift", helperB)
	writeConfig(t, root, groupRule(t, root))
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 0 || !strings.Contains(out, "新しく足された除外 2") {
		t.Errorf("新しい写しと、それを外す新しい除外: rc=0 と一覧の件数を期待: rc=%d out=%q err=%q", rc, out, errs)
	}
	if rc, out, _ := runGateCmd(t, "-fail-new-exclusions", "-base", "HEAD", root); rc != 1 {
		t.Errorf("-fail-new-exclusions で rc=1 を期待: rc=%d out=%q", rc, out)
	}
}

func TestPathExclusionCountsHiddenChangedUnits(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	writeConfig(t, root, map[string]any{"paths": []string{"Generated/**"}, "kind": "false-positive", "reason": "生成コード"})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Generated/Deep/B.swift", helperB)
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 0 || !strings.Contains(out, "パスの除外で通した変わった単位 1") {
		t.Errorf("glob の下の新しい写しを数えない: rc=%d out=%q err=%q", rc, out, errs)
	}
	// report では、除外の下の単位と外の単位の組は出す (外の単位から見れば写しがある)。全員が除外の下の組だけを出さない。
	if _, out, _ := runReport(t, root); !strings.Contains(out, "組 1") || strings.Contains(out, "効いていない除外") {
		t.Errorf("除外の外の単位との組が出ない・効いているのに効いていないと出た: %q", out)
	}
	writeFile(t, root, "Generated/Deep/C.swift", helperC)
	writeFile(t, root, "Sources/A.swift", "struct A {}\n")
	if _, out, _ := runReport(t, root); !strings.Contains(out, "重複なし") {
		t.Errorf("全員が除外の下の組が出た: %q", out)
	}
	writeConfig(t, root, map[string]any{"paths": []string{"Nowhere/**"}, "kind": "false-positive", "reason": "生成コード"})
	if _, out, _ := runReport(t, root); !strings.Contains(out, "効いていない除外 1") {
		t.Errorf("何にも当たらないパスの除外が効いていないに出ない: %q", out)
	}
}

func TestBadConfigIsUndecidable(t *testing.T) {
	for name, rule := range map[string]map[string]any{
		"理由が無い":     {"paths": []string{"X/**"}, "kind": "false-positive"},
		"種別が違う":     {"paths": []string{"X/**"}, "kind": "maybe", "reason": "r"},
		"細かい段だけ":    {"paths": []string{"X/**"}, "levels": []string{"L1"}, "kind": "false-positive", "reason": "r"},
		"知らない欄":     {"paths": []string{"X/**"}, "kind": "false-positive", "reason": "r", "pathz": 1},
		"釘でない":      {"unit": "Sources/A.swift|A|f()", "kind": "intentional", "reason": "r"},
		"2 つの形を混ぜる": {"paths": []string{"X/**"}, "unit": "a@b", "kind": "intentional", "reason": "r"},
	} {
		root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
		writeConfig(t, root, rule)
		if rc, _, errs := runReport(t, root); rc != 2 || !strings.Contains(errs, ".dupscan.json") {
			t.Errorf("%s: rc=2 を期待: rc=%d err=%q", name, rc, errs)
		}
		if rc, _, _ := runReport(t, "-no-exclude", root); rc != 2 {
			t.Errorf("%s: -no-exclude でも形の誤りは rc=2 を期待: rc=%d", name, rc)
		}
	}
}

func TestBrokenBaseConfigCountsAsEmpty(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, ".dupscan.json": "{ 壊れている"})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeConfig(t, root, map[string]any{"paths": []string{"Generated/**"}, "kind": "false-positive", "reason": "生成コード"})
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 0 || !strings.Contains(out, "base の除外の設定を読めないので空とみなした") || !strings.Contains(out, "新しく足された除外 1") {
		t.Errorf("壊れた base の設定を直す変更: rc=0 と注記を期待: rc=%d out=%q err=%q", rc, out, errs)
	}
}

func TestCopyOutOfExcludedPathIsViolation(t *testing.T) {
	// パスの除外の下の単位を、除外の外へ写した: 外の新しい写しは除外の効果で止まらない。
	for name, rule := range map[string]map[string]any{
		"全部の段":  {"paths": []string{"Generated/**"}, "kind": "false-positive", "reason": "生成コード"},
		"L2 から": {"paths": []string{"Generated/**"}, "levels": []string{"L2", "L3"}, "kind": "false-positive", "reason": "生成コード"},
	} {
		root := repoWith(t, map[string]string{"Generated/G.swift": helperA})
		writeConfig(t, root, rule)
		gitRun(t, root, "add", ".")
		gitRun(t, root, "commit", "-q", "-m", "base")
		writeFile(t, root, "Sources/B.swift", helperB)
		if rc, out, errs := runGateCmd(t, "-base", "HEAD", root); rc != 1 || !strings.Contains(out, "新しい: Sources/B.swift") {
			t.Errorf("%s: 除外の外への写しが通った: rc=%d out=%q err=%q", name, rc, out, errs)
		}
	}
}

func TestNewCopyUnderLevelsPathIsCounted(t *testing.T) {
	// 段を指定したパスの除外の下に新しい写しを置いた: 通すが、通した数を 1 行目に出し、-fail-new-exclusions で止める。
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	writeConfig(t, root, map[string]any{"paths": []string{"Mirror/**"}, "levels": []string{"L1", "L2", "L3"}, "kind": "false-positive", "reason": "鏡"})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Mirror/B.swift", helperB)
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 0 || !strings.Contains(out, "パスの除外で通した変わった単位 1") {
		t.Errorf("通した数を期待: rc=%d out=%q err=%q", rc, out, errs)
	}
	if rc, _, _ := runGateCmd(t, "-fail-new-exclusions", "-base", "HEAD", root); rc != 1 {
		t.Errorf("-fail-new-exclusions で rc=1 を期待: rc=%d", rc)
	}
}

func TestBadGlobIsUndecidable(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	writeConfig(t, root, map[string]any{"paths": []string{"Generated/[a-"}, "kind": "false-positive", "reason": "r"})
	if rc, _, errs := runReport(t, root); rc != 2 {
		t.Errorf("壊れた glob で rc=2 を期待: rc=%d err=%q", rc, errs)
	}
}

func TestNarrowedRunDoesNotCallExclusionStale(t *testing.T) {
	// -levels を絞った走査では、効いている組の除外を「効いていない」と言わない。
	l2copy := strings.Replace(helperB, `"conn"`, `"other"`, 1) // リテラルだけが違う L2 の写し
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": l2copy})
	r := groupRule(t, root)
	writeConfig(t, root, r)
	if _, out, _ := runReport(t, root); strings.Contains(out, "効いていない") {
		t.Fatalf("全部の段の走査で効いていないと出た: %q", out)
	}
	if _, out, _ := runReport(t, "-levels", "L1", root); strings.Contains(out, "効いていない") {
		t.Errorf("絞った走査で効いていないと出た: %q", out)
	}
}
