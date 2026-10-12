# 719 (feat): treefiler でカーソルの項目を Finder で表示する

起票日: 2026-10-12

## 概要

カーソルの項目を、Finder で選んだ状態で開く (`open -R <path>`)。今の `o` は項目そのものを外のアプリで開く (`open <path>`) だけ
(ユーザーの選択 2026-10-12)。

## 仕様 (案)

- `O` で Finder に表示 (キーはユーザー確認済み 2026-10-12。treebeard の `O` は「ソートの逆順」だが、spec §0.1 でソートの `o` `O` は写さないと決めている)
  - ui-guide §10 の「大文字は小文字の強い版・まとめ版」には当たらない (`o` = 開く / `O` = 開く代わりに Finder で見せる、の別名に近い)。例外として受ける
  - タイルの中の `o` (`tile.go`) にも `O` を足す (木だけに足すと README の木とタイルのキー表が食い違う)
- フォルダでもファイルでも、その項目を選んだ Finder の窓が出る。失敗は ✗ の toast
- 直す文書: `?` の一覧 (`model.go` の help の表。木の行とタイルの行)・`src/treefiler/README.md` のキー表 (木とタイル)・glogx-ui-guide §2 の `o` の行・
  spec §0.1 の表と §6.1 の `o` `O` の行 (2 か所)。`src/glogx/README.md` には filer のキー表が無い (`F` の 1 行だけ) ので対象外

## 対応方針

- `o` の起動 (`src/treefiler/filer/open.go` の `opener`。非同期で起こし、失敗を `take` で取り込む) に「Finder で表示」を足す。
  起動の経路を 2 本にしない。🚨 今の `opener.start(path, name)` と差し替え点 `openCommand(ctx, path)` には `open` の引数を変える口が無く、
  失敗の文言 (`開けません: `) も固定なので、**引数 (`-R` の有無) と失敗の文言を渡せる形へ署名を変える** (呼び出し元は `model.go` の `o` の bind と
  `tile.go` のタイルの `o`、fake は `open_test.go`)
- macOS 専用で構わない (repo の CLAUDE.md: Linux はサポート対象外)

## 受け入れ条件

- [ ] `O` で `open -R <abs>` が起動する (起動するコマンドを fake で確かめるテスト + 変異)
- [ ] 実端末で Finder の窓が出ることを 1 回目で見る

## 進捗

- 2026-10-12: 起票
- 2026-10-12: 反証レビュー (sonnet・読み取りのみ) で訂正: `opener` に引数の口が無い (署名の変更が要る) / タイルの `o` にも足す /
  spec は §0.1 と §6.1 の 2 か所 / glogx の README は対象外 / ui-guide §10 の大文字の規則の例外。反証できなかったもの: `O` は木・タイル・
  glogx の F の横取り (`crossTarget`) のどこでも空いている / `o` は非同期で失敗を `take` で取る / spec §0.1 の「写さない」の記述
