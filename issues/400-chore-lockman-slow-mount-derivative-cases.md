# lockman のテストが「速いローカル FS」を暗黙の前提にしている (鈍いマウントの派生ケースが無い)

> 🚨 **担当中: Claude セッション (opus, dotfiles)**（2026-09-20〜）

起票日: 2026-09-19
カテゴリ: chore / priority: **medium**
対象: `src/lockman/graveyard_retention_test.go` / `lock_test.go` (と、同種の前提を持つ他のテスト)
出典: [issue 364](364-bug-lockman-with-release-failure-and-graveyard-retention.md) の反証レビュー (2026-09-19) のぼやき
反証レビュー: 未実施

## 問題

364 の P1 は「**不可逆な操作 (`os.Rename`) のあとに残した I/O が `--io-timeout` を食い切ると、
その I/O が前提の不変条件が黙って壊れる**」という形だった。実測 (A-B、8 日前の lock を `break`):

| マウント | `BreakTimed` | graveyard |
|---|---|---|
| 速い (`--io-timeout 5s`) | nil | **1 件** |
| 鈍い (30ms) | 判定不能 | **0 件** (退避の記録が消える) |

この退行は**既存テストでは構造的に観測できなかった**。`newTestLocker` は
`NewLocker(t.TempDir(), 5*time.Second)` で、**速いローカル APFS + 余裕のある timeout** しか
作らないため。レビュワーが `stampGraveyard` の冒頭に遅延を注入して初めて見つかった。

364 では**順序そのもの**を seam (`breakAfterRenameHook`) で pin して閉じた。これは
壁時計に依存しない良い形だが、守っているのは「時刻の取得が rename より前に在ること」までで、
**「鈍いマウントでも記録が残る」という振る舞いそのもの**は CI で検査されていない。

## やること

`--io-timeout` を縮めた派生ケースを、既存のテーブルに 1 行足す。

- 対象の第一候補: `TestGraveyardRetentionIsMeasuredFromEviction` (break / takeover の 2 ケースを
  持つテーブルが既に在るので、`slow_mount` の行を足すのが素直)
- 同じ前提を持つ他のテストにも横展開できるか見る
  (`TestBreakTakesTheStampTimeBeforeRenaming` / `TestRenewExtendsHold` / `lock_test.go` の
  引き継ぎ系。**全部に足すのではなく、「不可逆操作のあとに I/O が残る」ものだけ**)

### 🚨 壁時計で作らないこと

`sleep` で遅さを模すと [`avoid-wall-clock-assertions.md`](../_claude/rules/avoid-wall-clock-assertions.md)
に正面から反する (速いマシンでは緑のまま通り、負荷が上がった日にだけ落ちる)。
窓は**決定論で**作る:

- 既存の seam の流儀に倣う (`cleanup.go` の `sweepPauseHook` は `atomic.Pointer[func()]`、
  `lock.go` の `readLockAfterStatHook` / `tryPlaceBeforeIdentityHook`、
  `with.go` の `escalateBeforeKillHook`)
- seam を「テストが解放するまでブロックする」形にすれば、`--io-timeout` を確実に食い切れる。
  `time.Sleep` で「たぶん超えるはず」を作らない
- ただし `--io-timeout` の発火自体は時間で起きるので、**timeout 値はテスト側で十分小さく固定**し、
  「超えたこと」ではなく「**何が起きたか**」(記録が残っているか / rc / stderr) で判定する

### 🚨 このケースが何を守るかを先に書くこと

足す前に「この行が red になる退行は何か」を 1 行で書き、**実際にその退行を当てて red を見る**。
364 の反証レビューは「対照を置いた」という記述が偽だった例を出している
(打刻の後に自分で mtime を上書きするので、打刻値を一度も観測していなかった)。

## 受け入れ条件

- [ ] `--io-timeout` を縮めた派生ケースが、少なくとも graveyard の退避経路に 1 本在る
- [ ] その窓が**決定論**で作られている (`sleep` で「たぶん超える」を作っていない)
- [ ] 「時刻の取得を rename の後ろへ戻す」変異で、**その派生ケースが** red になる
      (既存の構造 pin が先に落ちるだけ、になっていないこと。落ちるなら変異を分ける)
- [ ] 足したケースが CI で実際に走っていることをログで確認する (skip に化けていない)

## スコープ外 / 既知の限界

- **probe dir が壊れている lock** では `serverNow` が即座に失敗して打刻を諦めるので、
  鈍いマウントでなくても記録は旧 mtime のまま消える。これは 364 の修正でも直っておらず、
  直すなら別設計 (退避名に時刻を埋める等) が要る。本 issue の対象は**テストの射程**であって
  この限界の解消ではない
- 窓を 0 にすることは目標にしない。364 の修正で rename 後に残る I/O は `os.Chtimes` 1 本まで
  縮んでおり、**残った窓が実害になるか**は未計測
