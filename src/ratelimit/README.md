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
- 🚨 **Claude の枠の主な出所は statusline が書き出す `~/.cache/glog/claude-rate-limits.json`** (`usage/statusline.go`、issue 627)。
  Claude Code が推論の応答ヘッダで受け取り statusline の入力に載せる `rate_limits` を、`_claude/statusline-command.sh` の
  `write_rate_limits` が書く。サーバを余計に叩かない。7d(Fable) は来ないので出ない
  - 🚨 statusline は送信が無くても `refreshInterval` (60 秒) ごとに描画し、そのたびに最後に受け取った古い値が来る。書き手は窓ごとに
    値で新旧を比べ (リセット時刻が後、同じなら使用率が高い方)、新しい観測のときだけ書き換える。`observedAt` は
    「誰かが新しい値を受け取った時刻」で、15 分新しい値が来なければ下の予備へ落ちる (pro-con の PG は statusline を走らせないので、
    PG だけが動いている間は予備のサーバの値で見る)
- `claude -p /usage` は上のファイルが無いか古いときだけの予備で、全プロセス共有のゲート (`usage.FetchShared`、`usage/shared.go`)
  越しにしか起こさない。1 回ごとにサーバの `/api/oauth/usage` を叩き、これは強く rate limit される (glogx が 60 秒ごとに取ったら
  17 回目で 429。CLI は 429 をプロセスをまたいで覚えないので、起こせば毎回叩く。対話の `/usage` が 429 でも見えるのは応答ヘッダの値で
  答えるから)。呼び出し元は glogx (`FetchClaudePart` / `R` の `r` は `FetchClaudePartNow`)・`bin/ratelimit` (`Fetch`)・
  pro-con の dispatcher (自前の引数で起こし、`FetchShared` + `ParseStream` を通す)
  - 最後の結果を `~/.cache/glog/claude-usage-shared.json` に置き、成否によらず 5 分 (`SharedFresh`) は起こし直さない
  - サーバが枠を返さなかった (stream-json の `usage_report.rate_limits` が null。429 か通信の失敗) ら 50 分は起こさない
    (サーバの Retry-After は実測 45〜47 分)
  - 上の 2 つのキャッシュはこの手前にある呼び出し側の契約で、ゲートとは別物
- pace 判定は `_claude/statusline-command.sh` と二重実装。乖離は `usage/pace_drift_test.go` が突き合わせ、
  shell だけを変えたときは `tests/claude/test_statusline.sh` がこのテストを `-count=1` で叩く
- lint の規則 (`render.go` の I/O 禁止・stdout 直書き禁止・空白の確保・幅エンジンの一本化) は
  src/glogx から移したもの。ruleguard の規則の正本は `src/glogx/gorules/rules.go`
