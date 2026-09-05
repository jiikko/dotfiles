# research: glogx の performance / resource-leaks 監査 (2026-09-05)

起票日: 2026-09-05
カテゴリ: research

`/audit` を glogx に絞って実行した記録。実行方式は forge (Minimum / Go ロスター 3 体並行:
`go-architecture-designer` / `architecture-reviewer` / `test-coverage-advisor`) + main agent の
独立実測。**この issue は索引** — 起票したもの・却下したもの・攻めて外れた範囲を持つ。

対象: `src/glogx/`（`package main` フラット構成 + `issues/` `usage/` `subproc/` `termwidth/`、
非テスト 27,592 行）。観点は **performance** と **resource-leaks** の 2 つ。

## 全数勘定

エージェント 3 体の生指摘 **7 件** → main agent の検閲・重複統合・実測を経て **起票 5 件 / 却下 2 件**。

| # | カテゴリ | 内容 | 由来 | 裏取り |
|---|---|---|---|---|
| 268 | perf | issues viewer のフレームが可視行 × 全件で走る | **main agent が独立発見** | 実測（退行前後 + CI） |
| 269 | test | bench の時間予算が CI 実測の 16〜125 倍緩い | main agent | CI 実測の全数勘定 |
| 270 | perf | doctor が畳まれた行の detail/copyText を毎フレーム構築 | agent 2 体が独立に指摘 | main agent が実測（7.15ms） |
| 271 | bug | 見張りの fd 上限がディレクトリ数を数えている | `test-coverage-advisor` | main agent が独立に実測再現 |
| 272 | bug | `detailsLoading` の札が世代交代で永久に残る | `go-architecture-designer` | main agent が単体テストで再現 |
| 274 | perf | doctor の hint が選択ゼロでも毎フレーム全 Item を舐める | **270 の敵対的レビュー** | `-memprofile` で確定（確保の 88.9%） |

## 却下（理由つき。同じ指摘の再生成を止めるため）

- **却下: ratelimit ダッシュボードに毎フレームのゲートが無い** —
  `architecture-reviewer` が「全画面ビューア 4 枚のうち doctor と ratelimit にゲートが無い」
  として doctor と同列に挙げたが、`test-coverage-advisor` が**より具体的な反証**を出した:
  `usage.RenderDashboard` は毎フレーム `newBraille` でセル数ぶんのスライスを確保し直すものの、
  **データ着信後は `spinnerActive()` の `rlDashLoading()` が偽になり、`View()` は打鍵と
  1 分周期の usage 更新でしか回らない**。doctor と違って 12.5fps の描画経路に乗らない。
  ダイアルの枚数も枠数ぶんの小さい定数。**「予算ケースが無い」だけを根拠に指摘しない**
  （`refuse-low-value-coverage.md`）。doctor 側のゲート不足は 270 に含めた

- **却下: `doctorView.lines()` の毎フレーム再構築そのもの** — `issues/done/163` が実測
  172µs/call を根拠に既にメモ化を却下しており、`go-architecture-designer` の再実測
  (218µs/frame・210,794 B・2,022 allocs、32 エントリ / 67 行 / page=40) も同オーダーで
  その判断を追認した。**163 の却下は今も妥当**。270 が扱うのは同じ関数の別の駆動因
  （行数ではなく Items / Contents の総数）で、163 の trigger「行数が 200 を超えたら」では
  永久に発火しない部分（詳細は 270 の該当節）

## 保留（指摘にしなかったが、測るなら trigger を書いた）

- **`worktree_status.go:loadWorktreeStatus` が status viewer 表示中に 1.5 秒ごとに
  `git status` に加えて `rev-parse --show-toplevel` も fork する点**。
  `--show-toplevel` の値は glogx が `os.Chdir` を一切しない（repo 全体を grep して 0 件）ため
  実行中は不変なので 1 回で足りるが、**追加 fork の実コストは本監査では測っていない**。
  `statusPollInterval` のコメントは `git status` のポーリング自体を「シェルの prompt が
  毎コマンド叩いている程度のコスト」と明示的に価格づけしており、その規律の範囲内とも読める。
  **trigger**: status viewer が重いという報告が出たとき、または fork 回数を数える metric を
  `bench_budgets.ci` に足すとき

## 攻めて見つからなかった範囲（次の監査の起点。0 件と報告する）

エージェント 3 体が独立に悉皆列挙し、いずれも防御を確認した:

- **`context.CancelFunc` の取り違え**: `doctor_view.start` は先頭で `stop()` / `doctor_delete` の
  2 つの `WithCancel` は冒頭で前相の `cancel()` / `action_modal.startCancelable` は単一スロットで
  `cancelAll` から `stop()` / `usage_overlay.fetchCmd` は `inFlight` の single-flight +
  closure 側 `defer cancel()`。「新旧の取り違え」は成立しなかった
- **子プロセスの孤児化**: ctx を持たない `exec.Command`（`openEditorAtRoot` / `openFilerAtRoot` /
  `openJobLogInEditor` / `editorCommand`）は全て `tea.ExecProcess` 経由で bubbletea が Wait する。
  Start して Wait しない経路は 0 件。`usage/codex.go:FetchCodex` は `defer cancel(); cmd.Wait()` を
  必ず通る（早期 return も同じ defer を通る）
