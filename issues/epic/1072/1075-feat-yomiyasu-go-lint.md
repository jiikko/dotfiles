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
- 2026-11-13 G14 (calque 型 1〜5・metaphor_label・katakana_term・thin_evaluation) を入れて、40 規則が全部揃った。CLI の記録 8,072 件を出力の全体と rc まで全件比べて通る (stdout は SHA-256 の先頭 16 字と長さ・stderr は全文・rc。`TestCLIMatchesPythonRecording` の「残り 0 件」)。
  - 実装: `lint/g14_calque.go` (型 1〜5・`calqueMessage`・`g14WordFindings`)・`g14_label.go` (metaphor_label)・`g14_lists.go` (一覧と手掛かりの語の正規表現)・`g14_tables.go` (本家が組み終えた表)。commit は calque (型 1〜5。土台とラベルの判定を含む。型 2 が同じ行のラベルを見るため)・metaphor_label・katakana_term・thin_evaluation の順で 1 規則ずつ true にした (各 commit が単独で build とテストを通る)。
  - 比べた候補 (lint-raw。(入力の集まり 332 + rawgap 72) × 8 文脈 = 3,232 行。内部の欄 `_words`・`_word_types` を含む digest で一致): calque 7,648・metaphor_label 200・katakana_term 320・thin_evaluation 216。lint-post の `allow-partial|` の比べない件数は 2 から 0。
  - 表の比べ: 本家の定数 98 個 (正規表現・語の一覧・型の名前・`_V220_BUILTIN_LISTS`) を `lint-base.json` に足して Go の値と比べる。`_V220_BUILTIN_LISTS` は、Go が内蔵の一覧を持たず埋め込んだ一覧 (`lists/data`) を読むので、内蔵と同じ中身かを見る。`_v220_load_list` は katakana-terms と thin-evaluation も記録した。
    手書きの文での判定の比べ (lint-base に 7,623 呼び出し): 型 1〜5 (語の list を引数で受けて書き足すので、driver に `args_out` を足して「戻り値と呼んだ後の引数」を記録した。付けなければ前と同じ出力)・`_calque_saki_spans`・`_calque_has_org`・`_calque_subject_end`・`_v220_doc_tech`・`_calque_message`・`_label_is_phrase`・`_label_shape`・`_label_kanji_runs`。型 2・3 は技術の文書かどうかの 2 通りで通す。
  - 本家と違う書き方にした所: 手掛かりの語の正規表現 (`_v220_literal_re`) は、同じ長さの語の並びが集合の順 (実行ごとに変わる) なので、辞書順にした (同じ位置に同時に当たる同じ長さの別の語は無く、答えは変わらない)。一覧のファイルを読めないときに内蔵へ戻す枝は持たない (埋め込みの一覧は固定で、内蔵と同じ中身を上の記録が見る)。
    `_calque_type1` の `covered` (同じ所に当たった別の動詞を数えない) は本家のとおり写したが、いまの動詞の表では働かない (外す変異は緑のまま。下)。
  - 本家の表の項目を 1 つずつ外した比べ (`_v220_word_findings` を入力の集まりと rawgap の全文書に通し、結果が変わらない項目を数えた。正規表現 45・型 3 の句 37・型 1 の動詞 24・型 5 の語 15 行・定着した言い方 14・主語の語 68・定着した「〜先」241・型 1 の漢字の熟語を許す動詞など小さな表の 54・ラベルの表 353・技術の語の一覧 38・手掛かりの語 131・カタカナ語 30・評価の組 35 の 1,085 項目): 足した入力で 765 項目が結果を変える。入力を作れない (等価な) 項目は 319 で、理由は次のとおり (数えた 1,085 項目のうち G14 でない `_LABEL_WORD` (G20 の定数) 1 項目は除く)。
    - `_CALQUE_SAKI_TIME` (1): 「〜先」の当たりの語は送り仮名 (しせげけめりちきぎびみいえねべれじ) か「着地」などの語の最後の字で終わり、時の字 (年月日週間…) と重ならないので、どの入力でも当たらない。
    - 定着した「〜先」(198): 185 は、先の 1 つ前の字が送り仮名でも「着地」などの語でもないので「〜先」の当たりができない。4 (問合せ先・問合わせ先・支払い先・仕入れ先) は当たりの前に漢字があって当たりごと外れる。9 (申し込み先・書き込み先・読み込み先・取り込み先・振り込み先・書き出し先・呼び出し先・引き継ぎ先・受け入れ先) は、短い項目 (込み先・出し先・継ぎ先・入れ先) が語の終わりに一致する。
    - 型 1 の定着した言い方 `_CALQUE_ESTABLISHED_TYPE1` の 0 番 (1 番が上位集合)・4 番「疎通」(通 の前が漢字なので、先に熟語として外れる)。主語の語「サーバー」(「サーバ」に長音が続く形が主語として拾う)。
    - 依頼の形 (「てください」は「てくださ」の一部。「て下さい」「いで下さ」「て欲しい」は、動詞のあとの平仮名の続きに漢字が入らないので当たらない)・名詞を修飾する形の「いる」(「る」が拾う)・組織の語「事務局」(「局」が接尾辞で拾う)。
    - ラベルの表: 「よく使う節の名前」104 語は、表から外しても `_label_shape` が偽 (平仮名が 4 字続く・2 字未満・名前の語で終わる・句)。名前の語で終わる見出しの終わり 7 (方法→法・方式→式・報告書→書・手順→順・仕様→様・形式→式・一覧表→表) は、別の項目で終わる。
    足した入力 (`oracle/lintrawgap_g14.go`): 型 1〜5 の文書 (技術の語が 5 種類以上ある文書と、技術の語が無い文書)・主語の語・「〜先」・動詞の形 (漢字の熟語・依頼・名詞を修飾する形)・技術の文書の判定 (数えない語を全部並べ、1 語外れると技術の語が 5 種類になる)・metaphor_label (本文の字数 59 / 60・1 行目・frontmatter のあとの 1 行目・文として終わる 1 行目・リスト・伏せた英数字・区切り・番号・補足・引用の字・表の項目 228 を見出しにした 3 文書)・katakana_term (番号や記号の付いた名前・同じ語が 2 回)・thin_evaluation (前置き 20 字と 21 字・同じ名詞が 2 回)。lint-raw の既存の 3,072 行は変わらない。
    なお、「3.5時間を取る」のような小数の時間は、識別子の伏せ (`_V220_IDENTIFIER`) が「3.5」を空白にするので、本家はどの入力でも当てない (記録でも同じ)。
  - わざと壊した 35 本のうち 33 本が想定の検査で red (calque 型 1: 5 のうち 4・型 2: 4 のうち 3・型 3: 5・型 4: 2・型 5: 2・message 1・手掛かりの語 1・metaphor_label 5・katakana_term 3・thin_evaluation 4・表 3 (句・message・「〜先」の項目))。2 本は等価: (1) `_CALQUE_SAKI_TIME` を外す (上のとおり)、(2) `covered` を外す (動詞の表のどの 2 つも、同じ所に当たらない。引き込む と 引く は 3 字目が漢字と平仮名で排他。17,834 通りの文字列と手書きの文で重なる当たりが 0 件)。
    最初は 3 本が緑のまま通り、入力を足して red にした: `katakana_term` と `thin_evaluation` の重なりの除去 (同じ語が 1 行に 2 回の入力が無かった)。1 本は変異の書き方が構文エラーだったので当て直した (型 3 の対応表の句と型 1 の動詞の重ね合わせ)。
  - 未確認: 型 1 の動詞の表が将来増えたときに `covered` が働く入力は、いまは作れない (上)。

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
- 2026-11-13 G12 (`negative_parallelism`・`negative_parallelism_density`・`bold_label_list`) を入れた (`g12_negation.go`・正規表現は `g12_tables.go`。`g12Prepare`・`g12RecordBoldLabel`・`g12Negation`・`g12Document` の 4 つの口)。検出の生の並びの記録と比べた候補は negative_parallelism 2,224 件・negative_parallelism_density 152 件・bold_label_list 88 件 (本家と同じ)。
  - 入力を 1 つ足した (`g12-negation-forms`: 「ではなく」の前後が「、」と文頭・リストの行の「でもない。」・折り返した文を画像と引用で切る形・文末の飾りのあとの「じゃない、」)。記録は 2,912 → 2,920 件で既存の行は変わらない。
  - わざと壊した 11 本 (「でもある」の除外・「ではなく」の前後の字・「、」を挟む形・画像の行で切る・飾りを除いた文・密度の件数と 3 件目の行・太字の見出し語の 1 件目の行・行番号の列挙の上限・折り返した文でつないだ行・リストの行の「でもない。」) が red。等価と判断した 2 本: 「でもない。」が無い折り返した文を調べない前置き (正規表現が必要とする語)・引用の行で折り返した文を切る条件 (引用の行は後の判定が使わず、引用の行に続く行は単独でも同じ当たりになる)。
