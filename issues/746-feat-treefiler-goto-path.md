# 746 (feat): treefiler の `:` で入力したパスへカーソルを飛ばす

> 🚨 **担当中: claude (dotfiles-wt-goto)**（2026-10-13〜）

起票日: 2026-10-13

## 概要

ユーザー依頼 (2026-10-13)「treefiler で、ファイルパスを渡したらそこのパスに move する機能が欲しい」。
確認の結果、move = **カーソルをそのパスへ飛ばす** (ファイルは動かさない)。

## 仕様 (ユーザーと合意)

- キーは `:`。足元の入力欄 (`a` / `m` / `!` と同じ欄) でパスを受ける。貼り付けも効く
- 絶対パスはそのまま、`~/` は展開、相対パスは root 基準 (`Y` がコピーする形)
- root の中: 途中のフォルダを読み込んで開き、カーソルを移す
- root の外: 共通の祖先まで root を上げる (`-` を必要な回数押したのと同じ。開いていたフォルダは保つ)
- 無いパス: カーソルは動かさず ✗ の toast

## 進捗

- [x] 実装 (`filer/goto.go`。`RestorePlace` のカーソル復元を `reveal` に寄せた) — commit「feat(treefiler): : で入力したパスへカーソルを飛ばす」
- [x] テスト 8 本 (`filer/goto_test.go`: 中 / 実の綴り / 外 / 無い・空欄・Esc / `~` と貼り付け / 在るが木に載らない 6 形 / 途中で上がれない / root を指す symlink)
- [x] README のキー表・`docs/treefiler-spec.md` §0.1・`src/treefiler/CLAUDE.md` の地図

## 結果

- `make -C src/treefiler lint` 0 issues・`make -C src/treefiler test` rc=0
- 変異 10 本すべて狙いの検査で red (`bin/mutate-verify-list`): 綴り直しを外す / root を上げない / 存在の検査を外す / TrimSpace を外す /
  祖先を開かない / reveal 失敗時の undo を外す / reroot 失敗時の undo を外す / root の中の symlink を解く / 末尾を辿る / 頭の一致を最長で取る
- 敵対的レビュー 2 周 (read-only のサブエージェント)。再現した指摘はすべて直した:
  - P1 大文字小文字の違う綴り: macOS では Lstat が通るが木は綴りどおりに照らすので、root が / まで上がって戻らなかった → 上げる前の root へ戻す (`rootUndo`)
  - P2 root の外の symlink のフォルダ越し / 途中で上がれない (読めない祖先) → 同じく戻す
  - P2 root の中の symlink を実の綴りで書くと、解いた先の別の項目へ黙って飛んだ (lnb・self・up) → 綴り直すのは root にあたる頭だけ・上から最短の一致・各段の末尾は辿らない
  - P3 root を指す symlink そのもの → root でなく symlink の項目へ移る (末尾は辿らない) をテストで固定
- 壊せなかった観点 (レビューの報告): undo 後の木の一貫性 (View の木の部分・Place・`-` 後の kids が一致)、`!` の履歴・`a`/`m` への回帰、glogx の `:` との衝突 (glogx に `:` のバインドは無い)、`..`・末尾 `/`・root 自身・root が `/`、`-race`

## 残タスク

- スコープ外 (ぼやき): ターミナルへのドラッグで入る `My\ File` のようなエスケープ・`~user`・先頭や末尾が空白の名前 (TrimSpace で落ちる) は解釈しない
- 未検証: `rootUndo` の mtime / size の書き戻しは、rerootUp が写す値と元の値がテストでは同じになるため変異で区別できない (書き戻しは rerootUp が書く欄をそのまま戻す対称性のため残した)
