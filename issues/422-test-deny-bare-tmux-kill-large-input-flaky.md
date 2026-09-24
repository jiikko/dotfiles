# 422 (test): test_deny_bare_tmux_kill.sh の「大きな入力でも deny」が 1 度だけ allow になった

起票日: 2026-09-24

## 概要

`tests/claude/test_deny_bare_tmux_kill.sh` の「大きな入力 (90KB) でも deny を出す」ケースが、
2026-09-24 の `make test` で 1 度だけ allow になった。その後は再現していない。

## 詳細

(観測してから書く。原因を推測で書かない)

## 対応方針

(観測してから書く)

## 関連ファイル

- `tests/claude/test_deny_bare_tmux_kill.sh`
- `_claude/hooks/deny-bare-tmux-kill.sh`

## 進捗

- [ ] 起票