- 2026-11-13 G8〜G12 の表 (`gNN_tables.go`) を、本家の定数の記録 (`lint-base.json` の constants の case) と比べるようにした。`oracle.LintBaseConstants` に表と正規表現の 35 名を足し (SLOP_WORDS・SLOP_WORD_PATTERNS・METAPHOR_VERB_PATTERNS・TRANSLATED_TERM_PATTERNS・FILLER_PATTERNS・FILLER_NOTES・INFO_SENTENCE_PATTERNS・`_PATTERN_LITERAL_HINTS`・`_KIKU_RE`・各正規表現ほか)、driver (`call_function.py`) は `_LazyPattern` を `{flags, pattern}` にする。
  Go 側は `lint/tables_oracle_test.go` の `tableConstantValue` が値を作り、`TestLintBaseMatchesPythonRecording` が digest を比べる。`-only lint-base` で作り直して、constants 以外の 455 case と header は変わらなかった (emoji の表も同じ)。
  表の 1 項目を書き換える 14 本 (語の表・語の形のパターン・比喩動詞・直訳・「効く」の表・message の末尾・必要な語・フィラー・注記・info・「過言ではない」・G10・G11・G12) が `TestLintBaseMatchesPythonRecording` で red。生成の道具は要らなくなった。
- 2026-11-13 G15 (`renyo_noun`) を入れた (`g15_renyo.go`)。一覧は埋め込んだ `lists/data/renyo-nouns.txt` (本家の内蔵の一覧と同じ中身を実測で確かめた) を読み、正規表現を組む。検出の生の並びの記録と比べた候補は 1,008 件 (本家と同じ)。`renyoMessage` を実装し、lint-post の `allow-partial|` の比べが 1 件始まった (比べない件数 8 → 7)。
  - 文書全体に「の[*_]*語」を元の文字に当てて始まりの行だけを調べる絞り込みは、そのまま写した。
  - 入力を 1 つ足した (`g15-renyo-forms`: 引用・リスト・表・見出し・外す後ろの字 (育て・足し・試し)・語のあとの助詞・太字とカギ括弧・語の重なり)。記録は 2,920 → 2,928 件で既存の行は変わらない。
  - 本家の表の比べ: `lint-base.json` に定数 `_RENYO_BEFORE`・`_RENYO_AFTER`・`_RENYO_MESSAGE`・`_V220_MESSAGE_TAIL` と、関数 `_v220_load_list("renyo-nouns")` (項目と出どころ。表の case `tables/functions`) を足した。`_renyo_pattern` の組んだ正規表現は、同じ長さの語の並びが `PYTHONHASHSEED` で変わり記録が決まらない (2 回作った記録が違った) ので比べず、Go は (長さの降順, 字の順) で並べる。
  - わざと壊した 7 本 (外す後ろの字・見出しと表・語の重なりの除去・「の」の前の字・語のあとの助詞・先読みの「も」・message) と、表の比べで red の 3 本 (後ろの助詞・message・「の」) が red。等価と判断した 3 本: 引用の行を除く条件 (引用の行は見える文字が空)・語を長い順に並べる (先読みが後戻りで選ぶ)・元の文字での絞り込み (見える文字で「の」と語が隣り合うなら、元の文字でも `*` `_` を挟んで隣り合う)。
