package oracle

// CheckBaseFile は本家 yomiyasu_check.py の小道具・パスと状態フォルダの場所・似かた・tools の欄の関数の記録 (testdata/golden/ の中)。
// 形は visibility.json と同じ (case ごとの呼び出しの digest の列)。issue 1077 の 1 本目 (部品)。
const CheckBaseFile = "check-base.json"

// CheckModule は記録の対象の本家の module。
const CheckModule = "yomiyasu_check"

// CheckBaseGolden は check-base.json の中身。
type CheckBaseGolden = VisibilityGolden

// CheckBaseFunctions は記録の対象の関数 (module は CheckModule)。記録の件数が 0 の名前と、一覧にない名前は、記録の検査が落とす。
var CheckBaseFunctions = []string{
	"_sha256", "_text_sha256", "_parse_iso", "_iso", "_stamp", "_since",
	"_run_id_problem", "_run_id_case_problem", "_record_digest", "_status_line", "_host_value", "_fail_on", "_timeout",
	"_getcwd", "_abspath", "_disk_path", "_case_insensitive", "_place", "_same_path", "_same_file", "_home_path", "state_dir_for", "_state_dir", "_state_dir_given",
	"_compact", "_similarity", "_source_like",
	"_tool_versions", "_list_sha256", "_skill_info",
}

// CheckBaseConstants は Go の値と比べる本家の定数 (Get で読む)。
var CheckBaseConstants = []string{"_DEFAULT_TIMEOUT", "_SIMILAR_TO_SOURCE", "_TOOL_NAMES", "_LIST_NAMES", "_FACT_GAPS_KEY", "_GITIGNORE"}

func ccall(fn string, args ...any) Call {
	return Call{Module: CheckModule, Function: fn, Args: args, Digest: true}
}

func ckeep(c Call) Call { c.Keep = true; return c }

func cfold(c Call) Call { c.Folded = true; return c }

func csave(c Call, name string) Call { c.Save = name; return c }

func cget(name string) Call {
	return Call{Module: CheckModule, Function: name, Get: true, Digest: true, Keep: true}
}

// CheckBaseCases は check-base.json の case。
func CheckBaseCases() []VisCase {
	var cases []VisCase
	cases = append(cases, checkConstantCases()...)
	cases = append(cases, checkValueCases()...)
	cases = append(cases, checkTimeCases()...)
	cases = append(cases, checkOptionCases()...)
	cases = append(cases, checkSimilarCases()...)
	cases = append(cases, checkToolCases()...)
	cases = append(cases, checkPathCases()...)
	return cases
}

func checkConstantCases() []VisCase {
	calls := make([]Call, len(CheckBaseConstants))
	for i, name := range CheckBaseConstants {
		calls[i] = cget(name)
	}
	return []VisCase{{Name: "check/constants", Calls: calls}}
}
