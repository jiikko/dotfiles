# 425 (research): claude --bg の残りの挙動を実測する

> 🚨 **担当中: dotfiles-5c**（2026-09-24〜）

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md) の論点 2 / 5 / 8 / 11 と要件 14

## 概要

PG を `claude --bg -w` で動かす設計 (415 論点 2) のうち、測っていない挙動が残っている。
本物の PM / PG の backend ([427](427-feat-pro-con-real-pm-pg-backend.md)) と論点の決定 ([426](426-design-pro-con-open-decisions.md)) がこれに依存する。

## 測ること

- [ ] 完了と異常終了を `claude agents --json` (`--all` 含む) で区別できるか (status / state の値)
- [ ] `claude rm <id>` が未 push の commit・未コミットの変更を持つ worktree を消さないか (help の記述は未実測。`--discard-unpushed` / `--force-remove-worktree` を渡さない場合)
- [ ] 実行中の bg session へメッセージを直接送れるか (論点 8 の候補 B / 論点 11 の ①。送れないなら「idle を待って `--resume`」になる)
- [ ] 質問で止めた (待たせた) bg session に何が起きるか (論点 8)
- [ ] bg session がマシンの再起動を越えて戻るか (要件 14)
- [ ] 利用枠 (5h / weekly) の残量を機械で読む口があるか (論点 5 / 自動スケーリングの材料)
- [ ] 起動時に `TMUX` / `TMUX_PANE` を落とさない場合、PG が起動元 pane の `@claude_state` を上書きするか (論点 5。落とす前提なら省略可)

## 測り方の制約

- 本物の `claude --bg` を起動・停止するので**利用枠を使う**。モデルは haiku、1 項目 1 session で済ませる
- stdout / stderr / rc を分けて採り、Claude Code の版を併記する (`~/.claude/rules/measure-external-cli-streams-separately.md`)
- 再起動を越えるかは人間の操作 (再起動) が要る。そこだけ human に切り出すか、最後に回す

## 関連ファイル

- 415 の「`--bg` の実測」節 (論点 2 の中) が既に測った分

## 進捗

- [ ] 未着手
