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

- **関数の単位**: `oracle.CallFunctions` で `yomiyasu_lint.bold_problems(text)` を呼ぶ。入力は `testdata/corpus/` の全ファイルと、下の足す入力。戻りは `list[dict]` で JSON にそのまま入る
- **足す入力** (地図の 4.3 から G3 の分): 1 つの段落に崩れた太字が 5 組以上 (近道の `decide` を通す)・`<` を含む段落と参照の定義を持つ段落 (近道を使わない枝)・
  かっこ / 句読点 / 空白の 3 通りの案がどれも当たるもの・`\` で逃がした `*`・コードと URL とリンクの中の `**`・行頭が `**` のリストと引用・GFM と CommonMark で答えが分かれる記号 (Unicode の S: `**$100**円` など)
- **近道と解析し直しが同じ答えか**: Go のテストで、近道が答えを出した案ごとに解析し直した答えと比べる (本家のコメント 2396-2408 の主張を Go の側で確かめる)。近道を外して本家の記録と一致するかも見る
- 記録の置き場所と、`make golden` から記録を作る口 (`oracle/cmd/golden` に 1 行足す) は、1075 の担当と合わせる

## 進め方

1. 1074 (visibility) が master に入るまで: この設計を反証レビューに通す
2. 入ったら: `internal/bold` を作り、記録と突き合わせる。dupscan の gate・実装をわざと壊して記録との突き合わせが落ちることの確かめ・`make -C src/ja-writing lint test`・root の `make test-lint` を通す

## 受け入れ条件

- [ ] `bold_problems` の記録 (全入力) と Go の答えが一致する
- [ ] 近道 (`decide`) が答えを出した案で、解析し直した答えと一致する
- [ ] 実装をわざと壊して (3 の倍数の規則を外す・`bottoms` の鍵を GFM と CommonMark で揃える・近道の確かめを 1 段外す)、突き合わせが落ちる
- [ ] lint 側の候補の文面 (message・snippet) が本家の 5577-5584 行と同じ
- [ ] 反証レビュー (設計と実装)

## 関連

- 1074 (visibility)・1075 (lint の土台と他の群)・1076 (diff。同じ太字のコードを使う)
- 移植の地図: 1075 の「群と順」の節

## 進捗

- 2026-11-13: 本家の 2076〜2836 行と 5577〜5584 行を読み、作るものと Go の形を決めた (上の節)。1074 待ち
