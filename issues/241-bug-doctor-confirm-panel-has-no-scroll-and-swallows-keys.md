# 241 bug: 削除の確認パネルは入り切らない対象を見る手段が無く、見ようとした打鍵が確認ごと無言で消える

起票日: 2026-09-04
出典: audit (ux) 2026-09-04 / doctor スコープ
重要度: **P2**（破壊的操作の確認画面で「対象を確かめる」ができない。倒れる向きは安全側だが無言）
対象: `src/glogx/doctor_delete.go` の `handleDeleteKey`（`case d.confirm` の `default`）/
      `assembleDeletePanel` / `maxConfirmPaths`

## 症状と発火条件

選択が多くて `assembleDeletePanel` が `… 他 N 件は画面に入りません (端末を広げてください)` を出す状態、
または 1 エントリの対象が 11 件以上で `… 他 N 件` に畳まれた状態で、
`j` / `k` / `ctrl+d` / `Space` / `g` / `G` / `Enter` のいずれかを押したとき。

- `assembleDeletePanel` は塊単位で本文を落とし、案内は「端末を広げてください」だけ（**スクロールの語彙が無い**）
- `handleDeleteKey` の confirm 分岐は `case "y", "Y"` 以外を**すべて `d.reset()` + `doctorSwallow`** にするので、
  スクロールしようとした打鍵が**確認そのものを閉じる**
- `doctorSwallow` はトーストを出さないので**無言**。`v.selected` は残るため一覧に戻ると `*` 印だけが
  元のままで、「何が起きたか」が読み取れない

## 語彙の非対称（同じ経路）

glogx の破壊操作の確認は `status_view.go:discardBox` が `y/Enter: 実行   n/Esc: キャンセル` で、
`discardKey` の doc は「push/pull 確認と同じ語彙」と明記し issue 071 / 123 で 2 度再確認されている。
**doctor の削除確認だけ `Enter` がキャンセル**（hint も `y: 削除する   n/Esc: やめる`）。
倒れる向きは安全側だが、`Enter=実行` の手癖で押すと無言で閉じる。

## 直し方

確認パネルに窓を持たせる（同 package の `rowCursor.window` がそのまま使える）。
最低限 `j/k/ctrl+d/ctrl+u/g/G` は「飲むだけ」にして中止から外し、`maxConfirmPaths` の
打ち切りを画面高に応じて可変にする。`Enter` を受けるかは共通語彙の議論なので別途。

## 既存 issue との関係

issue 233 は「触らない対象まで数えて並べる」= 件数と対象集合の話で、
**表示しきれない対象を見る手段が無い / 見ようとするとキャンセルされる**は別。
issue 236 の P3-6（削除中の `ctrl+g`）は実行中フェーズの話で、こちらは確認フェーズ。
