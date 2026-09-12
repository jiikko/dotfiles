# --io-timeout が with と deferred cleanup を素通りする（仕様 091 の明文違反・受け入れ条件も未達）

起票日: 2026-09-11
カテゴリ: bug / priority: high
対象: `src/lockman/with.go` の `runWith` / `src/lockman/main.go` の `dispatch`（deferred `Cleanup`）/ `timed`
出典: resource-leaks 監査 2026-09-11（[issue 359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md)）
反証レビュー: 1 周実施。**起票時の「`with` だけが穴」は誤りで、`dispatch` の deferred `Cleanup` も
包まれていないことが判明した**（下の表）。指摘を反映済み

## 問題

🚨 **[362](362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) は別物で、本 issue の修正では消えない** (2026-09-12 追記)。あちらは「**包んだ** I/O の goroutine が
`withTimeout` に見捨てられた後も走り続け、失敗を報告した後に lock を置く」話。
`Cleanup` を `timed` で包むと defer は短くなるが、goroutine が回収されない事実は変わらない。


`--io-timeout`（既定 10s）は `main.go` の `timed()` = `withTimeout(l.timeout, fn)` だけが
効かせている。**包まれていない I/O が 2 系統ある。**

機械照合（`src/lockman/*.go` の production のみ）:

| 事実 | 数 | 場所 |
|---|---|---|
| `l.timeout` を読む箇所 | 1 | `main.go` の `timed` の中だけ |
| `timed(` を呼ぶ箇所 | 5 | `main.go` の `dispatch`（check / status / break）/ `cmdAcquire` / `cmdTokenOp`（release + renew） |
| `withTimeout(` を呼ぶ箇所 | 1 | `main.go` の `timed` 自身の中 |
| **未包み ①**: `runWith` から `Locker` の I/O を呼ぶ箇所 | 3 | `l.Acquire`（冒頭）/ `l.Release`（defer）/ `l.Renew`（ticker の case） |
| **未包み ②**: `dispatch` の deferred `Cleanup` | 1 | `acquire` / `with` / `break` / `cleanup` の 4 コマンドで必ず走る |

### 未包み ②（deferred `Cleanup`）が効く範囲は `with` に限らない

```go
// main.go の dispatch — acquire / with / break / cleanup で必ず走る
defer func() {
    res := l.Cleanup(cmd == "cleanup" || o.force, "")   // ← timed() を通っていない
```

`Cleanup` は `serverNow`（probe の `OpenFile` + `Stat` + `Remove`）と 3 回の `ReadDir` +
`Remove` を打つ。したがって **`lockman acquire --io-timeout 3s` は Acquire を 3 秒で
倒した後、deferred cleanup で無期限に固まりうる**。`--io-timeout` の目的（スクリプトが
無言で固まらない）が `with` 以外でも達成されていない。

つまり **`with` コマンドの未包み I/O は 3 ではなく 4**（`Acquire` / `Renew` / `Release` +
deferred `Cleanup`）で、修正スコープも 4 箇所。

## 仕様 091 の明文違反であり、受け入れ条件も未達

README の言い回しではなく、**仕様の正本が終了コードまで指定している**:

> **`--io-timeout` (既定 10 秒) を設け、超えたら「判定不能」= exit 1 (`with` は 125) で返す** (091:418)

| exit | 意味（091:398-399） |
|---|---|
| 122 | 走行中に lease を失った（`--on-lost=kill` で子を止めた） |
| **125** | **lockman 自体のエラー（I/O・権限・判定不能）** |

さらに 091 の受け入れ条件:

> **I/O が固まったときに沈黙しない**: 応答しないパス（FUSE でも `SIGSTOP` させた
> ヘルパーでもよい）を用意し、`--io-timeout` で exit 1 になること。**「空いている」に
> 倒れないこと**が本体 (091:496)

🚨 **このテストは存在しない**（`grep -n 'io-timeout\|ioTimeout\|timed(\|withTimeout(' *_test.go`
= **0 件**）。つまり `with` と deferred cleanup に包みが無いだけでなく、
**包んである 5 箇所も一度も検証されていない**。

## 発火条件

