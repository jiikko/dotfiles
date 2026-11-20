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

	// 標準入力を分ける区切りの行の書き方の案内と、書き損じの文 (本家の _STDIN_FORM・_BAD_MARKER*)。
	stdinForm = "<<<yomiyasu:original>>>\n（書き直す前の原文）\n<<<yomiyasu:final>>>\n（最終稿）\n" +
		"新規作成では <<<yomiyasu:original>>> の代わりに、資料ごとに <<<yomiyasu:material>>> の行と資料を書きます。"
	markerHead   = "<<<yomiyasu:"
	markerShown  = 60
	badMarkerKey = "決まった区切りの行と一字一句同じではありません"
	badMarker    = "%d行目の「%s」が、" + badMarkerKey +
		"。資料は <<<yomiyasu:material 出典>>>（出典は200字まで）、原文と最終稿は出典なしで書いてください"
	badMarkerTail = "本文に <<<yomiyasu: で始まる行があるときは、ファイルに保存して --source（新規作成では --material）と" +
		" --final で渡してください。"
	stdinSplitError = "--stdin の本文の区切りが正しくありません（%s）。行全体が区切りになるように、次の形で渡してください。\n%s"
	noStdinError    = "Error reading stdin: 標準入力を読めません（標準入力がありません）"
	finalNotRegular = "Error opening file %s: 通常のファイルではありません（パイプなど。検査のあとで読み直せません）。" +
		"標準入力から渡すときは --final - を使ってください"
	pathProblemError = "Error opening file %s: %s。%s"
)

// pathHints は _PATH_HINTS (読まないファイルを渡されたときの、subcommand ごとの案内)。
var pathHints = map[string]string{
	"run": "このファイルを検査するときは、--stdin の区切りの行（<<<yomiyasu:original>>>、<<<yomiyasu:final>>> など）で" +
		"標準入力から渡すか、ファイルの名前の代わりに - を渡してください（- は1か所だけ）",
	"verify": "このファイルを照らすときは、ファイルの名前の代わりに - を渡して、標準入力から渡してください（--final と --source で1か所だけ）",
	"start":  "start は標準入力を使えません。作業フォルダの中の、名前が点で始まらないファイルを渡してください",
	"export": "export は標準入力を使えません。作業フォルダの中の、名前が点で始まらないファイルを渡すか、verify --final - で照らしてください",
}
