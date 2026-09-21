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
    res := l.Cleanup(cmd == "cleanup" || o.force)   // ← timed() を通っていない
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

- [x] ~~4 箇所~~ **5 箇所**をタイムアウトで包む。数え直したら 1 つ多かった —
      `cmdAcquire` の**ロールバック解放** (`main.go`: token-file を書けなかったときに
      取ったロックを戻す `l.Release`) も素通りしていた
- [x] タイムアウトの返り値を 125 に揃える（091:418）。**ただし解放と掃除は warn に留める** —
      下の「決めたこと」参照
- [x] `Renew` の失敗を「lease 喪失（122）」と「判定不能（125）」に分ける
- [x] 091:496 の受け入れ条件テストを追加（**既存の包み 5 箇所も含めて**）
- [x] 変異検証: 包みを外す変異でそのテストの該当ケースが red になることを確認（7 本 + 配線 2 本）
- [x] 実 SMB での再現 → [issue 375](../375-human-verify-lockman-io-timeout-on-smb.md) へ起票

## 決めたこと（実装で分岐した点）

1. **包みを呼び出し側でなく `Locker` 側へ寄せた**。`timeout.go` を新設し、
   **`l.timeout` を読んでよいのはこのファイルだけ**にした（`AcquireTimed` /
   `RenewTimed` / `ReleaseTimed` / `InspectTimed` / `BreakTimed` / `CleanupTimed`）。
   `main.go` の `timed()` は削除。**生の I/O を呼ぶ形が production に残らない**ので、
   「呼び出し側が包むかどうかを選べる」という元の構造そのものが消える
2. **解放（`Release`）と掃除（`Cleanup`）のタイムアウトは終了コードを上書きしない**。
   091:418 の「超えたら 125」は取得・更新の経路に当てる規律とした。理由: 子の終了コードは
   呼び出し側の API で、子が成功したのに 125 を返すと透過の契約が壊れる。固まった事実は
   warn と `res.Errors` に出す（推奨対応 4 と同じ扱いを `Release` にも広げた）
3. タイムアウトを `errIOTimeout` の sentinel にした。文面で判定する形をやめ、
   `classifyRenewErr` が `errors.Is` で分類できるようにした
4. **`lost` の sticky は残した**。一度 lease を失ったら後の tick が成功しても 122 のまま。
   不変条件はその時点で破れているので、戻すほうが危険

## 結果（実測）

- **② は FIFO で再現できた**。issue 本文は「FIFO では作れない」と書いていたが、
  それは sweep の `ReadDir` / `Remove` と `cleanupDue` の `Stat` しか見ていなかった。
  **`stampCleanup` は `.cleanup_at` を write-only で open する**ので、そこを FIFO にすると
  読み手が来るまで返らない。`TestIOTimeoutWrapsDeferredCleanup` が決定論的に落とせる
- 変異検証（repo 外のコピーで実施。各変異はビルド成功を確認してから red/green を読んだ）:

  | 変異 | 落ちたケース |
  |---|---|
  | `runWith` の Acquire の包みを外す | `TestIOTimeoutFallsOverForCheckAndWith`（20s の安全網で赤） |
  | `runWith` の Renew の包みを外す | `…/I/O が詰まっただけなら 125` **だけ** |
  | `Release` の包みを外す | `…/ReleaseTimed` + 上と同じ 1 ケース |
  | `Cleanup` の包みを外す | `TestIOTimeoutWrapsDeferredCleanup` |
  | 分類をやめて全部 lease 喪失に畳む（退行前の挙動） | 4 ケース |
  | `errors.Is` を `==` に変える | `%w でラップされた errNotOwner は lease 喪失` |
  | 判定不能を「空いている」に倒す（091:496 が禁じる形） | `TestIOTimeoutFallsOverForCheckAndWith` |
  | （配線）`with.go` で生の `l.Renew` を呼ぶ | `TestRawLockerIOIsOnlyCalledFromTimeoutGo` |
  | （配線）`BreakTimed` を消して生の `Break` に戻す | 同上（2 つの assert が両方発火） |

