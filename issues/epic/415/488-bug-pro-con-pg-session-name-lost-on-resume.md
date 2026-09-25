# 488 (bug): PG の session の名前が、再開の後に `pc-c-NNN` でなくなりうる

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

440 の 1 回目の dogfooding の「未起票」から。PG は起動のとき `pc-c-NNN` の名前を付けるが、再開 (`--resume`) では名前を渡していない。
再開の後の session の名前が `pc-c-NNN` のままかは実測していない。名前が変わると、`claude` の session の一覧でどのカードの PG かが分からなくなる。

## 詳細 (2026-09-26 に読んだ)

- `src/pro-con/dispatcher/launcher.go` の `startArgs` は `--bg -w <name> -n <name>` で名前を付ける
- 同じファイルの `resumeArgs` は `--bg --resume <sessionID>` だけで、`-n` を渡していない
- 再開は、差し戻し・PG の質問への回答・テストの係の結果・dispatcher の起こし直しのたびに起きる

## 対応方針

1. **実測が先**: 本物の state dir と本物の PG に触らない形で、`--bg` を起動してから `--resume` したときの session の名前を確かめる。`--resume` に `-n` を足したときの名前も確かめる
   (`--bg` は信頼済みの repo の worktree でないと起動を拒む。440 の言語の A-B の注記を参照)
2. 名前が変わるなら、`resumeArgs` にも `-n <name>` を渡す (受けるなら)。受けないなら、名前が変わることを README に書き、画面の側でカードと session を ID で結ぶ

## 関連ファイル

- `src/pro-con/dispatcher/launcher.go` の `startArgs` / `resumeArgs`

## 関連

- 440 (dogfooding の記録。元の未起票の項目) / 465 (同じ名前の worktree)
