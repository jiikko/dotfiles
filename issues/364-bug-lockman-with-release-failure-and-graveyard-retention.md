# lockman の小粒な穴 4 件 (rc=0 での解放漏れ / graveyard の retention / reap 済み pgid への kill / 壁時計依存テスト)

起票日: 2026-09-12
カテゴリ: bug / priority: low〜medium
対象: `src/lockman/with.go` / `lock.go` / `cleanup.go` / `lock_test.go`
出典: [issue 358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) の敵対レビュー 5 周目
反証レビュー: 未実施。**出典は opus 3 体による実測**

単独では issue を立てるほどでない 4 件をまとめる。**それぞれ独立に直せる**。

## 1. `with` は「確実に解放」を謳うが、rc=0 のまま lock を握って終われる (medium)

`Release` は `serverNow()` を通るので probe を作れないと解放できない。
そのとき rc は**子のもの**が透過される。

| 腕 | rc | lock |
|---|---|---|
| 解放直前に `probe/` が消える | **0** (子は exit 0) | **残存** (`ttl_ms:1800000` = 30 分) |
| 対照 (何も消さない) | 0 | 解放された |

stderr に `解放に失敗:` は出るが rc は成功のまま。**rc だけを見る呼び出し側からは成功に見える**。
`_av1ify_lock.zsh:30` が運用として明記する `rm -rf ~/.lockman/av1c` が走行中に起きると
この状態になる。

## 2. `graveyard` の retention が「退避した時刻」でなく「lock が最後に打刻された時刻」から測られる (low)

`os.Rename` は mtime を保つので、`graveyardRetention = 7d` は
**最も記録する価値のある lock**（死んだマシンが放置したもの）にだけ成立しない。

| ケース | graveyard |
|---|---|
| 8 日前の lock を `break` | **空** (同じコマンドの defer `Cleanup` が即座に消した) |
| 8 日前の lock を `acquire` の引き継ぎで退避 | **空** |
| 対照: 今打刻された lock を `break` | 残った |

`lock.go:427` の「誰が握っていたかの記録も残す」という意図が、人が `break` を打つ場面
(= まさに「誰が握っていたのか」を知りたい場面) で無効になる。**正しさには影響しない**。

直すなら退避時に mtime を打ち直す (`Chtimes`) か、graveyard 側だけ別の時刻源を使う。

## 3. reap 済みの pgid へシグナルを撃つ窓 (low / **未確認リスク**)

`done <- cmd.Wait()` が返った時点で子は reap され、その pid (= `pgid`) は再利用可能になる。
`sigCh` と `done` が同時に ready なとき Go の `select` は一様ランダムに選ぶので、
**reap 済みの pgid に対して `syscall.Kill(-pgid, sig)`** が走りうる
(`case ticker.C` の `onLostKill` 経路も同じ)。

通常は ESRCH だが、`_ = syscall.Kill` が**すべてのエラーを捨てる** (EPERM も含むので、
転送の失敗自体が無音)。実害には窓内での PID 再利用が要り、**手元では再現できていない**。

再評価の trigger: `with` に実利用者が現れ、シグナル転送の取りこぼしが報告されたとき。

## 4. `TestRenewExtendsHold` が壁時計依存 (low)

`go test -race -count=20 -v` では **33 テスト × 20 = 660 PASS / FAIL 0 / DATA RACE 0** なので
**「flaky である」とは主張しない**。ただし失敗方向が危険で、余裕は 100ms しかない。

ttl=200ms に対し `Sleep(ttl/2)` + `Renew()` (readLock + serverNow + write + fsync ×2 +
serverNow + stat = FS 操作 6 回) を 3 周するので、1 周が 200ms を超えると赤くなる。

実測: `Renew` に 120ms (SMB では正常な範囲。30 分 TTL の production では無害な遅さ) を足すと

```
go build rc=0
--- FAIL: TestRenewExtendsHold (0.23s)
    lock_test.go:258: Renew: not the lock owner: lease が切れている
--- PASS: TestRenewRefusesExpiredLease / TestCleanupNeverRemovesLock
```

= このテストが測っているのは不変条件ではなく **I/O レイテンシ**
(`avoid-wall-clock-assertions.md`)。判定軸を「何が起きたか」へ移すか、
fake clock を注入する。

