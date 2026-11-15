// Package check は ja-writing check (lint と diff をまとめて実行して記録を残す) の Main と、その部品を持つ。
// Main はまだ stub。部品は本家 yomiyasu_check.py (v3.1.0) の関数を、本家と同じ答えを返すように写したもの。
//
// 本家の名前と Go の名前の対応 (本家の名前は先頭の _ を持つ非公開のもの。state_dir_for だけ公開):
//
//	_sha256 → SHA256 / _text_sha256 → TextSHA256 / _iso → ISO / _stamp → Stamp / _parse_iso → ParseISO / _since → Since
//	_run_id_problem → RunIDProblem / _run_id_case_problem → RunIDCaseProblem / _record_digest → RecordDigest
//	_status_line → StatusLine (引数は Status) / _host_value → HostValue / _fail_on → FailOn / _timeout → Timeout
//	_getcwd → Getcwd / _abspath → AbsPath / _disk_path → DiskPath / _case_insensitive → CaseInsensitive / _place → Place
//	_same_path → SamePath / _same_file → SameFile / _home_path → HomePath
//	state_dir_for → StateDirFor / _state_dir → StateDir / _state_dir_given → StateDirGiven
//	_compact → Compact / _similarity → Similarity / _source_like → SourceLike (しきい値 _SIMILAR_TO_SOURCE → SimilarToSource)
//	_tool_versions → ToolVersions / _list_sha256 → ListSHA256 / _skill_info → SkillInfo
//	_TOOL_NAMES → ToolNames / _LIST_NAMES → ListNames / _FACT_GAPS_KEY → FactGapsKey / _DEFAULT_TIMEOUT → DefaultTimeout / _GITIGNORE → GitignoreText
//
// まだ写していない部品 (後の本が足す): _utc_now・_file_sha256・_label・_skill_version (SKILL.md を持たないので使わない)・_materials_format_version・_version_line。
package check

import (
	"context"

	"ja-writing/cmdenv"
)

// Main は subcommand の入口。終了コードの意味は本家の argparse の定義に合わせる。
func Main(_ context.Context, env cmdenv.Env) int {
	return cmdenv.Unimplemented(env, "check")
}