- 2026-11-13 G16 (`ai_phrase`) を入れた (`g16_ai_phrase.go`・表は `g16_tables.go`)。検出の生の並びの記録と比べた候補は 256 件 (本家と同じ)。`aiPhraseMessage` を実装し、lint-post の `allow-partial|` の比べがもう 1 件始まった (比べない件数 7 → 6)。`pausecomma.TENotVerb` を使う。
  - 文書全体に手掛かり (`_AI_PHRASE_HEAD`) を元の文字に当てて始まりの行だけを調べる絞り込みは、そのまま写した (入力 `g16-ai-phrase-forms` の「`を**改善**。`」は、元の文字に手掛かりが無いので本家は拾わない。Go も拾わず、絞り込みを外す変異が red)。
  - 入力を 1 つ足した (同じ型が 1 行に 2 回・太字・判断した人と招く人・見出しと箇条・「について、」)。記録は 2,928 → 2,936 件で既存の行は変わらない。
  - 本家の表の比べ: 定数 `_V220_SENTENCE`・`_AI_PHRASE_HEAD`・`_AI_JUDGE`・`_AI_INVITER`・`_AI_PHRASE_FORMS`・`_AI_PHRASE_MESSAGE` を `lint-base.json` に足し (constants 以外の case は変わらない)、表の 1 項目を書き換える変異が red。
  - わざと壊した 11 本 (判断した人・招く人・「なんよ」の種類と行の種類・「て形」の種類と行の種類と `TENotVerb`・同じ型の重なり・行の種類・message・絞り込み) が red。等価と判断した 1 本: 引用の行を除く条件 (引用の行は見える文字が空)。
