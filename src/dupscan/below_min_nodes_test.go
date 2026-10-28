package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 0 件の「重複なし」は、節が下限に満たない小さい単位を比べていない。その数を 1 行に出す (黙らせない)。
// 小さい helper の写し (1 語だけ違う 10 行前後の関数) はここに入る。

const smallFunctions = "package a\n\nfunc first() int  { return 1 }\nfunc second() int { return 1 }\n"

func TestReportSaysHowManySmallUnitsWereNotCompared(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": smallFunctions})
	rc, out, _ := runReport(t, root)
	if rc != 0 || !strings.Contains(out, "重複なし") || !strings.Contains(out, "小さい単位 2 個は比べていない (節が 20 未満。-min-nodes で下げる)") {
		t.Fatalf("既定: rc=%d out=%q", rc, out)
	}
	// 下限を下げると比べる。組が出て、比べていない小さい単位の注記は消える
	_, out, _ = runReport(t, "-min-nodes", "3", root)
	if strings.Contains(out, "重複なし") || strings.Contains(out, "小さい単位") {
		t.Errorf("下限を下げると比べる: %q", out)
	}
}

func TestReportJSONCarriesBelowMinNodes(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": smallFunctions})
	_, out, _ := runReport(t, "-format", "json", root)
	var s struct {
		Units         int `json:"units"`
		BelowMinNodes int `json:"below_min_nodes"`
		MinNodes      int `json:"min_nodes"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	if s.BelowMinNodes != 2 || s.MinNodes != 20 || s.Units != 2 {
		t.Errorf("below_min_nodes=%d min_nodes=%d units=%d (2 / 20 / 2 を期待)", s.BelowMinNodes, s.MinNodes, s.Units)
	}
}

func TestReportOmitsTheNoteWhenEverythingWasCompared(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": "package a\n"})
	_, out, _ := runReport(t, root)
	if strings.Contains(out, "小さい単位") {
		t.Errorf("比べなかった単位が無ければ注記を出さない (1 行を短く保つ): %q", out)
	}
}

// 比べなかった小さい単位だけでできる同じ形の組の数も添える (contains / contains2 のような完全一致が、小さい単位の数に埋もれない)。
const midFunctions = `package a

func firstMid(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

func secondMid(ys []int, m int) bool {
	for _, y := range ys {
		if y == m {
			return true
		}
	}
	return false
}
`

func TestReportCountsGroupsAmongSmallUnits(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": midFunctions})
	_, out, _ := runReport(t, root)
	if !strings.Contains(out, "うち同じ形の組が 1 組ある (-min-nodes 12 で見える)") {
		t.Fatalf("小さい単位だけの組の数を添える: %q", out)
	}
	_, out, _ = runReport(t, "-format", "json", root)
	var s struct {
		Groups        int `json:"below_min_groups"`
		BelowMinFloor int `json:"below_min_floor"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || s.Groups != 1 || s.BelowMinFloor != 12 {
		t.Errorf("json: %+v err=%v", s, err)
	}
	// 小さすぎて下限 12 にも満たない単位の組は数えない (雑音)
	root = repoWith(t, map[string]string{"a.go": smallFunctions})
	if _, out, _ := runReport(t, root); strings.Contains(out, "うち同じ形") {
		t.Errorf("12 節未満の組は数えない: %q", out)
	}
}

// 読めない構文の関数は、重複が 1 組も無いときの -v でも 1 つずつ出す (「読めない構文があるファイル」だけだと、
// どの関数が比べられなかったかが分からず、同じファイルの他の関数まで外れたように読める)。
func TestReportVerboseListsUnreadableUnitsEvenWithoutGroups(t *testing.T) {
	root := repoWith(t, map[string]string{"a/Local.swift": "final class A {\n    func run(_ block: @escaping () -> Void) {\n        nonisolated(unsafe) let callback = block\n        callback()\n    }\n    func other() -> Int { 1 }\n}\n"})
	_, out, _ := runReport(t, "-v", root)
	if !strings.Contains(out, "重複なし") {
		t.Fatalf("前提: 重複が無い: %q", out)
	}
	if !strings.Contains(out, "読めない: a/Local.swift:2 A.run(") {
		t.Errorf("重複が無いときの -v に、読めない単位が出ない: %q", out)
	}
	if strings.Contains(out, "読めない: a/Local.swift:6") {
		t.Errorf("読めない構文を含まない other() まで読めないと出た: %q", out)
	}
	if _, plain, _ := runReport(t, root); strings.Contains(plain, "読めない: ") {
		t.Errorf("-v なしでは単位の一覧を出さない: %q", plain)
	}
}
