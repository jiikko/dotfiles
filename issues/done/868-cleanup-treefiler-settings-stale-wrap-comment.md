# 868 (cleanup): treefiler の settings.go 冒頭の「未対応: Wrap lines」が実装済みの今と合わない

起票日: 2026-10-22

## 概要

`src/treefiler/filer/settings.go` の冒頭コメントが、今の設定の項目と食い違っている。

- 「未対応: Wrap lines (タイルの長い行は今は切る)」とあるが、同じファイルの項目一覧に
  `boolItem("BEHAVIOR", "Wrap lines", "wrap", …)` がある (off なら右端で切る、on なら折り返す)
- 「写さない項目」に「Sort by / Reverse (ソートは後回し)」とあるが、同じ一覧に
  `choiceItem("ORDER", "Sort by", "sort_by", …)` と `boolItem("ORDER", "Reverse", "reverse", …)` がある

README (`src/treefiler/README.md`) は Wrap lines / Sort by / Reverse を設定として載せていて、こちらは実体と合っている。

## 対応方針

- 冒頭コメントから「未対応: Wrap lines」を消し、「写さない項目」から Sort by / Reverse を外す
- 出典の `docs/treefiler-spec.md` §0.1 は既に新しい (Sort by / Reverse は写す (issue 720)、§7 の表に Wrap lines (既定 on))。
  直すのはコードのコメントだけ (2026-10-22 に確認)
- 「写さない項目」の残り (Pipes の別方式・River tracks・Step through 等) が今も写していないかは未確認。直すときに §0.1 と突き合わせる
- 書き直すときはコメントの規範 (CLAUDE.md「何をどこに書くか」= Why not だけ) に合わせ、「(ソートは後回し)」のような経緯は残さない

## 関連ファイル

- `src/treefiler/filer/settings.go` の冒頭コメントと項目一覧 (`boolItem` / `choiceItem` の並び)
- `docs/treefiler-spec.md` §0.1

## 進捗

- [x] コメントを直す (commit「docs(treefiler): settings.go の冒頭の「写さない項目」「未対応」を今の設定の項目に合わせる」)。残りの写さない項目は、どれも項目一覧に無いことを確かめた。Mouse・Wheel speed・Momentum・Image previews・外部コマンドのプレビュー・Step through (j k の歩き方の行) は spec §0.1 にもある。Pipes の別方式・River tracks・Block glyphs は §0.1 の表に無い (項目一覧に無いことだけを確かめた)
- [x] spec §0.1 を確かめる (古い記述は無い。2026-10-22)
