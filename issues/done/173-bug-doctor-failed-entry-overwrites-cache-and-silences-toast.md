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
## 対応 (2026-09-03、commit `6bc006c1`)

予約セッション (05:00) が worktree で実装したが未コミットで終了していたため、後続セッションが
差分を検証してから master へ当てた。**予約セッションの「変異検証済み」の主張は証拠に数えず、
master 上でやり直した。**

**修正した。対応案の後者 (Failed 件数を持たせて沈黙させない) を採り、さらに構造を変えた。**

- `doctorDiskCache.Failed` に「診断できなかった」件数を持たせ、`doctorStartupToast` は閾値未満でも
  `Failed > 0` なら「N 件を診断できませんでした (解放量は未確定)」を出す。閾値以上なら合計に
  「、N 件は診断できず」を添える
- failed を含む**完了**結果は書く。走査は完走しており、その環境の現実がそれ。書かないと
  「1 エントリが恒久的に測れない Mac」でキャッシュが永久に凍結する
- partial (中断) は**前回の記録に重ねて書く** (エントリ単位マージ)。これにより 2026-09-02 P2 の
  「Esc 直後の数件で 45GB が 200MB に置き換わる」は構造的に防がれる

### 外した防御と、それがマスクしていたもの

`saveCache` の「partial で合計が前回より小さければ書かない」ガードを**外した**
([`list-masked-failure-modes-before-removing-guard.md`](../../_claude/rules/list-masked-failure-modes-before-removing-guard.md) に従い列挙):

| マスクしていた failure mode | 今は誰が防ぐか |
|---|---|
| Esc 直後の数件だけの partial が完全な結果を潰す | エントリ単位マージ (今回走査が届かなかったエントリは前回値を保つ) |
| 大きいエントリを 1 つ測った直後の Esc で「合計は前回より大きい」が通り、恒久 failed の記録が failed=0 に化ける | マージで前回の failed エントリも保たれる (予約セッションの敵対レビューが再現していた) |
| 副作用として: 測り直して**本当に縮んだ**エントリまで古い値へ差し戻していた | ガードを外したことで解消 (これはマスクではなく害だった) |

### 変異検証 (master 上、2026-09-03)

`c.Failed++` を外す (診断できずを数えない) → `TestDoctorSaveCacheWritesCompletedScanWithFailures` /
`TestDoctorSaveCachePartialMergesInsteadOfReplacing` / `TestDoctorSaveCacheDoesNotFreezeWithStaggeredReuse`
の 3 本が red。復元で green。

### 受け入れ条件

- [x] failed を含む完了が前回の大きい合計を潰さず、沈黙もしないことをテストで固定した
- [x] 変異検証: Failed を数えないと沈黙が再現する (上記)
