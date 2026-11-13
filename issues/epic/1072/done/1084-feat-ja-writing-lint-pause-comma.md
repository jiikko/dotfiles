# 1084 (feat): ja-writing lint の読点の群 G13 (pause_comma) を Go で作り直す

起票日: 2026-11-13

## 概要

1075 (lint の 40 規則) の群 G13 (規則 `pause_comma`) を切り出したもの (dotfiles-c5 からの分担。担当は dotfiles-bb)。短い主語・主題のすぐあとの読点
(「私は、」「設定を、」) を拾う規則で、本家の `yomiyasu_lint.py` (v3.1.0、`db78b9bc`) の 2967〜3380 行と、`lint_text` の本体の 5878〜5904 行にある。
G13 がほかから使うのは、T3 の `_prose_visible_text` (行をつなぐ関数が見える字を取る)・1074 の `analyze_markdown`・骨組みの行のループが作る `plain_text` だけで、ほかの群には頼らない。
G13 の外で使われるのは、G16 (ai_phrase) の 4997 行の `_TE_NOT_VERB` (`endswith` で使うので並びは無関係)・G17 (slide_comma) の 4868 行の `_PAUSE_TE_ATTR_OBJECT`・候補の欄 `_heads`・`_objects` だけ。

## 本家の中身

- 関数: `_pause_comma_chunk` (助詞のかたまりを長いものから照合)・`_pause_comma_next_subject` (読点のあとに別の主語・主題が来るか)・`_pause_comma_quote_blocks`・
  `_pause_comma_context` (行につき 1 回作る深さ・句の頭・文の範囲・文頭の表)・`_pause_comma_phrase_end`・`_pause_comma_prev_segment`・`_pause_comma_list_before`・
  `_pause_comma_hits` (「は、」「が、」「も、」の当たり。除外の条件 a〜t)・`_pause_comma_object_hits` (2.5.0 の短い目的語の「を、」)・
  `_pause_comma_line_hits` (段落と箇条書きの 1 項目ごとに見える行を "" でつないで当て、行番号ごとの当たりの表を返す。表にある行は当たりが無くても空の並びを持つ。
  引用の行・kind が paragraph でも list でもない行・見える字が空の行で切り、`_PAUSE_ITEM_START` (`•` と全角の `．` を含む) を raw に当てたときと、前の行が `_PAUSE_LABEL_LINE` (「■影響」) のときもつながない。
  段落がかなか読点で終わるときは最後の句点より後ろの当たりを捨てる (句点が 1 つも無いと `rfind` が -1 で、その段落の当たりは全部捨てる))
- 正規表現: module の読み込みで作るもの 27 (`_lazy_compile`) と、関数の中で書くもの 14 (`re.search` 10・`re.split`・`re.sub`・`re.finditer`・`re.match` 1 ずつ)。
  `_PAUSE_TE` は `_TE_NOT_VERB` の 80 語ごとの否定の後読み `(?<!語)` を連ねる。
- 写すときにずれやすい所: 「同じ文で並んだ句」の数 (条件 r) は、位置に当たる接尾辞を全部数え、`_pause_comma_chunk` の「こひもあ・ん・な」の除外を通さない
  (`ことは、猫とは、仲良し。` は 2 つとも r で外れる。2 つの照合を 1 つに寄せると壊れる)。`_pause_comma_chunk` の `prev in "こひもあ"` は行頭で prev が空文字なので常に真
  (`とは、何か。` は `[(1, 'は', 'と')]`)。`re.split(r"(、|。)")` は区切りを残す (pyre に split の区切りを残す形が無いので手で写す)。`lstrip(集合)`・`\s` は Python の意味で扱う
- 表: `_PAUSE_COMPOUND` 25 (長さの降順。set から作るので同じ長さの語の順は PYTHONHASHSEED で変わるが、同じ位置で同じ長さの 2 語が同時に当たらないので答えは変わらない)・
  `_PAUSE_CONNECTIVE` 33・`_PAUSE_SKIP_CHUNKS` 5・`_TE_NOT_VERB` 80・`_PAUSE_FRAME_CHUNKS` 6・`_PAUSE_OBJECT_QUESTION` 6
- `lint_text` の本体: 行のループは code・frontmatter・html_block・reference_definition (5659)・見える字が空の行 (5668)・引用 (5692)・見出しと表の行 (5726・5729) を pause の節より前で飛ばす
  (見出しには `_pause_comma_hits("# 会議は、明日です")` が当たるが、本家の lint は見出しの行を候補に出さない。骨組みで呼ぶ位置を誤ると Go だけが当てる)。
  `plain_text` は `_prose_visible_text(...).strip().replace("__", "").replace("*", "")` で、行をつなぐ関数の各行と同じ式。
  `pause_comma` を当てるか (`DOC_TYPE_ONLY_RULES`。13 種類のうち 11 種類で当て、種類の指定なし・promo・script では当てない)・`_pause_comma_line_hits(analysis, objects=doc_type != "essay")` を 1 度だけ作る・
  表に無い行は `_pause_comma_hits(plain_text)` (と essay 以外では目的語の当たり) で当て直す
  (入力の集まりと dotfiles の docs・issues の 842 本でこの経路に来た 149 行は全部 thematic_break か heading_underline で、当たりは 0。この経路を外す変異は緑のまま残りうる)・message (当たりの最初の 3 つを「句+かたまり、」でつなぎ、4 つ目からは「ほかNか所」。
  「を」の当たりがあれば「短い主語や主題、目的語」)・`_objects` (目的語+「を」の並び) と `_heads` (主題か主語の当たりがあるか)

