# 970 (refactor): dupscan で「組が別の組に含まれるか」の判定が 3 か所にある

起票日: 2026-10-29

## 概要

メンバーの集合が別の組のメンバーの集合に含まれるかを、3 か所が別々に書いている。1 つの関数に寄せる。

## 詳細

- `src/dupscan/internal/group/group.go` の `covered`: 組分けで、同じ集合の組を 2 度出さない (集合が等しいか)
- `src/dupscan/sweep_cmd.go` の `coveredByAny`: sweep の節 2 で、節 1 の組に全員が入る組を出さない (部分集合か)
- `src/dupscan/sweep_types.go` の `sweepTypedGroups`: sweep の節 6 で、他の組に含まれる組を束ねない (部分集合か。メンバーを `Unit.Key` の文字列で比べている)
- issue 955 の F2 の敵対的レビュー (4 周目) で、ぼやきとして挙がった

## 対応方針

- `group` package に「メンバーの集合 a が b に含まれるか」を 1 つ置き、3 か所から呼ぶ。集合が等しいかは、大きさが同じで含まれるか
- `sweepTypedGroups` の比べ方を `*Entry` の集合にそろえる (`Unit.Key` の文字列と同じ単位か確かめてから)
- 挙動は変えない。既存のテストを変えずに通ることを確かめる

## 進捗

- [x] 寄せる (2026-10-29。commit `refactor(dupscan): 組が別の組に含まれるかの判定を group.Subset に寄せる (issue 970)`)
  - `group.Subset` (a の全員が b に入るか) と `group.MemberSet` を置いた。`covered` は「大きさが同じで Subset」、`newGroups` は覆う側の集合を 1 度だけ作って `Subset`、`sweepTypedGroups` は `*Entry` の集合で `Subset`
  - `coveredByAny` は `newGroups` にしか呼ばれていなかったので消した。`dropNested` (クロージャが囲む単位の組の中にあるか) は別の判定なので対象外
  - 既存のテストを変えずに全部通った (dupscan の test・lint、root の `make test-lint`・`make test-dupscan-gate` が rc=0)
  - 変異: `Subset` の照合を外すと、3 か所それぞれのテストが赤 (組分けの `TestGoCopyIsReportedAlongsideSwift`・節 2 の `TestSweepL1mGroupAppearsOnlyInDeepSection` と 966 の `TestSweepReportsSilencedGroupsAndMiscountedAllowGroup`・節 6 の `TestSweepTypeCopies*` 16 本)
  - 敵対的レビューは省略 (判定の置き換えで、挙動を変えない)