**本番の発火条件は「応答しないマウント」（smbfs でサーバ不達）。これは未再現。**
lockman は smbfs 越しを想定して作られており（README「対象環境」）、`withTimeout` は
まさにその状況のために在る。

下の再現は **`.lockman/lock` を FIFO に差し替えるハーネス**で「その位置でブロックする
I/O」を人工的に作ったもの。`readLock` の `os.ReadFile` は FIFO を open した時点で
書き手が来るまで止まるため、詰まりの位置だけを本番と揃えられる。
🚨 **本番に FIFO は置かれない。** ハーネスが証明しているのは
「タイムアウトの包みが無い」「その位置でブロックすると戻らない」という**機構**で、
smbfs が実際にそこでブロックすることは別に実測が要る
（`_claude/rules/verify-execution-not-just-exit-code.md`「隔離環境での失敗も本番の失敗ではない」）。

**未包み ②（deferred `Cleanup`）は FIFO では再現できない**（sweep の `ReadDir` / `Remove` も
`cleanupDue` の `Stat` も FIFO ではブロックしない）。②は**機械照合だけの機構レベルの
指摘**として扱う。

### 再現 A: 同じ `--io-timeout 3s` で check は倒れ、with は倒れない

| コマンド | 結果 |
|---|---|
| `lockman check $D --io-timeout 3s` | **3.03s** で `I/O が 3s 以内に返らない (マウントが応答しない可能性): 判定不能` / rc=3 |
| `lockman with $D --io-timeout 3s --ttl 30s -- /bin/echo` | **10.2s 経っても戻らず**、外から SIGKILL で打ち切った |

### 再現 B: renew で詰まると SIGTERM / SIGINT / SIGHUP がすべて効かなくなる

`with --ttl 30s -- sleep 120` で正常に取得させた後、`lock` を FIFO に差し替えて
renew tick（`ttl / renewDivisor` = 10s）を詰まらせた実測:

| 撃ったシグナル | 結果 |
|---|---|
| SIGTERM | **効かない**（プロセス生存 / 子へも転送されない） |
| SIGINT | **効かない** |
| SIGHUP | **効かない** |

子（`sleep 120`）は生き続けた。SIGKILL で打ち切った。
（`signal.Notify` しているのは INT / TERM / HUP だけなので、**SIGQUIT など Notify して
いない致命シグナルでは終了する**。「SIGKILL しか残らない」ではない。）

理由は構造的で、Go のシグナル意味論から確定する: `signal.Notify(sigCh, INT, TERM, HUP)`
を張った時点で既定動作（プロセス終了）は置き換わり、シグナルは `sigCh` に入る。
転送は `select` ループの `case sig := <-sigCh:` でしか起きないので、**同じループが
`l.Renew` でブロックしている間はシグナルが 1 つも転送されず、自分も死なない**
（`sigCh` は容量 4 で、溢れた分は捨てられる）。

### なぜこれが「hang」ではなく「ロックの欠陥」か

Notify していないシグナル（QUIT / KILL）で殺すしかないので、`runWith` の
`defer l.Release(meta.Token)` は**走らない**。結果、詰まったマウントが復旧した後も
**lock は TTL（既定 30 分）まで残り**、その間ほかのマシンは acquire できない（rc=3）。
`--io-timeout` は本来その 30 分を 10 秒に縮めるための機構で、`with` ではその機構が
接続されていない。

環境: macOS 15（Darwin 24.6.0）/ ローカル APFS / `src/lockman` を scratchpad へ複製して
`go build` した実バイナリ。

## 併発: `lost` が「判定不能」と「lease 喪失」を区別していない

`runWith` は `l.Renew` のエラーを**すべて** `lost = true` に畳む。`Renew` は
`errNotOwner` 以外でも失敗する:

- `readLock` の I/O エラー
- `serverNow` の失敗（probe が作れない / stat できない）
- `clockSkewTolerance = 5s` を超える打刻ずれ

したがって**一過性の I/O ヒカップや 5 秒の打刻ずれ 1 回で、`--on-lost kill` が子を殺し、
rc=122「走行中に lease を失った」を返す** — lease は失われていないのに。091 の表では
それは 125（判定不能）であるべき。しかも `lost` は一度立つと戻らないので、次の tick で
renew が成功しても 122 のまま。

