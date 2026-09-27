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

- [x] 開いた直後の外部プロセスの本数・CPU と、体感の差 (usage のキャッシュの当たり / 外れ) を実測して本文に書く (下の「実測」)
- [ ] 本命が上の仮説なら直して、直す前後の同じ測り方の値を書く。外れなら外れと書いて、次の仮説へ進む
  - 仮説は**キー反応については外れ** (下の「実測」)。入れた修正 (CI 取得の後へ回す) はキー反応・最初の画面のどちらにも差を出していない。残すか revert するかはユーザーの判断待ち

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
- 敵対的レビュー (read-only のサブエージェント 1 体) の対応 (commit「glogx: 閉じる途中で預けた検査を放さない / usage のキャッシュは CI 取得中でも先に出す (issue 570 の敵対的レビュー)」):
  - P2-1 (レビュワーが probe で再現): `quitWith` は閉じる演出を始めると `done` を立てずに戻り、`cancelAll` が CI 取得を取り消すので、その結果が演出の間 (160ms) に届くと
    預けた検査を放し、終了直前に claude / codex を起こして孤児にしていた → 終了の時点で `deferredStartup` を捨てる
  - P2-2: usage のディスクキャッシュ読み (fork なし) まで預けていたので、CI 取得の間ずっと右上がスピナーだった → 取得中もキャッシュは同期で先に読み
    (`usageOverlay.showCached`)、外れたときだけ取得を預ける。R の復元・U / R の取得も、キャッシュが当たれば待たされない
  - P3-1: テストが usage を見ていなかった → `PATH` を空にして本物を起こさずに、usage の取得を投げたか・放したかを `usageMsg` で見る形にした。
    変異 3 本 (終了で捨てない / キャッシュを先に読まない / 取得中も usage の取得を直接投げる) が想定のケースで red。
    usage の timeout を「呼んだ時点」に戻す退行は今もテストで捕まえられない (未対応)
  - P3-2 (未確認・記録のみ): 起動時の取得中に pull などで `startCIFetch` が繰り返されると、預けが延び続ける。1 回は `fetchTimeout` (10s) で必ず返るので、永久には放されないことはない
  - 壊せなかった観点: 永久に放されない経路 / 2 回放す経路 / `update` から `Update` を呼ぶ経路 (0 件) / ctx の変更による goroutine・timer の漏れ
  - 直した差分への 2 周目のレビューは省いた: 修正は新しい判定を足しておらず (終了で捨てる 1 行と、既存の `loadUsageCache` を呼ぶだけ)、どちらも変異で直接確かめた
- 反証レビュー (read-only のサブエージェント 1 体): 事実誤認なし。指摘 2 件を反映した (codex 系の本数 3 → 4 / `--version` の重複は既に却下済みで、その理由を対応方針に書いた)

## 実測 (2026-09-28)

条件: Apple M3 Max (14 コア) / claude 2.1.283 / codex-cli 0.157.1 / gh 2.101.0。直す前 = `11d156bd`、直した後 = `24c1a693` を
それぞれ `go build` したバイナリを直接起動した (**`bin/glogx` の shim と tmux popup (C-g) の経路は通していない**)。
隔離した tmux (`-L` + 使い捨ての `TMUX_TMPDIR`、200x50) の中で、cwd は `~/dotfiles`。

- 「空」= `XDG_CACHE_HOME` を空の dir にして起動 (CI の取得・usage の取得・最新版の取得がすべて走る)。「2 回目」= 空の run の直後に同じ dir で起動 (usage の TTL 1 分に当たる)
- 子プロセス: 50ms ごとに `ps -Ao pid,ppid,time,command` を採り、glogx の子孫を数える。CPU は pid ごとの `time` の最大値。**50ms より短命なプロセスは取りこぼす**
- キー反応: 最初の画面 (行頭 `→ ` のカーソル行) が出てから 0.2s・0.7s・…・7.2s に `j` を送り、`capture-pane` でカーソル行の commit が変わるまでの時間

| 条件 (回数) | 最初の画面 | 3s 以内に起きた子の CPU | `claude -p /usage` の起動 → 終了 | キー反応 1〜2 回目 / 3 回目以降 |
|---|---|---|---|---|
| 直す前・空 (5) | 0.45〜0.87s (0.87 は初回の 1 本だけ) | 2.89〜2.95s | 0.42〜0.92s (その run の最初の画面と同時) → 4.65〜4.96s (CPU 2.32〜2.48s) | 25〜46ms / 117〜177ms |
| 直した後・空 (5) | 0.43〜0.60s | 2.84〜3.06s | 1.14〜1.50s → 4.99〜6.04s (CPU 2.33〜2.46s) | 23〜68ms / 116〜146ms |
| 直す前・2 回目 (5) | 0.43〜0.47s | 0.08〜0.21s | 走らない | 23〜44ms / 116〜145ms |
| 直した後・2 回目 (5) | 0.43〜0.49s | 0.11〜0.17s | 走らない | 23〜45ms / 116〜146ms |

(回数は run の数。キー反応の 3 回目以降だけは、後から測り直した 2 run ずつ (各 13 回) の値。最初の 3 run ずつは画面上の行番号で判定したため、スクロールの後を測れていなかった)

読み取れること:

- 子プロセスの CPU は **`claude -p /usage` の 1 本がほぼ全部** (1 回 ≈ 2.4s CPU / ≈ 4s wall)。`claude auth status` 0.09s、`gh` は合計 0.3〜0.4s、codex 系・`--version` は数十 ms 以下。usage の TTL が当たる 2 回目では、起動直後の子の CPU は 0.1〜0.2s に落ちる
- 直した後の版は、claude / codex の起動が最初の画面から 0.7〜0.9s 後 (CI の取得の後) へずれている。修正は設計どおり動いている
- **キー反応は、空 / 2 回目・直す前 / 後のどれでも同じ**。1〜2 回目は 23〜68ms、スクロールが始まる 3 回目以降は 116〜177ms で、
  3 回目以降の差は一覧のスクロールの glide (`scroll_glide.go` の `scrollAnimFrames` = 6 × `scrollInterval` 16ms ≈ 100ms) によるもの。
  glogx 自身の CPU は 15 回押して合計 0.2s 前後なので、計算で詰まってはいない。14 コアの機械では、usage の取得の CPU はキー処理と取り合っていない
- 付随して観測: `claude -p /usage` の子として bash 約 20 本・git・jq が同時に起きる。cwd の repo の SessionStart hook が走っているとみられる (未確認。短命なので CPU は取りこぼしている可能性がある)

未確認のまま残る候補 (「もっさり」がこの測り方で再現していない):

- `bin/glogx` の shim (`go_autobuild_exec --async`): ソースが変わった直後の初回起動では、裏で `go build` が走り、起動直後の CPU と重なる。glogx の commit が続いた日に開くとこの形になる
- tmux popup の経路 (C-g) と、実際の端末サイズ・負荷の高い時間帯

## 残タスク

- [ ] 入れた修正 (CI の取得の後へ回す) を残すか revert するかを決める。上の実測ではキー反応にも最初の画面にも差が無い (CLAUDE.md「効果がなかった修正は revert する」。ただしユーザーの指示で入れた修正なので判断はユーザー)
- [ ] 「もっさり」の次の仮説 (shim の裏ビルド / popup の経路) を、同じ測り方で popup 経由で測る。再現しなければ受容して done にする
