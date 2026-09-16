# `lockman with` の renew ラッチが「詰まったら二度と更新しない」— 自分が禁じた失敗を別の形で作り直している

起票日: 2026-09-16
カテゴリ: bug / priority: **high**
対象: `src/lockman/with.go` の select ループ (`renewCh` / `renewExpired`) と `on_lost_kill_test.go`
出典: [issue 366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) の敵対的レビュー (観点③ 並行・中断)
反証レビュー: 未実施

## 問題

`with.go` は更新中の goroutine が溜まらないよう `renewCh != nil` のあいだ tick を捨てる:

```go
case <-ticker.C:
    if renewCh != nil {
        continue // 前回の更新がまだ返っていない。新しく積まない
    }
    renewCh, renewExpired = l.renewAsync(meta.Token)
```

`renewCh` が nil に戻るのは **`case err := <-renewCh:` (詰まった `Renew` が返った) ときだけ**。
`renewExpired` は一度発火すると nil にされ、二度と鳴らない。したがって
**`Renew` の syscall が返らない限り、以後の tick はすべて `continue` で更新は永久に起きない**。

これは同じファイルの 🚨 が禁じている失敗そのもの:

> 🚨 **詰まっても更新をやめない。** 以前ここで `ticker.Stop()` していたが、それは
> 一過性のヒカップを**本物の lease 喪失**に変える: 更新が二度と走らないので lease は
> 実際に期限切れになり、他マシンが正当に引き継ぐ — 子はまだ走っているので二重実行。

`ticker.Stop()` を外して塞いだつもりの穴が、`renewCh` ラッチという別の形で残っている。
`case <-renewExpired:` のコメントは「詰まった 1 本が返れば renewCh が降りて再開する」と
書いており、**返らない場合を想定していない**。

## 二重実行になる列 (`--on-lost=warn`)

1. `t0`: `lockman with --on-lost=warn --ttl 30m -- job`。Acquire 成功 (token T1)。tick = 10m
2. `t0+10m`: tick → `renewAsync` の goroutine が `Renew` の中でブロック (マウント停止)
3. `t0+10m+io-timeout`: `renewExpired` 発火 → warn 1 回。`onLostKill == false` なので子は殺さない。
   `renewCh` は非 nil のまま
4. `t0+20m, 30m, …`: すべての tick が `continue`。**更新 0 回**
5. `t0+30m`: lock の mtime は `t0` のままなので lease が期限切れ
6. 別ホストが引き継ぐ。**子はまだ走っている = 2 人が同時に保持**

既定の `--on-lost=kill` では `escalateGroupKill` が子を落とすので、露出は `setsid()` した
子孫まで縮む (`with.go` が既知として記載済み)。**`--on-lost=warn` が本命**。

## テストが作っておきながら検査していない

- `on_lost_kill_test.go` の `TestRenewDoesNotPileUpGoroutinesWhenBlocked` は
  **恒久ラッチ状態を作っている** (`syscall.Mkfifo` で作った FIFO を lock に被せ、
  書き込み側を誰も開かないので `Renew` の read は返らない)。ところが assert は
  **exit code と `runtime.NumGoroutine()` の差だけ**で、**lease が生きているかを一度も見ていない**
  (実測 2026-09-16: 当該関数内の assert は 5 つで、`Inspect` / `readLock` / `Acquire` の
  呼び出しは 0 件)
- `TestLeaseSurvivesTransientRenewBlock` は**一過性**だけ。手順の途中で FIFO を退けて
  詰まった読み手を解放するので、`renewCh` が必ず返る条件でしか回していない

つまり「詰まっても更新をやめない」は *一過性* についてのみ守られており、*恒久* については
**テストが状態を作っておきながら検査していない**。

## 未確認

- 手順 2 の syscall が返らない (hard mount / stale handle) ことは実機で確認していない。
  マウントが完全に死んでいるあいだは**どの実装でも更新は成功しえない**ので、修正が効くのは
  「掴んだ fd は死んだまま、新しい open なら通る」形 (stale handle / 掴んだまま入れ替わった
  パス) に限る。テストはこの形を FIFO で再現している
