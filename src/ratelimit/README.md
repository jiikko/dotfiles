# ratelimit

Claude Code (`/usage`) と codex (app-server の rateLimits) の利用枠 (5h / weekly) を取得・整形する module と、
それを使う単独コマンド `bin/ratelimit`。

- `usage/` — 取得 (`Fetch` / `FetchCodex` / 出所ごとの `FetchClaudePart` / `FetchCodexPart`) と整形 (1 行・表・全画面ダッシュボード)。
  glogx の利用枠オーバーレイ (`U`) とダッシュボード (`R`) も replace でこれを取り込む
- `main.go` — `bin/ratelimit`。表示・閾値判定 (`-check`。5h 枠だけを見る)・JSON。使い方はファイル冒頭
  - Claude Code の UserPromptSubmit hook (`_claude/hooks/ratelimit-warn.sh`) が Claude の枠を、
    codex 系 skill (codex-drive / codex-lead / codex-review / cross-review) が起動時に codex の枠を見る。
    注入を受けたときの判断基準は `_claude/rules/subagent-model-tiering.md` の「枠の残量」

## 境界と制約

- 外部プロセスは `subproc.CommandContext` (../subproc) でしか起動しない。`exec_boundary_test.go` が
  「非テストの .go が os/exec を import しない」で固定する
- キャッシュは `~/.cache/glog/ratelimit-<source>.json` (出所ごと)。glogx の `claude-usage.json` とは分けている
  (理由は main.go 冒頭)。書き込みは `atomicfile.Write` (lint が `os.WriteFile` を禁止)
- 表示に載る文字列 (キャッシュ由来の Label) は入口で `termsafe` を通す
- 🚨 **`claude -p /usage` は全プロセス共有のゲート (`usage.FetchShared`、`usage/shared.go`) 越しにしか起こさない** (issue 627)。
  1 回ごとにサーバの `/api/oauth/usage` を叩き、これは強く rate limit される (glogx が 60 秒ごとに取ったら 17 回目で 429、
  以後約 45 分は枠が返らなかった)。呼び出し元は glogx (`FetchClaudePart` / `R` の `r` は `FetchClaudePartNow`)・`bin/ratelimit` (`Fetch`)・
  pro-con の dispatcher (自前の引数で起こし、`FetchShared` + `ParseStream` を通す)
  - 最後の結果を `~/.cache/glog/claude-usage-shared.json` に置き、成否によらず 5 分 (`SharedFresh`) は起こし直さない
  - サーバが枠を返さなかった (stream-json の `usage_report.rate_limits` が null。429 か通信の失敗) ら 10 分は起こさない
  - 上の 2 つのキャッシュはこの手前にある呼び出し側の契約で、ゲートとは別物
- pace 判定は `_claude/statusline-command.sh` と二重実装。乖離は `usage/pace_drift_test.go` が突き合わせ、
  shell だけを変えたときは `tests/claude/test_statusline.sh` がこのテストを `-count=1` で叩く
- lint の規則 (`render.go` の I/O 禁止・stdout 直書き禁止・空白の確保・幅エンジンの一本化) は
  src/glogx から移したもの。ruleguard の規則の正本は `src/glogx/gorules/rules.go`
