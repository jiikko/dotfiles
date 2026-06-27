---
description: 今の会話を --fork-session でフォークし detached tmux セッション claude-fork に作成（C-t b の popup で覗ける）
allowed-tools: Bash(tmux:*)
---

今の Claude 会話を `--fork-session` でフォークし、detached な tmux セッション `claude-fork` として起動する。元の会話（このセッション）はそのまま継続でき、フォークは新しいセッション ID に枝分かれするので競合しない。

次を **そのまま** 実行すること（コマンドを改変・分割・説明追記しない）:

```bash
if [ -z "$CLAUDE_CODE_SESSION_ID" ]; then
  echo "CLAUDE_CODE_SESSION_ID 未設定: fork 不可 (Claude Code の Bash tool 内で実行すること)"
else
  tmux kill-session -t claude-fork 2>/dev/null
  tmux new-session -d -s claude-fork -c "$PWD" "claude --resume \"$CLAUDE_CODE_SESSION_ID\" --fork-session"
  tmux has-session -t claude-fork 2>/dev/null && echo "fork OK: claude-fork (元 session=$CLAUDE_CODE_SESSION_ID)" || echo "fork FAILED"
fi
```

ポイント（実行前に理解しておくこと。出力には書かなくてよい）:

- `$CLAUDE_CODE_SESSION_ID` は Bash tool の env にある現在のセッション ID。シェルが展開してから tmux に渡る（tmux 側では展開されない）。未設定なら fork せず中断する（上の guard）。値は内側コマンド文字列内でもダブルクォート（`\"...\"`）で囲み、tmux が `/bin/sh -c` で再評価する際の word splitting を防ぐ。
- `-c "$PWD"` でフォークの cwd を今のプロジェクトに合わせる。
- 既存の `claude-fork` があれば kill して差し替える（**固定名・上書き運用**。常に最新フォーク1つだけを live で持つ）。**再 fork 時、前のフォークで実行中だった作業（未確定ターン等）は失われる**。確定済みの会話はディスクに残るので、古いフォークは後から `claude --resume`（引数なしで対話ピッカーが出る）で選び直して復帰できる（フォークの新セッション ID はこのコマンドの出力に出ないため、ID 指定ではなくピッカーから選ぶ）。
- detached（`-d`）なので非ブロッキング。display-popup と違い、この会話を止めない。

実行後、`fork OK` を確認できたらユーザーに次を簡潔に伝えること:

- フォークを作成した（tmux セッション名 `claude-fork`）。
- **`C-t b`（prefix + b）の popup で覗ける**。閉じるのも popup 内で `C-t b`。
- 初回 popup を開いた直後、フォーク側 claude の起動確認プロンプト（組織の managed settings 承認など）が出ることがある。その場合は Enter で進める（これは claude の標準挙動で、フォーク固有ではない）。

`fork FAILED` の場合は、`tmux list-sessions` と `claude --version` を確認し、`--fork-session` が使えるか（claude CLI が新しいか）を点検すること。