- **上限 (`maxInFlightRenews` = 8) に達したあとは、修正前と同じ「更新が走らない」状態に戻る**。
  `--on-lost=kill` (既定) なら初回の期限切れで子を止めにいっているので露出は縮むが、
  `--on-lost=warn` では二重実行の可能性が残る。honest な上限として warn を 1 回出している
  (`with.go` の `cappedWarned`)。上限を上げると issue 380 の窓が比例して広がる

## 進捗 (2026-09-16)

### A-B 実測 (残タスク ③ = `--on-lost=warn` の二重実行)

新テスト `TestLeaseSurvivesPermanentRenewBlock` が A-B そのもの。手順は
`TestLeaseSurvivesTransientRenewBlock` と 1 手だけ違う: **詰まった読み手を解放しない**
(パスは実ファイルへ戻すので、新しい `Renew` なら成功する)。

| 版 | 結果 |
|---|---|
| 修正前 (`renewCh` を握ったまま) | **3 回とも FAIL** — `other.Acquire` が `err=<nil>` で成功 = 他マシンが引き継いだ。`with` の exit=125 で子はまだ走っていた (= 二重実行) |
| 修正後 (期限で見捨てて次の tick で張り直す) | 3 回とも PASS (`errBusy` = 奪えない) |

条件: ttl 1200ms (tick 400ms) / io-timeout 200ms / 詰まりを 800ms 保ってからパスだけ復旧 /
復旧の 1600ms 後に別 Locker が `Acquire`。darwin arm64 / go1.25.4。

### 変異検証 (ケース名ごとの pass/fail で判定)

| 変異 | red になったケース | 緑のままのケース |
|---|---|---|
| M1: 期限切れで見捨てるのをやめる (`renewCh` を握る = 修正前) | `TestLeaseSurvivesPermanentRenewBlock` | `TestRenewDoesNotPileUpGoroutinesWhenBlocked` / `TestLeaseSurvivesTransientRenewBlock` |
| M2: 上限 (`maxInFlightRenews`) の判定を外す | `TestRenewDoesNotPileUpGoroutinesWhenBlocked` (10 本 vs 上限 2 本) | 他 2 本 |

M1 で transient 版が緑のままなのが、この issue の主張 (「一過性については守られていたが
恒久については守られていなかった」) の裏付け。どちらの変異もビルドが通ることと、
diff が意図した行だけであることを確認してから read/green を読んだ。

### 残タスク ① は誤りだった (書き戻し)

> - [ ] `TestRenewDoesNotPileUpGoroutinesWhenBlocked` に「lease が生きているか」の assert を足す
>       (今の状態で red になるはず。ならないなら前提が作れていない)

**成立しない。** あのテストは FIFO を最後まで退けないので、**どの実装でも `Renew` は 1 度も
成功しえず**、lease は修正版でも壊れた版でも死ぬ。あそこに lease の assert を置くと
「常に red」= 何も測らない assert になる。恒久ラッチの検査は、パスだけ復旧させて古い読み手を
返らないまま残す**別のテスト** (`TestLeaseSurvivesPermanentRenewBlock`) が持つべきもので、
`TestRenewDoesNotPileUpGoroutinesWhenBlocked` が守るのは**本数の上限**だけ。
この区別はテスト本体のコメントにも書いた。

### 決めたこと: `outcome` は sticky のまま (125 を 122 に上書きしない)

修正で「遅れて張り直した `Renew` が `errNotOwner` を返す」経路が**初めて到達可能**になった。
`outcome` は `if outcome == renewOK` で守られているので、判定不能 (125) のあとに確実な喪失
(122) を知っても exit は 125 のまま。091:398-399 はこの 2 つを分けているが、**判定不能だった
窓があった事実は後から消えない**ので 125 のままにする (終了コードは呼び出し側の API なので、
変えるなら契約変更として別 issue で扱う)。副作用ではなく意図的な据え置き。

### 敵対的レビュー (2026-09-16)

観点を分けて直列で実施。指摘はすべて実コードで裏取りしてから採否を決めた。

