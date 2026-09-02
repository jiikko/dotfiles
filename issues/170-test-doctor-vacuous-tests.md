# 170 test: doctor のテスト 11 箇所が変異しても green (前回 P2 の修正がテストで守られていない)

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 1 / 体 2) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「敵対的レビュー 2 回目」で直した partial 保存・再利用・fail-closed)

## 対象

`src/glogx/doctor_view_test.go` (`TestDoctorHandleKey` / `TestDoctorSavesCacheOnCompleteAndPartialPolicy` ほか)、
`src/doctor/svc/scan_test.go` の fake runner の key 一致

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
| `svc/scan_test.go` の fake key `"xcrun simctl --set"` は prefix 一致で、`--set <path> list devices -j` の**順序も path も見ない** | 順序を崩す変異を通す |

## 対応案

上の表の各行に対応するテストを足す。特に:

- partial 保存の 3 条件 (`prev.Partial` / `prev.Total >= rep.Total` / `close()` の合計計算) は
  **完全な結果 → partial → 完全** の時系列を作って、キャッシュに残る数字を assert する
- `age < 0` (時計を戻した) は `loadDoctorSnapshot` と `doctorReuseFrom` の両方で固定する
- fake runner は prefix 一致をやめ、argv 全体 (順序込み) を突合する形にする
- `TestDoctorHandleKey` の `r` は、diskResults を積んだ状態から押して partial が書かれることを見る

## 受け入れ条件

- [ ] 上表の 11 変異それぞれについて、当てたテストが red になることを確認する (`mutation-verify-new-tests.md`)
- [ ] 変異の内容を commit message に残す