- 2026-11-13 G17 (`slide_comma`) を入れた (`g17_slide_comma.go`・表は `g17_tables.go`)。検出の生の並びの記録と比べた候補は 631 件 (本家と同じ。slides の文脈だけ)。G13 の候補の `Heads`・`Objects` と `pausecomma.TEAttrObject()` を使う。
  - 入力を 1 つ足した (`g17-slide-forms`: 太字と `__` の記号を外した位置・リストの文字だけを言い方に使う・20 字より長い言い方)。記録は 2,936 → 2,944 件で既存の行は変わらない。
  - 本家の表の比べ: 定数 `_SLIDE_LINE_MAX`・`_SLIDE_COMMA`・`_SLIDE_ACTOR_AFTER`・`_SLIDE_LIST_AFTER`・`_SLIDE_SAME_NEXT`・`_SLIDE_LEAD`・`_SLIDE_BULLET`・`_SLIDE_MARKER_ZONE`・`_SLIDE_MESSAGE` を `lint-base.json` に足し (constants 以外の case は変わらない)、表の 1 項目を書き換える変異が red。
  - わざと壊した 17 本 (60 字の上限・pause_comma と重ねない・`Heads`・別の主語・名詞の並び・同じ助詞の並びの 2 通り・`Objects`・言い方の 20 字・言い方の重なり・複数行の段落と箇条・箇条の印の範囲・太字の記号・`__`・掛かり先を決める読点・リンクの文字) が red。

