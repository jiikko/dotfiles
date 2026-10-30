# 1005 (chore): treefiler の node.raw を setRaw の外で書き換えない規則を lint に足す

> 🚨 **担当中: dotfiles-2e (rawrule)**（2026-10-30〜）

起票日: 2026-10-30

## 概要

issue 1002 でノードに名前順の比較用の `lower` を足し、`raw` と一緒に `setRaw` で書くようにした。`raw` を直接書き換えると `lower` が古いまま残り、
並べ替えで食い違う (実装の敵対的レビューで、root を上げた後に旧 root が兄弟の先頭へ飛ぶ退行として見つかった)。setRaw の外での代入を ruleguard で止める。
