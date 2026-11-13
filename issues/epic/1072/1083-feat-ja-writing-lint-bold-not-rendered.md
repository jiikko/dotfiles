# 1083 (feat): ja-writing lint の表示されない太字 (G3 bold_not_rendered) を Go で作る

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-13〜）

起票日: 2026-11-13

## 概要

1075 の群 G3 (規則 `bold_not_rendered`) を切り出したもの (dotfiles-c5 から dotfiles-24 への分担。2026-11-13)。本家 `yomiyasu_lint.py` (db78b9bc) の
2076〜2836 行 (太字の区切りの印の組み合わせ・直し方の案とその確かめ) と、`lint_text` の中の 5577〜5584 行 (候補の文面) を Go で作り、本家と同じ答えを出す。
lint の CLI・`lint_text` の骨組み・後処理 (土台 T1〜T4) は 1075 の担当が作り、G3 の関数をそこから呼ぶ。前提の解析 (markdown_visibility) は 1074。

## 本家の中身 (2026-11-13 に読んだ)

`_bold_problems_with_analysis(text, analysis)` から辿れるものだけを作る。

| 本家 | 行 | 役目 |
|---|---|---|
| `BOLD_ASCII_PUNCT` `BOLD_BRACKETS` `_bold_ws` `_bold_punct_gfm` `_bold_punct_new` `_bold_can_open` `_bold_can_close` `_roles` | 2081-2103・2149 | 印の左右の字で開く・閉じるを決める。句読点の判定が 2 通り (GFM: ASCII の記号と Unicode の P / CommonMark: Unicode の P と S) |
| `_runs_from_masks` `_star_runs` `_direct_escape` `_star_extent` | 2117-2146・2273 | `*` の連なり (伏せた範囲を除く) |
| `_delimiter_parse` `_DelimiterStack` | 2163-2270 | 区切りの印のスタック。3 の倍数の規則・mod 3 と「閉じにもなれるか」の 6 つの組・見つからなかった下限 (`bottoms`。GFM は閉じの側が開けるかを鍵に入れない) |
| `_local_pair_ok` `_bold_scan_context` | 2283-2339 | 2 つの規則で組んだ太字の和と、どちらかで組めない隣の組 |
| `_bold_close_of` `_preserves_other_emphasis` `_bold_fix_with_context` | 2347-2392・2662-2753 | 直し方の案 (かっこの内側・句読点を外へ・空白) を作り、文を書き換えて解析し直して、狙いの組ができ、ほかの組が残るかを確かめる |
| `_bold_repair_index` `_BoldRepairIndex` (`decide`) と区間の小道具 | 2409-2659 | 崩れが多い段落で、解析し直さずに元の解析から答えを出す近道。決められないときは None を返し、解析し直す側へ回る |
| `bold_problems` `_bold_problems_with_analysis` `_bold_short` | 2783-2836 | ブロックごとに回し、候補 (`line` `found` `suggest` `how`) を返す |

作らないもの: `_bold_pairs` `_bold_pair_ok` `_strong_parse` `_bold_fix` `_bold_fix_checked` `_repair_context` `_bold_code_spans` `_bold_pairs_in_block` `_bold_pairs_from_runs` `_bold_scan_runs` `_line_containers`。
`lint_text` からも本家の他の道具からも呼ばれない (2026-11-13 に `grep -n` で名前を数えた。定義と、互いの中の呼び出しだけ)。

## Go の形

- **置き場所は `internal/bold` (lint と diff が共有する) を提案する**。本家の `yomiyasu_diff.py` は同じ関数を自分で持つ (1923〜2685 行)。
  lint の 2081〜2836 行と型注釈を外して AST で比べると、違いは正規表現を module で compile するか関数の中で書くか・`openers` の型注釈・変数の名前 (`repaired` → `repaired_pairs`)・`if changed` と `if len(changed)` だけで、答えを変える違いは無かった。
  lint の中に置くと、1076 の diff が同じものを写し、dupscan の gate が写しを落とす。1075 の担当は「lint/ の中の bold*.go」を想定していたので、置き場所は合意してから決める
- lint 側は `lint/` の中の 1 ファイル (候補の文面を作る 5577-5584 行) だけにし、`internal/bold` の `Problems(text, analysis)` を呼ぶ
- 位置は本家と同じく str の添字 (code point) で数える。入力は `[]rune` にしてから扱う (入力は `decode_source` が厳密な UTF-8 で解くので、単独のサロゲートは来ない)
- 字の判定は `internal/pyunicode` (一般カテゴリ・`str.isdigit`)、`\s` は Python の `str.isspace` と同じ集合、`unicode_whitespace` は 1074 の visibility のものを使う
- 答えが順に左右されないことを確かめた所: `buckets` は作った順 (mod 0〜2 × 閉じにもなれるか) に回し、同じ順位の候補は無い (`stack_index` は一意)。`set(original) | set(repaired)` と `delimiter_matches` の set は、回った結果を `any` か「None にするか」にしか使わず、途中で返さない。候補の並びは `sorted(pairs)`

