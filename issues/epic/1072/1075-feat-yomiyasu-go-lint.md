# 1075 (feat): yomiyasu の lint (40 規則) を Go で作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-12〜）

起票日: 2026-11-12

## 概要

本家の `yomiyasu_lint.py` (6,398 行) を `ja-writing lint` として作り直す。AI が書いた日本語の型と Markdown の書式の型を、直す候補として出す道具。
否定対比・前置き・比喩動詞・直訳語・体言止めの連続・装飾記号など、2026-11-12 の照合で dotfiles に入っていなかった型の多くがここにある。

## 本家が持つもの (v3.1.0)

- 規則の ID は 40 ある。どれも出力の `"rule"` の値になる
  - 名前で書かれた 31: `ai_phrase` `bold_label_list` `bold_not_rendered` `calque` `cleft_list_ending` `dialogue_period` `emoji_prohibited` `excess_bold`
    `excess_list` `fragment_run` `heading_colon` `instruction_residue` `katakana_term` `meta_filler` `metaphor_label` `metaphor_verb` `narrative_density`
    `negative_parallelism` `negative_parallelism_density` `pause_comma` `project_term` `redundant_bracket` `renyo_noun` `sentence_end_repetition`
    `short_mochiron` `short_summary_heading` `slide_comma` `slop_vocabulary` `thin_evaluation` `trailing_colon` `translated_term`
  - 表から登録される 9: `dash_decoration` `dash_insertion` `dash_list_ending` (`_dash_finding`)、`say_hedge` `copy_comma_noun` `bare_verb_chain`
    (`_SHAPE_RULE_MESSAGES`)、`summary_restatement` `meta_intro` `count_declaration` (`INFO_SENTENCE_PATTERNS`)
  - 重さは error・warn・info に分かれる。info は `--strict` と `--fail-on` に入れない
- オプション: `--json` `--strict` `--fail-on` `--doc-type` `--reader` `--words` `--no-words` `--version`
- 文書の種類 (`--doc-type`) と読み手 (`--reader`) で、外す規則と重さを変える
- 語の一覧のうち、`allow.txt` で候補を外し、`flag.txt` と `flag-patterns.txt` で候補を足す
- 出力は直す候補と件数と「意味の保持と分かりやすさは判定していない」の注意。`--json` は `LINT_FORMAT_VERSION = 2` の形を `indent=2` で出す。
  点数 (`score`) と `is_clean` は出さない
- `katakana_term`・`thin_evaluation`・`renyo_noun` の語は、スクリプトの隣の `../references/lists/<名前>.txt` を読む。読めなければ内蔵の一覧を使う
- 絵文字の判定は Unicode 18.0 の emoji-data などから作った表を持つ (実行時にファイルを読まない)。diff がこの数え方 (`_emoji_matches`) を借りる

## 対応方針

- 規則を 1 つずつ写す。規則ごとに、本家の関数・語の一覧・正規表現と Go の対応を表にして README に置く (本家の更新を追うときに引く)
- 6,398 行を 1 回で写さず、規則の群ごとに分けて commit する。群は、語の一覧で当てる規則 / 文の構造を見る規則 / Markdown の書式の規則 / 文書全体の密度の規則の 4 つ。
  各 commit は、その群の規則について 1073 の記録と一致する状態で入れる
- 規則ごとに似た形の関数が並ぶと dupscan の gate (節の下限 12) に当たるので、形の同じ規則は、本家の表 (`INFO_SENTENCE_PATTERNS` など) と同じく表で駆動する
- 語の一覧のファイルは 1073 で埋め込んだものを使う
- diff が使う絵文字の数え方は、公開の関数にして、1073 の関数の単位の記録で本家と比べる
- 速さ: 本家は 3.0.1 で正規表現を初めて使うときに組む形にして起動を速くした。Go では compile を package の初期化に置くか遅らせるかを、
  hook (1078) の時間の上限から決める。dotfiles の文書 (Markdown 約 1,400 本) を通した所要を本家と Go で測る。
  起動の時間と処理の時間を分け、同じ入力を交互に複数回測る。数字は README に書く
- 本家は語の一覧の set から選択肢を組むので、パターンの文字列と選択肢の並びが `PYTHONHASHSEED` で変わる。lint を写すときは選択肢の並びを決まった順にし、本家の答えが seed に依らないことを複数の seed で確かめる
- regexp2 は後戻りする照合器なので、行が長い入力での最悪の所要を測る。入力は本家の `tests/test_lint_performance_regressions.py` の入力のほかに、
  後戻りが増える形 (入れ子の繰り返し・長い同じ字の並び) を本家の全パターンの記録から選んで作る。テストの合否には時間を使わず、試す数か結果で判定する

### 群と順 (移植の地図 2026-11-13 から。fable が本家を読んで作った)

本家の行番号は `yomiyasu_lint.py` (v3.1.0)。地図の全文は `tmp/yomiyasu-gap-2026-11-12/map-lint.md` (tmp なので消える。結論はこの節に移した)。

- **土台 (候補を出さない。先に入れる)**: T1 絵文字の表と `_emoji_matches` (diff も使う)・T2 文末の飾り・T3 文字の下ごしらえ (引用の伏せ・文の切り出し・`_rule9_context`)・
  T4 後処理と出力と CLI (種類で外す・allow・`_public_finding`・点数・text と `--json` の形・rc の決まり方)