- 2026-11-13 G18 (say_hedge・copy_comma_noun・bare_verb_chain) を入れた (`lint/g18_shape.go`・`g18_tables.go`)。本家の `_v220_shape_findings` は既存の候補を読まないので、群の依存は無い。
  - 比べた候補: say_hedge 216・copy_comma_noun 165・bare_verb_chain 144 (lint-raw の 8 文脈。全 case で並びが一致)。表は定数 21 個 (`_SHAPE_*`・`_SAY_HEDGE*`・`_COPY_*`・`_BARE_VERB_*`) を記録と比べ、frozenset 2 つは driver が昇順の配列にして記録する (`call_function.py` の `encode_default` に集合を足した。これまで集合を記録した case は無く、既存の記録は変わらない)。
    文の判定 3 関数は手書きの 356 文で比べる (`oracle/lintshape_cases.go`。lint-base に 1,068 呼び出しを足した。記録の真偽は 123 対 933 で両方出る)。
  - 足した入力: `rawgap/g18-shape-tables.md` (say_hedge の形容動詞の語幹 19 語・感動詞などの 5 語・名詞 17 語を 1 行ずつ。corpus で結果を変えなかった項目を、本家で語を 1 つ外して確かめた)。
    等価で入力を作れないもの: `_COPY_NOT_MODIFIER` の 13 語 (いいえ・まあ・あのね・ええ・いや・おい・なお・ただ・しかし・ちなみ・つまり・さて・では) は、正規表現が修飾語の終わりを「な・い・た・の」に限るので、どの入力でも表に当たらない (記録では表の値として比べる)。
  - わざと壊した 13 本が想定の検査で red (say 2・copy 2・bare 2・門 3 (「えるでしょう」を外す・読点の門を狭める・promo の分岐)・表 4)。最初の 2 本は緑のまま通った: `say-judge` は「文 + x」で判定が変わらない (悪い壊し方) ので判定を外す形に直して red、`gate-comma` は `{1,14}` を `{1,13}` にしても copy が名詞 7 字以下・bare が別の読点の形で拾われるため境目が働かない (等価) ので `{1,2}` にして red。
  - 共通の helper: `candidateLines` から `linesAt` (位置から行の番号) を切り出した (G18 が「えるでしょう」の位置も混ぜるため)。

- 2026-11-13 G19 (dialogue_period・narrative_density) を入れた (`lint/g19_doc_types.go`・`g19_tables.go`)。script と essay だけで、文書に 1 件。
  - 比べた候補: dialogue_period 9・narrative_density 67 (lint-raw。corpus の当たりは少ないので、境目の入力を足した)。表の定数 14 個 (数 5・`_DOCUMENT_SKIP_KINDS`・`_QUOTE_PAIRS`・`_QUOTE_CLOSERS`・正規表現 6) を記録と比べる。
    台詞の見つけ方と地の文の数え方は、`_top_quote_contents` (手書き 20 文)・`_dialogue_lines`・`_narrative_counts` を、入力の集まりの全ファイルと枝を当てる文書 (`shape/forms`。名前：台詞・一人称の例外・識別子・バッジ・コード) に通して中身を比べる (lint-base に 3 関数の呼び出しを足した)。
  - 足した入力 (lint-raw のみ。14 件): 台詞 9 個 (出ない)・10 個で割合 0.5 ちょうど (出る)・0.4 (出ない)・丸め 87.5 → 88 と 62.5 → 62・名前：台詞・半角の「｡」、地の文 999 / 1,000 / 1,001 字・数字 7 / 8 個・一人称 1.0 ちょうど (出ない) と 0.999 (出る)・箇条書き。本家で出る・出ないが想定どおりになることを記録で確かめた。
  - わざと壊した 14 本が全部想定の検査で red (境目の `>=` を `>` にする 5 本・丸め・半角の句点・箇条書きの数え・千の区切り・小数の桁・カギ括弧の伏せ・話し手でない名前・一人称の表・飛ばす行の種類)。

