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

// verify の 1 行目 (本家の _VERIFY_LINES)。
var verifyLines = map[string]string{
	"executed": "この最終稿に必要な検査（%s）を実行した記録があり、最終稿のハッシュが記録と合いました。" +
		"合格の判定ではありません。完了報告を付けるときは、この出力の2行目を写してください。",
	"no_record": "この最終稿の検査記録がありません。yomiyasu_check.py run を実行してください。" +
		"実行できないときは、完了報告に実行しなかった検査として書いてください。",
	"final_changed": "最終稿が、最後に検査した本文と違います。本文を変えたなら yomiyasu_check.py run をもう一度実行し、" +
		"そのあと verify を実行してください。",
	"wrong_file":   "この実行IDの記録は別のファイルのものです。この最終稿で yomiyasu_check.py run を実行してください。",
	"other_source": "記録の原文が、指定した原文と違います。正しい原文で yomiyasu_check.py run を実行してください。",
	"ambiguous":    "同じ最終稿に、別の原文で作った記録が複数あります。--run-id か --source を付けて verify を実行してください。",
	"invalid_record": "記録の形が正しくありません（手で書いた記録や壊れた記録は使えません）。" +
		"yomiyasu_check.py run を実行してください。",
	"stale_record":     "指定した時刻より前の記録です。yomiyasu_check.py run をもう一度実行してください。",
	"incomplete":       "記録に %s の結果がありません。yomiyasu_check.py run をもう一度実行してください。",
	"tool_changed":     "記録のあとで検査スクリプトが変わりました。yomiyasu_check.py run をもう一度実行してください。",
	"not_reproducible": "記録のレポートを今のスクリプトで作り直せません。yomiyasu_check.py run をもう一度実行してください。",
}

// verify の人が読む文 (本家の同じ名前の定数。先頭の _ を外して小文字で始めた)。
const (
	listChangedLine          = "記録のあとで、検査に使う語の一覧（%s）が変わりました。yomiyasu_check.py run をもう一度実行してください。"
	scriptAndListChangedLine = "記録のあとで、検査スクリプトと、検査に使う語の一覧（%s）が変わりました。" +
		"yomiyasu_check.py run をもう一度実行してください。"
	replayTimeoutLine = "記録のレポートを作り直す途中で時間切れになりました（%s）。--timeout を長くして verify --replay を" +
		"もう一度実行してください。"
	lineEndingsNote      = "改行コードだけが違います（git の autocrlf やエディタの設定で変わることがあります）。"
	textSameVerifyNote   = "本文は記録と同じで、改行コード、BOM、末尾の改行だけが違います。run をもう一度実行しても修正には数えません。"
	ambiguousMaterials   = "同じ最終稿に、資料の集まりが違う記録が複数あります。どの検査の記録を使うかを決められないので、--run-id を付けて verify を実行してください。"
	ambiguousPlaceLine   = "この最終稿の記録と、このパスで最後に検査した記録とで、原文が違います。どちらの記録を使うかを決められないので、--run-id か --source を付けて verify を実行してください。"
	verifyPartialLine    = "記録の %sで終わっています。run をもう一度実行しても同じなら、完了報告に run の出力の2行目を写し、「実行しなかった検査」に「%s」と書いてください。"
	verifyEmptyLine      = "最終稿が空です。書き終えた最終稿を渡してください。"
	verifyEmptyRecord    = "記録: なし（最終稿が空のため、探していません）"
	noRecordLine         = "記録: なし（探した状態フォルダ: %s）"
	newerAttemptNote     = "この実行には、あとで検査した別の本文があります。直しが失われていないか確かめてください。"
	recordedSourceNote   = "渡した本文は、記録では原文か資料として使われています。--final には最終稿を渡してください。"
	keptMarkVerify       = "残した指摘の語: %s。完了報告を付けるときは、「書き手に確かめたい点」で尋ねたか（3点を超える分は「ほかに N か所」に数える）、残す理由を「残したAIっぽいところ」に書いたかを確かめてから提出してください。"
	verifyStateDirEmpty  = "--state-dir に空の値は使えません"
	verifyStdinTwice     = "標準入力（-）は1か所だけに使えます"
	replayNotImplemented = "yomiyasu_check.py: 内部エラー: --replay はまだ実装していません（ja-writing）。"
)

// failedNames は _FAILED_NAMES (記録の検査が終わらなかった状態の、人が読む名前)。
var failedNames = map[string]string{"timeout": "時間切れ", "error": "内部エラー"}

// checkLabels は _CHECK_LABELS (人が読む文に書く検査名)。
var checkLabels = map[string]string{"lint": "lint", "diff": "diff", "materials": "資料との照合"}

// label は _label。
func label(name string) string {
	if l, ok := checkLabels[name]; ok {
		return l
	}
	return name
}
