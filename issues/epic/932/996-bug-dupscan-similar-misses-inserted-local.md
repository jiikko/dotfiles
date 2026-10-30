# 996 (bug): ローカル変数を 1 つ足しただけの写しを near -similar と sweep の節 3 が見つけない

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-30〜）

起票日: 2026-10-30

## 概要

部分木の類似 (`near -similar` と sweep の節 3。どちらも `internal/norm/subtrees.go` の `Subtrees` の Dice) は、局所の名前を**単位全体で出た順の番号**にした L1 の形で
部分木の hash を取る。写しの途中にローカル変数を 1 つ足すと、それより後ろに初めて出る局所の名前の番号が全部 1 つずれ、後ろの部分木の hash が一つも一致しなくなる。
`Subtrees` の doc は「Local numbering is unit-wide: inserting a local can change later fingerprints.」と限界を書いていて、
[895](done/895-feat-dupscan-type3-candidates.md) は「局所宣言の挿入でずれる番号」を保証範囲から外し、「必要な具体例が出れば別の検出規則の採否を決める」とした。この issue がその具体例。

## 実例 (2026-10-30)

`internal/report/evidence.go` の `EvidenceText` は、`internal/report/grouped.go` の `EvidenceTextGrouped` に置き換わった後に残った写しだった (どこからも呼ばれず、
[982](done/982-cleanup-dupscan-unused-text-writers.md) で消した)。違いはループの前の `var group fileHeading` の 1 文、ループの中の書式の文字列・`e.File` の引数の削除・`group.write(...)` の呼び出し。

- 消す前の木 (`8ae039fbd`) で `dupscan near -similar src/dupscan/internal/report/evidence.go:EvidenceText src/dupscan` → `candidates=0` (しきい値 0.4・比べた単位 684)
- sweep の節 3 (Dice 0.60 以上) にも出ていなかった
- `explain -compare` は、ループの中の局所の名前が `$8` → `$9` にずれていることを示した
- 写しを見つけたのは `deadcode` (未使用の関数) だった
- **スコアの実測** (同じ木で `-similar-threshold 0.05`): 今の形で **0.388** (しきい値 0.4 のわずかに下)。`Subtrees` を `anon` にした版で **0.507**。
  番号のずれは主因の一部で、書式の違い・引数の削除も大きな祖先の部分木の一致を崩している (反証レビューの指摘で測った)。`anon` で `near -similar` (0.4) には出るが、sweep の節 3 (0.60) には出ない

## 発火条件

2 つの単位の大部分が同じで、片方の途中 (共通の部分より前) にだけ局所の名前の宣言 (`var x` / `x :=` / クロージャの引数) がある。足した名前より後ろに初めて出る局所の名前を含む部分木が全部一致しなくなる。
写しを手で直すと、変数が 1 つ増える・減るのは普通に起きる。

## 対応方針

- 部分木の hash では局所の名前を番号にせず、どれも同じ記号にする (`norm.writer` の `anon`。sweep の節 5 の `Seqs` が既に使う形)。足した変数で後ろがずれなくなる
- 代わりに、局所の名前の入れ替え (`a - b` と `b - a`) の区別が部分木の中で無くなり、似ていると出る対が増えうる。節 3 と `near -similar` は「似ている候補」で同一性を主張しない (`reuse=not_proven`) ので、増え方を測って採否を決める
- 局所の名前を部分木ごとに出た順で番号にする案は、部分木ごとに書き直すので単位の節の数の 2 乗の手間になる。まず `anon` で測る

## 受け入れ条件

- [x] 宣言を 1 文足しただけの写しの Dice が 1.0 になる (今は番号がずれて下がる) ことを `internal/norm` のテストにする (直す前に赤)
- [x] 実例の対のスコアを直す前後で書く (上の 0.388 → 0.507)
- [x] dotfiles の `src` で sweep の節 3 の対の数と所要を直す前後で測って書く (増えすぎるなら採らない)
- [x] `Subtrees` の doc と、`similar_cmd.go` の Limitations の `unit-wide local numbering` を直した形に合わせて書き直す (引数に先に番号を振る前処理も要らなくなる)

## 測った結果 (2026-10-30、dotfiles の `src`、`sweep -limit 0`)

| | 節 3 の対 | 所要 (sweep 全体) |
|---|---|---|
| 今の形 | 52 | 5.42 秒 |
| `anon` | 88 (+36、消えた対 0) | 5.42 秒 |

増えた 36 対は、本体が同じで引数の並び・数だけが違う小さな helper (多くは module をまたいだテストの `must` / `write` / `writeFile` 系) と、
`treefiler/filer/model.go` の `spawnFrom ~ ghostTarget` (0.768)。引数の宣言の順に番号を振っていたので、引数の並びが違うだけで別の形になっていた。
どれも「似ている候補」として筋が通るので採る (局所の名前の入れ替え `a - b` / `b - a` の区別は部分木の中で無くなるが、節 3 は同一性を主張しない)

## 進捗

- commit「fix(dupscan): 部分木の類似で局所の名前を番号にせず、宣言を 1 文足した写しを見つける (issue 996)」
  - `internal/norm/subtrees.go` の `Subtrees` の writer を `anon` にした (引数に先に番号を振る前処理は要らなくなったので消した)。doc と `similar_cmd.go` の Limitations・README の near -similar と sweep の節 3 の行を合わせた
  - テスト `TestSubtreesIgnoreInsertedLocalDeclaration` (宣言を 1 文足した写しと、根以外の部分木が全部一致する)。直す前は共有の重み 3 / 期待 12 で赤
  - `TestSimilarBoundsAndAncestorExclusion` の期待を直した: `outer` の中のクロージャは `copy` / `again` の本体と同じ形で、今までは番号のずれでたまたま一致せず `Total == 0` だった。
    守りたいのは「target の子孫は候補にしない」と `-limit` なので、子孫が候補に出ないこと・`Total == 2`・返すのは 1 件を直接見る形にした
  - 変異 (`mutate-verify`、狙ったテストだけが赤): `anon` を外す → `TestSubtreesIgnoreInsertedLocalDeclaration` / 子孫の除外を外す → `TestSimilarBoundsAndAncestorExclusion`
  - `make -C src/dupscan lint test` rc=0
- 敵対的レビュー (opus・読み取りのみ): 壊せなかった (呼び出し元は similar と sweep の 2 か所だけ・捕まえた名前と `$0` は今までどおり字面・テストは空回りしない)。
  指摘の「節 3 の README の行が古い」は直した。「閾値 0.6 / 0.4 を較正し直していない」は、上の実測 (52 → 88 対、消えた対 0、増えたのは本体が同じで引数の並びだけ違う helper) で採る判断をした。
  **再提起の条件**: 節 3 か near -similar で、局所の名前を同じ記号にしたせいで本体の違う単位が上位に出る実例が出たとき
