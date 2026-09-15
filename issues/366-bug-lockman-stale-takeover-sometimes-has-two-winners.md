# `TestStaleTakeoverHasExactlyOneWinner` が低頻度で「勝者 2 人」になる (原因未特定)

起票日: 2026-09-12
カテゴリ: bug / priority: **high**（主張の重さによる。実害の有無は未確定）
対象: `src/lockman/lock.go` の `tryTakeover` / `src/lockman/lock_test.go:88` の同テスト
出典: [issue 358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) の敵対レビュー 5 周目 (観点②) と、7 周目の作業中に再観測
反証レビュー: 敵対的レビュー実施 (2026-09-15。下の「敵対的レビュー」節)

## 問題

**「期限切れの引き継ぎを同時に狙っても、引き取れるのは 1 人だけ」**という
lockman の中核の不変条件を測るテストが、低頻度で `引き継ぎの勝者が 2 人 (期待 1)` で落ちる。
テスト自身のコメントが「ここが二重取得の最頻出経路」と書いている面。

**これがテストのハーネス由来なのか production の race なのかは未特定**。
主張の重さから priority: high としているが、**実害が確定したわけではない**。

## 実測 (2026-09-12 / darwin arm64 / go1.25.4)

単独実行 (`-run 'TestStaleTakeoverHasExactlyOneWinner$'`):

| 条件 | 失敗 |
|---|---|
| HEAD (`2c474a4d`)、`-count=100`、**他のエージェントが並行実行中** | **5/100** |
| HEAD、`-count=300`、静穏 | **0/300** |
| 5 周目より前 (`7ca2533b`)、`-count=100`、静穏 | 0/100 |
| 5 周目より前、`-count=300`、静穏 | **1/300** |
| HEAD、**`-race`** `-count=100` | 0/100 |
| full suite `-count=1` ×8 (静穏) | 0/8 |

観測された値はすべて「勝者が 2 人」(3 人以上は出ていない)。

**独立の観測**: issue 358 の 5 周目 (観点②) のレビュワーが、full suite 約 50 回のうち
2 回の原因不明 FAIL を観測し、うち 1 件がこのテストだった (並列実行中)。

## 分かっていること

- **issue 358 の掃除機構の変更が原因ではない**。`Acquire` は `Cleanup` を呼ばない
  (`grep -n 'Cleanup(' lock.go` が 0 件) ので、このテスト中に 358 で触ったコードは走らない。
  **両腕 (HEAD / 5 周目より前) の双方で再現した**
- **負荷に感応する**。並行して他の重いプロセスが走っているときに率が上がる
  (5/100 が出たのはその条件。静穏では 400 回中 1 回)
- **`-race` では出ない** (400 回相当で 0)。検出器が遅くする方向に効いて窓が閉じる

## 未特定 (次に見るべきところ)

- fixture の `ttl := 50 * time.Millisecond` + `time.Sleep(3 * ttl)` が、
  `serverNow()` の mtime 粒度 (サーバ側の打刻) に対して十分かどうか。
  粒度が 1 秒の FS では「期限切れ」の判定自体が揺れる
- `tryTakeover` の rename 引き継ぎで、2 つの goroutine が**別々の**期限切れ lock を
  見て両方成功する経路があるか (1 人目が引き継いだ直後の新 lock を、2 人目が
  まだ古い state で見ている窓)
- ハーネス由来なら、判定軸を壁時計から外す
  ([`avoid-wall-clock-assertions.md`](../_claude/rules/avoid-wall-clock-assertions.md))。
  [364](364-bug-lockman-with-release-failure-and-graveyard-retention.md) の 4 番
  (`TestRenewExtendsHold` の壁時計依存) と同じ族の可能性がある

## 再現手順

```sh
cd src/lockman
# 静穏だと 300〜400 回に 1 回。負荷をかけると率が上がる
go test -run 'TestStaleTakeoverHasExactlyOneWinner$' -count=300 ./...
```

## 結論 (2026-09-15): production の race。ハーネス由来ではない

`tryTakeover` の **「期限切れと判定する」から「rename で退ける」までが TOCTOU** だった。
rename は名前に対する操作なので、判定してから rename するまでに名前の指す先が入れ替わる。

壊れる順序 (2 者):

1. B が lock (世代 T0) を読み、期限切れと判定する
2. A が引き継ぎを完走する — T0 を graveyard へ退け、**自分の新しい lock T_A を置く**
3. B が古い判定のまま `rename(lock, graveyard/...)` を打つ → **T_A が退けられる**
4. B の `tryPlace` が空いた lock に成功する → **A と B の 2 人が勝つ**

`tryTakeover` の「rename なら勝者は 1 人に絞られる」というコメントは**偽**だった
(原子なのは操作であって、「判定したあの lock を動かす」ことではない)。

### 実測 (2026-09-15 / darwin arm64 / go1.25.4)

| 条件 | 結果 |
|---|---|
| 素の `-count=200` (静穏) | 1 件 FAIL (再現はするが統計的) |
| **判定と rename のあいだに `time.Sleep(60ms)` を挿入** | **10/10 FAIL。勝者 2〜5 人** |
| 同上で graveyard の中身を読む | **勝者の新しい lock が 2 件入っていた** (残り 1 件が死んだ lock) |

graveyard の直接証拠 (1 実行ぶん):

```
死んだ lock の token = c820f076...  / 勝者 3 人
graveyard: token=5000ac23... label=takeover -> 勝者の新しい lock (不当な退去)
graveyard: token=222f72dc... label=takeover -> 勝者の新しい lock (不当な退去)
graveyard: token=c820f076... label=dead     -> 死んだ lock (正当な退去)
```

### 却下した仮説 (次の監査が再生成しないように残す)

