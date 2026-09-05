# research: glogx の performance / resource-leaks 監査 (2026-09-05)

起票日: 2026-09-05
カテゴリ: research

`/audit` を glogx に絞って実行した記録。実行方式は forge (Minimum / Go ロスター 3 体並行:
`go-architecture-designer` / `architecture-reviewer` / `test-coverage-advisor`) + main agent の
独立実測。**この issue は索引** — 起票したもの・却下したもの・攻めて外れた範囲を持つ。

対象: `src/glogx/`（`package main` フラット構成 + `issues/` `usage/` `subproc/` `termwidth/`、
非テスト 27,592 行）。観点は **performance** と **resource-leaks** の 2 つ。

## 全数勘定

**母集合が 3 つあるので分けて数える**（合算しない）:

- **エージェント 3 体の生指摘 7 件** → 起票 **3 件**（270 / 271 / 272。うち 270 は 2 体の
  独立指摘を 1 件に統合）/ 却下 2 件（うち 1 件は後に撤回 → 275）
- **main agent の独立発見 2 件**（268 / 269。エージェントは誰も挙げていない）
- **敵対的レビュー由来 1 件**（274。270 のレビューが 270 の残余の帰属を反証する過程で発見）
- **却下の撤回 1 件**（275。273 のレビューが却下の機構を反証）

| # | カテゴリ | 内容 | 由来 | 裏取り |
|---|---|---|---|---|
| 268 | perf | issues viewer のフレームが可視行 × 全件で走る | **main agent が独立発見** | 実測（退行前後 + CI） |
| 269 | test | bench の時間予算が CI 実測の 16〜125 倍緩い | main agent | CI 実測の全数勘定 |
| 270 | perf | doctor が畳まれた行の detail/copyText/copyPath を毎フレーム構築 | agent 2 体が独立に指摘 | **実機 223µs** / 合成 6,400 items で 7.15ms（実機の約 220 倍） |
| 271 | bug | 見張りの fd 上限がディレクトリ数を数えている | `test-coverage-advisor` | main agent が独立に実測再現 |
| 272 | bug | `detailsLoading` の札が世代交代で永久に残る | `go-architecture-designer` | main agent が単体テストで再現 |
| 274 | perf | doctor の hint が選択ゼロでも毎フレーム全 Item を舐める | **270 の敵対的レビュー** | 確保の 88.9%（合成 6,400 items。実機 29 items では小さい） |
| 275 | perf | ratelimit ダッシュボードが毎フレーム全面再構築される | **273 の敵対的レビュー**（却下の撤回） | 実測 306µs / 377KB（200×50） |

## 却下（理由つき。同じ指摘の再生成を止めるため）

- 🚨 **撤回: ratelimit ダッシュボードに毎フレームのゲートが無い** — 監査時は
  「データ着信後は `rlDashLoading()` が偽になるので 12.5fps の描画経路に乗らない」として
  却下したが、**この機構は誤り**だった（本 issue の敵対的レビューが実験で反証）。
  `rlDashLoading()` は `spinnerActive()` の **18 項の OR のうちの 1 項**でしかなく、
  `toggleRatelimitDash` は長時間持続する `len(m.awaitCI) > 0`（push 直後）と
  `panelHasRunningJob()`（CI 実行中）を**どちらも落とさない**。しかも実測 **306µs / 377KB**
  （200×50）で、起票した doctor の実機値 223µs より**重い**。→ **issue 275 として起票し直した**
  - 教訓: **OR の 1 項が偽であることを、式全体が偽であることの根拠にしない**

- **却下: `doctorView.lines()` の毎フレーム再構築そのもの** — `issues/done/163` が実測
  172µs/call を根拠に既にメモ化を却下しており、`go-architecture-designer` の再実測
  (218µs/frame・210,794 B・2,022 allocs、32 エントリ / 67 行 / page=40) も同オーダーで
  その判断を追認した。**163 の却下は今も妥当**。270 が扱うのは同じ関数の別の駆動因
  （行数ではなく Items / Contents の総数）で、163 の trigger「行数が 200 を超えたら」では
  永久に発火しない部分（詳細は 270 の該当節）

## 保留（指摘にしなかったが、測るなら trigger を書いた）

