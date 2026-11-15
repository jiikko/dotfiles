package check

// 人が読む文の定数。本家の yomiyasu_check.py の定数と同じ字 (記録との突き合わせが字を比べる)。{} の所は fmt の %s に置き換えた。

const (
	// DefaultTimeout は --timeout を省いたときの子プロセスの検査に使える秒数の合計。
	DefaultTimeout = 90

	// GitignoreText は状態フォルダに置く .gitignore の中身 (記録を git に入れない)。
	GitignoreText = "# yomiyasu の検査記録。git に入れない。\n*\n"

	runIDError = "--run-id に使えない値です: %s。英数字で始まり、英数字と「.」「_」「-」だけの64字までにしてください。" +
		"末尾の「.」と、Windows で使えない名前（CON、NUL、COM1 など）は使えません。"
	runIDCaseError = "--run-id %s は、すでにある実行ID %s と大文字と小文字だけが違います。同じ綴りで指定するか、" +
		"別の実行IDにしてください。"
	emptyShown        = "（空）"
	failOnPrefix      = "--fail-on に使えない値です: %s。check で使えるのは meaning-risk、warn、error です。"
	timeoutError      = "--timeout には0より大きい秒数を渡してください: %s"
	sinceErrorPrefix  = "--since には時差つきの ISO 8601 の時刻を渡してください（例 2026-10-13T01:45:00Z、2026-10-13T10:45:00+09:00）: %s"
	noWorkingDirError = "yomiyasu_check.py: 作業ディレクトリを読めません（%s）。存在するフォルダに移ってから実行してください。"
)
