# 488 (bug): PG の session の名前が、再開の後に `pc-c-NNN` でなくなりうる

起票日: 2026-09-26

親: [415](../415-design-claude-pm-worker-orchestration.md)

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

## 実測 (2026-09-26 C-045。Claude Code 2.1.282・haiku・pro-con を通さず名前は pc-probe-*)

`claude --bg -n <name>` で起動 → `claude stop` → `claude --bg --resume <session-id>` を、`-n` の有無で比べた (手順は C-045 の run のログ
`~/.local/state/pro-con/live/runs/C-045-1790373553-34255ca5.log`):

| 再開の引数 | rc | `--bg` の出力 | 再開の後の `claude agents` の name |
|---|---|---|---|
| `-n` なし | 0 | `backgrounded · 50acec38` (名前なし) | `japanese language instruction detection` (AI の付けた題) |
| `-n pc-probe-B` | 0 | `backgrounded · 5594afb7 · pc-probe-B` | `pc-probe-B` |

→ `--bg --resume` は `-n` を受け、名前がそろう。付けないと 440 の 1 回目と同じく AI の題になる (C-045 の PG 自身も再開の後 `pc-c-045 probe resume` になっていた)。
🚨 どちらの再開も「stop の直後で、まだ実行中と見られてコピーが起動した」形 (session id が変わった) で測った。止まり切った session を同じ ID で再開する形での名前は測っていない
(`-n` を受けることは同じ引数の解釈なので、変わる理由は見当たらない)。

## 対応

- `Launcher.Resume` に name を足し、`ExecLauncher.resumeArgs` で `-n <name>` を渡す。PG は `sessionName(c)` (= 起動の名前)、役 (PM・取り込みの係) は
  記録に名前の欄が無いので worktree の名前 (`filepath.Base(row.Cwd)`。起動の `-w` と `-n` は同じ名前) を渡す
- テスト: `TestResumeRunsInSessionCwd` (PG) / `TestIntegratorRetoldAfterRework` (役) / `TestLauncherArgsPassLanguageAndNoAutoMemory` (引数)。
  変異 3 本 (PG に名前を渡さない / 役に渡さない / `-n` を引数から外す) でそれぞれ落ちることを確かめた


## 別件

- 実測で、stop の直後の `--resume` がコピーを起動し `note:` の行を出すのを見た (stdout か stderr かは未確認)。未起票として 440 の改善案に書いた
