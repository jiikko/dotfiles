package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Swift は予約語と同じ綴りの識別子を、バッククォートで囲んで宣言できる。使う側は囲まずに書く (`self.repeat(1)`)。
// 宣言名・呼び出し名・参照名・query が同じ識別子を指すとして照合する。表示用の署名・単位の鍵・除外の釘は元の綴りのまま。
const escapedSample = `struct A {
    var ` + "`default`" + `: Int { 1 }
    func ` + "`repeat`" + `(_ value: Int) -> Int { value + ` + "`default`" + ` }
    func caller() -> Int { self.repeat(1) }
    func viaEscapedCall() -> Int { ` + "`repeat`" + `(2) }
}
`

func TestEscapedIdentifierDefinitionIsFoundByBareAndEscapedQueries(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample})
	for _, q := range []string{"A.repeat", "A.`repeat`", "repeat", "`repeat`"} {
		rc, out, errs := runReport(t, "def", q, root)
		if rc != 0 || !strings.Contains(out, "A.`repeat`(_ value: Int) -> Int") {
			t.Errorf("def %s が宣言を取れない: rc=%d out=%q err=%q", q, rc, out, errs)
		}
	}
	// property も同じ
	for _, q := range []string{"A.default", "A.`default`"} {
		rc, out, _ := runReport(t, "def", q, root)
		if rc != 0 || !strings.Contains(out, "default") {
			t.Errorf("def %s が computed property を取れない: rc=%d out=%q", q, rc, out)
		}
	}
}

func TestEscapedIdentifierCallersAndContextAgreeForBothSpellings(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample})
	// 呼び出しは 2 つ: self.repeat(1) (裸) と `repeat`(2) (囲み)。どちらの綴りで問い合わせても両方に届く
	for _, q := range []string{"repeat", "`repeat`"} {
		_, out, _ := runReport(t, "callers", q, root)
		if !strings.Contains(out, "Sample.swift:4") || !strings.Contains(out, "Sample.swift:5") {
			t.Errorf("callers %s が両方の呼び出しに届かない: %q", q, out)
		}
	}
	for _, q := range []string{"A.repeat", "A.`repeat`"} {
		_, out, _ := runReport(t, "context", q, root)
		if !strings.Contains(out, "定義 1 件") || strings.Contains(out, "呼び出し 0 件") || strings.Contains(out, "参照 0 件") {
			t.Errorf("context %s の定義・参照・呼び出しが揃わない: %q", q, out)
		}
	}
}

func TestEscapedIdentifierCountsItsCallsInIndex(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample})
	_, out, _ := runReport(t, "index", "-format", "json", "-name", "repeat", root)
	var s struct {
		Entries []struct {
			Name        string `json:"name"`
			CallsApprox *int   `json:"calls_approx"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Entries) != 1 {
		t.Fatalf("index の結果を読めない: err=%v out=%q", err, out)
	}
	if s.Entries[0].Name != "`repeat`" {
		t.Errorf("表示用の名前は宣言の綴りのまま (`repeat`): %q", s.Entries[0].Name)
	}
	if s.Entries[0].CallsApprox == nil || *s.Entries[0].CallsApprox != 2 {
		t.Errorf("calls_approx が実在の呼び出し 2 件を数えない: %v", s.Entries[0].CallsApprox)
	}
}

// 照合のための正規化は、単位の鍵 (除外の釘に入る) と署名を変えない。変わると、既存の除外が黙って外れる。
func TestEscapedIdentifierKeepsUnitKeyAndSignatureSpelling(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample})
	_, out, _ := runReport(t, "index", "-format", "json", "-name", "repeat", root)
	var s struct {
		Entries []struct {
			Key       string `json:"key"`
			Signature string `json:"signature"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Entries) != 1 {
		t.Fatalf("index の結果を読めない: err=%v out=%q", err, out)
	}
	if got, want := s.Entries[0].Key, "Sample.swift|A|`repeat`(_ value: Int) -> Int"; got != want {
		t.Errorf("単位の鍵が変わった: %q, want %q", got, want)
	}
	if !strings.HasPrefix(s.Entries[0].Signature, "`repeat`(") {
		t.Errorf("署名の綴りが変わった: %q", s.Entries[0].Signature)
	}
}

// refs も、裸名と囲み付きのどちらの query でも同じ参照に届く。参照は `default` (裸名で使う) と、`repeat` の呼び出し (self.repeat)。
func TestEscapedIdentifierRefsAgreeForBothSpellings(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample})
	var counts []int
	for _, q := range []string{"default", "`default`"} {
		_, out, _ := runReport(t, "refs", "-format", "json", q, root)
		var s struct {
			Entries []struct {
				Line int `json:"line"`
			} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(out), &s); err != nil {
			t.Fatalf("refs %s の結果を読めない: %v %q", q, err, out)
		}
		counts = append(counts, len(s.Entries))
	}
	if counts[0] != counts[1] || counts[0] == 0 {
		t.Errorf("refs の裸名と囲み付きで届く参照の数が違う (または 0): 裸名=%d 囲み=%d", counts[0], counts[1])
	}
}