- 🚨 変異 1 本は**ビルド不能**（他のテストが参照していた）で第 3 の結果として扱い、当て直した
- `go test ./...` rc=0 / `go vet` OK

## 包み忘れの再発を止める検査について

issue の 🚨 が「①だけを数える検査は②を素通りさせる」と警告していたので、
**「生の I/O メソッドを呼んでいる箇所」を数える形**にした（`timeout_wiring_test.go`）。
`l.timeout` の読み出し数ではなく呼び出し側を見るので、①（`runWith`）と②（deferred
`Cleanup`）の**どちらの形でも当たる**。脅威モデルと「検出しない形」（`withTimeout` 直呼び /
関数値経由 / reflect）はテストのヘッダに凍結した。

🚨 372 の AST 走査テストと違い、**この検査は同じ package の同じディレクトリを読む**ので
`go test` のキャッシュ穴（module の外を読むと入力が記録されない）には当たらない。

## 進捗

- 2026-09-11: 起票。①（`with` の 3 箇所）は機械照合 + FIFO ハーネスで再現済み。
  反証レビューで②（deferred `Cleanup`）が判明し、根拠を README から 091 の明文へ格上げ、
  推奨対応 2 の終了コードを 122 → 125 に訂正
- 2026-09-15: 実装。`timeout.go` を新設して包みを `Locker` へ寄せ、素通りしていた **5 経路**
  （数え直しで 4 → 5）を塞いだ。`renewOutcome` の 3 値化、`errIOTimeout` の sentinel 化、
  091:496 の受け入れ条件テスト（着手時 0 件）を追加。変異 9 本で検出力を確認
  → commit `fix(lockman,357): --io-timeout の包みを Locker 側へ寄せ、素通りしていた 5 経路を塞ぐ`

## 残タスク

- **未検証**: smbfs のサーバ不達が実際に `readLock` / `serverNow` / `OpenFile` の
  どこでブロックするか → **[issue 375](../375-human-verify-lockman-io-timeout-on-smb.md) に
  human issue として起こした**（期限 2026-10-15）。手順と記録してほしい実測値はそちら
- ~~**未再現**: ②（deferred `Cleanup`）の詰まり~~ → **再現した**（2026-09-15）。
  `.cleanup_at` を FIFO にすると `stampCleanup` の write-only open がブロックする。
  「FIFO では作れない」は sweep しか見ていなかった誤り
- スコープ外: `Renew` の `readLock` → `OpenFile` の隙間
  （[issue 340](340-risk-av1ify-lock-unverified-residuals.md) 項目 1 の残り）。
  あちらは「窓が残る」話で、こちらは「包みが無い」話。別物

## 関連

- [issue 091](091-feat-lockman-directory-lease-lock.md) — 仕様の正本（:418 の io-timeout / :398-399 の終了コード表 / :496 の受け入れ条件）
- [issue 356](356-bug-lockman-with-releases-lock-while-grandchildren-run.md) — 同じ `runWith` の別の欠陥
- [issue 340](340-risk-av1ify-lock-unverified-residuals.md) — `Renew` の残り窓（別物）
- [issue 359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) — この issue の出典（監査記録）

## 敵対的レビュー (2026-09-15 / read-only サブエージェント 1 体)

主張 5 本のうち **2 本が偽**だった。**指摘の中心は「私がレビュー中に足した修正」の中にあった**
(goroutine 上限の commit)。採否:

