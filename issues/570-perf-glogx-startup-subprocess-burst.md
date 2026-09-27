# 570 (perf): glogx を開いた直後に外部プロセスが一斉に起動し、最初の数秒がもっさりする疑い

起票日: 2026-09-28

## 概要

ユーザー報告 (2026-09-28): glogx の git log を開いたら若干もっさりした。

机上で起動の経路を追った。**実測はしていない**。起動の道筋 (git の呼び出し・shim) には目立つ律速は見当たらなかった。
一番疑わしいのは、`browseModel.Init` (`src/glogx/tui.go`) が開いた直後に外部プロセスを 10 本近く同時に起こし、
最初の数秒に打ったキーへの反応と描画が CPU の取り合いで鈍る形。最初の画面自体はこれらを待たずに出る。

## 詳細 (コードで確かめたこと)

`Init` が起動直後に `tea.Batch` で投げる Cmd のうち、外部プロセスを起こすもの:

| Cmd | 起こすプロセス | 毎回走るか |
|---|---|---|
| `usageOverlay.fetchCmd(true)` → `usage.FetchAll` (`src/ratelimit/usage`) | `claude -p /usage --model haiku --output-format json` + `claude --version` (`Fetch` の中で並列) / `codex app-server` + `codex --version` (`FetchAll` の中で並列) | キャッシュの TTL は `usageCacheTTL = usageRefreshInterval` = **1 分**。前回の取得から 1 分を超えて開くと走る。`tui.go` の注記の実測は 1 回 ≈ 2.0s wall / 1.8s CPU (2026-07-25) |
| `checkClaudeVersionCmd` / `checkCodexVersionCmd` (`checkCLIVersionCmd`) | `claude --version` / `codex --version` (`usage.FetchVersion` / `usage.FetchCodexVersion`) | latest (registry) はキャッシュするが、インストール済みの版は**毎回**取り直す。usage 側と**同じコマンドを重複して**起こしている |
| `checkCLIHealthCmd` (`cli_health.go`) | `claude auth status` / `codex login status` | TTL なしで毎回 (ログイン状態は毎回見る、という意図がコメントにある) |
| 初回の CI 取得 (`m.fetch`) | `gh` の API | キャッシュに無い SHA があるとき |
| `loadTmuxPrefix` / `gitLogWatchDirsCmd` / `gitLogPollCmd` | tmux / git | 毎回 (軽い) |

`claude` は node なので、`--version` だけでも起動の CPU がかかる見込み (未実測)。usage の TTL が切れていると、
`claude` 系 4 本 (`-p /usage`・`--version` ×2・`auth status`)・`codex` 系 4 本 (`app-server`・`--version` ×2・`login status`) が同時に立つ。

付随して、usage の取得中は spinner の tick (`spinnerInterval` = 80ms) で `View()` → `viewLines()` が画面全体を組み直す。
一覧の行にはフレームのキャッシュが無い。既定の 20 件なら単独では軽いはずだが、上の CPU の取り合いと同じ時間帯に重なる。

### 律速でないと見たもの (机上)

- git の呼び出し: `runLog` (`main.go`) が git log・repo 解決・未 push 判定を並列化済み。注記の実測は最長の連鎖で fork 2 本 ≈ 12ms
- shim: `bin/lib/go_autobuild.zsh` のソースの指紋は zstat の一括呼び出し (注記の実測 0.75ms)。`--async` なのでビルドは待たない
- 1 画面に収まるかの判定 (`fitsTerminal(len(RenderLines(...)))`) は全行を 1 回組むが、既定は 20 件

## 対応方針

1. **先に測る** (perf-claims-need-measurement)。安い順:
   - 1 分以内に 2 回続けて開き、2 回目が軽いか (usage のキャッシュが当たる側)。差が出れば usage の取得が本命
   - 開いた直後に `ps -Ao pid,%cpu,etime,command | grep -E 'claude|codex|gh '` で、同時に走る本数と CPU を採る
   - `Init` の各 Cmd の開始・終了の時刻と、最初のキー入力への反応の時刻を並べる