**観点① 壊す** — P1 が 1 件。「中身を読めない lock は実効 TTL が 30m へ縮む」fail-open で、
`--ttl 2h` の生きた lease が正規手順で奪える。本修正が原因ではない (`renew` サブコマンド経由でも
到達しうる) が、`Renew` が `O_TRUNC` まで届く機会が最大 8 倍になったので露出は広がった。
機構は実コードで確認し、[383](383-bug-lockman-unreadable-lock-collapses-ttl-to-default.md) に
起票した (再現の実行は未追試)。あわせて `--ttl 30s --io-timeout 5m` のように
**io-timeout > tick** にできる (`main.go` は両者の関係を検証していない) 場合、本修正は
何の保護も与えないことを確認した (修正前と同じ挙動に戻るだけで悪化はしない)。

**観点② 素通り (false green)** — P1 が 1 件、P2 が 3 件。12 本の変異のうち **10 本が
M1/M2 を素通り**した。最大のものは `defer inFlight.Add(-1)` の削除で、**詰まりが 1 度も
無い健全なマウントでも上限 tick 目に更新が止まる** (既定値なら約 80 分で lease 喪失 = 381 の
バグより悪い) のに全緑だった。自分でも追試して rc=0 を確認し、テスト 2 本を足して塞いだ
(上記 commit)。既存 2 本の assert も `errBusy` だけを根拠にしない形へ強くした。

#### 塞がずに残した (理由つき)

- **`warnf` の文言が無検査**: 上限到達の報告を消しても、文言を sticky な outcome 基準へ
  戻しても全緑。`warnf` に差し替え可能な出力先が無いので**構造上テストできない**。
  実行の証拠は手で確認した (`go test -v` の出力に上限報告が出る) が、退行のガードは無い。
  **trigger**: 文言・報告の有無が運用判断に使われるようになったら
  `var warnOut io.Writer = os.Stderr` の seam を入れる
- **`escalate.Do` の「1 回だけ」ガードが無検査**: 外しても全緑。本修正で `reportRenewErr` が
  毎 tick 鳴るのが常態になり、このガードにかかる負荷だけが増えた (ガード自体は本修正の一部
  ではない)。`TestOnLostKillEscalatesToSigkill` は反復報告を起こしているが終了コードしか見ない
- **上限の off-by-one が無検査**: `n >= maxInFlight` を `n > maxInFlight` にしても、
  pile-up の閾値 (4 本) の内側なので緑。境界そのものを固定する assert は無い
- **`inFlight.Add(1)` を `go` の前で行う理由が無検査**: goroutine 内へ動かしても緑。
  コメントが主張している「起動までの窓」を観測するテストは無い
- **`runtime.NumGoroutine()` の閾値の余裕が 1 本しかない**: 原本 2〜3 本に対して閾値 4 本。
  goroutine を持つテストを 1 本足すと偽陽性になりうる

## 残タスク

- [x] 恒久ラッチを解く設計を決めて実装する — 期限切れで参照を捨て、次の tick で新しい更新を
      積む。溜まる本数は `maxInFlightRenews` (既定 8、goroutine 自身が減らすので復旧すれば枠が戻る)
      で抑える。[359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) 項目 4 の
      「上限を置くか決めろ」への回答でもある
- [x] `--on-lost=warn` での二重実行を A-B で実測する (上表)
- [x] ~~`TestRenewDoesNotPileUpGoroutinesWhenBlocked` に lease の assert を足す~~ → 誤り。上記
- [ ] **スコープ外**: [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) の窓が
      本修正で 1 本 → 最大 8 本に広がった (380 側にも追記済み)。380 の優先度判断に影響する
- [ ] **未検証**: 上限到達の報告と `reportRenewErr` の文言は、`warnf` に seam が無いため
      **構造上テストできていない** (上の「塞がずに残した」を参照)。実行の証拠は手で確認済み
- [ ] **スコープ外**: [383](383-bug-lockman-unreadable-lock-collapses-ttl-to-default.md) —
      中身を読めない lock の実効 TTL が 30m へ縮む fail-open。本修正で露出が広がった

## 関連

- [362](362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) — 見捨てられた goroutine が
  副作用を残す族。本 issue は「見捨てた**後**に更新を再開しない」側
- [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) — `Renew` 自体の TOCTOU