- **検出の群** (括弧は規則):
  G1 指標 (excess_bold・excess_list)・G2 文末の反復 (sentence_end_repetition)・G3 太字 (bold_not_rendered。760 行で他に頼らない)・G4 絵文字 (emoji_prohibited)・
  G5 ダッシュ (dash_* の 3)・G6 見出し (heading_colon・short_summary_heading・redundant_bracket)・G7 文末のコロン (trailing_colon)・
  G8 語彙 (slop_vocabulary・metaphor_verb・translated_term)・G9 文の型 (meta_filler・summary_restatement・meta_intro・count_declaration)・G10 指示の残り (instruction_residue)・
  G11 短い文の型 (short_mochiron・fragment_run・cleft_list_ending)・G12 否定の対比 (negative_parallelism・negative_parallelism_density・bold_label_list)・G13 読点 (pause_comma)・
  G14 直訳語と語の一覧 (calque の型 1〜5・metaphor_label・katakana_term・thin_evaluation。正規表現 133)・G15 連用形の名詞 (renyo_noun)・G16 AI の言い回し (ai_phrase)・
  G17 スライドの読点 (slide_comma)・G18 文の形 (say_hedge・copy_comma_noun・bare_verb_chain)・G19 文書の型 (dialogue_period・narrative_density)・G20 語の一覧の語 (project_term)
- 群どうしの依存は 3 つだけ: G11 → G9 (cleft は filler に当たる文を外す)・G14 → G8 (同じ行の metaphor_verb・translated_term で calque を出さない)・G17 → G13。
  入れる順は T1 → T2 → T3 → T4 → G1〜G20 (G3 はどこでもよい)
- **群ごとに確かめる方法**: CLI の出力の全体は全部の群が揃うまで一致しないので、(1) 本家の lint の記録 (`--json`) を規則で絞り、入れた群の規則だけを比べる。
  ただし上の依存と、種類で外す・allow・ダッシュの上限は後処理なので、比べるのは後処理の前の検出の並びにする。(2) 本家の検出の生の並びは、`lint_text` が最後に呼ぶ
  `_project_term_findings` を driver が包み、その時点の `analysis["_findings"]` (内部の欄つき) を記録する (本家のファイルに手を入れずに包める。未確認)。(3) str だけを取る関数
  (`_emoji_matches`・`_sentence_end_type`・`_has_negative_parallelism`・`bold_problems` など) は関数の単位の記録で比べる
- **分担**: T1〜T4 と骨組み (群を呼ぶ順と、群ごとのファイル) は dotfiles-c5 が先に入れる。そのあと群を別のセッションに分けられる。G3 は [1083](done/1083-feat-ja-writing-lint-bold-not-rendered.md) に切り出した (dotfiles-24 が担当。2026-11-13)。G13 は [1084](done/1084-feat-ja-writing-lint-pause-comma.md) に切り出した (dotfiles-bb が担当。2026-11-13。str を受ける関数を先に作り、骨組みへのつなぎ込みは土台が入った後)

### G3 をつなぐ口 (1083 からの申し送り、dotfiles-24、2026-11-13)

- 骨組みから呼ぶのは `lint.BoldFindings(text string, a *visibility.Analysis) ([]lint.BoldFinding, error)`。`BoldFinding` は `Rule`・`Line`・`Severity`・`Message`・`Snippet` を持ち、本家の `lint_text` の 5577〜5584 行の dict と同じ値を入れる。候補の型が決まったら、骨組みの側でこの欄を写す
- 中身は `internal/bold.ProblemsWithAnalysis`。`internal/bold.Problems(text, skipFrontmatter)` が本家の `bold_problems` に当たり、diff (1076) からはこちらを呼ぶ
- visibility に公開の入口を 2 つ足した (`Escaped`・`HasPrefixAt`)。記録は `testdata/golden/bold.json` (本家の lint と diff に同じ入力を当てて 1,456 case が一致)。`go run ./oracle/cmd/golden -only bold` で作り直せる

### G13 をつなぐ口 (1084 からの申し送り、dotfiles-bb、2026-11-13)

- G13 は別の package `lint/pausecomma` (lint と同じ package に置くと、骨組みにつなぐまで unused が全部を落とすため)。つなぎ込みは骨組みの側 (dotfiles-c5) が G3 と一緒に行う
- `pausecomma.LineHits(a *visibility.Analysis, visible func(lineNo int) string, objects bool) map[int][]Hit` が本家の `_pause_comma_line_hits`。`visible` に T3 の `_prose_visible_text` を渡す。表にある行は当たりが無くても空の並びを持つので、`_, ok := m[line]` が本家の `line_no in pause_lines`
- `pausecomma.Fallback(plainText, objects) []Hit` は表に無い行で当て直す形。`objects` は `doc_type != "essay"`。`Hit` は `{Pos, Chunk, Phrase}`
- G16 は `pausecomma.TENotVerb`、G17 は `pausecomma.TEAttrObject()` (pyre の Pattern。本家と同じく `match(body, end)` で使う)
- 本家の `lint_text` は見出しと表の行を pause の節より前で飛ばす (5726・5729)。骨組みでこの順を崩すと Go だけが見出しに当てる。pause_comma は種類の指定なし・promo・script では当てないので、比べられる本家の記録は種類を付けた run だけ

### 答えがずれやすい所 (地図の 3 節の要点)

