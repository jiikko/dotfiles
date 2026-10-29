package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// `// dupscan:allow-group <人数> <種別> <理由>`: 組のメンバーの 1 つに書くと、その単位と同じ本体の単位の組を、人数が合う間だけ許可する。

func allowedGroup(src, rest string) string { return marked(src, "dupscan:allow-group "+rest) }

func TestAllowGroupSilencesTheWholeGroup(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": allowedGroup(helperA, "2 intentional 分けておく"), "Sources/B.swift": helperB})
	rc, out, errs := runReport(t, "-v", root)
	if rc != 0 || !strings.Contains(out, "重複なし") || strings.Contains(out, "効いていない除外") {
		t.Errorf("メンバーの 1 つの allow-group で組ごと消える: rc=%d out=%q err=%q", rc, out, errs)
	}
	if _, out, _ := runReport(t, "-no-exclude", root); !strings.Contains(out, "組 1") {
		t.Errorf("-no-exclude では組が出る: %q", out)
	}
}

// 写しが増えて人数が合わなくなったら、許可は何も消さない: 新しい写しは gate で止まり、許可は人数の食い違いとして効いていない除外に出る。
// 人数の合わない許可は要素を持たないので、「新しく足された除外」にも数えない。
func TestAllowGroupDoesNotCoverANewCopy(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": allowedGroup(helperA, "2 intentional 分けておく"), "Sources/B.swift": helperB})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/C.swift", helperC)
	rc, out, errs := runGateCmd(t, "-v", "-base", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "新しい: Sources/C.swift") {
		t.Errorf("増えた写し C を違反に期待: rc=%d out=%q err=%q", rc, out, errs)
	}
	if !strings.Contains(out, "理由: dupscan:allow-group の人数と、同じ本体の単位の数が合わない") || !strings.Contains(out, "人数 2・同じ本体の単位 L1 3") {
		t.Errorf("人数の食い違いを効いていない除外の理由に出す: %q", out)
	}
	if strings.Contains(out, "新しく足された除外") || strings.Contains(out, "名指しできない") {
		t.Errorf("人数の合わない許可を新しく足された除外・名指しできない除外に数えない: %q", out)
	}
}

// head で書いた allow-group の人数が合わなければ何も消さないので、新しく足された除外にも数えない (増えた写しが組として止まるだけ)。
func TestAllowGroupWithWrongCountIsNotANewExclusion(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/B.swift", allowedGroup(helperB, "3 intentional 人数違い"))
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "新しい: Sources/B.swift") || strings.Contains(out, "新しく足された除外") {
		t.Errorf("人数の合わない新しい allow-group は何も消さず、新しく足された除外にも出ない: rc=%d out=%q err=%q", rc, out, errs)
	}
}

const helperOther = `struct D {
    func describe(_ n: Int) -> String {
        String(repeating: "-", count: n) + String(n * 2) + "units"
    }
}
`

// base で人数が合わず効いていなかった allow-group は、後から写しを足して人数が合っても、その新しい写しを覆わない
// (コメントは diff で 1 行も変わっていないので、人の目に届かない)。
func TestUnchangedAllowGroupDoesNotCoverACopyThatFillsTheCount(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": allowedGroup(helperA, "3 intentional 分けておく"), "Sources/B.swift": helperB, "Sources/D.swift": helperOther})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/C.swift", helperC)
	rc, out, errs := runGateCmd(t, "-v", "-base", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "新しい: Sources/C.swift") {
		t.Errorf("人数を埋めた新しい写し C を違反に期待: rc=%d out=%q err=%q", rc, out, errs)
	}
	if strings.Contains(out, "・新しく足された除外") || !strings.Contains(out, "理由: 文面 (人数・種別・理由) が base のどれかの dupscan:allow-group と同じ許可は、変わっていないとみなして base にあった単位だけを覆う") {
		t.Errorf("覆わなかった許可は新しく足された除外に数えず、専用の理由で効いていない除外に出す: %q", out)
	}
	// 書いた単位を移しても・改名しても、コメントの中身が同じなら変わっていない許可のまま (diff にコメントの行が出ない)
	for name, move := range map[string]func(){
		"ファイルの移動": func() {
			gitRun(t, root, "mv", "Sources/A.swift", "Sources/A2.swift")
		},
		"関数の改名": func() {
			writeFile(t, root, "Sources/A.swift", allowedGroup(strings.Replace(helperA, "func location", "func location2", 1), "3 intentional 分けておく"))
		},
	} {
		gitRun(t, root, "reset", "-q", "--hard")
		writeFile(t, root, "Sources/C.swift", helperC)
		move()
		if rc, out, _ := runGateCmd(t, "-base", "HEAD", root); rc != 1 || !strings.Contains(out, "新しい: Sources/C.swift") {
			t.Errorf("%s: 写し C を違反に期待: rc=%d out=%q", name, rc, out)
		}
	}
	// 理由を書き換えた許可は新しい許可として C を覆い、新しく足された除外に出す (コメントの行が diff に出る)
	gitRun(t, root, "reset", "-q", "--hard")
	writeFile(t, root, "Sources/C.swift", helperC)
	writeFile(t, root, "Sources/A.swift", allowedGroup(helperA, "3 intentional C も含めて分けておく"))
	if rc, out, _ := runGateCmd(t, "-base", "HEAD", root); rc != 0 || !strings.Contains(out, "新しく足された除外") {
		t.Errorf("理由を書き換えた許可は C を新しく足された除外として通す: rc=%d out=%q", rc, out)
	}
}

