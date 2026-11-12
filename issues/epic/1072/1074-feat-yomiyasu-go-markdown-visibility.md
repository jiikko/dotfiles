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
- 本家との突き合わせ: `oracle.VisibilityCases` が 903 case・240,196 呼び出しを作り、`go run ./oracle/cmd/golden` (`make golden`) が本家に通して `testdata/golden/visibility.json` (約 2.5 MB) に記録する。
  結果は正規の JSON の SHA-256 の先頭 8 字で、dict / list は形ごと、例外は型と文ごとに比べる。入力は入力の集まり 332 ファイル・手書きの Markdown 106 件・乱数 (400 文書・4,000 インライン・3,000 参照定義)・
  `decode_source` の bytes・`check_pattern` 約 3.3 万・語の一覧と git の fixture・パスの fixture。driver (`py/call_function.py`) には save / `$ref`・bytes・サロゲート・digest・作業フォルダの印・fixture の op を足した。
  `make golden` を全部回し直し、既存の記録 (patterns・pyre・unicode・json・pystr・smoke) は 1 バイトも変わらなかった。
- 語の一覧のフォルダの探し方: 通常の checkout・linked worktree・`--separate-git-dir` と `core.worktree` の共有の checkout を、テストの中で `t.TempDir()` に本物の `git init` / `git worktree add` で作り (`oracle.Fixture`。git の環境は空から作る)、
  同じ操作を本家の sandbox の中でも行って記録した。手で書いた git の形 46 種・`--words`・`~` の展開も同じ。
- 空振りを止める検査 (`TestRecordingCoversEveryPublicFunction`): 関数ごとの記録の件数が 0 でないこと、例外を投げうる関数は例外と成功の両方があること。
- 最悪の組 (`go run ./oracle/cmd/worstcase`): 10 万字の行に試す数 512 の最短一致の組を当て、本家と Go を 5 回ずつ交互に測った (Apple M3 Max)。Go の最悪は `\w{0,7}?[a-z]{0,7}?\S{0,7}?d` の search 0.64 秒 (Python は 0.49 秒)、
  起動は Python 0.021 秒・Go 0.008 秒、compile は 1 ms 未満。上限は 512 のまま据え置く。数字の全部と条件は README。
- 新しく書いたテストをわざと壊して確かめた: 46 本の壊し方を `bin/mutate-verify-list` で当てた。構文エラーだった 6 本は当て直した。結果は、40 本が想定どおり落ち、1 本 (`doctypes-order`) は落ちたが想定と別の検査で、5 本が落ちなかった。
  落ちなかった 5 本は、結果が変わらない書き換えと判断した: `table-no-pop` (空のブロックが後の工程で捨てられる)・`columns-tab` (桁数は 4 以上かだけを見る)・`home-empty-only` (HOME が "/" のとき後の検査が同じ値を返す)・
  `pattern-class-dash` (前の字が無いとき 0 の字と比べるだけ)・`pure-join-normalize` (後で `AbsolutePath` が同じ整え方をする)。`doctypes-order` は落ちたが、落ちた検査の名前が想定と別 (15 件の表示の上限の外に定数の食い違いが出る) で、順の違いが文に出る別の呼び出しが先に落ちた。
  最初に落ちなかった `tag-closing`・`samefile-branch` は、入力 (`</b c>` と大文字小文字だけが違う cwd のパス) を足して落ちるようにした。
- 確かめたコマンド: `make -C src/ja-writing lint test`・`make test-dupscan-gate`・root の `make test-lint` (`src/PACKAGES.md` は `scripts/go_packages_index.sh` で作り直し)。
- 未確認: CI の macOS runner での所要 (測ったのは手元の 1 台)・lint (1075) が `Required` で行を絞るか。
