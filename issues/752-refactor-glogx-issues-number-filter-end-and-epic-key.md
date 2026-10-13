# 752 (refactor): glogx の issues viewer: 番号フィルタの終わらせ方が 2 つの型に分かれている / epic group の子の判定が写されている

> 🚨 **担当中: Claude (issues viewer の監査の対応セッション)**（2026-10-14〜）

起票日: 2026-10-13

## 概要

2026-10-13 の監査 (記録は 754) の設計の観点で見つけた潜在の 2 件。どちらも今は誤動作しない。

## 詳細

### 1. 番号フィルタを終わらせる手順を呼び出し側が組んでいる (encapsulation E2)

- 該当: `src/glogx/issues_number_filter.go` の `issuesNumberFilter.clear` / `confirm` と、`src/glogx/issues_group_expansion.go` の `groupExpansion.endFilter`。
  2 つを対にして呼ぶのは `issues_view.go` の `clearNumberFilter` と `finishClose` の 2 か所
- 潜在の形:
  - `issuesNumberFilter.confirm` は検索語が空なら `f.clear()` で終わらせる分岐を持ち、ここは `endFilter` を通らない。唯一の呼び出し側 `numberFilterKey` が先に空を見て
    `clearNumberFilter` へ逃がしているので、今は到達しない。前置きの if を消すと、666 の E が防いだ「解除後も collapsed が残り、展開しても開かない」が compile を通ったまま戻る
  - `endFilter` の doc は「auto を読むので setAuto(nil) より前に呼ぶ」と順序を課す。`finishClose` は `numFilter.clear()` → `refresh()` → `groups.endFilter("")` の順。keep="" なので auto を読まず、今は無害
- 守っているのはコメントだけ (compile もテストも止めない)
- 直し方: `issuesView.endNumberFilter(keep)` を 1 本置き、`numFilter.clear` と `groups.endFilter` と refresh をまとめる。`confirm` の空の分岐は消すか、呼び出し側へ任せる。
  `endFilter` の中で auto を空にすれば、呼ぶ順序の制約も無くなる

### 2. 「epic group の子か」の判定を viewer の 6 か所が手で書いている (duplication)

- `GroupKind == GroupEpic` (に `&& GroupKey != ""` が付く箇所もある) を、`issuesNumberFilter.groupKeys` / `applyScreen` / `pruneExpandedGroups` / `rebuildDisplayRows` / `rowsForTab` / `clearNumberFilter` が書いている
  (ほかに `issues` 側の `retiredGroupKeys` / `closedGroupKeys` / `showsIssue` / `IsGroupParent` (こちらは `Group == ""` を見る))。数えは監査体の grep
- 物理の配置は 665 C で `ContainerDir` に寄せたが、group として畳むかの判定は寄せていない。今は Scan の保証 (GroupEpic なら必ず GroupKey を持つ) により書き方が違っても結果は同じ
- 直し方の案: `Issue.EpicGroupKey() (string, bool)`。発火を示せないので、1 を直すとき、または判定の条件が変わるときに寄せる (trigger)

## 関連ファイル

- `src/glogx/issues_view.go`・`src/glogx/issues_number_filter.go`・`src/glogx/issues_group_expansion.go`・`src/glogx/issues/parse.go`
- 前回: 666 (E で状態を `groupExpansion` に寄せた)。監査の記録: 754

## 進捗

- [ ] 未着手
