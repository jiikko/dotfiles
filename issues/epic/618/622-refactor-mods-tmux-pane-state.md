# 622 (refactor): tmux のペインの状態表示 (`tmux-pane-state.sh`) を mod へ移す

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。

## 概要

`_claude/hooks/tmux-pane-state.sh` (209 行) は 6 種のイベント (SessionStart / UserPromptSubmit / PostToolUse / Notification / Stop / SessionEnd) で
tmux の pane option `@claude_state` を書く。PostToolUse にも配線されているので、**ツール呼び出しのたびに bash が 1 本起動する**。
また Notification の stdin に `background_tasks` が無いため、Stop が書いた `@claude_bg` を Notification が pane option 経由で読む工夫をしている (script 冒頭)。

mod なら状態を常駐側に持て、`turn.start` / `turn.complete` / `classic.Notification` / `session.end` で書き換えられる。

## 対応方針

1. **先に before を測る** (`perf-claims-need-measurement.md`): 1 ターンあたりの hook の起動回数と所要時間。
   測る手段が作れないなら、「未実測」と理由を書いてから進める
2. `_claude/mods/tmux-state/`: tmux への書き込みは `$.process.run` で `tmux set-option -p ...` を呼ぶ。
   tmux の実体は `command -v` で解決した絶対パスを使う (`bin/tmux` の shim を通すかは、shim が `set-option` を素通しするので、どちらでもよい。理由を書いて決める)
3. bg 待ちの判定 (`@claude_bg`) は mod のメモリで持つ。ただし表示側 (`_tmux.conf` / `tmux_agent_panel.sh` / `tmux_agent_jump.sh`) が読む pane option の値と形は変えない
4. macOS の通知とベルの条件 (ペインが見えていないときだけ) は今の script の判定をそのまま移す。`tests/claude/test_tmux_pane_state_bell.sh` が守っている条件を、mod のテストに写す
5. 切り替えの commit で settings の 6 行を外す

## 確かめること

- [ ] after を before と同じ方法で測った
- [ ] `classic.Notification` の `e` に `notification_type` が来ることを実測した (型には在る)
- [ ] 隔離した tmux サーバ (`-L`) で、working / input / idle / bg の 4 状態の表示を確かめた (`tmux-probe-requires-socket-isolation.md`)

## 進捗

- 2026-10-02: 起票
