# 172 bug: 再利用した計測値がトーストの材料に乗り、実体が消えても最大 1 時間「解放できます」と言い続ける

起票日: 2026-09-02
重要度: **P3**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 2) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「追記」のスキャンのコスト = `Reuse` / 「トースト」)

## 対象

`src/glogx/doctor_view.go` の `receiveDisk` → `saveCache(*rep)` (`rep.Total` は Reused 分を含む) / `doctorReuseFrom`

## 何が起きるか

issue 148 は「再利用中に実体が変わっていれば表示は古い (最大 1 時間)。行の注記で分かる形にしてある」を許容と書いたが、
**その古い値がトースト用の `doctor-disk.json` にも書き込まれる**。トーストには注記が無いので、
ユーザーには「消したのにまた同じ量を消せると言ってくる」ようにしか見えない。

さらに悪いのは、トーストは**次に doctor 画面を開くまで更新されない**こと。「開けば直る」が 1 時間効かない。

## 再現手順 (実証済み。worktree の demo test で Total=20GB / toast に 20.0GB を確認)

1. T0: doctor を開いて完全走査。DerivedData が 20GB (計測 5.7 秒 → 重いエントリとして再利用対象になる)
2. T0+10m: 手で `rm -rf ~/Library/Developer/Xcode/DerivedData/*` する
3. T0+20m: doctor を開く。snapshot の 5 分 TTL は切れているので走査は走る
4. DerivedData は「計測から 1 時間以内 + 前回 2 秒以上」なので**再利用**され、20GB の行が出る
5. 完了時に `saveCache` が走り、`doctor-disk.json` の Total が 20GB に**更新される**
6. 次回 glogx 起動時のトースト: 「20.0GB 解放できます (Xcode 20.0GB)」

## 対応案

どちらか:

- Reused を含む完了では `doctor-disk.json` を更新しない (前回値を維持する)
- `doctorDiskCache.Entries` に reused フラグを持たせ、トーストの合計から除外するか注記を添える

前者が単純。トーストは「最後に実測した値」を出す方が意味的にも正しい。

## 受け入れ条件

- [ ] 再利用を含む完了でトーストの数字が古い実測値に引きずられないことをテストで固定する
- [ ] 変異検証: 除外を外すとトーストに再利用値が乗ることを確認する