// 人数だけを書き換えた (新しい) allow-group は、新しい写しを新しく足された除外として通す。
func TestChangedAllowGroupCountCoversTheNewCopyAsNewExclusion(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": allowedGroup(helperA, "2 intentional 分けておく"), "Sources/B.swift": helperB})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Sources/C.swift", helperC)
	writeFile(t, root, "Sources/A.swift", allowedGroup(helperA, "3 intentional 分けておく"))
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 0 || !strings.Contains(out, "新しく足された除外") {
		t.Errorf("人数を書き換えた許可は C を新しく足された除外として通す: rc=%d out=%q err=%q", rc, out, errs)
	}
}

func TestMalformedAllowGroupIsUndecidable(t *testing.T) {
	for _, rest := range []string{"intentional 人数が無い", "1 intentional 1 人", "2 intentional", "2 maybe 種別が違う"} {
		root := repoWith(t, map[string]string{"Sources/A.swift": allowedGroup(helperA, rest), "Sources/B.swift": helperB})
		if rc, _, errs := runReport(t, root); rc != 2 || !strings.Contains(errs, "Sources/A.swift:2: dupscan:allow-group") {
			t.Errorf("%q: rc=2 と場所を期待: rc=%d err=%q", rest, rc, errs)
		}
	}
	root := repoWith(t, map[string]string{"Sources/A.swift": marked(helperA, "dupscan:allow-groups 2 intentional r"), "Sources/B.swift": helperB})
	if rc, _, errs := runReport(t, root); rc != 2 || !strings.Contains(errs, "知らない印 dupscan:allow-groups") {
		t.Errorf("知らない印は rc=2: rc=%d err=%q", rc, errs)
	}
}

// 下限未満で組にならない組の許可 (sweep の節 2 のための許可) は、本体も顔ぶれも変わっていなければ「見直せ」と言わない。
func TestStaleAllowGroupBelowMinNodesKeepsItsBody(t *testing.T) {
	small := func(name string) string {
		return "struct " + name + " {\n    func tag(_ s: String) -> String { s + \"!\" }\n}\n"
	}
	root := repoWith(t, map[string]string{"Sources/A.swift": allowedGroup(small("A"), "2 intentional 小さい"), "Sources/B.swift": small("B")})
	_, out, _ := runReport(t, "-v", root)
	if !strings.Contains(out, "効いていない除外 1") {
		t.Fatalf("前提: 下限未満の組の許可は効いていないと出る: %q", out)
	}
	if !strings.Contains(out, "理由: 釘の指す単位は釘の本体のまま今も在る (本体は変わっていない。組の除外なら顔ぶれも同じ)") {
		t.Errorf("本体も顔ぶれも同じ組の除外は「釘の本体のまま」と出す: %q", out)
	}
	// 顔ぶれが増えたら本体のままとは言わない: allow-group なら人数の食い違い、設定の組の除外なら従来の理由
	var pins string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "効いていない除外: "); ok {
			pins = rest
		}
	}
	writeFile(t, root, "Sources/C.swift", small("C"))
	_, out, _ = runReport(t, "-v", root)
	if strings.Contains(out, "釘の本体のまま") || !strings.Contains(out, "人数 2・同じ本体の単位 L1 3") {
		t.Errorf("増えた組の許可を「本体のまま」と言わない: %q", out)
	}
	var rule map[string]any
	if err := json.Unmarshal([]byte(pins), &rule); err != nil || len(rule["group"].([]any)) != 2 {
		t.Fatalf("前提: 効いていない除外の行から 2 人の組の釘を取る: %v %q", err, pins)
	}
	writeFile(t, root, "Sources/A.swift", small("A"))
	writeConfig(t, root, map[string]any{"group": rule["group"], "level": rule["level"], "kind": "intentional", "reason": "r"})
	_, out, _ = runReport(t, "-v", root)
	if strings.Contains(out, "釘の本体のまま") || !strings.Contains(out, "理由: 同じ鍵 (ファイル・型・署名) の単位は今も在る") {
		t.Errorf("顔ぶれの増えた設定の組の除外を「本体のまま」と言わない: %q", out)
	}
}

// sweep の組も allow-group で消え、節 3 に似ている対として出し直さない。
func TestSweepAllowGroupSilencesGroupAndItsPairs(t *testing.T) {
	src := func(name string) string {
		return "struct " + name + " {\n    func label(_ s: String, _ n: Int) -> String {\n        let head = s.prefix(n)\n        return String(head) + \":\" + String(n)\n    }\n}\n"
	}
	files := map[string]string{"Sources/A.swift": src("A"), "Sources/B.swift": src("B"), "Sources/C.swift": src("C")}
	root := repoWith(t, files)
	_, out, _ := sweep(t, root)
	if !strings.Contains(sweepSection(t, out, 1), "×3") {
		t.Fatalf("前提: 節 1 に 3 人の組が出る: %q", out)
	}
	files["Sources/A.swift"] = allowedGroup(src("A"), "3 intentional 型ごとに持つ")
	root = repoWith(t, files)
	rc, out, errs := sweep(t, root)
	if rc != 0 || !strings.Contains(sweepSection(t, out, 1), "なし") || !strings.Contains(sweepSection(t, out, 3), "なし") {
		t.Errorf("allow-group で組と節 3 の対が消える: rc=%d out=%q err=%q", rc, out, errs)
	}
}
