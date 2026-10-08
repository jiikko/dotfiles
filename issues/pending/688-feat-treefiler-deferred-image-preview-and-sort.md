# 688 (feat): treefiler の後回しにした機能 (画像のプレビューとソート)

起票日: 2026-10-09

## 概要

issue 662 (treefiler) で、ユーザーの回答により後回しにした 2 つ。662 を閉じるために切り出した。

- **画像のプレビュー** (2026-10-07 回答「画像の読み切りは pending で」)。今は開かずに「画像はまだ表示できません」の toast。
  **PDF は写さない** (2026-10-09 回答「pdf のプレビューはやらんでええよ」。「PDF は表示できません」の toast で断る)
  (`src/treefiler/filer/preview.go` の `imageExt` と `refuse`)。写すなら文字のブロックで描く (回答「文字のブロックで書いて」)。
  形 (half / quadrants / sextants)・配置は `docs/treefiler-spec.md` §9 (PDF の部分は除く)、タイルの中のキー (`↑` `↓` で同じフォルダの
  前後の画像。ページ送りは PDF 用なので要らない) は §6.4 と issue 662 の C 節。ピクセル描画 (kitty / sixel / iTerm2) と `i` は写さない
- **ソート** (2026-10-07 回答「ソートは後回しで。後から考える」)。今は名前順だけ (設定の Folders first / Natural sort は在る)。
  treebeard の巡回 (名前 → 新しい順 → 大きい順 → 種類)・逆順・ステータスバーのソート表示・設定の Sort by / Reverse は入れていない。
  キーも決めていない。treebeard の `o` (ソートの巡回) は glogx の「外で開く」が取っている。`O` (逆順) は空いている。
  🚨 spec の中で、§0.1 (ソートは後回し) と §6.1 の表 (`o` `O` ソート切替 — 写さない) が食い違うので、再開時にどちらへ揃えるか決める

2026-10-09: ユーザー回答「あと魔wしでok」(「後回しで ok」と読んだ。画像とソートは後回しのまま)。

## 再開の条件 (trigger)

ユーザーが「画像を見たい」「並べ替えたい」と言ったとき。画像は、Go の標準で読めない形式 (webp / svg / heic 等) に外部コマンド (ImageMagick) を
使うか・手元に在るかを先に確かめる。ソートはキーを先に決める (`o` は使えない)。

## 関連ファイル

- `src/treefiler/filer/preview.go` (`imageExt` / `refuse`)、`src/treefiler/filer/tree.go` (`sortKids` / `lessName`)
- `docs/treefiler-spec.md` (§0.1 の決定・§5.6 のソート・§6.4・§7 の Sort by / Reverse・§9)