- **`worktree_status.go:loadWorktreeStatus` が status viewer 表示中に 1.5 秒ごとに
  `git status` に加えて `rev-parse --show-toplevel` も fork する点**。
  `--show-toplevel` の値は glogx が `os.Chdir` を**非テストソースで**一切しない（`open_workspace.go`
  の 2 件は `cmd.Dir` = 子プロセスの cwd。テストには `t.Chdir` が在るので「repo 全体で 0 件」は
  不正確）ため実行中は不変で 1 回で足りるが、**追加 fork の実コストは本監査では測っていない**。
  `statusPollInterval` のコメントは `git status` のポーリング自体を「シェルの prompt が
  毎コマンド叩いている程度のコスト」と明示的に価格づけしており、その規律の範囲内とも読める。
  🚨 **呼び出し元は poll だけではない**: `status_view.go:runDiscard` が**破壊的操作の preflight**
  （確認中に変わっていないかの再検証）で同期的に `loadWorktreeStatus()` を呼ぶ。
  「1.5 秒ごと」の枠取りはこの呼び出しを数えていない。
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
  Start して Wait しない経路は 0 件。`usage/codex.go:FetchCodex` は **`Start` 成功後は** `defer cancel(); cmd.Wait()` を必ず通る。
  🚨 「早期 return も同じ defer を通る」は誤り — `defer` の登録は `Start` 成功**後**で、
  その前に `StdinPipe` / `StdoutPipe` 失敗の 2 本の早期 return がある。ただしプロセスが無いので
  回収対象も無い（`StdinPipe` 成功 → `StdoutPipe` 失敗の経路だけ親側 pipe が閉じられないが、
  `StdoutPipe` は Stdout 既設定か Start 後にしか失敗しないので実際上到達しない）
- **channel 送信 goroutine の詰まり**: `main.go` の `planCh` / `decorCh` / `displayCh`、
  `github.go:ResolveRepo` の `originCh`、`cli_health.go` の `results`、`usage/codex.go:FetchAll` の
  `ch` / `verCh` はすべて「cap ≥ 送信回数」かつ送信者が 1 本で、受信を飛ばす return 経路でもブロックしない。
  doctor の `diskCh` は `catalogN+1` で `OnResult` の呼び出し回数と対応。
  🚨 **「バッファ付きだから詰まらない」を全体の規則にしないこと**: 非テストの `make(chan` は
  全 12 箇所で、`doctor_delete.go` の 3 本と `probe.go` の signal は**別の設計**で安全
  （`OnCommand` は `select { case ch <- …: default: }` の非ブロッキング送信、`OnPhase` は
  `sendLatestProgress`、完了だけが専用 cap 1）。次に channel を足す人が cap を増やすだけで
  済ませないよう、理由を取り違えないこと
- **ファイル / タイマー / latch**: `cache.go:writeAtomic` は write / Close / rename の 3 分岐すべてで
  temp を掃除（残る `CreateTemp`〜`Remove` 間の SIGKILL は issue 219 に明記済み）。
  `probe.go` の `writeProbeFile` / `probeAppend` は全分岐で Close。`status_view.go:untrackedPreview` と
  `issues/parse.go:LoadMeta` は `defer Close`。`issues_watch.go:drainWatchEvents` は
  `defer timer.Stop()` + Stop/drain/Reset の標準形。`cleanupLatch` は WaitGroup の
  Add-during-Wait 問題を避けた実装で n<0 も塞いでおり、`cancelAll` → `waitDoctorCleanup` →
  `waitPullCleanup` の看取り順も成立している。🚨 ただし**機構は「defer LIFO」ではない** —
  `syscall.Exec` 経路は defer を 1 つも通らないので、`main.go` が `cancelAll` と
  `restartSelf` の間で**明示的に呼んでいる**（コメントも明記）。「defer と冗長だから外す」の
  誘い水にならないよう、機構を取り違えないこと
- **`gitlog_watch.go` の 2 チェーン状態機械**（`evArmed` / `pollArmed` / `measuring` / `reloading` /
  `gen` / `reloadSeq`）: 全 return 経路を追ったが札の取りこぼし無し。`handleGitLogReload` の
  `seq` 不一致経路は `applyLogData` が `reloadSeq` を進めると同時に `reloading` を降ろすため閉じている。
  `issues_watch.go:handleWatch` も同様