- 2026-11-13 G20 (project_term) を入れた (`lint/g20_project_term.go`・`g20_tables.go`)。`projectTermMessage` も本家どおりに書いた (allow で語の一部だけを外す message の作り直し)。
  - 比べた候補: project_term 296 (lint-raw の words の文脈。内部の欄 `_words`・`_replacements`・`_pattern_matches`・`_flag_hits` を含む digest で一致)。lint-post の allow で語の一部を外す case のうち、規則が入るまで比べない件数が 6 から 2 に減った (残りは calque・katakana_term)。
    表は定数 10 個 (message 3・語の境目 7) を記録と比べる。`_flag_pattern` (語の並び 11 通り)・`_replacement_bounded` (26 通り)・`_project_term_message` (13 通り)・`_project_term_findings` (flag と使う語の 5 通りを手書きの文書 1 つに) の中身も本家と比べる (lint-base に 4 関数を足した)。
  - 足した入力: `rawgap/g20-forms.md` (使う語の中に収まる当たり・語とパターンが同じ行・パターンの名前だけ・引用・コード・表・見出し)。`_pattern_matches` で同じ名前のパターンが 2 つある形は、fixture の語の一覧が持たないので比べていない (未確認。flag-patterns.txt に同じ名前の行を 2 つ書いたときに `_pattern_matches` の値を合わせる分岐)。
  - わざと壊した 15 本が想定の検査で red (使う語の除外・必須の字の絞り込み・message の 3 形のうち 2・内部の欄 2・引用の伏せ・使う語の添え・当たりの重なりの除去・長い語を先にする並び・漢字の端の境目・使う語の英数字の境目・表 2)。1 本は等価と判断した: `isOpaqueRow` を外す変異 (コード・frontmatter・HTML・参照の定義の行は `proseVisibleText` が全部を空白にするので、飛ばさなくても `Strip == ""` で飛ぶ)。
  - README の「message を作り直す関数」の節を、G14 の `calqueMessage` だけが panic する状態に直した。

### G14 の敵対的レビュー (opus、2026-11-13)

対象は G14 の 4 規則 (calque・metaphor_label・katakana_term・thin_evaluation) と、40 規則の全件比較。本家 (Python 3.15.0) と 114 万件の入力で検出の生の並び (内部の欄を含む) を比べて違いは 0 件、CLI の出力 18,000 件も stdout と rc が全件一致で、本家と違う答えを緑のまま通す指摘 (P1) は 0 件。P2 が 10 件、P3 が 2 件で、実装を崩しても `go test ./lint` が緑のままなのは、記録に入力が無いためだった。
壊せなかったもの: saki の 8 字の窓 (当たりの前に延びるのは最大 2 字)・End で見る「渡」「わた」・`person.End > subjectEnd`・`の題` の門・`labelMarks` の 2 か所・句の名前を探すループの `break`・thin の名詞の `break`・先頭の平仮名を外す処理 (どれも等価。レビューの redteam.md に理由)。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P2-1 `!= "より"` を外しても緑 | 直した | `rawgap/g14-guard-forms` に「サーバーは項より先に送ります。」。外すと red |
| P2-2 「先」のあとのカタカナの条件を外しても緑 | 直した | 同じ入力に「逃がし先サーバーへ」。外すと red |
| P2-3 前の窓 10 字 → 9 字でも緑 | 直した | 同じ入力に「コンピュータールーム入口」。9 字にすると red |
| P2-4 の口の窓 6 字 → 5 字でも緑 | 採らない (等価) | 窓の中に要るのは `calqueMouthReal` の最長の語「トンネル」(4 字) まで。窓が 4 字以上なら答えが変わらないので、6→5・6→4 は本家と同じ答えを出す。入力「トンネルの口」で 3 字に縮めると red になるので、窓の下限は固定されている。再提起に要るのは、6→5 か 6→4 で本家と違う答えを出す入力 (`calqueMouthReal` に 5 字以上の語を足さない限り作れない) |
| P2-5 「全身」の除外を外しても緑 | 直した | `g14-guard-tech-hibiku` (技術の語 6 種類 + 「APIの変更で全身に響きます。」)。外すと red |
| P2-6 `start < span[1]` → `<=` でも緑 | 直した | 同じ入力 forms に「課題を抱え拾います。」(本家は「拾う」を出す)。`<=` にすると red |
| P2-7 `QuoteDepth != 0` を外しても緑 | 直した (指摘の入力では再現せず) | 段落の引用の行は伏せた本文が空になるので、外しても答えが変わらない (指摘の「引用の行」は、レビューの入力の別の形に当たっていた)。差が出るのは引用の中の箇条書き (`> - サーバーが通知を拾う。`)。`g14-guard-quote` に足し、外すと本家に無い calque が出て red |
| P2-8 `label == "" &&` を外しても緑 | 直した | `g14-guard-label-place` (1 行目が「着地先」で本文 60 字以上)。外すと calque も出て red |
| P2-9 ラベルの語尾の除去を外しても緑 | 直した | `g14-guard-label-stem` (`# 受付係` と「交付係」を含み「受付」を含まない本文)。語幹の除去を外すと red |
| P2-10 型の番号の重なりの除去を外しても緑 | 直した | `lint-post.json` に `allow-partial\|calque\|same-type-rest` (同じ型の語が 2 つ残る形)。外すと message に「型1。型1」が並んで red。CLI の allow.txt では足さない (記録の case を 1 つ足せば済むので) |
| P3-1 `TestCLIMatchesPythonRecording` が全件を固定していない (`allRulesImplemented` の頭に `return false` で緑) | 直した | 規則が入ったかで比べる範囲を変える枝と、役目を終えた `allRulesImplemented`・`implementedCount` を消し、全 8,072 件を比べる。比べた件数が case の数と違えば落とす |
| P3-2 型 5 の `break` を外しても緑 | 直した | `g14-guard-forms` に「データベースは背景実行で背景実行を使う。」。外すと `_word_types` が重なって red |