// 単位の Name (表示と除外の一覧に出る名前) も元の綴りのまま。照合のための正規化が、保存する名前まで書き換えない。
func TestEscapedIdentifierKeepsUnitNameSpelling(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample})
	_, out, _ := runReport(t, "index", "-format", "json", "-name", "repeat", root)
	var s struct {
		Entries []struct {
			Name string `json:"name"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Entries) != 1 || s.Entries[0].Name != "`repeat`" {
		t.Errorf("単位の Name が元の綴り (`repeat`) でない: err=%v %+v", err, s.Entries)
	}
	_, def, _ := runReport(t, "def", "-format", "json", "A.repeat", root)
	var d struct {
		Entries []struct {
			Name string `json:"name"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(def), &d); err != nil || len(d.Entries) != 1 || d.Entries[0].Name != "`repeat`" {
		t.Errorf("def の定義の Name が元の綴りでない: err=%v %+v", err, d.Entries)
	}
}

// 読めない構文のファイルがあるとき、引用付きの query でも解析不完全の警告を落とさない
// (照合は成功するのに「問い合わせの名前を含まない」と通知してはいけない)。
func TestEscapedQueryKeepsTheParseWarningForUnreadableFiles(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample, "Broken.swift": "func broken(\nfunc run() { repeat(1) }\n"})
	for _, q := range []string{"repeat", "`repeat`"} {
		_, _, errs := runReport(t, "callers", q, root)
		if strings.Contains(errs, "は問い合わせの名前を含まない") {
			t.Errorf("callers %s: 本文に名前がある読めないファイルを、名前を含まないと通知した: %q", q, errs)
		}
		if !strings.Contains(errs, "Broken.swift") {
			t.Errorf("callers %s: 読めないファイルの警告が落ちた: %q", q, errs)
		}
	}
}

// 型名・引数ラベルが囲まれていても、囲まれていない query / 呼び出しが同じ対象に届く。
func TestEscapedTypeNameAndArgumentLabelAreMatchedToo(t *testing.T) {
	src := "struct `A` {\n    func `repeat`(`default` value: Int) -> Int { value }\n    func caller() -> Int { self.repeat(default: 1) }\n}\n"
	root := repoWith(t, map[string]string{"S.swift": src})
	for _, q := range []string{"A.repeat", "`A`.repeat", "A.repeat(default:)"} {
		rc, out, _ := runReport(t, "def", q, root)
		if rc != 0 || !strings.Contains(out, "repeat") {
			t.Errorf("def %s が型名・ラベルが囲まれた宣言に届かない: rc=%d out=%q", q, rc, out)
		}
	}
	_, out, _ := runReport(t, "index", "-format", "json", "-name", "repeat", root)
	var s struct {
		Entries []struct {
			CallsApprox *int `json:"calls_approx"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Entries) != 1 || s.Entries[0].CallsApprox == nil || *s.Entries[0].CallsApprox != 1 {
		t.Errorf("ラベルが囲まれた宣言への裸のラベルの呼び出しを数えない: err=%v %+v", err, s.Entries)
	}
}

// evidence は、定義のある名前の呼び出しを数える。定義が囲み付きでも、裸名の呼び出しを数える。
func TestEscapedDefinitionIsCountedAsCalledInEvidence(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample})
	_, out, _ := runReport(t, "evidence", "repeat", root)
	// 実際の文言は「そこで呼ばれている名前 N 個」。fixture は裸名の呼び出し (self.repeat) と囲み付きの呼び出し (`repeat`(2)) の 2 行で、
	// 名前は repeat と `repeat` の 2 つ。囲み付きの定義と合わせて数えないと、裸名の側が落ちて 1 個になる。
	if !strings.Contains(out, "そこで呼ばれている名前 2 個") {
		t.Errorf("裸名の呼び出しを、囲み付きの定義の呼び出しとして数えない: %q", out)
	}
}

// near / explain -compare は、裸名で囲み付きの単位を選べる。
func TestEscapedUnitCanBeSelectedByBareNameInNear(t *testing.T) {
	root := repoWith(t, map[string]string{"Sample.swift": escapedSample})
	for _, sel := range []string{"repeat", "`repeat`"} {
		rc, out, errs := runReport(t, "near", filepath.Join(root, "Sample.swift")+":"+sel)
		if rc != 0 {
			t.Errorf("near Sample.swift:%s が単位を選べない: rc=%d out=%q err=%q", sel, rc, out, errs)
		}
	}
}

// search の完全一致優先は、囲み付きの名前も完全一致として扱う。
func TestSearchPrefersTheEscapedExactNameOverPrefixMatch(t *testing.T) {
	root := repoWith(t, map[string]string{
		"A.swift": "struct P {\n    func repeatSuffix() -> Int { 1 }\n}\n",
		"B.swift": "struct Q {\n    func `repeat`() -> Int { 1 }\n}\n",
	})
	_, out, _ := runReport(t, "search", "-query", "repeat", "-limit", "1", root)
	if !strings.Contains(out, "`repeat`") || strings.Contains(out, "repeatSuffix") {
		t.Errorf("完全一致の囲み付きの名前が、前方一致より後ろになった: %q", out)
	}
}
