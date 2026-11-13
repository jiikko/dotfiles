package oracle

// G20 (project_term) の語の一覧の組み立てと、flag.txt の語の当て方の記録。パターン (flag-patterns.txt) 側は lint-raw の words の文脈が比べる。

// lintProjectWordLists は _flag_pattern に通す語の並び (長い語を先にする・英数字の端の境目・漢字と仮名の両方を含む語・重なりの除去)。
var lintProjectWordLists = [][]any{
	{}, {"サーバ"}, {"サーバ", "サーバー", "サ"}, {"a", "ab", "abc", "ab"}, {"user", "end user"}, {"x.y", "a/b", "_t", "9", "v1.2"},
	{"根の", "の木", "公開鍵", "大根の", "ソフト、", "ね根"}, {"日本語ABC", "ABC日本語", "漢字かな", "かな漢字", "カナ漢", "漢カナ", "ー漢", "漢ー"}, {"ヵ", "々", "〆切", "切〆", "龥", "一る"},
	{"API", "api", "Api"}, {"fingerprint", "指紋"},
}

// lintProjectBoundedCases は _replacement_bounded に通す (text, start, 使う語)。
var lintProjectBoundedCases = []struct {
	text        string
	start       int
	replacement string
}{
	{"end user", 0, "end user"}, {"backend user", 4, "end user"}, {"backend user", 0, "end user"}, {"the end user", 4, "end user"}, {"the end users", 4, "end user"},
	{"end_user", 0, "end user"}, {"end user_x", 0, "end user"}, {"end user.x", 0, "end user"}, {"end user x", 0, "end user"}, {"x-end user", 2, "end user"}, {"x/end user", 2, "end user"},
	{"サーバー", 0, "サーバー"}, {"aサーバー", 1, "サーバー"}, {"RDB", 0, "RDB"}, {"xRDB", 1, "RDB"}, {"RDBx", 0, "RDB"}, {"RDB-1", 0, "RDB"}, {"RDB-x", 0, "RDB"}, {"RDB.x", 0, "RDB"},
	{"RDB\\x", 0, "RDB"}, {"x\\RDB", 2, "RDB"}, {"1RDB", 1, "RDB"}, {"_RDB", 1, "RDB"}, {"RDB_", 0, "RDB"}, {"", 0, "あ"}, {"あ", 0, "あ"},
}

// lintProjectMessageCases は _project_term_message に通す (語, パターンの名前, flag.txt の語 (null は未指定), 使う語)。
var lintProjectMessageCases = []struct {
	words, patterns []any
	flagHits        any
	replacements    any
}{
	{[]any{"サーバ"}, []any{}, nil, nil}, {[]any{"サーバ"}, []any{}, []any{"サーバ"}, nil}, {[]any{"サーバ"}, []any{}, []any{"サーバ"}, map[string]any{"サーバ": "サーバー"}},
	{[]any{"鍵"}, []any{"鍵"}, nil, nil}, {[]any{"鍵"}, []any{"鍵"}, []any{}, nil}, {[]any{"鍵"}, []any{"鍵"}, []any{"鍵"}, nil},
	{[]any{"サーバ", "三点リーダ"}, []any{"三点リーダ"}, []any{"サーバ"}, map[string]any{"サーバ": "サーバー"}}, {[]any{"サーバ", "三点リーダ"}, []any{"三点リーダ"}, nil, nil},
	{[]any{"サーバ", "指紋", "x"}, []any{}, []any{"サーバ", "指紋", "x"}, map[string]any{"サーバ": "サーバー", "指紋": "fingerprint", "ほか": "y"}},
	{[]any{"サーバ", "指紋"}, []any{}, []any{"サーバ", "指紋"}, map[string]any{"指紋": "fingerprint"}}, {[]any{"同名"}, []any{"同名"}, []any{"同名"}, map[string]any{"同名": "同じ名"}},
	{[]any{"a", "同名"}, []any{"同名"}, []any{"a", "同名"}, nil}, {[]any{"a", "同名"}, []any{"同名"}, []any{}, map[string]any{"a": "b"}},
}

// lintProjectWords は _project_term_findings に渡す語の一覧 (flag と flag_replacements だけ。パターンは lint-raw が比べる)。
var lintProjectWords = []map[string]any{
	{},
	{"flag": []any{}},
	{"flag": []any{"サーバ", "指紋", "user", "DB", "根の", "の木", "公開鍵", "v1.2", "API", "ログ"}},
	{"flag": []any{"サーバ", "指紋", "user", "DB", "根の", "の木", "公開鍵", "v1.2", "API", "ログ"},
		"flag_replacements": map[string]any{"サーバ": "サーバー", "指紋": "fingerprint", "user": "end user", "DB": "RDB", "API": "Web API", "ログ": "ログ出力", "v1.2": "v1.2.3"}},
	{"flag": []any{"サーバ"}, "flag_replacements": map[string]any{"サーバ": "サーバ"}},
}

const lintProjectDoc = "サーバーとサーバと指紋。\n\nend user と user と enduser と backend user と user_id と user.name と x/user と user-1。\n\nRDB と DB と xRDB と RDBx と 1DB と DB1。\n\n" +
	"根の木 と 大根の木構造 と の木 と 大根の と 根の。\n\n公開鍵 と 公開鍵暗号 と 非公開鍵。\n\nv1.2 と v1.2.3 と xv1.2 と v1.2x。\n\nWeb API と API と api と APIs と my_API。\n\nログ出力 と ログ と ログイン。\n\n" +
	"「サーバ」は引用。 『指紋』と [DB] と サーバ。\n\n> サーバの引用\n\n`サーバ` と [サーバ](http://x/サーバ) と <https://a/user>。\n\n```\nサーバ user DB\n```\n\n# サーバの見出し\n\n- user の箇条\n\n| サーバ | 指紋 |\n| - | - |\n\n" +
	"<div>サーバ</div>\n\nサーバー サーバー サーバ サーバ。\n\n---\ntitle: サーバ\n---\n"

func lintProjectCases() []VisCase {
	rec := recorder{true}
	var calls []Call
	for _, words := range lintProjectWordLists {
		calls = append(calls, rec.call("_flag_pattern", words))
	}
	var bounded []Call
	for _, c := range lintProjectBoundedCases {
		bounded = append(bounded, rec.call("_replacement_bounded", c.text, c.start, c.replacement))
	}
	var messages []Call
	for _, c := range lintProjectMessageCases {
		messages = append(messages, rec.call("_project_term_message", c.words, c.patterns, c.flagHits, c.replacements))
	}
	docCalls := []Call{lanalyze(lintProjectDoc, "a")}
	for _, words := range lintProjectWords {
		docCalls = append(docCalls, rec.call("_project_term_findings", Ref("a"), words))
	}
	return []VisCase{{Name: "project/flag-pattern", Calls: calls}, {Name: "project/bounded", Calls: bounded}, {Name: "project/message", Calls: messages}, {Name: "project/findings", Calls: docCalls}}
}