## 本家と同じ答えを確かめる記録

- **関数の単位**: `oracle.CallFunctions` で `yomiyasu_lint.bold_problems(text, skip_frontmatter)` と `yomiyasu_diff.bold_problems(text, skip_frontmatter)` を同じ入力に当て、
  Go の `internal/bold` と 3 者が同じ答えになることを記録で確かめる (diff 側を写さずに済む根拠を実行で持つ。1075 の担当の依頼)。
  `lint_text` は `analyze_markdown(text)` (skip_frontmatter=True) を呼ぶので、関数の初期値と同じ答えになる。戻りは `list[dict]` で JSON にそのまま入る。
  先に本家の 2 つだけで確かめた: `testdata/corpus/` の 332 ファイル × skip_frontmatter の 2 通り = 664 の呼び出しで、lint と diff の答えは全部同じ (候補は計 172 件。2026-11-13)
- **足す入力** (入力の集まりは今、本家のテストの単位を 1 段落に 1 つずつしか持たず、近道 `decide` の条件 `several`・`slow_tries >= 2`・`remaining >= 3` を満たさない)
  - 近道を通す: 本家のテストと同じ組み方で、崩れた太字の単位を並べた段落 (`unit * 4`・単位の連結・`"\n".join`・`"\n\n".join([unit * count] * 3)`。`tests/test_v202_diff_bold_performance.py` の 74・92 行)。
    単位は、かっこ・句読点・空白の 3 通りの案が当たるもの、`\` で逃がした `*`、コード・URL・リンクの中の `**`、`]` + `(` と `!` + `[` の並び、行頭が `**` のリストと引用を、それぞれ 1 段落の中で並べる (ガードが近道の中で働く場面を通すため)
  - 近道を使わない枝 (2466-2470 行): `<` を含む段落・先頭行が ` {0,3}[` で始まる段落・リスト・引用・見出し・解析し直すと参照の定義の行が現れる段落 (kind が reference_definition のブロックは 2799 行で先に飛ばされるので、それとは別)
  - 3 の倍数の規則と `bottoms` の鍵: 長さ 2 の印だけでは 3 の倍数の規則が答えに出ない (2+2 は 3 で割り切れない)。`*`・`***`・`****` を混ぜた段落を足す
  - GFM と CommonMark で答えが分かれる入力: ASCII の記号は Unicode の P と S にちょうど重なる (BOLD_ASCII_PUNCT と ASCII の P∪S の差は 0 字) ので、分かれるのは ASCII の外の S の字だけ。`**★重要**です`・`**→次**へ`・`**¥100**円`・`**©表示**の` の形
- **近道と解析し直しが同じ答えか**: Go のテストに近道を強制する差し込み口 (本家のテストが `slow_tries = 2` を入れる 3 例。同じ文字列が `testdata/corpus/upstream-tests/test_lint_performance_regressions.txt` の 91・93 行にある) を作り、
  近道が答えを出した案ごとに、解析し直した答えと比べる
- 記録の置き場所は `testdata/golden/` の新しいファイル 1 本。`make golden` から作る口は `oracle/cmd/golden` に 1 行足す (1075 の担当と合意済み。1074 も同じファイルを触るので、後から入れる側が rebase で合わせる)

## Go に写すときに答えがずれやすい所

- **空の文字列**: 本家は文の端の字を `""` で表す (`_roles` の 2150-2151 行・`char_after` の範囲外・`body[:1]` / `body[-1:]`)。`unicode_whitespace("")` は True、`_bold_punct_*("")` は False。
  `[]rune` にした Go の側では「字が無い」を別の値で持ち、この 2 つの答えを揃える
- 段落ごとの状態 (`slow_tries`・`remaining`・`"repair_index" in context`。None を入れても「ある」と読む) を同じ形で持つ。近道が解析し直しと同じ答えを出す限り答えには出ないが、食い違ったときに本家と同じ答えを再現するために要る
- `_URL_START` は先読み `(?=https?://|www\.)` の正規表現。一致は重ならないので、文字列の前から順に見る手書きの走査で同じ位置の列になる
- 1074 の visibility に要る口: `analyze_markdown(text, skip_frontmatter)` (False でも呼ぶ)・`mask_spans` と `mask_ends`・行の `start` `raw` `content` `content_start` `line` `kind`・ブロックの `kind` `lines`・kind つきの `inline_protected_spans`・`unicode_whitespace`

## 進め方

1. 1074 (visibility) が master に入るまで: この設計を反証レビューに通す
2. 入ったら: `internal/bold` を作り、記録と突き合わせる。dupscan の gate・実装をわざと壊して記録との突き合わせが落ちることの確かめ・`make -C src/ja-writing lint test`・root の `make test-lint` を通す

## 受け入れ条件

- [x] 本家の lint と diff の `bold_problems` の記録 (全入力) と Go の答えの 3 者が一致する
- [x] 近道 (`decide`) が答えを出した案で、解析し直した答えと一致する (近道の条件を問わず、案を確かめられる全部の組で近道を作って比べる)
- [x] 実装をわざと壊して (3 の倍数の規則を外す・`bottoms` の鍵を GFM と CommonMark で揃える・近道の確かめを 1 段外す)、突き合わせが落ちる (下の進捗。緑のまま通った 2 本は理由を書いた)
- [x] lint 側の候補の文面 (message・snippet) が本家の 5577-5584 行と同じ
- [x] 反証レビュー (設計と実装)

## 関連

- 1074 (visibility)・1075 (lint の土台と他の群)・1076 (diff。同じ太字のコードを使う)
- 移植の地図: 1075 の「群と順」の節

## 進捗

- 2026-11-13: 本家の 2076〜2836 行と 5577〜5584 行を読み、作るものと Go の形を決めた (上の節)。1074 待ち
- 反証レビュー (opus のサブエージェント 1 体、読み取りのみ): 作るものと作らないものの線・答えが順に左右されないこと・lint と diff の比べ方は崩れなかった。採った指摘
  - P1: 足す入力が近道 `decide` に届かない (本家のテストの単位が 1 段落に 1 つずつ) → 本家のテストと同じ組み方の段落と、近道を強制する差し込み口を足した
  - P1: GFM と CommonMark で分かれる例 `**$100**円` は分かれない (`$` は BOLD_ASCII_PUNCT に入る) → ASCII の外の S の字に替えた
  - P2: 長さ 2 の印だけでは 3 の倍数の規則が答えに出ない・わざと壊して落ちるかを入力の名前で確かめていない・空の文字列の扱い → 入力と受け入れ条件と「ずれやすい所」に足した
  - P3: 段落ごとの状態・参照の定義の枝の書き方・`_URL_START` の先読み・1074 に要る口 → 書いた
  - 却下: なし
- 2026-11-13 (5h 枠のリセット後): 実装
  - `internal/bold` (chars・delimiter・scan・fix・repair・problems) と `lint/bold.go` (`lint.BoldFindings(text, analysis)`。候補の文面) を作った。lint の骨組みはこれを呼ぶ (1075 の「G3 をつなぐ口」)。
    visibility に公開の入口 `Escaped`・`HasPrefixAt` を足した (dupscan の gate が、太字の側に同じ形の関数を書いた写しを落としたため)
  - 記録 `testdata/golden/bold.json` (`go run ./oracle/cmd/golden -only bold`。`oracle/bold.go`・`bold_cases.go`): 入力の集まりの 332 ファイルと足した入力を、skip_frontmatter の 2 通りで本家の lint と diff の両方に当てる。
    2 つの答えが 1 つでも違えば記録を作らずに止まる。全部 (1464 case) で lint と diff は同じ答えだった
  - Go と記録の一致: 1464 case・候補 1847 件の全部 (`TestProblemsMatchUpstream`)。足した入力が全部記録にあることも見る。
    近道と解析し直し: 近道が答えを出した 500 案の全部で一致 (`TestRepairIndexAgreesWithReparse`。決めずに解析し直しへ回した案 74)。lint の文面は本家の `lint_text` に通して取った値と一致 (`lint/bold_test.go`)
  - 実装をわざと壊した確かめ (`mutate-verify-list`): 3 の倍数の規則を外す・`bottoms` の鍵を GFM でも開けるかで分ける・近道の URL の確かめを外す・近道の escape の確かめを外す・GFM の ASCII の記号の扱いを外す (ASCII の S の字 `$**「x」**$` などを足してから)、の 5 本はどれも red。緑のまま通った 2 本:
    - 近道を一度も使わない: 近道は解析し直しと同じ答えを出す作りなので、答えは変わらない (速さだけが変わる)。答えの変わらない形で、守る試験は無くてよい
    - 近道の「読みかけのリンク (`](`)」の確かめを外す: 読み損ねるリンクの入力 (`x [a](これは**「重要」**です` など 4 つ) を足して、この確かめが働く案が 8 つ増えたが、外しても近道の答えは解析し直しと一致した。
      本家が「どこまで読んだか分からないので確かめない」と控えめに置いた確かめで、外して答えが変わる入力は作れなかった。外して答えが変わる入力が見つかれば、その入力を記録に足す
  - `make -C src/ja-writing lint test` (test は -race つき) 緑・root の `make test-dupscan-gate` (新しい重複なし)・`scripts/check_unused_excluding_tests.sh` (到達不能なシンボルなし)
  - 敵対的レビュー (opus のサブエージェント 1 体、読み取りのみ): 本家と違う答えを出す入力は見つからなかった。本家の decide と解析し直しも、乱数の入力 60,000 本で 3,254 案すべて一致した。採った P3
    - 記録の入力が通らない近道の枝 4 つ (本家 2521・2523・2554・2619 行) → 入力を足した
    - 記録の case が欠けても緑のまま通る → 足した入力が全部記録にあることを見る assert を足した
    - 近道の試験が skip_frontmatter の偽の case を回していない → 回すようにした
    - 本物の経路で近道が False を返す枝は記録の比較で通っていない → 受容。近道の答えは `TestRepairIndexAgreesWithReparse` が解析し直しと比べて守る