- 記録の作り直しは `-only lint-raw` と `-only lint-post` だけ。`lint-cli.json` は入力が変わらないので作り直していない (rawgap の入力は CLI の case に入らない)。
- わざと壊した 13 本 (P2-1・2・3・5・6・7・8・9・10 と P2-4 を 3 字に縮めたもの・P3-2 の 11 本、P3-1 の飛ばしを戻したもの、等価と判断した P2-4 の 5 字) のうち、12 本が想定の検査 (`TestRawFindingsMatchPythonRecording` の該当入力・`TestPostProcessingMatchesPythonRecording` の該当 case・`TestCLIMatchesPythonRecording` の件数の検査) で red、等価の 1 本は green。ログは tmp/yomiyasu-gap-2026-11-12/lint-groups/g14/fix-mut-*.log。
- 未確認 (レビューの記録): 生成器が本家の字面から語を作るので、人の自然な文でしか通らない分岐 (「全〇〇に響く」で主語の終わりが「全〇〇」より前にある細かい形など) を取りこぼしている可能性。比べた Python は手元の 3.15.0 だけで、記録を作ったバージョンと同じ答えかは確かめていない (記録の作り直しは同じバージョンで通った)。

### production 到達不能の検査 (make test-unused-excluding-tests) の赤を直した (2026-11-13)

