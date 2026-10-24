package swift

// 文法の名前つきの node の種類の分類。文法の全部の種類がどれか 1 つに入る (TestEveryKindIsClassified が文法と突き合わせる。
// 文法の版を上げて種類が増えたら、そこで落ちる)。

// exprKinds は L3 で畳める式の種類。
var exprKinds = set(
	"_expression", "additive_expression", "array_literal", "as_expression", "await_expression", "bitwise_operation",
	"call_expression", "check_expression", "comparison_expression", "conjunction_expression", "constructor_expression",
	"consume_expression", "dictionary_literal", "directly_assignable_expression", "disjunction_expression",
	"equality_expression", "fully_open_range", "infix_expression", "key_path_expression", "key_path_string_expression",
	"lambda_literal", "line_string_literal", "macro_invocation", "multi_line_string_literal", "multiplicative_expression",
	"navigation_expression", "nil_coalescing_expression", "nil_literal", "open_end_range_expression",
	"open_start_range_expression", "playground_literal", "postfix_expression", "prefix_expression", "range_expression",
	"raw_string_literal", "selector_expression", "self_expression", "special_literal", "super_expression",
	"ternary_expression", "try_expression", "tuple_expression", "unsafe_expression", "value_pack_expansion",
)

// literalKinds はリテラルの葉と、その種類 (L2 で同じ種類どうしを同じ記号にする)。
var literalKinds = map[string]string{
	"integer_literal": "num", "real_literal": "num", "hex_literal": "num", "bin_literal": "num", "oct_literal": "num",
	"boolean_literal": "bool",
	"line_str_text":   "str", "multi_line_str_text": "str", "raw_str_part": "str", "str_escaped_char": "str",
	"regex_literal": "regex",
}

// nameKinds は名前の葉。役割 (参照・宣言・メンバー・ラベル) は木の中の位置で決める。
var nameKinds = set("simple_identifier")

// typeKinds は型の位置の名前。
var typeKinds = set("type_identifier")

// commentKinds は捨てる種類。
var commentKinds = set("comment", "multiline_comment", "shebang_line", "diagnostic")

// structKinds は構造だけの種類 (種類だけをハッシュに入れる)。
var structKinds = set(
	"array_type", "as_operator", "assignment", "associatedtype_declaration", "attribute", "availability_condition",
	"bang", "bracket_qualified_type", "call_suffix", "capture_list", "capture_list_item", "catch_block", "catch_keyword", "class_body",
	"class_declaration", "computed_getter", "computed_modify", "computed_property", "computed_setter",
	"constructor_suffix", "control_transfer_statement", "custom_operator", "default_keyword", "deinit_declaration",
	"deprecated_operator_declaration_body", "dictionary_type", "didset_clause", "directive", "discard_statement", "do_statement",
	"else", "enum_class_body", "enum_entry", "enum_type_parameters", "equality_constraint", "existential_type",
	"external_macro_definition", "for_statement", "function_body", "function_declaration", "function_modifier",
	"function_type", "getter_specifier", "guard_statement", "identifier", "if_statement", "import_declaration",
	"inheritance_constraint", "inheritance_modifier", "inheritance_specifier", "init_declaration",
	"interpolated_expression", "lambda_function_type", "lambda_function_type_parameters", "lambda_parameter",
	"macro_declaration", "macro_definition", "member_modifier", "metatype", "modifiers", "modify_specifier",
	"mutation_modifier", "navigation_suffix", "opaque_type", "operator_declaration", "optional_type",
	"ownership_modifier", "parameter", "parameter_modifier", "parameter_modifiers", "pattern",
	"precedence_group_attribute", "precedence_group_attributes", "precedence_group_declaration",
	"property_behavior_modifier", "property_declaration", "property_modifier", "protocol_body",
	"protocol_composition_type", "protocol_declaration", "protocol_function_declaration",
	"protocol_property_declaration", "protocol_property_requirements", "raw_str_continuing_indicator",
	"raw_str_end_part", "raw_str_interpolation", "raw_str_interpolation_start", "repeat_while_statement",
	"setter_specifier", "source_file", "statement_label", "statements", "subscript_declaration",
	"suppressed_constraint", "switch_entry", "switch_pattern", "switch_statement", "throw_keyword", "throws",
	"throws_clause", "try_operator", "tuple_type", "tuple_type_item", "type_annotation", "type_arguments",
	"type_constraint", "type_constraints", "type_modifiers", "type_pack_expansion", "type_parameter",
	"type_parameter_modifiers", "type_parameter_pack", "type_parameters", "typealias_declaration", "user_type",
	"value_argument", "value_argument_label", "value_arguments", "value_binding_pattern", "value_parameter_pack",
	"visibility_modifier", "where_clause", "where_keyword", "while_statement", "wildcard_pattern",
	"willset_clause", "willset_didset_block",
)

func set(ks ...string) map[string]bool {
	m := make(map[string]bool, len(ks))
	for _, k := range ks {
		m[k] = true
	}
	return m
}
