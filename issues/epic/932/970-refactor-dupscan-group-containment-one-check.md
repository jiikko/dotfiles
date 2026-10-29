# 970 (refactor): dupscan で「組が別の組に含まれるか」の判定が 3 か所にある

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

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

- [ ] 寄せる
