# 996 (bug): ローカル変数を 1 つ足しただけの写しを near -similar と sweep の節 3 が見つけない

起票日: 2026-10-30

## 概要

部分木の類似 (`near -similar` と sweep の節 3。どちらも `internal/norm/subtrees.go` の `Subtrees` の Dice) は、局所の名前を**単位全体で出た順の番号**にした L1 の形で
部分木の hash を取る。写しの途中にローカル変数を 1 つ足すと、それより後ろに初めて出る局所の名前の番号が全部 1 つずれ、後ろの部分木の hash が一つも一致しなくなる。
`Subtrees` の doc は「Local numbering is unit-wide: inserting a local can change later fingerprints.」と限界を書いていて、
[895](done/895-feat-dupscan-type3-candidates.md) は「局所宣言の挿入でずれる番号」を保証範囲から外し、「必要な具体例が出れば別の検出規則の採否を決める」とした。この issue がその具体例。

## 実例 (2026-10-30)

`internal/report/evidence.go` の `EvidenceText` は、`internal/report/grouped.go` の `EvidenceTextGrouped` に置き換わった後に残った写しだった (どこからも呼ばれず、
[982](done/982-cleanup-dupscan-unused-text-writers.md) で消した)。違いはループの前の `var group fileHeading` の 1 文と、書式の文字列・1 つの呼び出し。

- 消す前の木 (`8ae039fbd`) で `dupscan near -similar src/dupscan/internal/report/evidence.go:EvidenceText src/dupscan` → `candidates=0` (しきい値 0.4・比べた単位 684)
- sweep の節 3 (Dice 0.60 以上) にも出ていなかった
- `explain -compare` は、ループの中の局所の名前が `$8` → `$9` にずれていることを示した
- 写しを見つけたのは `deadcode` (未使用の関数) だった

## 発火条件

2 つの単位の大部分が同じで、片方の途中 (共通の部分より前) にだけ局所の名前の宣言 (`var x` / `x :=` / クロージャの引数) がある。足した名前より後ろに初めて出る局所の名前を含む部分木が全部一致しなくなる。
写しを手で直すと、変数が 1 つ増える・減るのは普通に起きる。

## 対応方針

- 部分木の hash では局所の名前を番号にせず、どれも同じ記号にする (`norm.writer` の `anon`。sweep の節 5 の `Seqs` が既に使う形)。足した変数で後ろがずれなくなる
- 代わりに、局所の名前の入れ替え (`a - b` と `b - a`) の区別が部分木の中で無くなり、似ていると出る対が増えうる。節 3 と `near -similar` は「似ている候補」で同一性を主張しない (`reuse=not_proven`) ので、増え方を測って採否を決める
- 局所の名前を部分木ごとに出た順で番号にする案は、部分木ごとに書き直すので単位の節の数の 2 乗の手間になる。まず `anon` で測る

## 受け入れ条件

- [ ] 上の実例の形 (途中に 1 文の宣言を足した写し) をテストにして、`near -similar` の候補に出ることを確かめる (直す前に赤)
- [ ] dotfiles の `src` で sweep の節 3 の対の数と所要を直す前後で測って書く (増えすぎるなら採らない)
- [ ] `Subtrees` の doc の限界の行を、直した形に合わせて書き直す
