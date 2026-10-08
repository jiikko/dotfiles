# 688 (feat): treefiler の後回しにした機能 (画像・PDF のプレビューとソート)

起票日: 2026-10-09

## 概要

issue 662 (treefiler) で、ユーザーの回答により後回しにした 2 つ。662 を閉じるために切り出した。

- **画像・PDF のプレビュー** (2026-10-07 回答「画像の読み切りは pending で」)。今は開かずに「画像と PDF はまだ表示できません」の toast
  (`src/treefiler/filer/preview.go` の `imageExt` と `refuse`)。写すなら文字のブロックで描く (回答「文字のブロックで書いて」)。
  形 (half / quadrants / sextants) と、タイルの中のキー (`j` `k` `Space` でページ、`↑` `↓` で同じフォルダの前後の画像) は
  `docs/treefiler-spec.md` の §8.3 と C 節。ピクセル描画 (kitty / sixel / iTerm2) と `i` は写さない
- **ソート** (2026-10-07 回答「ソートは後回しで。後から考える」)。今は名前順だけ (設定の Folders first / Natural sort は在る)。
  treebeard の巡回 (名前 → 新しい順 → 大きい順 → 種類)・逆順・ステータスバーのソート表示・設定の Sort by / Reverse は入れていない。
  キーも決めていない (treebeard の `o` `O` は glogx の「外で開く」が取っている)

## 再開の条件 (trigger)

ユーザーが「画像を見たい」「並べ替えたい」と言ったとき。ソートはキーを先に決める (`o` は使えない)。

## 関連ファイル

- `src/treefiler/filer/preview.go` (`imageExt` / `refuse`)、`src/treefiler/filer/tree.go` (`sortKids` / `lessName`)
- `docs/treefiler-spec.md` (§0.1 の決定・§5.5・§8.3)
