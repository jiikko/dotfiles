package swift

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// scopeKinds は、レビューで見つけた形から積み上げた一覧ではなく、文法の節を全部判定した結果で決める。
// 中に宣言を持ちうる節 (宣言を作る節を子に持てる節) を文法の node-types.json から全部挙げ、下の表のどれかに分類する。
// 文法の版を上げて節の種類が増えると、分類されていない節としてここが赤になる (scope にするかを決めてから足す)。
var scopeKindDecisions = map[string]string{
	// scope: 中で効いた宣言を、出るときに外す
	"statements": "scope", "lambda_literal": "scope", "for_statement": "scope", "while_statement": "scope",
	"switch_entry": "scope", "catch_block": "scope", "if_statement": "scope", "guard_statement": "scope",
	"function_declaration": "scope", "class_body": "scope", "enum_class_body": "scope",
	"didset_clause": "scope", "willset_clause": "scope", "init_declaration": "scope", "subscript_declaration": "scope",
	// 素通り: 宣言は中の scope の節 (block・closure・型の本体) を通してしか持たない。宣言を包むだけの節も同じ
	"function_body": "pass", "do_statement": "pass", "switch_statement": "pass", "switch_pattern": "pass",
	"class_declaration": "pass", "capture_list": "pass", "lambda_function_type": "pass",
	"computed_property": "pass", "computed_getter": "pass", "computed_setter": "pass", "computed_modify": "pass",
	"willset_didset_block": "pass", "property_declaration": "pass", "pattern": "pass", "capture_list_item": "pass",
	"lambda_function_type_parameters": "pass",
	// repeat-while の条件の束縛は Swift では書けない (文法が許すだけ)。本体は statements で閉じる
	"repeat_while_statement": "pass",
	// 関数の中に書けない (単位の本体に現れない)
	"source_file": "outside", "protocol_declaration": "outside", "protocol_body": "outside",
	"protocol_function_declaration": "outside", "protocol_property_declaration": "outside",
	"macro_declaration": "outside", "macro_definition": "outside",
}

// declMakers は、子に持つと「中に宣言を持ちうる」と数える節 (宣言そのもの・宣言を中に持つ節)。
// swift.go の nameRole が RoleDecl を返す親の節 (lambda_parameter・parameter・didset / willset・capture_list_item・pattern) を
// 足したら、ここにも足す (足さないと、その節を持つ親が分類を求められない)。
var declMakers = map[string]bool{
	"statements": true, "property_declaration": true, "value_binding_pattern": true, "lambda_parameter": true, "parameter": true,
	"pattern": true, "function_declaration": true, "class_declaration": true, "protocol_declaration": true,
	"typealias_declaration": true, "init_declaration": true, "subscript_declaration": true, "catch_block": true,
	"switch_entry": true, "didset_clause": true, "willset_clause": true, "computed_getter": true, "computed_setter": true,
	"computed_modify": true, "enum_class_body": true, "class_body": true, "protocol_body": true, "lambda_literal": true,
	"capture_list_item": true, "lambda_function_type_parameters": true,
}

func grammarNodeTypes(t *testing.T) []byte {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/alex-pinkus/tree-sitter-swift").Output()
	if err != nil {
		t.Fatalf("文法の module の置き場所を引けない: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "src", "node-types.json"))
	if err != nil {
		t.Fatalf("node-types.json を読めない: %v", err)
	}
	return b
}

func TestScopeKindsCoverEveryGrammarNodeThatHoldsDeclarations(t *testing.T) {
	var types []struct {
		Type     string                                             `json:"type"`
		Named    bool                                               `json:"named"`
		Fields   map[string]struct{ Types []struct{ Type string } } `json:"fields"`
		Children *struct{ Types []struct{ Type string } }           `json:"children"`
	}
	if err := json.Unmarshal(grammarNodeTypes(t), &types); err != nil {
		t.Fatal(err)
	}
	var holders, unclassified []string
	for _, nt := range types {
		if !nt.Named {
			continue
		}
		var kids []string
		for _, f := range nt.Fields {
			for _, k := range f.Types {
				kids = append(kids, k.Type)
			}
		}
		if nt.Children != nil {
			for _, k := range nt.Children.Types {
				kids = append(kids, k.Type)
			}
		}
		holds, onlyLambda := false, true
		for _, k := range kids {
			if declMakers[k] {
				holds = true
				onlyLambda = onlyLambda && k == "lambda_literal"
			}
		}
		if !holds {
			continue
		}
		holders = append(holders, nt.Type)
		// 宣言を中の closure を通してしか持たない式の節は素通り (closure が scope)
		if _, ok := scopeKindDecisions[nt.Type]; !ok && !onlyLambda {
			unclassified = append(unclassified, nt.Type)
		}
	}
	if len(holders) < 50 {
		t.Fatalf("中に宣言を持ちうる節が %d 種類しか見つからない (node-types.json の読み方が壊れている)", len(holders))
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Errorf("scope にするかを決めていない節: %v (scopeKindDecisions に分類を足し、scope なら scopeKinds にも足す)", unclassified)
	}
	for kind, decision := range scopeKindDecisions {
		if (decision == "scope") != scopeKinds[kind] {
			t.Errorf("%s: 判定は %s なのに scopeKinds にあるか = %v", kind, decision, scopeKinds[kind])
		}
	}
	for kind := range scopeKinds {
		if scopeKindDecisions[kind] != "scope" {
			t.Errorf("scopeKinds の %s が判定の表で scope になっていない", kind)
		}
	}
}