## 関連する観測 (追わないが記録する)

5 周目の観点②のレビュワーが full suite 約 50 回のうち **2 回、単発の原因不明 FAIL** を観測した
(1 件は `TestStaleTakeoverHasExactlyOneWinner`、もう 1 件は名前を採り損ねた)。
統制下の再現 (単独 `-count=30` / 4 並列 ×8 / 6 並列) では **0 件**。
上の 4 番 (レイテンシ依存) が出典の候補だが**未確認**。追わずに記録だけ残す。


## 進捗 (2026-09-19): 2 と 4 を実装した

**1 と 3 は未着手**。1 は exit code の契約変更なので独立して扱う (下の残タスク)、
3 は本文どおり未確認リスクのまま (trigger は本文に記載)。

| # | 状態 | 実装 |
|---|---|---|
| 1 | **完了** | 解放に失敗したとき、**子が rc=0 のときだけ** 125 へ上げる。非 0 の rc は既に失敗を伝えているので透過を保つ (真の穴は「子が成功して lock が残る」= 呼び出し側から成功と区別できない場合だけ)。番号は新設せず 125 (spec 091 の「lockman 自体のエラー」に当たる)。help の表にも明記 |
| 2 | **完了** | `stampGraveyard` を新設し、`tryTakeover` と `Break` の rename 直後に **退避した時刻 (`serverNow`) で打刻し直す**。best-effort (打刻できなければ従来どおり早く消えるだけ) で、退避自体は止めない。鳴らさない理由もコードに残した (退避は正常系にも出る操作で、ここで warn を足すと「良性の状態で鳴る診断」= issue 383 の 2 周目 P2 になる) |
| 3 | **未着手 (意図的)** | 再現手段が無い。推測で防御を足さない |
| 4 | **完了** | `TestRenewExtendsHold` の判定軸を「経過時間」から「引き継げるか」へ移した。mtime を期限の手前へ寄せてから Renew する形で、**壁時計の待ちはゼロ** (旧 0.23s → 0.04s)。対照 (Renew しなければ同じ経過で奪える) も付けた |

### 変異検証 (ケース名ごとの PASS/FAIL で判定)

| 変異 | 結果 |
|---|---|
| `tryTakeover` の打刻を外す | `TestGraveyardRetentionIsMeasuredFromEviction/takeover` **FAIL** / `/break` は PASS |
| `Break` の打刻を外す | 同 `/break` **FAIL** / `/takeover` は PASS |
| `Renew` に「内容が同じなら書き直さない」最適化を入れる | `TestRenewExtendsHold/renewed` **FAIL** / `/not_renewed` は PASS |

🚨 **この「対照を置いた」は誤りだった** (2026-09-19 の反証レビュー P2 で判明)。
`TestGraveyardStillExpiresAfterRetention` は打刻の**後に自分で mtime を上書きしてから**
Cleanup を回すので、`stampGraveyard` が打った値を一度も観測しない。実測: 打刻を
`now.Add(100*24*time.Hour)` (= 100 日消えない) へ変異させても **full suite が全緑**だった。
つまり当時のテスト対は「打刻しなさすぎ」しか検出せず、**「打刻しすぎ」は 1 本も検出していなかった**
(打刻しすぎは graveyard が retention を無視して無限に育つ形)。
→ `TestGraveyardStampIsTheEvictionTime` を新設し、打刻値そのものを**上下両方向**で pin した。

### 結果

- `go test -race ./...` 緑 / 対象 2 本は `-count=3` でも緑 / golangci-lint 0 issues
- 新規テスト: `src/lockman/graveyard_retention_test.go` (2 本 + ヘルパー)
- 🚨 **fixture の古さは retention (7d) + 24h に置いた**。足りないと「打ち直さなくても残る」ので
  変異を当てても緑で通る (テスト内にコメントで残した)

## 反証レビュー (2026-09-19) — P1 1 件 / P2 1 件を実装で解消