| # | 指摘 | 対応 |
|---|---|---|
| P1-1 | 🚨 **goroutine 上限として入れた `ticker.Stop()` が、一過性のヒカップを本物の lease 喪失に変えていた**。更新が二度と走らないので lease は実際に期限切れになり、他マシンが正当に引き継ぐ = 子が走ったまま二重実行。A/B 実測つき (止めた版は他マシンの Acquire が**成功**、止めない版は拒否)。しかも既定値 (ttl 30m / tick 10m) のほうが猶予が短い | **採用**。更新を止めるのをやめ、**同時 1 本に制限**する形へ作り替えた (`renewAsync`)。詰まっている間は積まないが、復旧すれば更新が再開する。期限は**報告**にだけ使う |
| P1-2 | `TestOnLostWarnDoesNotKillChild` が何も守っていない。`if onLostKill` → `if true` の変異が**緑のまま通る** (outcome が renewLost なら、子が殺されていても 122 が返るので終了コードでは区別できない) | **採用**。子自身に印を書かせて観測する形に変えた |
| P1-3 | **主張 1 は偽**。`--io-timeout` を素通りする production の I/O が 3 経路残っていた: `NewLocker` の `os.Stat` (**全サブコマンドが通る。対象は共有そのもの**) / `resolveToken` の `ReadFile` / `cmdAcquire` の `WriteFile`。実測: 3s ブロックするマウントで `check --io-timeout 200ms` が **exit=0 / 所要 3.001s** | **採用**。3 経路とも包んだ。挙動テストは書けない (Stat がブロックするマウントを作れない) ので**配線を静的に pin** した |
| P2-1 | 配線ゲートの回避 3 形。うち `lk.Renew(...)` (レシーバ名が `l` / `locker` 以外) は**宣言した脅威モデルに正面から当たる**のに素通りした | **採用**。レシーバ名の近似をやめた。残り 2 形 (新メソッドの追加 / Locker メソッド以外の I/O) は「検出しない形」へ**追記**した (§8) |
| P2-2 | 主張 3 は半分。昇格したのは lease 喪失の経路だけで、**シグナル経路 (Ctrl-C / kill) には昇格が無い** | **記録のみ**。ここは**意図的に昇格しない** — 撃ったのは人で、`with` は子の終了コードを透過する薄い包みなので、子が TERM を無視するなら素で実行したときと同じ振る舞いにする。昇格するのは「他者が既に引き継いでいて、止めないと二重実行になる」ときだけ。理由をコードに書いた |
| P2-3 | `--io-timeout` の値検証が無い (0 / 負値 / 1ns / 10h が通る)。`cleanup.go` の注記が「356 / 357 で直す」と予告していた分 | **採用**。100ms 〜 5m の範囲検証を入れた (issue 359 の残タスクもこれで消える) |
| P3-1 | `escalateGroupKill` が `exited` を見る**前**に SIGTERM を撃つ。「close(exited) を先にしたので塞いだ」は SIGKILL 側だけ | **採用**。撃つ前に non-blocking で `exited` を見る |

**壊せなかった主張**: 終了コードの表 (091:398-399 と実バイナリで端から一致) / `killGroup` の
ガード / 新しい並行性 (`-race -count=5` クリーン) / `CleanupTimed` の戻り値の誤読経路。

### 修正後の変異検証 (レビュワーが素通りさせた形を当て直した)

| 変異 | 結果 |
|---|---|
| 最初の失敗以降は更新しない (= `ticker.Stop()` 版) | `TestLeaseSurvivesTransientRenewBlock` **RED**、`TestRenewDoesNotPileUp…` は PASS のまま |
| `if onLostKill` → `if true` | `TestOnLostWarnDoesNotKillChild` **RED** |
| `NewLocker` の Stat の包みを外す | `TestRawLockerIO…` **RED** (配線 pin が発火) |

🚨 **新テストの最初の版は、退行を当てても緑だった。** 詰まりの窓 (400ms) が
「最初の tick (300ms) + 期限 (200ms)」より短く、検査したい状態に入る前に復旧していた。
窓を 900ms へ広げて RED を確認した (`mutation-verify-new-tests.md` の
「差が出ない状況しか作っていないか」そのもの)。
