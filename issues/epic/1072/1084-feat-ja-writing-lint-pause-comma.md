# 1084 (feat): ja-writing lint の読点の群 G13 (pause_comma) を Go で作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-13〜）

起票日: 2026-11-13

## 概要

1075 (lint の 40 規則) の群 G13 (規則 `pause_comma`) を切り出したもの (dotfiles-c5 からの分担。担当は dotfiles-bb)。短い主語・主題のすぐあとの読点
(「私は、」「設定を、」) を拾う規則で、本家の `yomiyasu_lint.py` (v3.1.0、`db78b9bc`) の 2967〜3380 行と、`lint_text` の本体の 5878〜5904 行にある。
G13 はほかの群と土台の T1〜T3 に頼らない。G16 (ai_phrase) と G17 (slide_comma) が G13 の `_TE_NOT_VERB`・`_PAUSE_TE_ATTR_OBJECT` と、候補の内部の欄 (`_heads`・`_objects`) を使う。

## 本家の中身

- 関数: `_pause_comma_chunk` (助詞のかたまりを長いものから照合)・`_pause_comma_next_subject` (読点のあとに別の主語・主題が来るか)・`_pause_comma_quote_blocks`・
  `_pause_comma_context` (行につき 1 回作る深さ・句の頭・文の範囲・文頭の表)・`_pause_comma_phrase_end`・`_pause_comma_prev_segment`・`_pause_comma_list_before`・
  `_pause_comma_hits` (「は、」「が、」「も、」の当たり。除外の条件 a〜t)・`_pause_comma_object_hits` (2.5.0 の短い目的語の「を、」)・
  `_pause_comma_line_hits` (段落と箇条書きの 1 項目ごとに見える行をつないで当て、行番号ごとの当たりの表を返す)
- 正規表現: module の読み込みで作るもの 27 (`_lazy_compile`) と、関数の中で書くもの 14 (`re.search` 10・`re.split`・`re.sub`・`re.finditer`・`re.match` 1 ずつ)。
  `_PAUSE_TE` は `_TE_NOT_VERB` の 80 語ごとの否定の後読み `(?<!語)` を連ねる
- 表: `_PAUSE_COMPOUND` 25 (長さの降順。set から作るので同じ長さの語の順は PYTHONHASHSEED で変わるが、同じ位置で同じ長さの 2 語が同時に当たらないので答えは変わらない)・
  `_PAUSE_CONNECTIVE` 33・`_PAUSE_SKIP_CHUNKS` 5・`_TE_NOT_VERB` 80・`_PAUSE_FRAME_CHUNKS` 6・`_PAUSE_OBJECT_QUESTION` 6
- `lint_text` の本体: `pause_comma` を当てるか (`DOC_TYPE_ONLY_RULES`)・`_pause_comma_line_hits(analysis, objects=doc_type != "essay")` を 1 度だけ作る・
  表に無い行は `_pause_comma_hits(plain_text)` (と essay 以外では目的語の当たり) で当て直す・message (当たりの最初の 3 つを「句+かたまり、」でつなぎ、4 つ目からは「ほかNか所」。
  「を」の当たりがあれば「短い主語や主題、目的語」)・`_objects` (目的語+「を」の並び) と `_heads` (主題か主語の当たりがあるか)

## 対応方針

1. **str を受ける関数** (`_pause_comma_hits`・`_pause_comma_object_hits` とその部品) を `src/ja-writing/lint/` の G13 専用のファイルに写す。位置はコードポイントの添字、
   正規表現は `internal/pyre`、`strip` などは `internal/pystr` で本家と同じ意味にする。識別子は `pause` で始め、土台 (T1〜T4・骨組み) が同じ package に足すものとぶつからないようにする
2. **行をつなぐ関数** (`_pause_comma_line_hits`) は `visibility.Analysis` を受け、見える字の取り方 (本家の `_prose_visible_text`。T3) は引数の関数で受ける。
   骨組みが入るまでは、試験の中で `Analysis.LineVisibleText` と本家と同じ種類の一覧 (`_PROSE_SCAN_KINDS`) を使う
3. **G16・G17 が使うもの** (`_TE_NOT_VERB`・`_PAUSE_TE_ATTR_OBJECT`・`_PAUSE_VERB_ATTR`) は同じ package の名前の付いた値にする。`_heads` と `_objects` は候補の欄なので、骨組みにつなぐときに入れる
4. **骨組みへのつなぎ** (行のループから呼ぶ・message・`_objects`・`_heads`・実装済みの規則の表に足す) は、dotfiles-c5 の土台が master に入った後に行う。それまで `lint/lint.go` と骨組みのファイルには触らない
5. **本家と同じ答えの確かめ**: 自分用の driver (`oracle/py/record_pause_comma.py`) と記録 (`testdata/golden/pause_comma.json`)。
   - str の関数は、入力の集まりの全ファイルの各行 (と、段落としてつないだ文字列) を本家の `_pause_comma_hits` と `_pause_comma_object_hits` に通した結果を記録する。
     ほかに、除外の条件 a〜t と目的語の条件のそれぞれに当たる行と当たらない行を手で作って足す (入力の集まりだけでは通らない条件があるため。条件ごとの当たりの数を記録に残して、0 の条件が無いことを試験で見る)
   - 行をつなぐ関数は、入力の集まりの全ファイルを `analyze_markdown` に通してから `_pause_comma_line_hits(analysis, objects)` (objects は真と偽の両方) を呼んだ結果を記録する
   - 記録は大きくなりうるので、入力の集まりのファイルは中身を書かずにパスと SHA-256 で持つ (1081 の `difflib.json` と同じ形)
   - header は他の記録と同じ (`TestGoldenFilesShareOneEnvironment` に足す)
6. 共有のファイル (`oracle/cmd/golden/main.go`・`oracle/golden_test.go`・README・`src/PACKAGES.md`) への追記は登録の数行と表の行に留める

## 受け入れ条件

- [ ] str の 2 関数が、記録の全入力で本家と同じ (位置・かたまり・句) の並びを返す
- [ ] 除外の条件 a〜t と目的語の条件のそれぞれで、当たる入力と外れる入力が記録にある (条件ごとの数を試験で見る)
- [ ] 行をつなぐ関数が、入力の集まりの全ファイルで本家と同じ表を返す (objects の真と偽)
- [ ] G16・G17 が使う値が同じ package から読める
- [ ] 新しい試験は実装を壊すと落ちることを `mutate-verify-list` で確かめた
- [ ] `make -C src/ja-writing lint test`・root の `make test-lint`・`make test-dupscan-gate` が通る
- [ ] 骨組みにつないだ後、本家の lint の記録を `pause_comma` で絞った検出が一致する (土台が入った後)

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_lint.py` (2967〜3380 行・5878〜5904 行)
- `src/ja-writing/lint/`・`src/ja-writing/visibility/` (1074)・`src/ja-writing/oracle/`
- 1075 (lint の土台と群の分け方)・1081 (記録の形の前例)

## 進捗

- (未着手)
