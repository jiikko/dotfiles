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
| 1 | **未着手** | — |
| 2 | **完了** | `stampGraveyard` を新設し、`tryTakeover` と `Break` の rename 直後に **退避した時刻 (`serverNow`) で打刻し直す**。best-effort (打刻できなければ従来どおり早く消えるだけ) で、退避自体は止めない。鳴らさない理由もコードに残した (退避は正常系にも出る操作で、ここで warn を足すと「良性の状態で鳴る診断」= issue 383 の 2 周目 P2 になる) |
| 3 | **未着手 (意図的)** | 再現手段が無い。推測で防御を足さない |
| 4 | **完了** | `TestRenewExtendsHold` の判定軸を「経過時間」から「引き継げるか」へ移した。mtime を期限の手前へ寄せてから Renew する形で、**壁時計の待ちはゼロ** (旧 0.23s → 0.04s)。対照 (Renew しなければ同じ経過で奪える) も付けた |

### 変異検証 (ケース名ごとの PASS/FAIL で判定)

| 変異 | 結果 |
|---|---|
| `tryTakeover` の打刻を外す | `TestGraveyardRetentionIsMeasuredFromEviction/takeover` **FAIL** / `/break` は PASS |
| `Break` の打刻を外す | 同 `/break` **FAIL** / `/takeover` は PASS |
| `Renew` に「内容が同じなら書き直さない」最適化を入れる | `TestRenewExtendsHold/renewed` **FAIL** / `/not_renewed` は PASS |

対照も置いた: `TestGraveyardStillExpiresAfterRetention` (打ち直しが「常に残す」へ倒れていないこと。
倒れると graveyard が無限に育つ)。

### 結果

- `go test -race ./...` 緑 / 対象 2 本は `-count=3` でも緑 / golangci-lint 0 issues
- 新規テスト: `src/lockman/graveyard_retention_test.go` (2 本 + ヘルパー)
- 🚨 **fixture の古さは retention (7d) + 24h に置いた**。足りないと「打ち直さなくても残る」ので
  変異を当てても緑で通る (テスト内にコメントで残した)

## 残タスク

- [ ] 反証レビュー (2 / 4 の実装は未レビュー。変異検証のみ)
- [ ] **1 の対応 (未着手)**。`with` が解放に失敗しても子の rc を透過する。exit code の契約変更に
      なるので、呼び出し側 (`_av1ify_lock.zsh` ほか) の期待を洗ってから決める
- [x] 2 の対応 (`stampGraveyard`。変異 2 本で red)
- [ ] 3 は未確認リスクのまま。trigger は本文に記載
- [x] 4 の判定軸の変更 (`TestRenewExtendsHold`。変異 1 本で red)