- 278882a82 から 6 回続けて赤だった (CI の Tests。`src/ja-writing/lint` の 52 件)。原因は、群を入れる間 `make -C src/ja-writing lint test` だけを回し、repo の root の `make test-unused-excluding-tests` (staticcheck の U1000 を `-tests=false` で回す。module の lint にもテストにも入らない) を手元で回していなかったこと。手元で回すと同じ 52 件が出た。
- 直した中身 (6 commit)。allowlist は使わなかった。
  - G14 の定数 5 個 (`calquePersonEnd`・`kikuLiteral`・`publicationWords`・`calqueStyleContextPattern`・`calqueSubjects`): 本番の正規表現が同じ中身を別に写して持ち、定数は本家と比べる値としてだけ在った。本家と同じく定数から組む形にして写しを 1 つにした (`calqueSubjectWord`・`calqueTech`・`calquePersonSubject`・`calquePersonExt`・`calqueVersionPub`・`calqueStyleContext`・罫の手掛かり・`kikuRE`・`calqueMattersFocus`・`translatedTermPatterns` の `_VERSION_DEMONSTRATIVE` の行)。`kikuLiteral` は本家と同じく `kikuLiteralNear` と `kikuLiteralSoftware` に分けた (`context_near` は前者だけを使う)。
  - 絵文字の 2 個 (`emojiFlagPattern`・`emojiPatternText`): 本番は正規表現で照合しないので、テストだけが使う。`emoji_pattern_test.go` へ移した。
  - 候補の内部の欄を記録と比べる値にする 5 個 (`internalValue` ほか): `finding_internal_test.go` へ移した。
  - G1〜G10・G14〜G20 の空の `gNNState` と `lintState` の欄、何も呼ばない `g08Prepare`、欄の重なりを見る `TestGroupStatesAreSeparateFields` と `groupStates`: 並行して群を入れるときに欄がぶつからないための足場で、40 規則が入った今は役目を終えたので消した。
  - `implementedRules`・`groupDependencies`・`sortedRules`: 消した。守っていたのは「依存先の群が入るまで依存元の規則を true にしない」という入れる順だけで、全部が入って true にする表も無くなった。raw の比べは 40 規則の全部を検出の順のまま比べる形になり、規則の表にない規則の候補が出たら落ちる・規則ごとに 1 件以上比べる、は残した (`TestRawFindingsMatchPythonRecording`)。`ruleGroup` は規則 ID の集合の一覧として `rules_test.go` へ移した。`findingsOf` もテストへ移した。
  - 重複の検査 (`make test-dupscan-gate`) が、直した後の `calqueSubjectWord` と `calqueTech` の組み立ての写しを止めたので、`calqueSubjectPattern` に寄せた。
- G14 の定数 5 個の結論: 本番の値と比べる値は、どちらも組んだ後の正規表現として本家の記録と比べられていたので、比べが完全に空振りしていたわけではない。ただし定数と本番の写しが別々に在ると、定数側だけを直しても本番が変わらず、定数の比べは本番を守らない写しを見ることになる。本家の死蔵の定数は無かった (5 個とも本家で `_CALQUE_SUBJECT_WORD` などの組み立てに使われている。`_CALQUE_PERSON_END` は 3452・3453 行、`_KIKU_LITERAL` は 1255・1270・3679 行、`_PUBLICATION_WORDS` は 989・4451 行、`_CALQUE_STYLE_CONTEXT_PATTERN` は 4479・4521 行、`_CALQUE_SUBJECTS` は 3447・3556 行)。
- わざと壊した本数: G14 の定数 5 本 (`calquePersonEnd`・`kikuLiteral`・`publicationWords`・`calqueStyleContextPattern`・`calqueSubjects` の中身を 1 字変える。どれも `TestLintBaseMatchesPythonRecording` が red) と、規則の足場の撤去 4 本 (`g20ProjectTermFindings` の呼び出しを外す → `TestRawFindingsMatchPythonRecording` が red・規則 ID を表にないものに変える → 「規則の表にない」で red・`ruleGroup` から 1 規則を外す → `TestEveryRuleHasHitsInRawRecording` が red・`projectTermMessage` を壊す → `TestPostProcessingMatchesPythonRecording` が red)。9 本とも想定の検査で red。
- 確かめていない: 手で回した検査が CI より狭い範囲か (CI の Tests の run は push 後に見る)。移した関数 (`emojiFlagPattern` ほか) は挙動を変えない移動なので、わざと壊す確認はしていない (移した先のテストが緑のまま通ること・staticcheck が報告しなくなったことだけ見た)。