- **描画ホットパスの O(n) 再計算**: 結論は 0 件のままだが、🚨 **理由が誤っていた**。
  `tui.go:lines` は幅・色を鍵にしたメモ化ではなく `linesValid` の 1 フラグ（無効化点 14 箇所）で、
  `RenderLines` は窓ではなく**全コミット**を組む。しかも `m.fetching() || len(m.awaitCI) > 0`
  の間は 12.5fps で全行を組み直す。0 件と言えるのは、`tui.go` の 2026-07-25 perf 監査が
  これを**明示的に受容している**から（既定 7.8µs、`-p` 併用時のみ 332µs / 733KB）。
  status は実測で平坦（40→2000 行で 311→316 allocs）。
  **issues viewer は平坦ではなかった** → 268

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
- **271**: 🚨 **推奨対応が原理的に機能しなかった**。fsnotify は Add 済み dir に後から生えた
  エントリにもイベント経路で fd を開く（実測 +24 → +4024）ので、「起動時に `os.ReadDir` の
  件数で打ち切る」は 1 本も止めない。さらに `issues` 側は **watch 集合が指紋の入力を兼ねている**
  ため打ち切ると新規 issue が永久に無音になる（既存テストは緑のまま）。
  実効 fd 上限も `ulimit -n` ではなく `kern.maxfilesperproc = 245,760`（Go が soft→hard へ上げる）。
  「fetch 直後の窓」も誤りで、半分は reflog 由来で `pack-refs`/`gc` では減らない。
  副産物として **EMFILE の部分失敗が恒久で、冪等な再 Add が `err=nil` を返して回復しない**ことも判明
- **272**: 🚨 **症状の記述が両方向に誤っていた**。`tickMsg` の invalidate ゲートは
  `detailsLoading` を**名指しで除外**しているので「80ms ごとに全行を組み直す」は起きない。
  逆に、見落としていた**可視の症状**がある — `panelLines` が札で最初に分岐するので
  **開いているパネルが「取得中」で永久に固まり CI job が二度と出ない**。
  発火条件にも決定的な項が抜けていた（**`-n 401 以上` が要る。既定は 20**）ので優先度を 低 へ。
  推奨対応も別経路を壊す（実取得分岐の札まで落とし、同一 SHA への 2 本目の GraphQL を招く）。
  正しい不変条件は「キー集合」ではなく「**札には必ず live Cmd がある**」
- **273（索引そのもの）**: 🚨 **却下 1 件の機構が誤っていた**（→ 上の撤回と 275）。
  索引の誤りは「次の監査が飛ばす範囲」を作るので害が最大。加えて「攻めて見つからなかった範囲」の
  **結論は全部正しかったが理由が 3 件誤っていた**（defer LIFO / バッファ付きだから / 幅・色で
  メモ化）。**理由が誤った 0 件宣言は、結論が正しくても次の監査を誤誘導する**
- 「不在の主張」の探索範囲に**テストファイルを入れていなかった**のが 270 の一番の反省

### 5 トピック通しての一般化

- **OR の 1 項が偽であることを、式全体が偽であることの根拠にしない**（275 の直接の原因）
- **母集合の違う数を足さない**（全数勘定が閉じなかった原因）
- **「既存テストが緑」を修正の安全性の根拠にしない**（268 / 272 の両方で出た）
- **合成フィクスチャの数字を見出しに置くなら、実機の規模を隣に置く**（270）
- **推奨対応は「当てたら何が壊れるか」まで実験してから書く**（271 は案が原理的に無効、
  272 は別経路を壊し、269 は捕まえたい退行を通す設計だった）
  （「畳まれた行の detail を処理するのは意図的」はテストのコメントに書かれていた）

## 残課題

- [ ] 268（比較を O(1) 化）/ 270（detail・copyText・copyPath の遅延化）/ 274（早期 return）
- [ ] 269（比 metric の導入 + `check_bench_budgets.sh` の書式）
- [ ] 271（コメントの訂正が最小で最も価値がある。fd の bound は動的な会計が要る）/ 272
- [ ] 275（ratelimit のメモ化 + 比 metric。270 / 274 と同じ「全画面ビューアの毎フレーム
      再構築」ファミリーなのでまとめて設計する価値がある）
- [x] 敵対的レビュー 5 トピック（268+269 / 270 / 271 / 272 / 273）— 全部完了。訂正は各本文へ

## 記録として残らなかったもの

- 268 の「選択あり」のベンチ（2000 件 / 20 行選択で 431.7µs）は使い捨て worktree にしか無く、
  **今の repo からは再現できない**。268 の推奨対応は 269 の比 metric を前提にしているので
  実害は小さいが、記録としては穴（`move-report-conclusions-to-issues.md` の形）。
  269 / 275 のゲートを入れるときに、選択ありのケースも恒久ベンチにすること
