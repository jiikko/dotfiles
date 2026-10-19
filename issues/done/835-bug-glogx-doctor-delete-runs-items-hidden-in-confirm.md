# 835 (bug): doctor の削除で、確認画面が「省く」と出した Item が本番の削除に渡る (P2)

起票日: 2026-10-19

## 概要

`src/glogx/internal/doctorview/doctor_delete.go` の `handleDeleteKey` は、確認中の `y` で
`plannedTargets(d.plan, v.selectedResults())` を本番の削除に渡す。`plannedTargets` が下見 (DryRun) の結果で絞るのは
**エントリ単位**だけで、渡す `Result.Items` は選んだ全部のまま。
一方、確認画面は `plannedItems` で Planned の Item だけを件数とパスに出し、落ちた Item は `droppedNote` で「省いた」と見せる。
`doctor/disk.Delete` は本番でも渡された Items から計画を作り直す (`planDelete` が `for _, want := range t.Items` を回す) ので、
下見で落ちた Item が本番で候補に戻ると、確認画面に出していないものが消える。

## 詳細

- 発火条件: 下見でエントリは Planned、その中の一部の Item だけが **Item 単位の guard** で Failed / Skipped (「いまは削除の候補ではありません」・既に無い) になり、他の Item は Planned に残る。
  `y` を押すまでにその Item が候補に戻る (キャッシュのディレクトリが作り直された など。同名で別の inode でも、本番の再走査は新しい実体を検査するので止まらない)
  - 「Chrome を終了した」は例にならない: `chrome-tmp` の起動中の判定は `src/doctor/disk/scan.go` の `scanEntry` でエントリ全体を blocked にするので、そもそも Planned にならない (codex の反証レビューの指摘)
- `plannedTargets` の doc は「確認画面に出した対象と実行対象を一致させる」と言うが、一致しているのはエントリの集合だけ
- 裏取り: コードを読んで確認した (`src/doctor/disk/delete.go` の本番ループが `planDelete(ctx, t, opt)` を呼び、`planDelete` が `t.Items` を走査し直す)。削除経路は実機で走らせていない

## 対応方針

- 本番の再走査・guard・実体の検査は残し、**その手前で「確認画面で同意した Item の集合」を上限にする**。照合の鍵は `src/doctor/disk/delete.go` の `itemKey` と揃える (CLI の対象は path ではなく `Ref` で照合する。path だけで絞ると CLI の Item を取りこぼす)
- `execCLI` の ref を取らないコマンドは、Items を絞ってもコマンド全体の効果を限定できない。この種の Item は「一部が落ちたら、そのエントリごと実行しない」に倒すか、確認画面でその旨を出す
- 回帰テスト: 下見で一部の Item が落ちたエントリを、落ちた Item が候補に戻った状態で `y` したとき、その Item が `Delete` に渡らないこと (fake の `Delete` が受け取った Items を記録して assert)

## 関連ファイル

- `src/glogx/internal/doctorview/doctor_delete.go` (`handleDeleteKey` / `plannedTargets` / `plannedItems` / `droppedNote`)
- `src/doctor/disk/delete.go` (`Delete` / `planDelete`)

## 進捗

- [x] 対象を Item で絞る + 回帰テスト (2026-10-19。commit「glogx: 全体監査 (844) の issue 834〜843 を直す」)
  - `src/doctor/disk/delete.go` に `ConsentedTargets` (確認画面に出した Item の集合。照合の鍵は `itemKey` と同じ) を置き、glogx の `plannedTargets` はそれを通す (`planRaw` = 無害化する前の下見を持つ)
  - ref を取らない CLI のコマンドは、Item の一部が落ちた・同意の外の候補がある (`cliScopeMismatch`) ならエントリごと実行しない
  - 回帰テスト: `TestDeleteCLISkipsCommandWhenSomeTargetsAreNoLongerCandidates` / `TestDeleteCLISkipsCommandWhenCandidatesOutsideConsent` / `TestConsentedTargets` / glogx の `TestDeleteKeyYPassesOnlyItemsTheConfirmShowed`
- 変異: Item の絞り込み・ref なし CLI の照合などを外す変異がすべて red
- レビュー: codex の実装レビュー (指摘は反映済み)