- **channel 送信 goroutine の詰まり**: `main.go` の `planCh` / `decorCh` / `displayCh`、
  `github.go:ResolveRepo` の `originCh`、`cli_health.go` の `results`、`usage/codex.go:FetchAll` の
  `ch` / `verCh` はすべてバッファ付きで、受信を飛ばす return 経路でもブロックしない。
  doctor の `diskCh` は `catalogN+1` で `OnResult` の呼び出し回数と対応
- **ファイル / タイマー / latch**: `cache.go:writeAtomic` は write / Close / rename の 3 分岐すべてで
  temp を掃除（残る `CreateTemp`〜`Remove` 間の SIGKILL は issue 219 に明記済み）。
  `probe.go` の `writeProbeFile` / `probeAppend` は全分岐で Close。`status_view.go:untrackedPreview` と
  `issues/parse.go:LoadMeta` は `defer Close`。`issues_watch.go:drainWatchEvents` は
  `defer timer.Stop()` + Stop/drain/Reset の標準形。`cleanupLatch` は WaitGroup の
  Add-during-Wait 問題を避けた実装で n<0 も塞いでおり、`cancelAll` → `waitDoctorCleanup` →
  `waitPullCleanup` の看取り順も `main.go` の defer LIFO で成立（`syscall.Exec` 経路含む）
- **`gitlog_watch.go` の 2 チェーン状態機械**（`evArmed` / `pollArmed` / `measuring` / `reloading` /
  `gen` / `reloadSeq`）: 全 return 経路を追ったが札の取りこぼし無し。`handleGitLogReload` の
  `seq` 不一致経路は `applyLogData` が `reloadSeq` を進めると同時に `reloading` を降ろすため閉じている。
  `issues_watch.go:handleWatch` も同様
- **描画ホットパスの O(n) 再計算**: `issues/body.go:Body.Lines` と `tui.go:lines` は幅・色で
  メモ化済み。**実測で行数に対し平坦**（status 40→2000 行で 311→316 allocs）。
  ただし **issues viewer は平坦ではなかった** → 268

## 監査の作法についての記録

- **read-only の 3 体が並行で走っている間、main agent は同じ working tree で変異を当てない**
  （`parallel-write-agents-need-worktree-isolation.md`）。本監査の実測は全て
  `~/wt-audit-glogx`（`origin/master` の使い捨て worktree）で行い、終了後に復元して clean を確認した
- **エージェントの報告を無検閲で issue に転記しない**（`subagent-model-tiering.md`）。
  実際に効いた: 268（agent は誰も見つけていない）/ 270（2 体が指摘したが片方は「未実測」、
  もう片方は 163 の却下で取り下げ → main agent の実測で確定）/ 271・272（agent の指摘を
  main agent が独立に再現）/ ratelimit（2 体の意見が割れ、具体的な反証を持つ方を採用）
- **ベンチのフィクスチャは「その分岐に到達しているか」を確かめる**。選択中フレームを測る
  ベンチで `v := m.issuesOv` と**値で**受けたため `marked` の設定が本体に届かず、
  12.8 倍の増幅を 1.0 倍と誤読しかけた（`&m.issuesOv` で修正）。
  `mutation-verify-new-tests.md` の「fixture が狙った分岐に届いているか」の実例

## 敵対的レビュー（2026-09-05、opus・トピックごとに直列）

起票後に「issue は間違っている前提で反証しろ」で red team を当てた。実測値はすべて
再現されたが、**推奨対応と規模の主張に誤りが出た**。訂正は各 issue の本文に入っている。

- **268 / 269**: 269 の「CI 実測の ~4 倍へ締める」が 268 の退行（2.74 倍）を定義上通す、
  という自己矛盾。主案を「同一 run 内の比」へ差し替え（repo に `agent_panel_tick_scale_x100`
  の前例があり、私の却下欄は「run を跨ぐ差分」と混同していた）。
  `check_bench_budgets.sh` の `rel` が実効上限を 0.1ms 刻みに丸める点も発見
- **270**: 🚨 **規模の主張が実機データで反証された**。著者の実機 snapshot は総 Items **29 件**で、
  フィクスチャ（6,400 件）は約 220 倍。実機では 223µs で、163 の却下値 172µs と同じ桁 =
  **163 の判断は今も正しい**。優先度を 高 → 中 に下げ、「163 の射程外」を撤回した。
  新規性は「規模」ではなく「構造」にある。残余の帰属も誤っていた（→ 274）
- 「不在の主張」の探索範囲に**テストファイルを入れていなかった**のが 270 の一番の反省
  （「畳まれた行の detail を処理するのは意図的」はテストのコメントに書かれていた）

## 残課題

- [ ] 268（比較を O(1) 化）/ 270（detail・copyText・copyPath の遅延化）/ 274（早期 return）
- [ ] 269（比 metric の導入 + `check_bench_budgets.sh` の書式）
- [ ] 271 / 272
- [ ] 271 / 272 / 273 の敵対的レビュー（トピック 3〜5。未実施）
