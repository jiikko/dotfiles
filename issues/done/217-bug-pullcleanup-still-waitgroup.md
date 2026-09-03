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

🚨 **本 repo では再現できていない**。発火には「waiter が居る状態でカウンタが 0→1」が要るので
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

## 対応 (2026-09-03。`26a7a174`)

**載せ替える方を採った。** 制約をコメントで書くだけ (もう一方の案) は「今は当たらない」に
依存し続ける形で、登録地点が 1 つ増えた瞬間に窓が開く。WaitGroup の panic は
`race.Enabled` に囲まれていないので production でも落ちうる。

### 1. latch を共有の道具として切り出した

`scanLatch` は doctor 専用の名前と置き場所だったが、実体は「終了前に看取るべき仕事を数える」
汎用の道具。`src/glogx/cleanup_latch.go` へ切り出し、**`cleanupLatch` へ改名**した
(pull でも使うので "scan" は嘘になる)。

- `external_commands.go`: `var pullCleanup cleanupLatch` / `waitPullCleanup` は `wait()` を受ける
- `action_modal.go`: `Add(1)` / `Done()` → `add()` / `done()`
- **インスタンスは別のまま**。待ちの上限も出すメッセージも別で、片方の遅れをもう片方の
  理由として表示すると診断を誤らせる (元のコメントの判断をそのまま維持)
- `doctor_cleanup.go` の「形は pullCleanup と同じ」を**訂正**し、同じ型を使うことを明記した

### 2. 中心の不変条件を直接検査するテストを新設

「**カウント 0 での登録が、看取りと同時に走っても壊れない**」が `sync.WaitGroup` を
使えない理由そのものなのに、既存のテストは `doctorTrack` 経由で「走査が看取られるか」を
見ており、**この最も危ない瞬間を作っていなかった**。217 が取り残しとして残った遠因はここ。

`src/glogx/cleanup_latch_test.go` で 200 回交差させる。pull / doctor の両方が同じ型に
載っていることも名指しで固定した (型が WaitGroup へ戻るとコンパイルできない)。

### 3. `collectVisits` の暗黙の前提も消した (本文後半の記録分)

`disk/guard.go` の package 変数 `collectVisits` (`atomic.Int64`) を**消し**、再帰へ
`*int64` を通す形にした (`collectBundleIDsCounted` を追加)。テストが `Store(0)` してから
絶対値を見る形は「同 package に並行して走る走査が無い」ことを暗黙に前提にしており、
`t.Parallel()` を 1 つ足した瞬間に flaky になる (幅が 9 しかない)。

`TestConcurrentScansDoNotRaceOnCollectVisits` は**消さずに主張を移した**
([`list-masked-failure-modes-before-removing-guard.md`](../../_claude/rules/list-masked-failure-modes-before-removing-guard.md))。
マスクしていたもの:

| マスクしていた失敗モード | 外した後 |
|---|---|
| 計測点そのもののデータ競合 | **構造的に解消** (共有変数が存在しない) |
| 走査の途中に共有状態を再導入する変更 | **まだ起こりうる**。作り直したテストが受け持つ |
| 並行に回したとき各呼び出しが自分の結果を得ること | 同上。訪問数と ids を全呼び出しぶん突き合わせる |

3 つ目を assert しないと「落ちなかった」だけの test になり、共有 ids へ書くような変更を通す。

### 変異

| 変異 | 結果 |
|---|---|
| pull 側を `sync.WaitGroup` に戻す | **コンパイル不可**。⚠️ ビルド不能は red でも green でもない第 3 の結果なので出所を確認した: production 単体は `go build ./` が rc=0 で、失敗は `cleanup_latch_test.go:73` の型不一致 (= 私のテストが型で捕まえている) |
| latch の `done` で `waitC` を閉じない | red 2 件 |
| `wait` を常に閉じ済みで返す | red 1 件 |
| 巡回検出 (`seen` の重複判定) を外す | red |
| 数え上げを package 変数へ戻す | red (**作り直した並行テストが「訪問数が違う」で捕まえた**) |

`make test` rc=0。`src/doctor` に入った `.golangci.yml` の gofmt ゲートも通してある。

## レビュー状態

WaitGroup が残っている事実・Add / Wait の位置・コメントが嘘になっている事実は main agent が
コードで裏取り済み。**pull 側の発火経路は最後まで未確認**(登録が 1 か所しかないため、
本 repo では原理的に再現できない)。それを承知で構造を閉じた。

⚠️ **敵対的レビューは未実施**。この変更は「安全機構を自分で作り替えた」形なので
[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md)
の発動条件を満たす。変異 5 本は通したが、あちらが言うとおり**変異は自分が想定した
不変条件しか試さない**。並行・終了・外部プロセスが絡むので、レビューを 1 本通す価値はある。