## 対応方針

1. **str を受ける関数** (`_pause_comma_hits`・`_pause_comma_object_hits` とその部品) を `src/ja-writing/lint/` の G13 専用のファイルに写す。位置はコードポイントの添字、
   正規表現は `internal/pyre`、`strip` などは `internal/pystr` で本家と同じ意味にする。識別子は `pause` で始め、土台 (T1〜T4・骨組み) が同じ package に足すものとぶつからないようにする
2. **行をつなぐ関数** (`_pause_comma_line_hits`) は `visibility.Analysis` を受け、見える字の取り方 (本家の `_prose_visible_text`。T3) は引数の関数で受ける。
   骨組みが入るまでは、試験の中で `Analysis.LineVisibleText` と本家と同じ種類の一覧 (`_PROSE_SCAN_KINDS`) を使う
3. **G16・G17 が使うもの** (`_TE_NOT_VERB`・`_PAUSE_TE_ATTR_OBJECT`) は公開する。骨組みにつなぐまで G13 の関数は製品のコードから呼ばれず、lint の unused が落とすので、
   G13 は `lint/pausecomma` の package に置いて入口を公開する (骨組みと G16・G17 は import して使う。名前のぶつかりも無くなる)。`_heads` と `_objects` は候補の欄なので、骨組みにつなぐときに入れる
4. **骨組みへのつなぎ** (行のループから呼ぶ・message・`_objects`・`_heads`・実装済みの規則の表に足す) は、dotfiles-c5 の土台が master に入った後に行う。それまで `lint/lint.go` と骨組みのファイルには触らない
5. **本家と同じ答えの確かめ**: 自分用の driver (`oracle/py/record_pause_comma.py`) と記録 (`testdata/golden/pause_comma.json`)。
   - str の関数は、入力の集まりの全ファイル (lint と同じく `decode_source(bytes)[0]`) の行のうち、trigger の字 (は、が、も、を、など) を含む行を本家の 2 関数に通した結果を記録する。
     段落としてつないだ文字列は、行をつなぐ関数の記録が通す (本家と同じく "" でつなぐ)。
     ほかに、除外の条件のそれぞれに当たる文を手で作って足す (入力の集まりでは 0 回の条件がある: e・「も」の逆接・手紙の宛名・r の「も」・t・目的語の 3 つ)
   - 条件ごとの通過は Go の側で数える: `Hits`・`ObjectHits` の除外ごとに名前を付け (本家の a〜t の文字に無い条件や、1 行に OR でつないだ条件には枝の名前を足す)、
     記録の全入力で全部の名前が 1 回以上通ることを試験で見る (本家の通過を数えるには settrace が要り、それでも OR のどちらかは分からないので、Python の側では数えない。
     Go の答えが記録の全入力で本家と同じなので、Go で通った条件は本家の同じ条件の答えでもある)
   - 行をつなぐ関数は、入力の集まりの全ファイルを `analyze_markdown` に通してから `_pause_comma_line_hits(analysis, objects)` (objects は真と偽の両方) を呼んだ結果を記録する。
     入力の集まりでは「つながない」2 つの分岐が 2 回と 6 回しか通らないので、手で作った Markdown の文書 (行をまたぐ文・項目の切れ目・見出しのような行・引用・かなと読点で終わる段落・句点の無い段落) も足す
   - 記録は大きくなりうるので、入力の集まりのファイルは中身を書かずにパスと SHA-256 で持つ (1081 の `difflib.json` と同じ形)
   - header は他の記録と同じ (`TestGoldenFilesShareOneEnvironment` に足す)。`make golden` は PYTHONHASHSEED を変えて 2 回作り、同じバイト列のときだけ書く (`_PAUSE_COMPOUND` の並びが seed で変わっても答えが変わらないことをここで見る)
6. 共有のファイル (`oracle/cmd/golden/main.go`・`oracle/golden_test.go`・README・`src/PACKAGES.md`) への追記は登録の数行と表の行に留める

## 受け入れ条件