| 指摘 | 判定 | 対応 |
|---|---|---|
| **P1**: 項目 2 の修正が**鈍いマウントでは効かず、退避の記録が黙って消える** | 実測で裏取り | 直した (下記) |
| **P2**: 「対照を置いた」は偽。打刻しすぎを 1 本も検出していない | 実測で裏取り | 対照を新設 |
| P3: 解放時に lease が期限切れだと rc=0・lock 残存・stderr 無音 | 根拠を実測で確認 | **現状維持** (期限切れの lock は次の acquire が即座に引き継げるので待たされる害が出ない。コメントに事実を明記済み) |
| 項目 1 / 項目 4 | 壊せなかった | — |

### P1 の中身 (項目 2 の修正が効かない母集団)

`stampGraveyard` の I/O (`serverNow` = probe の作成 + stat + 削除) が **不可逆な `os.Rename` の
後ろ**にあり、`BreakTimed` の `--io-timeout` 予算に丸ごと乗っていた。鈍いマウントではそこで
食い切って打刻が着地せず、`dispatch` が break のあとに必ず走らせる `Cleanup` が
**旧 mtime (8 日前) のまま retention 超過と判定して記録をその場で消す**。しかも人には
「判定不能」という別の話が出るので、記録を失ったことが見えない。

実測 (A-B、8 日前の lock を `break`):

| マウント | `BreakTimed` | graveyard |
|---|---|---|
| 速い (`--io-timeout 5s`) | nil | **1 件** |
| 鈍い (`--io-timeout 30ms`) | 判定不能 | **0 件** |

**「死んだマシンが放置した lock を break する」は項目 2 が存在する理由そのものの母集団**なので、
そこでだけ効かないのは実害。→ `Break` も `tryTakeover` と同じく `serverNow` を **rename の前**に
取る形へ変更。rename 後に残る I/O は `os.Chtimes` 1 本になり、窓は**縮む** (0 にはならない)。

🚨 **残る限界**: probe dir が壊れている lock では `serverNow` が即座に失敗し、best-effort で
打刻を諦める → 旧 mtime のまま Cleanup に消える。これは今回の変更でも直らない
(直すなら退避名に時刻を埋める等の別設計が要る)。再評価の trigger: 実運用で
「break したのに graveyard に記録が無い」が報告されたとき。

### 追加した変異検証 (ケース名ごとの PASS/FAIL)

| 変異 | red になったテスト |
|---|---|
| `Break` の時刻取得を rename の後ろへ戻す | `TestBreakTakesTheStampTimeBeforeRenaming` |
| 打刻しすぎ (100 日先) | `TestGraveyardStampIsTheEvictionTime` の 2 ケース |
| 打刻しない | 上記 + `TestGraveyardRetentionIsMeasuredFromEviction` の 2 ケース |

🚨 順序の検査は**壁時計で再現しない** (`avoid-wall-clock-assertions.md`)。rename 直後の seam
(`breakAfterRenameHook`) で「その時点で打刻用の時刻を持っているか」を観測する形にした。

🚨 変異ハーネス自身の欠陥も 1 件踏んだ: 判定に `grep -E '^\s+--- FAIL'` を使っていたため、
**サブテストでない `--- FAIL:` (インデントなし) を取りこぼして M1 を「全緑」と誤読**した。
判定式は `^ *--- FAIL` に直した。

## 派生 issue

- [issue 400](400-chore-lockman-slow-mount-derivative-cases.md): テストが「速いローカル FS」を
  暗黙の前提にしている。今回の P1 は **既存テストでは構造的に観測できず**、レビュワーが
  遅延を注入して初めて見つかった。`--io-timeout` を縮めた派生ケースを足せば、このクラスを
  CI で捕まえられる (順序の pin は 364 で入れたが、振る舞いそのものは未検査)

## 残タスク

- [x] 反証レビュー (P1 / P2 を実装で解消。P3 は現状維持の理由をコードに明記)
- [x] **1 の対応**。呼び出し側は全数確認済み: `lockman with` を実行する production コードは 0 件
      (issue 359 の全数勘定)、`__av1ify_lock_still_held` は rc≠0 をすべて「保持していない」に倒す
      fail-closed なので厳しくしても安全側にしか動かない
- [x] 2 の対応 (`stampGraveyard`。変異 2 本で red)
- [ ] 3 は未確認リスクのまま。trigger は本文に記載
- [x] 4 の判定軸の変更 (`TestRenewExtendsHold`。変異 1 本で red)