2. 当たりだったときの直し方の候補 (どれを採るかは測ってから決める):
   - `claude --version` / `codex --version` の重複をやめる (usage の取得とバージョン検査で 1 回に寄せる)。
     🚨 これは 2026-07-25 の perf 監査で「対応しない」と判断済み (`src/glogx/claude_version.go` の `fetchInstalledClaudeVersion` のコメント:
     usage のキャッシュが当たれば重複は消える / memo 化すると `claude update` の前後の版の表示が壊れる)。採るならその再評価条件を先に満たす
   - usage は TTL が切れていても、まず古いキャッシュで表示し、取り直しを起動から数秒遅らせる
   - ログイン検査とバージョン検査も起動から遅らせる (最初の数秒の CPU を空ける)

## 受け入れ条件

- [ ] 開いた直後の外部プロセスの本数・CPU と、体感の差 (usage のキャッシュの当たり / 外れ) を実測して本文に書く
- [ ] 本命が上の仮説なら直して、直す前後の同じ測り方の値を書く。外れなら外れと書いて、次の仮説へ進む

## 関連ファイル

- `src/glogx/tui.go` の `browseModel.Init` / `spinnerInterval` / `usageRefreshInterval`
- `src/glogx/usage_overlay.go` の `fetchCmd`、`src/glogx/usage_cache.go` の `usageCacheTTL`
- `src/glogx/claude_version.go` の `checkCLIVersionCmd`、`src/glogx/codex_version.go`
- `src/glogx/cli_health.go` の `checkCLIHealthCmd`
- `src/ratelimit/usage/usage.go` の `Fetch` / `FetchVersion`、`src/ratelimit/usage/codex.go` の `FetchAll` / `FetchCodexVersion`
- `src/glogx/main.go` の `runLog` / `gatherRepoPlan`

## 進捗

- 2026-09-28: ユーザーの指示「usage・バージョン検査・ログイン検査は CI の取得が終わったら呼び出して」で、測る前に直し方を決めた
  (commit「glogx: usage・バージョン・ログイン検査を、起動時の CI 取得が終わってから投げる (issue 570)」)
  - 起動時の CI 取得がある間は 3 つの検査を `browseModel.deferredStartup` に預け、`Update` の外側 (`releaseStartupChecks`) で
    「取得中でなくなった」状態を見て 1 回だけ放す。出来事 (最後のチャンクの `ciResultMsg`) でなく状態で見るのは、
    pull の後に取得を始めない分岐 (`reloadAfterPull` の `pendingFetches = 0`) が結果を経ずに取得をやめるため。取得が無い起動では今までどおりすぐ投げる
  - `usageOverlay.fetchCmd` は呼んだ時点で timeout (`fetchTimeout` = 10s) を刻み始めていた。預けている間に持ち時間を失うので、
    取り消し (`stop`) は呼んだ時点で持ち、timeout は走り出してから刻む形にした
  - テスト `TestBrowseStartupChecksWaitForCIFetch` (途中のチャンクでは放さない / 最後のチャンクで放す / 2 回放さない / 結果を経ずに取得をやめても放す)。
    変異 4 本 (預けない / 放すのを `ciResultMsg` に限る / 取得中かを見ずに放す / 放した後に消さない) がどれも想定のケースだけ red (`bin/mutate-verify`)
  - usage の timeout を走り出してから刻む変更は、テストで固定していない (壁時計に頼らずに観測する口が無い)。コードを読んで確かめただけ
- 反証レビュー (read-only のサブエージェント 1 体): 事実誤認なし。指摘 2 件を反映した (codex 系の本数 3 → 4 / `--version` の重複は既に却下済みで、その理由を対応方針に書いた)

## 残タスク

- [ ] 直した後の体感と、開いた直後の外部プロセスの本数・CPU を実測して書く (受け入れ条件 1。直す前の値は採っていない)
