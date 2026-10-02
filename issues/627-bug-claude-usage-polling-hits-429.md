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
  `usage.FetchAll` → `claude -p /usage` を起こす (起票時点。同日の 626 で `FetchClaudePart` に置き換わった)。ratelimit (hook) はキャッシュ 5 分 (`cacheTTL`) で、別に起こす

## 対応方針 (ユーザー判断 2026-10-02: 頻度を落とし、429 で止める)

- `claude -p /usage` を起こす全ての呼び出し元 (glogx / bin/ratelimit / pro-con の dispatcher) を、全プロセス共有のゲート
  (`usage.FetchShared`、`src/ratelimit/usage/shared.go`) に通す。最後の結果を `~/.cache/glog/claude-usage-shared.json` に書き出し、
  全ての取得元がまずそこを読む (ユーザー提案 2026-10-02)。claude の起動は flock で 1 本に絞る
  - 成否によらず、5 分以内に誰かが claude を起こしていれば起こし直さない (成功ならその結果、失敗ならその理由を返す)
  - サーバが枠を返さなかった (`usage_report.rate_limits == null`。429 か通信の失敗) ら、10 分は起こさず、理由と再開時刻をエラーにする
  - glogx の R の `r` (人の操作) だけは 5 分の間引きを飛ばす。止めている間は飛ばさない
- 失敗の理由を「検出できず」ではなく「サーバから利用枠を受け取れない (429 か通信の失敗)。HH:MM まで取得を止める」にする
- glogx / ratelimit のキャッシュの契約 (main.go 冒頭の分離理由) は変えない

## 関連ファイル

- `src/ratelimit/usage/usage.go` (`Fetch`) / `src/ratelimit/usage/codex.go` (`FetchClaudePart` / `FetchClaudePartNow`)
- `src/glogx/tui.go` (`usageRefreshInterval`) / `src/glogx/usage_overlay.go`
- `src/ratelimit/main.go` (`cacheTTL` / `refreshInterval`)

## 進捗

- 2026-10-02: 起票。原因を実測で特定
- 2026-10-02: 実装 (commit「fix(ratelimit): Claude の利用枠の取得を全プロセス共有のゲートに通し、429 のあいだは止める」
  「fix(ratelimit,glogx,pro-con): 共有ゲートの敵対的レビュー指摘を直す」「fix(ratelimit): 共有ゲートの 2 周目の指摘を直す」
  「docs(ratelimit,glogx,pro-con): claude -p /usage の共有ゲートを入口の文書に書く」)
  - [x] 共有ゲート (5 分の共有・失敗の共有・10 分の停止・flock・未来の時刻を信用しない)
  - [x] glogx / bin/ratelimit / pro-con の dispatcher をゲートに通す (pro-con は自前の引数のまま `FetchShared` + `ParseStream`)
  - [x] glogx の R の `r` は間引きを飛ばす (`FetchClaudePartNow`。626 の出所ごとの取得の上に載せ直した)。フッターは `usage.SharedFresh` から「5分ごとに更新」
  - [x] ratelimit / glogx / pro-con の lint と全テストが緑。変異 23 本 (ゲートの各判定・stream-json の判定・glogx の `r` の配線・
        pro-con の素通り) がすべて想定どおり red
  - [x] 実環境 (429 中): worktree の build で `ratelimit -source claude` が 1 回目 3.2 秒で「…14:14 まで取得を止める」、2 回目は 0.00 秒
        (claude を起こさない)。この時点の止める期間は 30 分 (後で 10 分に変えた)
  - [ ] 実環境の成功経路 (サーバが枠を返す状態で stream-json から枠が読めること) の観測。CLI の 429 の覚えが切れるまで待つ

### 敵対的レビュー (opus、観点を分けて 3 本 + 2 周目 1 本)

採用して直した: pro-con の dispatcher がゲートを素通りしていた / 読めない応答のたびに起こし直す / blockedUntil に上限が無い
(→ 丸める) / 未来の fetchedAt を信用する / R の `r` が取り直さない / フッターの「1分ごと」/ rate_limits=null は通信断でも出うる
(→ 文言と 10 分) / type が result でない行の result キー / 共有する失敗の理由の長さ

記録だけ (今より悪くはしない。直していない):
- 他の呼び出し元 (起こし方が違う pro-con 等) の失敗が、自分の取得を 5 分止める。止まりきりにはならない
- claude が呼び出し側の持ち時間 (glogx は 10 秒。ロック待ちを含む) より遅いと、時間切れは共有しないので毎回起こしては kill する
- 止めている間でも、5 分以内に取れていた枠は force 無しの呼び出しに成功として返る (glogx の staleErr の注記が消える)
- Snapshot に取得時刻が無く、受け取った側が自分の時刻で記録する (各キャッシュの古さが最大 5 分若く見える)。
  claude の版の表示も最大 5 分古い
- ロック待ちで打ち切ると「他のプロセスが利用枠を取得中で、待ちきれなかった」を出す (以前は自分で起こしていた)
- `cachedir.Base()` が失敗する環境 (HOME も XDG も無い) ではゲートを通らない

却下: pro-con の旧 ParseUsage が受けていた小数・「<1%」を usage.Parse が受けない — `-p` の /usage は Math.floor の整数で出す
(2.1.287 のバイナリで確認)。resets の無い行は 0% の枠。理由は `src/pro-con/dispatcher/usage.go` の `usageOf` のコメント
