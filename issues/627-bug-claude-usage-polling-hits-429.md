# 627 (bug): Claude の利用枠の取得が `/api/oauth/usage` を 1 分ごとに叩いて 429 になり、枠が取れなくなる

> 🚨 **担当中: ratelimit-429 のセッション**（2026-10-02〜）

起票日: 2026-10-02

## 概要

`ratelimit -source claude` が rc=3 `ratelimit: claude: /usage 出力から利用枠を検出できず` を返し、glogx の U / R も Claude の枠を
出さなくなった (2026-10-02 12:10 以降)。

## 詳細 (実測 2026-10-02、Claude Code 2.1.287)

- `claude -p /usage --output-format json` は rc=0・`is_error: false` だが、`result` に `Current session: N% used · resets …` の行が無い
  (「What's contributing to your limits usage?」の内訳だけ)。2.1.286 でも同じ
- `--output-format stream-json --verbose` の assistant 行の `usage_report.rate_limits` が `null`
- `--debug-file` のログ:
  `fetchUtilization: GET /api/oauth/usage (attempt 1)` → `fetchUtilization: 429 remembered for this bearer; not asking again for 2692s` →
  `Failed to load usage data: Request failed with status code 429`
- CLI の `/usage` (local command) は `/api/oauth/usage` の結果から枠の行を組み、取れなければ**その行を黙って省く** (エラーにしない)
- 誰が叩いたか: `claude -p` は一時ディレクトリの project にセッション記録を残す (`usage.Fetch` の cmd.Dir のコメント)。
  `/usage` を含む記録の mtime を数えると、普段は 1 時間に 2〜8 回、10-02 は 11 時 12 回 / 12 時 46 回 / 13 時 34 回。
  11:53:41 から **ちょうど 60 秒ごと**に 17 回成功し、12:10:41 から枠の無い応答になった
- 60 秒周期の出所は glogx: U の箱 / R のダッシュボードの表示中、`usageRefreshInterval = time.Minute` (`src/glogx/tui.go`) ごとに
  `usage.FetchAll` → `claude -p /usage` を起こす。ratelimit (hook) はキャッシュ 5 分 (`cacheTTL`) で、別に起こす

## 対応方針 (ユーザー判断 2026-10-02: 頻度を落とし、429 で止める)

- `usage.Fetch` の下に、全プロセス共有の Claude 取得ゲートを置く。最後に取れた結果を `~/.cache/glog/claude-usage-shared.json` に書き出し、
  全ての取得元 (glogx / ratelimit) がまずそこを読む (ユーザー提案 2026-10-02)。claude の起動は flock で 1 本に絞る
  - 5 分以内に誰か (glogx / ratelimit / 他の glogx) が取った結果があれば、`claude` を起こさずそれを返す
  - サーバが枠を返さなかった (`usage_report.rate_limits == null`) ら、30 分は `claude` を起こさず、理由と再開時刻をエラーにする
- 失敗の理由を「検出できず」ではなく「サーバが利用枠を返さない (429 の可能性)。HH:MM まで取得を止める」にする
- glogx / ratelimit のキャッシュの契約 (main.go 冒頭の分離理由) は変えない

## 関連ファイル

- `src/ratelimit/usage/usage.go` (`Fetch`) / `src/ratelimit/usage/codex.go` (`FetchAll`)
- `src/glogx/tui.go` (`usageRefreshInterval`) / `src/glogx/usage_overlay.go`
- `src/ratelimit/main.go` (`cacheTTL` / `refreshInterval`)

## 進捗

- 2026-10-02: 起票。原因を実測で特定
