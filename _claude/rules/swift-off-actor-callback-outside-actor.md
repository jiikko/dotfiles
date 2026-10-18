---
paths:
  - "**/*.swift"
---
<!-- paths: Swift のコードを書く場面でしか発動しないので、Swift のファイルを Read したときに読み込む (常時読み込みの指示ファイル合計の上限のため) -->
# 別の thread から呼ばれる callback を、actor に属する型の中の closure で書かない (Swift)

> **トリガー型ルール。** `@MainActor` の型 (や default isolation が MainActor の module) の中で、別の thread から呼ばれる
> callback を書く瞬間、または既存の callback が呼ばれる thread を変えた瞬間に発動する。

## ルール

- **別の thread から呼ばれる callback** (C の関数ポインタ (`CGEvent.tapCreate` 等)・GCD の queue の handler・Network.framework の handler・
  別 thread の `CFRunLoop` の source) を、actor に属する型の中の closure で書かない。closure はその actor に属し、別の thread から呼ばれた瞬間に
  実行時の検査で落ちる (SIGTRAP)。actor に属さない場所 (ファイルの外側の関数・`nonisolated`) に置き、main の状態に触るときは明示的に main へ渡す
- **callback が呼ばれる thread を変えた直後は、実物を起動して callback の経路を 1 回通す**。単体テストはその経路 (tap を張る・queue に投げる) を
  通らないことが多く、緑のまま起動直後に落ちる。iOS では `no-ios-simulator-verification.md` が優先する (実機・シミュレータの確認はユーザーに委ねる)

## なぜ

起源: screen-share-app の retro 808, 2026-10-17。根拠・実例は `~/dotfiles/_claude/rules-rationale/swift-off-actor-callback-outside-actor.md`
(起動時には読まれない。ルールを疑う・改訂するときに読む)。