## 推奨対応

1. **`runWith` の `Acquire` / `Renew` / `Release` と、`dispatch` の deferred `Cleanup` の
   計 4 箇所をタイムアウトで包む**。`timed` は `main.go` にあるので、`Locker` 側のメソッドに
   するか、`runWith` へ `func(func() error) error` を渡す形にする
2. **タイムアウトは 122 ではなく 125（`exitWithInvalid`）で返す**（091:418）。
   子は fail-closed で止める（`--on-lost kill` の意図）が、返す番号は「判定不能」。
   **「タイムアウトしたので次の tick まで待つ」に倒さないこと** — TTL を超えれば他者が
   引き継ぐので、待つほど二重実行に近づく
3. `Renew` の失敗を `errNotOwner`（= 本当に lease を失った → 122）と
   それ以外（= 判定不能 → 125）に分ける。`lost` を単一の bool から状態へ変える
4. deferred `Cleanup` のタイムアウトは**致命にしない**（`Cleanup` は正しさに関与しない設計）。
   件数と一緒に `res.Errors` へ入れて `--verbose` に出す

🚨 **配線したら、091:496 の受け入れ条件を満たすテストを入れること**（今 0 件）。
再現 A（同じ `--io-timeout` で check と with が同じ時間で倒れる）を FIFO ハーネスで
落とし、包みを外す変異で**そのケースだけが** red になるか確認する。
**既に包んである 5 箇所にも同じテストが要る**（091 の未達分）。

🚨 **「`l.timeout` の読み出し箇所が 1 → 2 以上になったことを機械で数える検査」を置くなら、
deferred `Cleanup` の箇所も数える設計にすること**。①だけを数える検査は②を素通りさせ、
「検査があるのに同じ穴が残る」形になる
（`_claude/rules/verify-execution-not-just-exit-code.md`「有無で結果が変わらない観測を証拠に数えない」）。

## todolist

- [ ] `runWith` の 3 箇所 + `dispatch` の deferred `Cleanup` = 4 箇所をタイムアウトで包む
- [ ] タイムアウトの返り値を 125 に揃える（091:418）
- [ ] `Renew` の失敗を「lease 喪失（122）」と「判定不能（125）」に分ける
- [ ] 091:496 の受け入れ条件テストを追加（**既存の包み 5 箇所も含めて**）
- [ ] 変異検証: 包みを外す変異でそのテストの該当ケースが red になることを確認
- [ ] 実 SMB での再現（human issue。下の「残タスク」）

## 進捗

- 2026-09-11: 起票。①（`with` の 3 箇所）は機械照合 + FIFO ハーネスで再現済み。
  反証レビューで②（deferred `Cleanup`）が判明し、根拠を README から 091 の明文へ格上げ、
  推奨対応 2 の終了コードを 122 → 125 に訂正（未着手）

## 残タスク

- **未検証**: smbfs のサーバ不達が実際に `readLock` / `serverNow` / `OpenFile` の
  どこでブロックするか。**再開の trigger**: 実 SMB 共有で `with` 実行中にサーバを
  落として再現できたとき。README「実機で測っていない前提」の 4 項目と同じ扱いで、
  人が測る作業なので human issue に起こす価値がある
- **未再現**: ②（deferred `Cleanup`）の詰まり。FIFO では作れないので、
  `SIGSTOP` させたヘルパー FS か実 SMB が要る（091:496 が例示している手段）
- スコープ外: `Renew` の `readLock` → `OpenFile` の隙間
  （[issue 340](done/340-risk-av1ify-lock-unverified-residuals.md) 項目 1 の残り）。
  あちらは「窓が残る」話で、こちらは「包みが無い」話。別物

## 関連

- [issue 091](done/091-feat-lockman-directory-lease-lock.md) — 仕様の正本（:418 の io-timeout / :398-399 の終了コード表 / :496 の受け入れ条件）
- [issue 356](356-bug-lockman-with-releases-lock-while-grandchildren-run.md) — 同じ `runWith` の別の欠陥
- [issue 340](done/340-risk-av1ify-lock-unverified-residuals.md) — `Renew` の残り窓（別物）
- [issue 359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) — この issue の出典（監査記録）
