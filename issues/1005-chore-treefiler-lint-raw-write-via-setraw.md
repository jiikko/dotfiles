# 1005 (chore): treefiler の node.raw を setRaw の外で書き換えない規則を lint に足す

> 🚨 **担当中: dotfiles-2e (rawrule)**（2026-10-30〜）

起票日: 2026-10-30

## 概要

issue 1002 でノードに名前順の比較用の `lower` を足し、`raw` と一緒に `setRaw` で書くようにした。`raw` を直接書き換えると `lower` が古いまま残り、
並べ替えで食い違う (実装の敵対的レビューで、root を上げた後に旧 root が兄弟の先頭へ飛ぶ退行として見つかった)。setRaw の外での代入を ruleguard で止める。

## 対応

- `src/treefiler/gorules/rules.go` に `rawViaSetRaw` を足した: `$x.raw = $_`・`$x.raw, $_ = $_, $_`・`$_, $x.raw = $_, $_` を止める。
  setRaw の 1 行だけ `//nolint:gocritic` (理由つき。nolintlint が「使われていない例外」を見るので、規則が効かなくなれば lint が気づく)
- 型では絞れなかった: ruleguard の `Type.Is` に同じパッケージの型 (`*node`・`*treefiler/filer.node`) を書くと、規則の読み込みごと失敗して
  どの規則も効かなくなる (全ファイルで `ruleguard: execution error: used Run() with an empty rule set`)。左辺に `$*_` を書く形も同じ。
  raw という欄への代入を全部止める形にした (filer で raw を持つ他の型は composite literal でしか書かない。lint は 0 件)
- 検出しない形: 3 つ以上を並べた代入・composite literal・reflect / unsafe (規則のコメントに書いた)

## 進捗

- [x] 規則と例外の印。make -C src/treefiler lint で 0 件 (空の結果キャッシュで)
- [x] 規則が退行を捕まえることを変異で確認 (bin/mutate-verify。verify は空の結果キャッシュで make lint):
  rerootUp を `old.raw, old.parent = k.raw, nr` に戻す → 落ちる / tile.go を `n.raw = path` に戻す → 落ちる

## 記録 (この issue の外)

- 🚨 規則の読み込みに失敗する rules.go で一度 lint を回すと、その checkout の golangci-lint の結果キャッシュ (`GOLANGCI_LINT_CACHE`。
  既定は ~/Library/Caches/golangci-lint で全セッション共有) が失敗を覚え、rules.go を元に戻しても同じ checkout では `empty rule set` が出続けた。
  規則のファイルの中身がキャッシュの鍵に入っていないと見られる。本体の checkout は鍵が checkout ごとに分かれていて巻き込まれなかった。
  rules.go を試すときは `GOLANGCI_LINT_CACHE=$(mktemp -d)` を付けて回す