- [x] str の 2 関数が、記録の全入力で本家と同じ (位置・かたまり・句) の並びを返す
- [x] 除外の条件 a〜t と目的語の条件のそれぞれで、当たる入力と外れる入力が記録にある (条件ごとの数を試験で見る)
- [x] 行をつなぐ関数が、入力の集まりの全ファイルで本家と同じ表を返す (objects の真と偽)
- [x] G16・G17 が使う値が読める (`lint/pausecomma` の `TENotVerb`・`TEAttrObject`)
- [x] 新しい試験は実装を壊すと落ちることを `mutate-verify-list` で確かめた
- [x] `make -C src/ja-writing lint test`・root の `make test-lint`・`make test-dupscan-gate` が通る
- [ ] (1075 へ移した) 骨組みにつないだ後、本家の lint の記録を `pause_comma` で絞った検出が一致する (土台が入った後。比べられるのは種類を付けた run (1 種類につき 12 本) だけで、素の run には pause_comma が出ない)

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_lint.py` (2967〜3380 行・5878〜5904 行)
- `src/ja-writing/lint/`・`src/ja-writing/visibility/` (1074)・`src/ja-writing/oracle/`
- 1075 (lint の土台と群の分け方)・1081 (記録の形の前例)

## 進捗

- 実装 (2026-11-13、dotfiles-bb): `feat(ja-writing): lint/pausecomma に G13 (pause_comma) を写す (issue 1084)`
  - `src/ja-writing/lint/pausecomma`: `Hits` (_pause_comma_hits)・`ObjectHits` (_pause_comma_object_hits)・`Fallback` (lint_text が表に無い行で当て直す形と flush の形)・
    `LineHits` (_pause_comma_line_hits。見える字は引数の関数で受ける)・`TENotVerb` (G16 用)・`TEAttrObject` (G17 用)。骨組みにつなぐまで製品のコードから呼ばれず、
    同じ package に置くと lint の unused が全部を落とすので、別の package にして入口を公開した
  - 記録: `oracle/py/record_pause_comma.py`・`oracle/pause_comma.go`・`oracle/pause_comma_texts.go`・`testdata/golden/pause_comma.json` (約 380 KB)。
    手で作った文 122・Markdown の文書 21・入力の集まりの 332 ファイル (trigger の字を含む 2,768 行と、objects の真偽の行をつなぐ表)。他の記録は作り直しても変わらなかった
- 試験: `TestMatchesPythonRecording` が記録の全入力で 3 関数の一致を見る。除外の条件 36 個 (本家の a〜t と、同じ字の条件の中の枝・目的語の条件) が全部 1 回以上通ることも見る
- 実装を壊して落ちることの確認 (`mutate-verify-list`): 22 本中 21 本が想定どおり赤 (行頭の空の prev・r の数え方・かたまりの並び・手紙の挨拶の位置・かっこの中の長さ・空白を除いた長さ・
  84 字の窓・文の終わりで窓を切る (主題と目的語)・かっこの伏せ字・読点の直後の「は」・目的語の長さ・句点の無い段落・見出しのような行・`*` を外す・bisect_right・`_TE_NOT_VERB` の後読み・
  「と」で始まる句の前の読点・前の句の記号を外す・`Fallback` の並べ替え)。最初の run で緑だった 8 本のうち 7 本は、記録に境目の入力を足して赤にした。
  緑のまま通る 1 本 (`gaIsConjunction` の名詞の終わりの否定を外す) は等価: 述語の終わりは全部ひらがなで終わり、名詞の終わりの字の集合はひらがなを含まない (理由はコードのコメント)
- 敵対的レビュー (opus 1 体、実装): ランダムな文字列 23 万件 (サロゲート・改行・各種の空白・表の語を混ぜる) と Markdown 2.4 万件で本家との違いは 0 件・panic 0 件。
  指摘は試験の穴 6 件 (全部、入力を足して赤にした) と、骨組みが当て直しの並べ替えを写さずに済む口が無い P3 (`Fallback` を足した)。
  等価として残したもの: `nonlead` による文頭の判定は本家でも `j == 文の始まり` と同じ値になる (レビューが証明と 3 万件で確かめた)・`ObjectHits` の pos つきの match は切り出しと同じ (i+1 が読点なので後読みが前を見ても当たらない。未証明)
- CI (2026-11-13、85089b1c2): src/ja-writing・unused・dupscan gate・Lint が緑
- 1075 へ移したもの (dotfiles-c5 の判断で、G3 と一緒に骨組みにつなぐ): 骨組みへのつなぎ (行のループから `Fallback`・`LineHits` を呼ぶ・message・`_objects`・`_heads`・実装済みの規則の表) と、本家の lint の記録を `pause_comma` で絞った突き合わせ。口の形は 1075 の「G13 をつなぐ口」に書いてある
- 反証レビュー (opus 1 体、2026-11-13): P1 0・P2 5・P3 5。全部を本文に入れた (G13 がほかから使うもの・行のループで飛ぶ行・当て直しの経路が実質 0・行をつなぐ関数の分岐・
  r の数え方・条件の数え方の単位・行頭の空の prev・G13 の外で使われる名前・比べられる記録・"" でつなぐ・seed)。反証できなかった主張: 行の範囲・正規表現と表の数・PYTHONHASHSEED で答えが変わらないこと・message と `_objects` `_heads` の作り方
