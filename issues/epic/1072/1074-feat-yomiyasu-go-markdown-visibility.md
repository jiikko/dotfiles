# 1074 (feat): yomiyasu の markdown_visibility を Go で作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-12〜）

起票日: 2026-11-12

## 概要

本家の `markdown_visibility.py` (1,429 行) を `src/ja-writing` の package として作り直す。lint・diff・check がどれも読む共通部で、1073 の土台の次に要る。

## 本家が持つもの (v3.1.0 の top-level の定義から)

- Markdown の解析 (`analyze_markdown`): 行ごとのブロックの種類・コードや HTML・リンクなど「直す候補にしない範囲」(`inline_protected_spans`)・
  行の見える字 (`line_visible_text`)・範囲が保護された所に掛かるか (`range_overlaps_protected`)。CommonMark / GFM を全部は実装しない、狙いを絞った解析
  (本家の docstring がそう書く)。位置は Python の str の半開区間
- 入力の読み方 (`decode_source`): bytes を str にする規則と、その結果の情報
- 文書の種類と読み手 (`DOC_TYPES`・`READER_LEVELS`・`normalize_doc_type`・`normalize_reader`・`context_label`)
- 語の一覧 (`.yomiyasu-words/` の `allow.txt`・`flag.txt`・`flag-patterns.txt`): フォルダの探し方 (`find_words_folder`。git の worktree と共有の checkout を
  辿る)・読み方と上限 (行数・字数・`{m,n}` の上限・試す数の上限)・`flag-patterns.txt` の正規表現の部分集合を検査する解析器 (`check_pattern`。
  使えるのは `\d \D \w \W \s \S \b \B \A \Z \t \n` と記号の前の `\`)
- 入力のパスの問題 (`input_path_problem`)

## 対応方針

- 本家の関数の単位を Go の関数に写し、名前の対応表を package の doc か README に置く (本家の更新を追うときに、どの関数を見直すかを引けるように)
- 正規表現は 1073 の `internal/pyre` を通す。`flag-patterns.txt` の検査器は、利用者が書いたパターンを Python の `re` として compile する前提なので、
  Go 側でも検査を通った部分集合を `internal/pyre` で compile する
- 試す数の上限 (`PATTERNS_MAX_COST = 512`) は、本家が Python の `re` の時間を手元の macOS と CI の Linux で測って決めた値。regexp2 では時間の伸び方が違いうるので、
  同じ最悪の組 (本家のコメント: 10 万字の行で最短一致の繰り返しを重ねた組) を Go で測ってから、値を据え置くかを決める
- 位置は rune の添字で持つ (Python の str の添字と同じ単位)。byte の添字と混ぜない

## 受け入れ条件

- [x] 本家の公開している関数 (lint・diff・check が import する名前) が Go にあり、入力の集まりで本家と同じ値を返す (1073 の関数の単位の記録との突き合わせ。subcommand は要らない)
- [x] 語の一覧のフォルダの探し方が、通常の checkout・git の worktree・共有の checkout の 3 つで本家と同じフォルダを選ぶ。
  3 つの形の git の fixture は、テストの中で `t.TempDir()` に本物の `git init` / `git worktree add` で作る (継承した `GIT_DIR` などを外してから)。
  同じ fixture の上で本家の関数の記録も取る
- [x] `flag-patterns.txt` の検査器が、本家が受ける・断るパターンを同じように受ける・断る (断る理由の文も揃える)
- [x] 最悪の組の所要を本家と Go で測り、上限の値の判断を README に書く。測るのは同じ入力を交互に複数回で、起動の時間を分けて書く。
  テストの合否には時間を使わず、試す数で判定する
- [x] `make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/markdown_visibility.py`
- 1073 (土台)・1075 (lint が最初の利用者)

## 進捗

- 2026-11-13 実装した (`src/ja-writing/visibility/`)。公開面の全関数と、その下の `reference_definition_end`・`find_words_folder`・`load_words`・`check_pattern` を Go にし、名前の対応表は `visibility/doc.go`。
  位置は rune の添字。正規表現は全部 `internal/pyre`。
- 本家との突き合わせ: `oracle.VisibilityCases` が 910 case・240,358 呼び出しを作り、`go run ./oracle/cmd/golden` (`make golden`) が本家に通して `testdata/golden/visibility.json` (約 2.5 MB) に記録する。
  結果は正規の JSON の SHA-256 の先頭 8 字で、dict / list は形ごと、例外は型と文ごとに比べる。入力は入力の集まり 332 ファイル・手書きの Markdown 106 件・乱数 (400 文書・4,000 インライン・3,000 参照定義)・
  `decode_source` の bytes・`check_pattern` 約 3.3 万・語の一覧と git の fixture・パスの fixture。driver (`py/call_function.py`) には save / `$ref`・bytes・サロゲート・digest・作業フォルダの印・fixture の op を足した。
  `make golden` を全部回し直し、既存の記録 (patterns・pyre・unicode・json・pystr・smoke) は 1 バイトも変わらなかった。
- 語の一覧のフォルダの探し方: 通常の checkout・linked worktree・`--separate-git-dir` と `core.worktree` の共有の checkout を、テストの中で `t.TempDir()` に本物の `git init` / `git worktree add` で作り (`oracle.Fixture`。git の環境は空から作る)、
  同じ操作を本家の sandbox の中でも行って記録した。手で書いた git の形 52 種・`--words`・`~` の展開も同じ。
- 空振りを止める検査 (`TestRecordingCoversEveryPublicFunction`): 関数ごとの記録の件数が 0 でないこと、例外を投げうる関数は例外と成功の両方があること。
- 最悪の組 (`go run ./oracle/cmd/worstcase`): 10 万字の行に試す数 512 の最短一致の組を当て、本家と Go を 5 回ずつ交互に測った (Apple M3 Max)。Go の最悪は `\w{0,7}?[a-z]{0,7}?\S{0,7}?d` の search 0.64 秒 (Python は 0.49 秒)、
  起動は Python 0.021 秒・Go 0.008 秒、compile は 1 ms 未満。上限は 512 のまま据え置く。数字の全部と条件は README。
- 新しく書いたテストをわざと壊して確かめた: 46 本の壊し方を `bin/mutate-verify-list` で当てた。構文エラーだった 6 本は当て直した。結果は、40 本が想定どおり落ち、1 本 (`doctypes-order`) は落ちたが想定と別の検査で、5 本が落ちなかった。
  落ちなかった 5 本は、結果が変わらない書き換えと判断した: `table-no-pop` (空のブロックが後の工程で捨てられる)・`columns-tab` (桁数は 4 以上かだけを見る)・`home-empty-only` (HOME が "/" のとき後の検査が同じ値を返す)・
  `pattern-class-dash` (前の字が無いとき 0 の字と比べるだけ)・`pure-join-normalize` (後で `AbsolutePath` が同じ整え方をする)。`doctypes-order` は落ちたが、落ちた検査の名前が想定と別 (15 件の表示の上限の外に定数の食い違いが出る) で、順の違いが文に出る別の呼び出しが先に落ちた。
  最初に落ちなかった `tag-closing`・`samefile-branch` は、入力 (`</b c>` と大文字小文字だけが違う cwd のパス) を足して落ちるようにした。
- 確かめたコマンド: `make -C src/ja-writing lint test`・`make test-dupscan-gate`・root の `make test-lint` (`src/PACKAGES.md` は `scripts/go_packages_index.sh` で作り直し)。
- 未確認: CI の macOS runner での所要 (測ったのは手元の 1 台)・lint (1075) が `Required` で行を絞るか。

### 敵対的レビュー (opus、2026-11-13)

P1・P2 は無し (壊せなかったもの: `analyze_markdown` 系の乱数 2 万文書超・長大入力・範囲の端 26 万回・`decode_source` 2 万件・Unicode 全 code point 350 万字列・`check_pattern` 約 20 万件・`load_words` と `find_words_folder` の実フォルダ各約 3,000・`input_path_problem` 3,000 件で、本家と不一致 0)。指摘は P3 の 2 件で、どちらも再現して直した。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P3-1: `ReferenceDefinitionEnd` が、行の中に改行の字がある入力 (`{"[a]: b \"t", "\n"}`) で panic する (本家は `IndexError`)。`analyze_markdown` は改行のない行だけを渡すので届かないが、lint などが改行つきの行を渡すと本家の例外でなく panic になる | 直した。`rowCursor.nextRowBlank` が範囲を見て `*Error` (`IndexError: list index out of range`) を投げ、公開の `ReferenceDefinitionEnd` が `error` として返す (署名に `error` を足した)。記録の `reference-definition-end` に改行つきの行 10 通りを足した (6 件が例外) | 本家と一致。記録の呼び出しは 3,197 から 3,241 に増えた |
| P3-2: `CurrentDirectory` / `InputPathProblem` が、作業フォルダを消して同じ名前で作り直したとき本家と違う (Python は読めない、Go は読めると答える) | 直した。`getcwd` が `syscall.Getwd` の字面の `os.Stat` と `.` の `os.Stat` を `os.SameFile` で比べ、違えば失敗にする。記録の `paths` の case に `reborn` (作り直したフォルダ) の呼び出し 10 件を足した | 本家と一致 (Python は `None` と「作業フォルダを読めない」の文) |

- 記録を作り直した差分: `visibility.json` で変わったのは `reference-definition-end` と `fixture/paths` の 2 case と関数の件数の表だけ (ほかの 902 case の digest は同じ。ほかの記録 (patterns・pyre・unicode・json・pystr・smoke) は触れていない)。
- 新しいテストをわざと壊して確かめた (`bin/mutate-verify-list` の 4 本、全部 `TestVisibilityMatchesPythonRecording` が落ちた): 範囲の検査を外す・例外を握りつぶして `nil` を返す・例外の型を `ValueError` にする・`SameFile` の検査を外す。最初に当てた 2 本は構文エラー (未使用の変数) だったので当て直した。
- 採らなかったもの: 無し。レビューが等価と判断した 3 件 (`analyze.go` の `>= 0`・`> end`・`<= len(rows)`) は、結果が変わらない書き換えで、本家と並べても不一致 0 だったので手を入れない。

### codex の敵対的レビュー (2026-11-13)

codex はこの環境でコマンドを実行できず、差分をプロンプトに入れて読ませた (主張は読んだだけで、実行した結果ではない)。5 件とも本家 (Python 3.15.0) と Go で再現してから直した。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P1-1: `check_pattern("a\ud800b")` の `Required` が、Go では U+FFFD になる (`string(literal)` が単独のサロゲートを U+FFFD にする。本家は `[["a\ud800b"]]`) | 再現した (記録に足した 7 パターンを、直す前の Go に通すと `surrogates` の 45〜50 番目が落ちた)。`visibility/pattern.go` の 2 か所を `str([]rune{literal})` (WTF-8 を保つ変換) にした。記録の `surrogates` に、サロゲートと通常の字が続く 7 パターン (繰り返しの前後・組の中・`\` の後・字の集まりの中) を足した | 本家と一致 |
| P1-2: 種類の集合の覚えの鍵を NUL でつなぐと、`["escape", "inline_code"]` と `["escape\u0000inline_code"]` が衝突する (結果の覚え 2 か所) | 再現した (直す前の Go は、先に問うた方の答えを後の問い合わせにも返した)。`kindSet` の鍵を、名前ごとに長さを前に付けた符号化 (`6:escape11:inline_code`) にした。種類別のマスクと行の覚えは同じ `kindSet` を通る。記録の `analysis-queries` に、2 つの集合を両方の順で問う 10 呼び出しを足した | 本家と一致 |
| P2-3: 不正な UTF-8 の fixture が、JSON を通ると U+FFFD になり、Go と Python が別のバイト列を書く (`core-bad-utf8`・`core-bad-utf8-in-value`・`commondir-bad-utf8`・`gitfile-bad-utf8`) | 再現した (JSON を通した Python 側のバイト列が `ef bf bd` になることを、`pythonWrites` で確かめた)。`fixtureCase.op` が不正な UTF-8 の中身を `BytesB64` に直す。`bytes_b64` の中の `<<ROOT>>` も、Go (`Fixture.WriteData`) と Python (`run_op`) の両方で作業フォルダの実パスにする。`TestFixtureWritesSameBytesInGoAndPython` が、全 case の write で「Go が書くバイト列」と「JSON を通って Python が書くバイト列」の一致と、不正な UTF-8 を書く fixture が 4 件以上あることを見る | 記録の digest は変わらなかった (既存の case は、別のバイト列でも同じ結果に着地していた。codex の言うとおり) |
| P2-4: fixture の操作が Python で失敗しても記録の生成が止まらない (存在しないパスの `remove` は Python で `FileNotFoundError`、Go の `os.RemoveAll` は成功) | 再現した (`assembleVisibility` に失敗した操作を渡すと、直す前は黙って記録した)。driver が失敗した操作の `exception` を返し、`assembleVisibility` (`RecordVisibility` の後半を関数に分けた) が case 名・操作の位置・種類・パス・例外を出して止まる。`TestAssembleVisibilityStopsOnFailedOperation` と、`visibility/oracle_test.go` のコメントを実装に合わせた | 関数の例外は今までどおり記録に入る |
| P3-5: Python の `begin` が import の前にあり、起動 (壁時計 - 中の所要) から import の時間が抜ける (Go は package の初期化を起動に含む) | 再現した (README に書いた「起動に import を含む」と値が合わなかった: 起動 0.021 秒に import の 0.013 秒が入っていない)。`time_patterns.py` の `begin` を import の後に移し、4 組を 5 回ずつ交互に測り直して README の表を直した | Python の起動は 0.021 → 0.036〜0.038 秒。search と Go は同じ (誤差の範囲)。Go の最悪 (`word`) は 0.66 秒、Python 0.48 秒の 1.4 倍で、上限 512 の判断は変わらない。他の session が走っていて load average 12〜15 の測定 |

- 未確認のリスクの扱い:
  - 32 bit の digest の衝突: 直さない。誤った答えが緑で通るには、誤った呼び出しの全部が記録と同じ先頭 32 bit に当たる必要があり、1 つの誤りあたり 2^-32 (誤りは多数の呼び出しに現れる)。全桁にすると記録が数倍になり差分を読めない。理由を `DigestWidth` のコメントに書いた。
  - `words_context(None)` / `words_label(None)`: 本家の注釈は `dict` (`Optional` ではない) で、check・lint・diff の 7 か所の呼び出し元は全部 `words is not None` の後に呼ぶ。足さない (README の「揃わない範囲」)。
  - `HOME` 未設定: 本家は `pwd.getpwuid`、Go は `os/user.LookupId`。結果に実行した人のホームが入るので記録できない。手元で `HOME` を外して両方が同じホームを返すこと (`CGO_ENABLED` が 0 と 1 のどちらでも) だけ確かめ、README に書いた。
  - 語の一覧の U+2028・U+0085・U+2029: 本家の `_words_text_rows` は LF だけで割る (`splitlines` ではない)。一方 `_shared_checkout` は config を `splitlines` で割る。Go は同じ割り方で、記録に足しても一致した (`allow-unicode-separators`・`allow-10000-with-separators`・`allow-10001-with-separators`・`core-ls-*` 6 case)。
  - 閉じの柵の compile の覚え (`compiledCache`): 鍵は固定の終わりの形と、柵の (字, 幅) だけ。幅が n 種類なら読んだ字数が n^2/2 以上になるので、増える量は読んだ字数の平方根で、上限は置かない。理由を `compiledCache` のコメントに書いた。
  - `Unroot` の同一化 (実際の root と字面の `<<ROOT>>` が同じ結果になる)・compile の後の照合 (`checkedValue` が `pattern` と `flags` だけを比べ、受理したパターンの `Search` の結果まで比べない): 記録して閉じる。前者は、入力の `<<ROOT>>` は全部実パスに置き換わるので、字面の `<<ROOT>>` が結果に現れる入力を作れず、返り値の違いを隠す入力を示せていない。後者は `check_pattern` 以外の記録 (`internal/pyre` の `patterns.json`、870 パターンを 33,644 単位に当てた突き合わせ) が `Search` の結果を本家と比べている。どちらも、反例の入力が示されるまで足さない。
- 記録を作り直した差分: `visibility.json` で変わったのは `analysis-queries` (72 → 82 呼び出し)・`surrogates` (45 → 52)・`fixture/words-load` (257 → 266) と、足した `fixture/hand-core-ls-*` 6 case と関数の件数の表だけ。ほかの 901 case の digest は同じ (bad-utf8 の 4 case を含む)。
- 新しいテストをわざと壊して確かめた (`bin/mutate-verify-list` の 8 本、全部想定どおり落ちた): `pattern-required-fffd`・`kindset-nul-join`・`config-split-lf-only`・`allow-split-splitlines` (どれも `TestVisibilityMatchesPythonRecording`)・`fixture-b64-no-root-token` (同)・`fixture-invalid-utf8-in-content` (`TestFixtureWritesSameBytesInGoAndPython`)・`op-failure-recorded` (`TestAssembleVisibilityStopsOnFailedOperation`)。最初に当てた `fixture-b64-no-root-token` は構文エラー (未使用の import) だったので当て直した。起動の区切り (P3-5) は測定で、テストが無いので壊し方は当てていない。
- 確かめたコマンド: `make -C src/ja-writing lint test`・`make test-dupscan-gate`・root の `make test-lint`。
