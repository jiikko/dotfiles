package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 値の位置 (リテラル・静的な値) だけが違う 2 つ。L3 でだけ一致する (どちらも節 20 以上)。
const l3OnlyP = `struct P {
    func f(_ x: Int, _ y: Int) -> Int {
        let a = x + Limits.config.maximum.value
        let b = y * Config.shared.factor
        return a + b + Defaults.extra.amount
    }
}
`

const l3OnlyQ = `struct Q {
    func g(_ p: Int, _ q: Int) -> Int {
        let a = p + 1
        let b = q * 2
        return a + b + 3
    }
}
`

// 節 20 未満の 1 行の委譲の写し。
const tinyA = "struct TA {\n    func save(_ v: Int) throws { try Store.write(v, to: url) }\n}\n"
const tinyB = "struct TB {\n    func save(_ v: Int) throws { try Store.write(v, to: url) }\n}\n"

// 既定は下限 20 節・L1 と L2 だけ: 小さい写しと L3 だけの一致は出さず、下限と段を明示すれば出る。
func TestReportDefaultsSkipTinyCopiesAndL3(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/P.swift": l3OnlyP, "Sources/Q.swift": l3OnlyQ, "Sources/TA.swift": tinyA, "Sources/TB.swift": tinyB})
	if rc, out, _ := runReport(t, root); rc != 0 || !strings.Contains(out, "重複なし") {
		t.Errorf("既定では小さい写しと L3 だけの一致を出さない: rc=%d out=%q", rc, out)
	}
	if _, out, _ := runReport(t, "-levels", "L1,L2,L3", root); !strings.Contains(out, "組 1 (L3 1)") {
		t.Errorf("-levels で L3 を足せば出る (入力の前提): %q", out)
	}
	if _, out, _ := runReport(t, "-min-nodes", "5", root); !strings.Contains(out, "Sources/TA.swift") {
		t.Errorf("下限を下げれば小さい写しも出る (入力の前提): %q", out)
	}
}

// L3 を見ない既定の走査では、L3 の組の除外は組が作られないだけなので「効いていない」に数えない。L3 も見れば数える。
func TestL3GroupExclusionIsNotStaleWhenL3IsNotScanned(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/P.swift": l3OnlyP, "Sources/Q.swift": l3OnlyQ})
	_, out, _ := runReport(t, "-levels", "L1,L2,L3", "-format", "json", root)
	var s struct{ Groups []struct{ Exclude string } }
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Groups) != 1 {
		t.Fatalf("L3 の組 1 つを期待: %v %s", err, out)
	}
	var r map[string]any
	if err := json.Unmarshal([]byte(s.Groups[0].Exclude), &r); err != nil {
		t.Fatal(err)
	}
	r["kind"], r["reason"] = "false-positive", "値が違う"
	writeConfig(t, root, r)
	if _, out, _ := runReport(t, root); strings.Contains(out, "効いていない除外") {
		t.Errorf("L3 を見ない既定の走査で、L3 の組の除外を効いていないと言った: %q", out)
	}
	if _, out, _ := runReport(t, "-levels", "L1,L2,L3", root); strings.Contains(out, "効いていない除外") || !strings.Contains(out, "重複なし") {
		t.Errorf("L3 も見れば除外が効いて組が消える: %q", out)
	}
	writeFile(t, root, "Sources/Q.swift", strings.Replace(l3OnlyQ, "a + b + 3", "a - b", 1))
	if _, out, _ := runReport(t, "-levels", "L1,L2,L3", root); !strings.Contains(out, "効いていない除外 1") {
		t.Errorf("L3 も見る走査で組が無くなれば、効いていない除外に出す: %q", out)
	}
}

// 組は大きい順に並べ、同じファイルの組み合わせに組が 2 つ以上あれば、先頭に件数と節の合計を出す。
func TestReportSortsBySizeAndSummarizesFilePairs(t *testing.T) {
	big := `struct A {
    func location(path: String) -> StorageLocation {
        StorageLocation(connectionID: ConnectionID(rawValue: "conn"), path: ObjectPath(rawValue: path))
    }
    func zeta(_ x: Int, _ y: Int) -> Int {
        let a = x + Limits.config.maximum.value
        let b = y * Config.shared.factor
        let c = a * b - Defaults.extra.amount
        return a + b + c + Defaults.extra.amount * 2
    }
}
`
	root := repoWith(t, map[string]string{
		"Sources/A.swift": big, "Sources/B.swift": strings.Replace(big, "struct A", "struct B", 1),
		"Sources/U.swift": unrelated, "Sources/U2.swift": strings.Replace(unrelated, "struct U", "struct U2", 1),
	})
	_, out, errs := runReport(t, root)
	lines := strings.Split(out, "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[1], "ファイルの組") || !strings.Contains(lines[2], "2 組・節") || !strings.Contains(lines[2], "Sources/A.swift ↔ Sources/B.swift") {
		t.Fatalf("先頭にファイルの組の要約を期待: %q err=%q", out, errs)
	}
	if strings.Contains(out, "↔ Sources/U2.swift") {
		t.Errorf("組が 1 つだけのファイルの組は要約に出さない (組の一覧と同じ): %q", out)
	}
	fi, li := strings.Index(out, "A.zeta("), strings.Index(out, "A.location(")
	if fi < 0 || li < 0 || fi > li {
		t.Errorf("大きい組 (zeta) を、名前の順では前の小さい組 (location) より前に: %q", out)
	}
}
