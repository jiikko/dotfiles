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

- [ ] 本家の公開している関数 (lint・diff・check が import する名前) が Go にあり、入力の集まりで本家と同じ値を返す (1073 の関数の単位の記録との突き合わせ。subcommand は要らない)
- [ ] 語の一覧のフォルダの探し方が、通常の checkout・git の worktree・共有の checkout の 3 つで本家と同じフォルダを選ぶ。
  3 つの形の git の fixture は、テストの中で `t.TempDir()` に本物の `git init` / `git worktree add` で作る (継承した `GIT_DIR` などを外してから)。
  同じ fixture の上で本家の関数の記録も取る
- [ ] `flag-patterns.txt` の検査器が、本家が受ける・断るパターンを同じように受ける・断る (断る理由の文も揃える)
- [ ] 最悪の組の所要を本家と Go で測り、上限の値の判断を README に書く。測るのは同じ入力を交互に複数回で、起動の時間を分けて書く。
  テストの合否には時間を使わず、試す数で判定する
- [ ] `make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/markdown_visibility.py`
- 1073 (土台)・1075 (lint が最初の利用者)

## 進捗

- (未着手)
