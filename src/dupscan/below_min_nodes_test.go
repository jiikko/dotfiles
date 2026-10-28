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
