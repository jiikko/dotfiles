# ratelimit

Claude Code (`/usage`) と codex (app-server の rateLimits) の利用枠 (5h / weekly) を取得・整形する module と、
それを使う単独コマンド `bin/ratelimit`。

- `usage/` — 取得 (`Fetch` / `FetchCodex` / `FetchAll`) と整形 (1 行・表・全画面ダッシュボード)。
  glogx の利用枠オーバーレイ (`U`) とダッシュボード (`R`) も replace でこれを取り込む
- `main.go` — `bin/ratelimit`。表示・閾値判定 (`-check`)・JSON。使い方はファイル冒頭
  - Claude Code の UserPromptSubmit hook (`_claude/hooks/ratelimit-warn.sh`) が Claude の枠を、
    codex 系 skill (codex-drive / codex-lead / codex-review / cross-review) が起動時に codex の枠を見る。
    注入を受けたときの判断基準は `_claude/rules/subagent-model-tiering.md` の「枠の残量」

## 境界と制約

- 外部プロセスは `subproc.CommandContext` (../subproc) でしか起動しない。`exec_boundary_test.go` が
  「非テストの .go が os/exec を import しない」で固定する
- キャッシュは `~/.cache/glog/ratelimit-<source>.json` (出所ごと)。glogx の `claude-usage.json` とは分けている
  (理由は main.go 冒頭)。書き込みは `atomicfile.Write` (lint が `os.WriteFile` を禁止)
- 表示に載る文字列 (キャッシュ由来の Label) は入口で `termsafe` を通す
- pace 判定は `_claude/statusline-command.sh` と二重実装。乖離は `usage/pace_drift_test.go` が突き合わせ、
  shell だけを変えたときは `tests/claude/test_statusline.sh` がこのテストを `-count=1` で叩く
- lint の規則 (`render.go` の I/O 禁止・stdout 直書き禁止・空白の確保・幅エンジンの一本化) は
  src/glogx から移したもの。ruleguard の規則の正本は `src/glogx/gorules/rules.go`
