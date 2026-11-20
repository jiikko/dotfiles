// Package check は ja-writing check (lint と diff をまとめて実行して記録を残す) の Main と、その部品を持つ。
// Main は run・verify (--replay と --since を含む) と、subcommand の前の -h・--version を写した (start・export・end はまだ)。部品は本家 yomiyasu_check.py (v3.1.0) の関数を、本家と同じ答えを返すように写したもの。
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
//	_write_atomic → WriteAtomic / _ensure_state → EnsureState / _write_problem → WriteProblem / _read_record_status → ReadRecordStatus / _read_record → ReadRecord
//	_mkstemp・_replace・_raise_if_hidden・_still_placed・_place_new_file・_write_state_files・_add_state_files は同じ名前の非公開の関数 (_raise_if_hidden → hiddenError)
//	_Input → Input / _read_stdin_bytes → ReadStdinBytes / _path_problem → PathProblem / _read_inputs → ReadInputs (引数は InputArgs)
//	_Target → Target / _read_target → ReadTarget
//	_make_input・_read_named・_material_marker・_split_material・_split_stdin は同じ名前の非公開の関数
//	_record_shape → RecordShape / _fact_gaps_shape → FactGapsShape / _words_shape → WordsShape (検査の項目は record_shape.go の recordRules の表)
//	_mode_checks・_record_treated・_known_value → modeChecks・recordTreated・knownValue
//	_TOOL_NAMES → ToolNames / _LIST_NAMES → ListNames / _FACT_GAPS_KEY → FactGapsKey / _DEFAULT_TIMEOUT → DefaultTimeout / _GITIGNORE → GitignoreText
//	verify (verify.go・verify_summary.go・verify_cli.go): _Found → found / _find → find / _verify_record → verifyRecord / _verify → runVerify・verifyOutcome /
//	_summary_problems → summaryProblems / _kept_marks → keptMarks / _record_kept_marks → recordKeptMarks / _kept_marks_text → keptMarksText (使う語なし) /
//	_file_sha256 → fileSHA256 / _label → label / _tool_changed_line → toolChangedLine / _verified_contents_line → verifiedContentsLine。ほかの verify の関数は同じ名前の非公開の関数
//	(_index_found → indexFound・_by_path_entries → byPathEntries・_origin → originOf など。名前の対応は各関数の doc コメント)
//
//	run (run_link.go・children.go・run_checks.go・run_output.go・run_save.go・run_cli.go): _Children → children / _Context → runContext /
//	_run_impl → runImpl / _save → saveRun / _decide → decide / _sections → sections / _utc_now・time.monotonic・os.urandom → Runtime の Now・Monotonic・RandomHex。
//	ほかの run の関数は同じ名前の非公開の関数 (_link → linkRun・_history → history など。名前の対応は各関数の doc コメント)
//
//	verify --replay (verify_replay.go): _replay → replayer.run / _ReplayError → replayError (_Material は本家でも使われていないので写さない)
//
// まだ写していない部品 (後の本が足す): _skill_version (SKILL.md を持たないので使わない)・start の登録 (armed/)。
package check