- set から選択肢を組む所 (2977・3990・4028・4033・4715 など) は、どれも答えに影響しないと読めた (同じ位置で同じ長さの語が同時に当たらない)。Go は (長さの降順, 字の順) で固定し、map を反復して出力を組まない
- `dict.fromkeys` の重なりの除去は挿入の順を保つ。Go は順を保つ helper を 1 つ作って使う
- **速さのための絞り込みが答えを変える所はそのまま写す**: katakana / thin / calque / renyo / ai_phrase / shape は、元の文字 (Markdown の記号を含む) に手掛かりを当てて行を絞る。
  `マイル**ストーン**` は見える文字ではカタカナ語になるが、元の文字に手掛かりが無いので本家は拾わない。Go も同じく拾わない
- 太字の `_BoldRepairIndex.decide` の速い道は、本家のコメントの主張 (解析し直したときと同じ答え) が未確認。Go で速い道を省くなら記録で確かめる
- 入力の集まりに足す形は地図の 4.3 節 (比喩動詞の残りの 7 型・SLOP_WORDS の残り 26 語・ダッシュ 5 行以上・`<details>` の入れ子・台詞の数と句点の割合の境界など)

## 受け入れ条件

- [ ] 40 規則すべてが入力の集まりで本家と同じ候補 (行・列・規則・重さ・文) を出す。`--json` の出力も字まで同じ
- [ ] 規則 ID ごとに、当たる入力と当たらない入力が入力の集まりにあり、その件数を記録から数える検査が通る (1073 の空振りを止める検査)
- [ ] rc が本家と同じ (`--strict`・`--fail-on` の組み合わせを含む)
- [ ] 語の一覧の読み込みと、`--doc-type` / `--reader` の切り替えが本家と同じ結果になる
- [x] 絵文字の数え方の公開の関数が、本家の `_emoji_matches` と同じ値を返す (`lint.EmojiMatches`。`lint-base.json` の入力の集まりの全行・全コードポイント・旗 676 組と、opus の敵対的レビューの乱数 9 万件で一致)
- [ ] dotfiles の文書を通した所要を、本家と Go の両方で測った数字がある
- [ ] 規則を 1 本ずつ外したとき (群ごとに最低 1 本)、記録との突き合わせが落ちることを確かめた (`mutate-verify`)
- [ ] `make test-dupscan-gate` が通る
- [ ] 1080 (done) の表で「Go で扱う」とした 76 項目のうちの 40 項目について、当てた規則の名前が本当にその型を拾うかを、lint の規則の実装と記録で確かめる
  (1080 は規則の名前を grep で当てただけ。lint の範囲でないもの (diff の検出) は 1076 で確かめる)

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_lint.py`・`skills/yomiyasu/references/lists/*.txt`・`docs/tools.md`
- 1074 (markdown_visibility を使う)・1076 (diff が絵文字の数え方を借りる)

## 進捗

- 2026-11-13 土台 T1〜T3 を入れた (`src/ja-writing/lint/`。候補はまだ出さない)。T4 (後処理・出力・CLI) と G1〜G20 は続き。
  - 作ったもの: 絵文字 (`EmojiMatches` が公開。表は `lint-base.json` の定数から `oracle.GenerateLintEmojiTables` が作る `emoji_tables_gen.go`)・文末の飾り (`endmark.go`)・文字の下ごしらえ (`prose.go`。解析と覚えは `document`)・
    初めて使うときに組む正規表現 (`lazyPattern`)・重なりを除いて順を保つ helper (`uniqueInOrder`。1 つ)。本家の名前との対応は各ファイルの冒頭と README の「lint の土台」。
  - 本家との突き合わせ: `testdata/golden/lint-base.json` (456 case・484,515 呼び出し・約 4.4 MB)。str を取る関数は入力の集まりの全行と手書きの文・全コードポイントの走査で、analysis を取る関数は driver で `analyze_markdown` を `save` して `Ref` で渡す
    (driver に `quiet` を足した。結果の記録が要らない呼び出しの digest を 8 つの 0 にする)。定数 21 個も記録して Go の値と比べる。
  - 本家で `analyze_markdown` が `AssertionError` になる 2 入力 (`html-script-long-s`・`html-style-long-s`) は、analysis を作れないので土台の記録から外した (visibility.json は例外として記録している)。
  - 実装をわざと壊して `bin/mutate-verify-list` で 22 本を確かめた (絵文字 4・文末の飾り 7・文字の下ごしらえ 10・重なりの除去 1)。20 本が想定の検査 (`TestLintBaseMatchesPythonRecording` か `TestUniqueInOrderKeepsFirstOccurrence`) で red、2 本は等価と判断した。
    20 本のうち 1 本 (`bracketOpensSentence` の 256 字の境目) は最初は緑のまま通ったので、境目の前後の入力 55 件を足して red にした。
  - 等価と判断した 2 本: (1) `decorationBreaks` の `!hasText ||`: `hasText` が偽のとき、分けた所から今の並びの始まりまでの字は空白だけなので、あとの `last == start` が必ず真になる (本家の冗長な条件をそのまま写してある)。
    (2) `imageOnlyRow` の `following == "]("` (リンクの宛先の範囲は `(` から始まり、宛先が `image_end+1` から始まるのは `](` の直後だけ。`[![a](b)](c)` など 6 形で範囲の始まりを確かめた。未確認: 宛先の範囲を作る全経路)。
  - `_QUOTED_SEGMENT`・`_LIST_MARKER_LINE`・`_SENTENCE_END_POSITION` など、土台の関数が使わない正規表現は、使う群 (G5・G1) が持つ。`_SENTENCE_END_POSITION` と `_LINE_SENTENCE_BREAK` の split は、正規表現でなく記号の直後で切る走査にした (記録で同じ答え)。
  - 次の係へ: 群は `lint/<群>.go` に 1 ファイルずつ足し、`*document` (解析 `an`・`proseVisibleText`・`endClosersFor`・`rule9Context`・`plainSentenceRecords`・`v220ScanText`) を受け取る。
    文末の飾りの字を正規表現へ埋め込む群 (G8 の `_MOMENT_PATTERN`・G9・G11・G12 など) は `endDecorationRun()` を使う (表から作る文字列。記録の定数 `_END_DECORATION_RUN` と同じ)。
    記録を足すときは `oracle/lintbase_cases.go` の形 (`recorder`・`lintDocCalls`) に倣い、`lint/oracle_test.go` の `invoke` に関数を足す。群の str の関数と analysis を取る関数を同じ記録に入れるか、新しい記録のファイルにするかは、1 ファイルの大きさ (いま 4.4 MB) で決める。

- 2026-11-13 T4 (後処理・出力・CLI) と骨組みを入れた。`ja-writing lint` は動くが、検出する規則は 0 (候補が出ない)。群 G1〜G20 は別のセッションが足す。
  - 作ったもの (`src/ja-writing/lint/`): CLI (`lint.go`・`args.go`。argparse の写しと `-h` の文面 `help.txt`)・後処理 (`post.go`: 種類で外す・allow・ダッシュの上限・点数)・出力 (`result.go`: `--json` の値と text)・
    規則の表 (`rules.go`: 40 規則 → 群・種類ごとの扱い・実装済みの規則の表 `implementedRules`)・骨組み (`lint_text.go`: `lint_text` の順。行のループは `beginRow` と `bodySteps`)・群ごとの口 (`g01`〜`g20`)。
    本家の `analyze_markdown_metrics` に当たる文書の指標は G1 のファイルに入れた (出力の `metrics` と点数に要るため。`excess_bold` / `excess_list` の候補は空)。
  - **群ごとの呼び出し口の約束**: 群は自分のファイル (`g<番号>_*.go`) の `lintState` のメソッドだけを書く。口の一覧と呼ばれる順は README の「lint の骨組みと群の足し方」。状態は群ごとの `g<番号>State` (欄は最初から全部ある)。
    規則を入れたら `implementedRules` の値を true にし、`lint-raw.json` との突き合わせに通す。依存 (G11 → G9・G14 → G8・G17 → G13) は、依存先が全部入るまで入れられない。
    G3 の口は `g03Bold` (1083 の `lint.BoldFindings(s.text, s.an)` を呼ぶ。`internal/bold` と `lint/bold*.go` はこの段で作っていない)。
    allow.txt で語を一部だけ外すときに message を作り直す 4 つの関数 (`calqueMessage`・`renyoMessage`・`aiPhraseMessage`・`projectTermMessage`) は群のファイルにあり、呼ばれたら panic する。
  - 記録 3 本 (`testdata/golden/`): `lint-cli.json` (8,065 件。全ファイル × 種類 13・語の一覧・text・条件、誤りの呼び出し・標準入力・作業フォルダの違い 88 件)・`lint-raw.json` (2,656 件。検出の生の並びと metrics。全 40 規則に当たる入力がある)・
    `lint-post.json` (77 件。本家の検出を作った候補に差し替えて、後処理と出力だけを本家で走らせたもの)。driver は `oracle/py/record_lint_{cli,raw,post}.py`。本家のファイルには手を入れず、`_project_term_findings` を driver が包んだ (動くことを確かめた)。
  - 比べた結果: CLI の記録のうち、本家が候補を 1 件も出さない入力と検査まで進まない呼び出し 3,777 件の rc・stderr・stdout (SHA-256 と長さ。全文は 16 件) が本家と同じ。
    指標 (`metrics`) は 2,656 件全部、後処理と出力は 77 件全部で同じ。40 規則が全部入ると、残り 4,288 件 (本家が候補を出す入力) も全件比べる段に切り替わる。
  - 実装をわざと壊して `bin/mutate-verify-list` で 38 本を確かめた (引数 6・CLI 6・出力 4・後処理 9・規則の表 4・指標 6・骨組み 3。うち 4 本は最初は構文エラーか緑のままで、書き直すか、入力のない所の単体のテスト (`TestIsDetailsName`) を足して red にした)。38 本とも想定の検査で red。
  - 満たせていないこと: 受け入れ条件のうち、40 規則の一致・規則ごとの当たる入力と当たらない入力の件数・dotfiles の文書を通した所要・群ごとに実装をわざと壊す確かめは、群が入ってから。
    地図 4.3 節の入力 (比喩動詞の残りの 7 型など) は足していない (入力の集まりを足すと全部の記録を作り直すので、群を入れる人が必要な形を足す)。
    `-h` の文面は本家の出力の写し、argparse は記録にある形だけを確かめた (README の「揃わない範囲」)。check (1077) が子の JSON から `render_report` を呼ぶ入口は足していない。

### 土台の敵対的レビュー (opus、2026-11-13)

対象は 8767e39ee と 86b38a17a。本家 (Python 3.15.0・v3.1.0) と並べて走らせた結果、本家と違う答えを緑のまま通す指摘 (P1) は 0 件、P2 が 3 件、P3 が 3 件。6 件とも再現できたので直した (採らない指摘は無い)。
壊せなかったもの: 文字列の関数 9 つ (乱数 9 万件)・解析を取る関数 (マークダウンの乱数 2.9 万件と断片 4,000 件)・後処理と出力 (乱数の候補 1,864 件)・CLI の引数 181 通り・検出の生の並びの digest (本家の候補 774 件)・12 本の記録の作り直し (バイト列が同じ)・長い入力の所要 (2 乗の形なし)。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P2-1 `run` の rc 1 と「終了コード1」の行の配線を検査するテストがない | 直した | `run` が検査の本体を引数 (`lintFunc`) で受ける形にし、検出を注入した `TestRunExitCodeAndExitLine` (条件 3 種 × 重さ 3 種・`--json` 付きの対も) を足した。実装をわざと壊した 3 本 (rc 1 を 0 にする・末尾の行を外す・当たった条件を空にする) が red |
| P2-2 行のループの分岐が群が入るまで検査されない | 直した | 口を呼ぶ直前の記録 (`lintState.trace`。テストだけが使う) を足し、`TestEachRowBranchesAndSentenceSplit` (引用・表・見出しの分岐と前の文・文の切り方 `!?`)・`TestDashStepRunsOnlyWithADashCharacter` (4 つのダッシュの字) を足した。実装をわざと壊した 6 本 (引用と表の前の文を消さない・表でダッシュを呼ぶ・見出しの `continue` を外す・`!?` で切らない・`━` を外す) が red。本家の 5655-5750 行と並べて、期待の列 (見出しは前の文に残る・引用の行は前の文が空・`g12RecordBoldLabel` は文を切る前) を確かめた |
| P2-3 入力の集まりに出ない語・型がある (`SLOP_WORDS` 5 語・`METAPHOR_VERB_PATTERNS` 5 型・当たりの少ない規則) | 直した (一部は未測定) | `oracle/lintrawgap.go` に入力 6 つ (語 5・型 5・`heading_colon`・`redundant_bracket` 5 種・`count_declaration` 7 形・`dialogue_period`・先頭が空白のバッジ) を足し、`lint-raw.json` を作り直した (2,656 → 2,704 件)。既存の 2,656 行は 1 バイトも変わらず、lint-cli.json は作り直してもバイト列が同じ。本家で 1 行が 1 件になることを確かめ、`TestGapInputsHitInRawRecording` が件数を見る。入力の語を 1 行消すと `TestRawFindingsMatchPythonRecording` が red。他の表 (ai_phrase・calque・katakana・shape・スライド) の当たらない項目は未測定で、README に、群を入れる人が数えると書いた |
| P3-1 allow で語の一部だけを外すときの message の作り直しに本家の記録がない | 直した (比べるのは群が入ってから) | `LintPostFinding` に `_word_types`・`_flag_hits`・`_replacements` を足し、calque 2 形・ai_phrase・renyo_noun・project_term 4 形の 8 件 (`allow-partial\|`) を `lint-post.json` に足した (77 → 85 件)。Go の message の関数は stub なので、その規則が `implementedRules` で true になるまで比べない (比べない件数をテストが出す) |
| P3-2 先頭が空白のバッジの行を検査する入力がない | 直した | `TestIndentedBadgeRowBreaksThePreviousSentence` (空白つき・空白なし・文が続く形)。`imageBreakRow` の `LStrip` を外す・`imageOnlyRow` の `start` を行頭にする 2 本が red。入力 `rawgap/badge-indented.md` も指標の比較に入る |
| P3-3 `groupDependencies` の G14→G8・G17→G13 は表から外しても緑 | 直した | `TestDependencyCheckDetectsAViolation` が、表の組ごとに依存先の規則を外して違反が出ることを見て、表の中身を本家の読み (4588・4845・4846 行) と照合した値と比べる。G14・G17 の組を外す 2 本が red |

- 実装をわざと壊した 14 本 (`bin/mutate-verify-list`。ログは tmp/yomiyasu-gap-2026-11-12/mut2/) が、どれも名指しの検査だけで red。巻き添えの失敗は各ログに無い。
- 満たせていないこと: 本家の候補と Go の候補の突き合わせは、群が 0 個なので実行できていない (レビューでも未確認)。`allow-partial|` の比べは群が入るまで。

- 2026-11-13 origin/master (1083・1084 が入ったもの) へ rebase した。記録の登録 (`oracle/cmd/golden`) の衝突は、bold・pause_comma・lint の 4 本を全部残して解いた。
  - `make golden` を記録の表 (`goldens`) 1 つから回す形にした (commit「make golden を記録の表 1 つから回し…」)。前は記録ごとに 4 か所と一覧 2 つへ手で書いていて、
    1083 の `bold.json` が同じ環境の検査 (`TestGoldenFilesShareOneEnvironment`) から漏れていた。いまは `testdata/golden/*.json` を全部読み、表とファイルが 1 対 1 かを `TestGoldensMatchGoldenDir` が見る。
    全部の記録を作り直して 14 本と Go の表 3 つが前と同じバイト列になることを確かめた。
- 2026-11-13 G3 (`bold_not_rendered`) と G13 (`pause_comma`) を骨組みにつないだ (`g03_bold.go`・`g13_pause.go`。`implementedRules` の 2 規則が true)。
  検出の生の並びの記録と比べた候補は bold_not_rendered 688 件・pause_comma 3,291 件で、全部本家と同じ。実装をわざと壊した 6 本 (候補を足さない・行をつないだ表を引かない・essay でも目的語を当てる・Heads を常に真・種類の制限を外す・「ほか N か所」の境) が red。

### 土台の codex の敵対的レビュー (gpt-6.1-sol・high、2026-11-13)

対象は rebase 後の 3 commit (土台・T4・opus の指摘の直し) と記録の表の書き換え。T1〜T3 の実装 (乱数の突き合わせを通したもの) は渡していない。codex は道具を使えない環境なので、差分と本家の該当部分 (`_apply_words`〜`lint_text`・`render_report`〜`main`・点数) を依頼文に入れた。
指摘 12 件 + 未確認リスク 5 件。直した 8 件 (commit「lint の土台の codex の敵対的レビューの指摘を直す」)・採らない 4 件・未確認リスクは 1 件を実測して直し、4 件を記録。

| 指摘 | 扱い | 理由・結果 |
|---|---|---|
| 1 群が一部だけ入った間、公開の候補の鍵の順を壊しても全部の比較が緑 (post の比較が sort_keys で並べ直す) | 直した | `lint-post.json` に `--json` の出力 (鍵を並べ直さない `indent=2`) の digest を足し、`TestPostProcessingMatchesPythonRecording` が比べる。鍵の順を line → rule にわざと壊すと red。CLI の記録を規則の組で部分的に比べる案は採らない (post の比較で出力の形は見える。CLI の記録に生の規則の組を持たせると driver の作りが重くなる) |
| 2 lint-raw の記録の `n` を壊しても検出しない・「比べた件数」の検査が発火しない | 直した | `LintRawRecord.CheckCount` を作る側と読む側の両方で当てる。比べた候補を規則ごとに数え、入った規則で 0 件なら落とす。`n` を +1 にわざと壊すと red |
| 3 「strict と info だけ」のテストの候補 (pause_comma) が種類なしで外れ、候補 0 件のテストになっている | 直した | 種類で外れない `instruction_residue` にし、`--json` の `candidate_count` が注入した数のままかを全 case で見る。pause_comma に戻すと red |
| 4 argparse の「ignored explicit argument」の値の引用が Python の repr と違う | 直した | `pyre` の `pyRepr` を `pystr.Repr` へ移して共有し、使った。CLI の記録に scenario (改行・単引用符・両方の引用符) を足した |
| 5 負数らしい引数の判定が Unicode の数字で違う | 直した | ローカルの Python 3.15.0 の argparse は `-\.?\d` の前方一致で、Go の写しは古い Python の形 (`^-\d+$|^-\d*\.\d+$`) だった。ASCII の `-1x` でも答えが違った。pyre で 3.15 の形にし、scenario (アラビア数字・全角数字・`-1x`・`-.5`) を足した |
| 6 `--fail-on` の語末の Σ | 採らない | README の「揃わない範囲」に書いた既知の差。使えない値の文に出る字だけに現れ、Python の文脈つきの小文字化 (cased・case-ignorable の表) を写す費用に見合わない。再提起するなら、ASCII 以外の `--fail-on` の値を実際に使う場面を示す |
| 7 どの書き込みの誤りも BrokenPipe と扱う | 直した | EPIPE だけを「出力先が閉じられた」にし、ほか (ENOSPC) は内部エラー。`TestWriteFailuresFollowPythonExitPaths` |
| 8 使い方の誤りの文が書けないとき、本家は rc 3 | 直した (rc は実測に合わせた) | 本家を Python 3.15.0 で実測すると rc 3 ではなく 120 (書き込みが終わり際の flush まで遅れる)。`-h`・`--version` も同じく 120 と「Exception ignored while flushing sys.stdout:」の 2 行。Go をこの形に揃え、閉じたパイプでの rc と標準エラーが本家と同じことを実物で確かめた |
| 9 lint-post の `natural=0` が、差し替えた project_term の本来の候補を数えていない | 直した | driver が元の `_project_term_findings` の戻りも数える。作り直しても全件 0 のまま |
| 10 G13 の種類の条件を骨組みのテストが本家と逆の契約で固定している | 採らない | 種類の条件は G13 の口の中 (`g13PauseComma`) に置いた。骨組みは口を呼ぶだけで、trace のテストは「口を呼んだ」を見る。種類なしの文書で pause_comma が出ないことは、検出の生の並びの比較 (種類なしの文脈を含む) が見ている (種類の制限をわざと外すと red) |
| 11 記録の表の書き換えが、`-only` の受ける名前・誤りの順・`checkEnv` の順を変えた | 採らない | 全部の記録の名前を `-only` で受けるのは意図した拡張で、commit message に書いた。知らない名前を本家を取りに行く前に断るのは早く失敗するための変更。誤りが重なったときにどちらを先に出すかに意味は無い (開発の道具で、どの順でも作り直しは止まる) |
| 12 2 回の生成の間に語の一覧が変わると、前は成功した条件で失敗する | 採らない | 作り直しの最中に testdata を書き換える形で、起きる経路が無い。起きたら 2 回の不一致で止まる (書き換えずに止まるのは安全側) |
| 未確認: 実プロセスの SIGPIPE | 実測して直した | Go は SIGPIPE で落ちて rc 141 だった (本家は rc 3)。入口で SIGPIPE を `signal.Notify` で受ける (Ignore は exec した子へ無視が引き継がれるので使わない) |
| 未確認: digest の衝突 (候補 32 bit・CLI と post 64 bit) | 記録 | 衝突の組は作れていない。README の「全件、字まで比べる」は言い過ぎだったので「stdout は SHA-256 の先頭 16 字と長さ・stderr は全文」に直した |
| 未確認: G18 の依存 | 記録 | G18 を入れる人が、本家の `_v220_shape_findings` が既存の候補の何を読むかを確かめ、読むなら `groupDependencies` に足す |
| 未確認: 点数の整数の幅 (32 bit) | 採らない | 対象は macOS の arm64 だけ (dotfiles の CLAUDE.md)。int は 64 bit |
| 未確認: 表の出力先の欄を消した | 記録 | `paths` の欄を使っていたのは `oracle/cmd/golden/main.go` だけ (grep で確かめた) |


- 2026-11-13 G1 (`excess_bold`・`excess_list`) を入れた (`g01_metrics.go` の `g01Document`)。検出の生の並びの記録と比べた候補は excess_bold 152 件・excess_list 192 件で、全部本家と同じ。
  - 入力を 7 つ足した (`oracle/lintrawgap.go`): 地の文 300 字と 301 字・1,000 字あたり 3.0 個ちょうどと 4 個・箇条書きの比率 0.25 ちょうど・`<details>` の中の箇条書きを除いた字数 300 と 301。corpus の指標はこれらの境目に当たっていなかった。記録は 2,704 → 2,760 件で、既存の 2,704 行は変わらない (新しい行は名前順に挟まる)。
  - 実装をわざと壊した 6 本 (`>= 300`・`>= 3.0`・箇条書きの門を地の文の字数に替える・`>= 0.25`・`<details>` の注記を出さない・比率の `round` を外す) が `TestRawFindingsMatchPythonRecording` で red。
- 2026-11-13 G2 (`sentence_end_repetition`) を入れた (`g02_sentence_end.go`)。検出の生の並びの記録と比べた候補は 760 件で、全部本家と同じ。
  - 入力を 2 つ足した (`g2-endings`: corpus に出ない語尾 4 つ・`g2-decoration`: 絵文字で続けた文の分け方・句点つきの絵文字・先頭のコードで見える文字と元の行の幅が違う場合・分けた後ろが 3 字で分けない場合・閉じかっこ)。記録は 2,760 → 2,776 件で既存の行は変わらない。
  - わざと壊した 8 本 (3 回目・段落の組・「その他」の除外・幅が同じ扱い・後ろの 3 字・前の 8 字の窓・「だ」の語尾・文末の飾りの除去) が red。分けを早く抜ける `if !found` を外す 1 本は等価 (最適化だけ)。
  - 本家の `_sentence_end_type` の「末尾 64 字まで手で削る・それより長い並びは正規表現」は、どちらも並びを全部削るので 1 つの走査にした。lint_text の経路には、文末の並びが 2 つ以上続く文が来ない (文は句点ごとに切れ、空白は strip される) ので記録では確かめられない。
- 2026-11-13 G4 (`emoji_prohibited`) を入れた (`g04_emoji.go`)。検出の生の並びの記録と比べた候補は 1,680 件 (本家と同じ)。
  - 入力を 1 つ足した (`g4-emoji`: 引用の中・引用の中の箇条書き・5 個並び (message は最初の 3 つ)・ZWJ・旗・キーキャップ・コードの中)。corpus には引用の中・ZWJ・旗の当たりが無かった。記録は 2,776 → 2,784 件で既存の行は変わらない。
  - わざと壊した 5 本 (最初の 4 つを出す・区切りを空白でなくす・snippet に前後の空白を残す・コードの中も数える・重さを info にする) が red。
- 2026-11-13 G5 (`dash_decoration`・`dash_insertion`・`dash_list_ending`) を入れた (`g05_dash.go`)。検出の生の並びの記録と比べた候補は dash_decoration 592 件・dash_insertion 64 件・dash_list_ending 96 件 (本家と同じ。上限の 3 件は後処理なので、ここでは全部足す)。
  - 入力を 1 つ足した (`g5-dash`): 区間のダッシュを普通名詞で取り違えない 13 語 (corpus は人間・時間・目線だけだった)・接尾辞の 1 字と 2 字だけ・連続したダッシュ・文の端・名詞でない隣・長音と「々」の隣・記号だけの行・dash_list_ending の末尾 4 形と項目の字数と文頭の位置。記録は 2,784 → 2,792 件で既存の行は変わらない。
  - わざと壊した 11 本が red (普通名詞の除外・「区間」の 2 字・出典の英語の形・文末の飾りの並び・罫線・重なったときの規則の順・長音・「々」・記号だけの判定の Z・並べる語数の下限・リンクの出典の除去)。等価と判断した 2 本: 連続したダッシュの長さの判定 (`!= 1` を `> 2` にしても、2 字のときは 1 字目だけ空白になり 2 字目が残るので規則は変わらない)・リンクの出典を `]` に置く文字を変える (`[…]` が引用として先に伏せられ、ダッシュごと消える)。
- 2026-11-13 G6 (`heading_colon`・`short_summary_heading`・`redundant_bracket`) を入れた (`g06_heading.go`)。検出の生の並びの記録と比べた候補は heading_colon 32 件・short_summary_heading 104 件・redundant_bracket 56 件 (本家と同じ)。
  - 入力を 3 つ足した (`g6-summary-1999`・`g6-summary-2000`: 地の文の字数の境目・`g6-summary-forms`: 「おわりに」・見出しの水準 1〜6・末尾の絵文字と空白・「まとめと感想」・半角のコロンの `とは:`)。記録は 2,792 → 2,816 件で既存の行は変わらない。
  - わざと壊した 6 本 (字数の境目 `<=`・末尾の飾りを除いた見出し・半角のコロン・「（概要）」・重さ・見出しの水準 1〜5) が red。
- 2026-11-13 G7 (`trailing_colon`) を入れた (`g07_colon.go`)。検出の生の並びの記録と比べた候補は 1,360 件 (本家と同じ)。
  - 入力を 1 つ足した (`g7-colon`: 全角と半角・コロンだけの行 2 つと 3 つ以上・http で始まる行・コードを伏せると http で始まる行・箇条書き・行末の空白・URL の後ろ・最後のコロンが保護された範囲に入る行 (閉じない `<https://…`)。corpus には最後のコロンが保護された範囲に入る行が無かった)。記録は 2,816 → 2,824 件で既存の行は変わらない。
  - わざと壊した 6 本 (コロンだけの行の長さの境目・半角のコロン・http の判定・保護された範囲の判定の位置・行末の空白を除いた位置・コードだけを伏せる指定) が red。
- 2026-11-13 G8 (`slop_vocabulary`・`metaphor_verb`・`translated_term`) を入れた (`g08_vocab.go`・表は `g08_tables.go`)。検出の生の並びの記録と比べた候補は slop_vocabulary 1,048 件・metaphor_verb 1,624 件・translated_term 544 件 (本家と同じ。内部の欄 `Words` まで digest に入る)。
  - 表は本家の変数を組んだ後の文字列のまま、生成した Go に写した (手で写さない。SLOP_WORD_PATTERNS は (語, パターン) の順で、他の表と逆)。`_MOMENT_PATTERN` が使う `_END_DECORATION_RUN` は `endDecorationRun()` を使うときに埋める。
  - 本家の文書での絞り込み (`_narrowed_document_rows`) は持たない。行ごとの必要な語の判定 (`_may_match_literals`) が同じ行を落とすので、結果は変わらない。必要な語の判定を外す変更は、記録の範囲では変わらなかった (必要な語が必要条件なので等価と判断)。
  - 入力を 7 つ足した: SLOP_WORDS の残り 21 語・語の形で除く例外 (ゲートウェイ・安全装置・真理値・推移閉包・会計台帳・OSX など)・比喩動詞の「引き返す」「添える」と「壊れる」「静かに壊れる」の二重の反応・当たった文字列の前の空白・比喩の「効く」の枝 (文字どおり・慣用句・打ち消し・名詞の主語・効き目・だんだん効く・副詞・時間・前の 3 行の窓・同じ行の前の文)・「版」と「回帰」の例外。記録は 2,824 → 2,880 件で既存の行は変わらない。
  - わざと壊した 14 本が red (語の表から 1 語を外す・必要な語・当たりの語の注記除去・前後の空白・「壊れる」の範囲・比喩の「効く」の v210・打ち消し・名詞の主語・効き目・時間と近くの語・前の行の窓・同じ行の前の文・「効く」の判定を外す・当たった語の重なりの除去)。
- 2026-11-13 G9 (`meta_filler`・`summary_restatement`・`meta_intro`・`count_declaration`) を入れた (`g09_filler.go`・表は `g09_tables.go`。`fillerPatterns` を G11 が読む)。検出の生の並びの記録と比べた候補は meta_filler 1,472 件・summary_restatement 72 件・meta_intro 160 件・count_declaration 96 件 (本家と同じ)。
  - 入力を 1 つ足した (`g9-filler-forms`: 「過言ではない」の型を引用を伏せた文に当てる・文末の飾りを除いた文に当てる・対のない閉じ引用符と閉じかっこ・info の 3 型)。表の 15 型と 3 型は corpus で全部当たっていた。記録は 2,880 → 2,888 件で既存の行は変わらない。
  - わざと壊した 7 本 (引用を伏せた文を行の文に替える・文末の飾りを除いた文を使わない・閉じ字・文末固定の判定・残してよい場合の注記・info の重さ・「過言ではない」を特別扱いしない) が red。
- 2026-11-13 G10 (`instruction_residue`) を入れた (`g10_residue.go`・正規表現は `g10_tables.go`)。検出の生の並びの記録と比べた候補は 208 件 (本家と同じ)。
  - 入力を 1 つ足した (`g10-residue-forms`: 文末の飾りを除いた文・閉じかっこ・「ます」のない書く行為と書き出し・文の中の参照・絵文字で分けた文と次の文の参照・「触れず」)。corpus には、飾りを除いた文でだけ当たる形と「ます」のない形が無かった。記録は 2,888 → 2,896 件で既存の行は変わらない。
  - わざと壊した 12 本 (飾りを除いた文・絵文字で分けた文・次の文・「ます」・書き出し・書き方の条件・範囲の限定・読者のための範囲・文の中の参照・次の文の参照・閉じ字・重さ) が red。
- 2026-11-13 G11 (`short_mochiron`・`fragment_run`・`cleft_list_ending`) を入れた (`g11_short.go`・正規表現は `g11_tables.go`。cleft は G9 の `fillerPatterns` を読む)。検出の生の並びの記録と比べた候補は short_mochiron 152 件・fragment_run 72 件・cleft_list_ending 296 件 (本家と同じ)。
  - 入力を 2 つ足した (`g11-forms`: 引用の中の短い文・「と」の 1 回と 2 回・最後の項目の「の」・前の文が問いか同じ動詞の漢字を含む・文末の飾りと句点のあとの飾り・「もちろん」の答えと問いのあと、`g11-cleft-first`: 文書の最初のブロック)。記録は 2,896 → 2,912 件で既存の行は変わらない。
  - わざと壊した 17 本が red (答えの定型・飾りを除いた文・前の文・文頭の前の文・引用の伏せ・「と」の数・最後の項目の「の」・問いの判定・次の文・フィラーの型・前のブロック・見出し・伏せた文の数・前の文を探す句点の数・語幹)。等価と判断した 1 本: `fragment_run` の「。」「！」が 3 つ以上という前置きの条件 (正規表現が 3 つの短い文を要るので、外しても結果は同じ)。
