# 173 bug: 重いエントリが failed した「完全な」結果が partial ガードを通り抜けてキャッシュを潰し、トーストが沈黙する

起票日: 2026-09-02
重要度: **P3**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 2) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「敵対的レビュー 2 回目」の partial 保存 P2 / 2 章「検出の健全性」)

## 対象

`src/glogx/doctor_view.go` の `saveCache` (ガードは `rep.Partial` しか見ない) / `src/doctor/disk/report.go` の `SumDeletable`
(failed を 0 として扱う)

## 何が起きるか

前回の敵対的レビューで「partial が完全な結果を潰す」P2 を直し、`rep.Partial` のときだけ前回値と比べるようにした。
しかし**走査は完走したが個々のエントリが failed した結果**は `Partial=false` なので、ガードを通らずそのまま上書きする。

failed エントリは `SumDeletable` で 0 として数えられるため、合計が激減する。

実測 (実証済み。demo test):

| 状態 | doctor-disk.json の Total | トースト |
|---|---|---|
| 前回 (正常な完全走査) | 45GB | 「45.0GB 解放できます」 |
| 今回 (重いエントリが 60 秒 timeout で failed。Partial=false) | 1GB | `""` (閾値未満で沈黙) |

画面を開けば「走査できず」が出るが、**トーストには「診断できず」を伝える語彙が無い**ので、
ユーザーから見ると「昨日まで 45GB と言っていたのに今日は何も言わない」になる。
これは issue 148 2 章の「検出の健全性 (sinking silently の禁止)」に反する。

## 再現手順

1. 完全走査を 1 回成功させて `doctor-disk.json` に大きい Total を作る
2. 重いエントリ (DerivedData 等) の走査を失敗させる (timeout を極小にする / 対象を読めない権限にする)
3. doctor を開いて完走させる → `doctor-disk.json` の Total が小さくなる
4. glogx を再起動 → トーストが出ない

## 対応案

どちらか:

- failed を含む完了も partial と同じ「前回より小さければ潰さない」ガードに載せる
- `doctorDiskCache` に failed 件数を持たせ、トーストに「N 件は診断できず」を添える (沈黙させない)

後者の方が規律 (sinking silently の禁止) に忠実。両方入れてもよい。

## 受け入れ条件

- [ ] failed を含む完了が前回の大きい合計を潰さない、または沈黙しないことをテストで固定する
- [ ] 変異検証: ガードを外すと上書き + 沈黙が再現することを確認する
