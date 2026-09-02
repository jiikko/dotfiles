# 170 test: doctor のテスト 11 箇所が変異しても green (前回 P2 の修正がテストで守られていない)

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 1 / 体 2) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「敵対的レビュー 2 回目」で直した partial 保存・再利用・fail-closed)

## 対象

`src/glogx/doctor_view_test.go` (`TestDoctorHandleKey` / `TestDoctorSavesCacheOnCompleteAndPartialPolicy` ほか)、
`src/doctor/disk/scan_test.go` の fake runner の key 一致 (issue 起票時は svc 側と書いていたが、`--set` を使う fake は disk 側)

## 何が起きるか

体 2 が worktree で 11 個の変異を当て、**全部 green のまま**だった (ビルド可を確認済み。実証済み)。
前回の敵対的レビューで直した規律が、テストでは 1 つも固定されていない。

| 変異 | 結果 |
|---|---|
| `TestDoctorHandleKey` の `r` を `v.close()` に置換 | green (open() 直後で diskResults が空なので close() も何も書かない = テストが到達していない) |
| `saveCache` の `prev.Total >= rep.Total` を `true` に (大きい partial が完全な結果を潰す分岐) | green |
| `saveCache` の `!prev.Partial` 条件を削除 | green |
| `close()` の `rep.Total = SumDeletable` を削除 (partial Total=0 になる) | green |
| `doctorReuseFrom` の `age<0` 条件を削除 (時計を戻したときの再利用) | green |
| `doctorReuseFrom` の `Status != OK` 条件を削除 | green |
| `loadDoctorSnapshot` の `age<0` 条件を削除 | green |
| `saveDoctorSnapshot` の `Svc.Interrupted` 条件を削除 | green |
| `receiveDisk` の `!v.shown` 条件を削除 | green |
| `doctorCacheFromReport` の Status を `""` に (実経路のトースト文面を pin していない) | green |
| `disk/scan_test.go` の fake key `"xcrun simctl --set"` は prefix 一致で、`--set <path> list devices -j` の**順序も path も見ない** | 順序を崩す変異を通す |

## 対応案

上の表の各行に対応するテストを足す。特に:

- partial 保存の 3 条件 (`prev.Partial` / `prev.Total >= rep.Total` / `close()` の合計計算) は
  **完全な結果 → partial → 完全** の時系列を作って、キャッシュに残る数字を assert する
- `age < 0` (時計を戻した) は `loadDoctorSnapshot` と `doctorReuseFrom` の両方で固定する
- fake runner は prefix 一致をやめ、argv 全体 (順序込み) を突合する形にする
- `TestDoctorHandleKey` の `r` は、diskResults を積んだ状態から押して partial が書かれることを見る

## 受け入れ条件

- [x] 上表の 11 変異それぞれについて、当てたテストが red になることを確認する (`mutation-verify-new-tests.md`)
- [x] 変異の内容を commit message に残す

## 対応 (2026-09-02)

**既存テストの穴を塞ぎ、変異 12 本すべてで red を確認した** (11 本は表のもの、1 本は作業中に見つけた追加)。
production のコードは変えていない (テストだけの変更)。

### 既存テストの修正 (vacuous の原因を潰す)

- `TestDoctorHandleKey` の `r`: `open()` 直後 (`diskResults` が空) で見ていたので、`r` を `close()` に
  変えても「どちらも何も書かない」で通っていた。**走査中で結果を持っている状態**から押す形に直した。
  この「まだ完了していないが結果を持っている」状態は partial 保存の規律が効く唯一の状態なので、
  作り方を `doctorFirstDiskEvent` ヘルパーに切り出した (既存テスト内のローカルなクロージャを共通化)
- `TestDoctorReusesHeavyEntries` の除外ケース: `light` / `old` しか無かったので
  `future` (時計を戻した) / `failed` / `blocked` / `nomeasure` を足し、
  **partial な snapshot** と**読めていない snapshot** から再利用しないことも固定した

### 新規テスト

| テスト | 固定する主張 |
|---|---|
| `TestDoctorSaveCachePartialBoundaries` | 大きい partial は書く / 前回が partial なら小さくても置き換える / 完全な結果は常に書く / `close` の partial に合計が入る |
| `TestDoctorSnapshotRejectsFutureAndInterrupted` | 未来の `ScannedAt` を TTL 内と読まない / 中断した svc と partial な disk を snapshot に書かない (書ける場合の確認も同じテストに入れて、assert が常に false で通らないようにした) |
| `TestDoctorIgnoresDiskMsgAfterClose` | 閉じた後の Msg で状態もキャッシュも更新しない (gen が同じでも) |
| `TestDoctorStartupToastThroughRealPath` | Report → キャッシュ → 文面の実経路で合計・上位 2 件・誘導文が出る / failed の数字は混ぜない / cooldown の内外 |
| `disk/scan_test.go` の `--set` argv 突合 | `xcrun simctl --set <path> list devices -j` を順序込みで検証 (空白入りパスが 1 引数で渡ることも同時に見る) |

### 変異検証 (使い捨て worktree。12 本すべて red)

| # | 変異 | 判定 |
|---|---|---|
| 1 | `TestDoctorHandleKey` の `r` を `close()` に | red |
| 2 | `saveCache` の `prev.Total >= rep.Total` を落とす | red |
| 3 | `saveCache` の `!prev.Partial` を落とす | red |
| 4 | `close()` の `rep.Total = SumDeletable(...)` を削除 | red |
| 5 | `doctorReuseFrom` の `age < 0` を削除 | red |
| 6 | `doctorReuseFrom` の `Status != OK` を削除 | red |
| 7 | `loadDoctorSnapshot` の `age < 0` を削除 | red |
| 8 | `saveDoctorSnapshot` の `Svc.Interrupted` を削除 | red |
| 9 | `receiveDisk` の `!v.shown` を削除 | red |
| 10 | `doctorCacheFromReport` の `Status` を空文字に | red |
| 11 | `simDeviceUDIDsInto` の argv 順序を崩す (`--set` を後ろへ) | red (disk 側) |
| 12 | `doctorCacheFromReport` の「候補 0 件は持たない」を削除 (追加分) | red |

各変異は `go vet` でビルド可を確認してから red/green を読んだ (ビルド不能な変異の緑を
「検知できなかった」と誤読しないため)。`make test` は rc=0。

### 期待値の作り方について

`close` の合計を見る assert では、production の `disk.SumDeletable` を使わず
**受け取った `Items` から自前で足した値**と比べている (期待値を production と同じ式から作らない)。
