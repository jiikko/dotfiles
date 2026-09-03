# 217 bug: `pullCleanup` が `sync.WaitGroup` のまま残っている (doctor 側だけ latch 化した)

起票日: 2026-09-03
出典: issue 214 の敵対的レビュー (opus, red team)
重要度: P3 (**発火経路は未確認**。構造の取り残しと、嘘になったコメント)
関連: `src/glogx/external_commands.go` の `pullCleanup` / `waitPullCleanup`、
      `src/glogx/action_modal.go` の `pullCleanup.Add`、`src/glogx/doctor_cleanup.go` の `scanLatch`

## 症状 (取り残し)

`doctor_cleanup.go` は「**`sync.WaitGroup` は使えない**。Add が Wait と同時に走りうる。
`-race` が実際にデータ競合として検出した」と書いて `scanLatch` を新設したのに、
**同じ形の兄弟である `pullCleanup` は `sync.WaitGroup` のまま**:

- Add: `src/glogx/action_modal.go` の tea.Cmd closure 側 (`pullCleanup.Add(1)`)
- Wait: `src/glogx/external_commands.go` の `waitPullCleanup` (main.go から)

したがって `doctor_cleanup.go` の「形は pullCleanup (external_commands.go) と同じ」は
**もう嘘**である (片方だけ直った)。

## 何が起こりうるか (実測、ただし本 repo では未再現)

使い捨て module で同じ形 (カウント 0 での Add と Wait の同時実行) を再現すると:

- `-race` あり → `DATA RACE` + `panic: sync: WaitGroup misuse: Add called concurrently with Wait`
- panic 側は `sync/waitgroup.go` で **`race.Enabled` に囲まれていない** → **production でも
  落ちうる** (TUI が quit 時に panic)

⚠️ **本 repo では再現できていない**。発火には「waiter が居る状態でカウンタが 0→1」が要るので
Add 地点が 2 つ以上必要で、`pullCleanup` の Add は 1 箇所しかなく、pull は `a.pulling` で
二重起動が塞がれている。`doctorCleanup` が実際に赤くなったのは Add が 3 箇所 (svc / brew / 削除)
あったからで、整合は取れている。

## 直し方

`pullCleanup` を `scanLatch` に載せ替える (道具は既にある)。載せ替えないなら、
`doctor_cleanup.go` の「形は pullCleanup と同じ」を訂正し、
**pullCleanup 側に「Add 地点を 2 つ以上に増やすなら latch へ移すこと」**を制約として書く
(増やした瞬間に quit 時 panic の窓が開く。実装では強制できない)。

## 関連の記録: `collectVisits` の assert 側に残った暗黙の前提 (未着手)

`src/doctor/disk/scan_test.go` の巡回検出テストは `collectVisits.Store(0)` →
`Load() > wantMax` で **グローバル計測点の絶対値**を見ている。issue 214 で「並行走査は正常」と
宣言した package で、この assert だけが暗黙に「同時に走査が無いこと」を前提にしている。

- 現状は安全: 同 package に `t.Parallel()` が 0 件、並行テストは `wg.Wait()` で join 済み。
  `-shuffle=on` × 3 回・`-count=2` でも緑 (敵対レビューが実測)
- 将来 `t.Parallel()` を 1 つ足した瞬間に flaky になる (幅が 9 しかないので当たりやすい)
- 直すなら差分 (`Load() - before`) で見るか、`collectBundleIDsSeen` に計測用の out パラメータを渡す

## レビュー状態

WaitGroup が残っている事実・Add / Wait の位置・コメントが嘘になっている事実は main agent が
コードで裏取り済み。**発火経路は未確認**。反証レビューは未実施。