- **❌ ハーネス由来 / 壁時計依存**: 窓を広げると 10/10 で決定論的に再現し、graveyard に
  勝者の lock が入る。fixture の `ttl` や `time.Sleep` の長さとは無関係。
  **[364](364-bug-lockman-with-release-failure-and-graveyard-retention.md) の 4 番
  (`TestRenewExtendsHold` の壁時計依存) とは別族**なので、あちらの結論を流用しない
- **❌ mtime 粒度 (serverNow の打刻)**: 粒度が粗いなら「期限切れの判定自体が揺れる」形に
  なるはずだが、実際は判定は正しく、判定**後**の rename が別の対象を掴んでいた
- **❌ issue 358 の掃除機構**: 元の issue の記述どおり無関係 (両腕で再現)

## 修正

`tryTakeover` を 2 段にした。どちらも「判定できないなら退けない」へ倒す
(取り逃した引き継ぎは次の acquire で済むが、誤った引き継ぎは二重実行になる)。

1. **調停**: 観測した世代 (lock の token) から決まる名前 `tmp/<gen>.takeover` を
   **O_EXCL** で取る。同じ世代を見た者は同じ名前を狙うので、退ける役が 1 人に絞られる。
   置き場が `tmp/` なのは掃除の保持期間が短いほう (`scratchRetention` = 1h) だから —
   目印を取った直後に落ちたプロセスがあると、その世代は掃除まで引き継げなくなる
2. **直前の再照合**: 破壊的操作の直前に lock を読み直し、**同じ世代・同じ mtime** かを
   確かめる。1 だけでは、目印が掃除された後に現れる遅延観測者と、`Break` / `Release` の
   割り込みが残る

補助: 世代 id に mtime を混ぜない (延長で動くと調停をすり抜ける)。token は他ホストが
書いた値なのでパスに使う前に小文字 hex へ検疫する (`isHexToken`)。

`os.Link` + `SameFile` 案は採らなかった: `tryPlace` が smbfs で `os.Link` が ENOTSUP に
なりうる前提で O_EXCL へ落としている以上、link 必須の調停は**本番でだけ無音で消える**。
O_EXCL の作成は link が通らない環境でも成立する。

### 残した窓 (直さないと決めた)

再照合から rename までのあいだに、保持者の `Release` と別者の `acquire` が両方通ると、
置かれたばかりの lock を退けうる。`Release` は期限切れでは通らないので、成立には
「保持者だけが期限内と判定する」時計の際どさが要る (syscall 2 つぶんの窓)。0 にするには
rename ではなく「inode を指定した削除」が要り、POSIX にその原始操作が無い。
再開の trigger: 実 lock を使った並行実験でこの経路を再現できたとき。

## 検証

回帰テストは **seam (`takeoverObservedHook`) で順序を決定論にして**書いた。素の競争は
`-count=200` に 1 回しか落ちないので、統計テストは「負荷次第で緑になる assert」になる
([`avoid-wall-clock-assertions.md`](../_claude/rules/avoid-wall-clock-assertions.md))。
assert は勝者の数だけでなく **graveyard に入ってよいのは死んだ lock だけ**まで見る。

2 段構えなので、**段ごとに単独で red になる**ことを確認した
([`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §1.5)。
変異はすべて「ビルドできたこと」を別に確認し、判定は rc ではなくテストごとの PASS/FAIL で行った。

| 変異 | red になったテスト |
|---|---|
| 1 段目を外す (O_EXCL を落とす) | `TestConcurrentTakeoverElectsExactlyOneEvictor` |
| 2 段目を外す | `TestStaleTakeoverRefusesWhenGenerationChangedAfterClaimSwept` / `TestStaleTakeoverRefusesRenewedLock` |
| 2 段目のうち mtime 照合だけ外す | `TestStaleTakeoverRefusesRenewedLock` |
| **両段を外す (修正前の実装へ戻す)** | 上記 3 つ + `TestStaleTakeoverDoesNotEvictFreshLock` + `TestTakeoverClaimStaysInsideTmpForHostileToken` (5 件) |
| 成功時に目印を残さない | `TestStaleTakeoverRefusesWhenGenerationChangedAfterClaimSwept` / `TestTakeoverClaimStaysInsideTmpForHostileToken` |
| token の検疫を外す | `TestTakeoverClaimStaysInsideTmpForHostileToken` |

🚨 **既存の `TestStaleTakeoverHasExactlyOneWinner` は「両段を外す」変異でも緑だった**
(200 回に 1 回しか落ちないので、1 回の実行では捕まらない)。この issue が測っていた
テストは、退行検出力がほぼ無い。新しい 5 本が実質の守りで、既存の 1 本は smoke test。

## 横展開で見つかった別件 (この issue では直さない)

同じ「照合してから名前に対して破壊的操作」の形が 2 箇所ある。同じ seam の手口で
**両方とも再現した** (2026-09-15):

- `Renew`: 照合後の `O_TRUNC` 書き込みが、引き継がれた後の**別人の lock を自分のメタで
  上書き**した。これは `Renew` のコメントが書いていた「再開の trigger: 実 lock を使った
  並行実験でこの上書きを再現できたとき」の**発火**にあたる
- `Release`: 照合後の `os.Remove` が、引き継がれた後の**別人の lock を消した**

→ issue 380 へ起票。

## 残タスク

- [x] ハーネス由来か production の race かを切り分ける → **production の race**
- [x] production の race だった場合の修正 → 2 段の調停 + 再照合
- [x] ハーネス由来だった場合は判定軸を壁時計から外す → 該当なし (ただし回帰テストは
      seam で決定論にし、壁時計に依存させていない)
- [ ] `Renew` / `Release` の同型 (issue 380)
