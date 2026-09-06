# issues viewer: 「見えている範囲はバッジが示す」が epic group の子で嘘になった

- 種別: bug（表示の嘘。動作は正しい）
- 対象: `src/glogx/issues_view.go` の hint 組み立て（`a: +⏸` の出し分け）と、その直前のコメント
- 優先度: low（急がない。表示が誤解を招くだけで、操作は壊れていない）
- 出典: dotfiles-cd セッションからの申し送り（291 の敵対レビュー P3-1）

## 何が嘘になったか

hint のコメントはこう書いている:

```go
// a は 3 段の巡回なので「次に押すと何が増えるか」を出す (現在どこまで見えているかはタブ行
// 右端のバッジ ○/○⏸/○⏸✓ が示すので、ここで二重に説明しない)。
```

issue 291 で **epic group の子は状態フィルタの対象外**になった:

```go
// src/glogx/issues/parse.go
func (f StatusFilter) showsIssue(iss *Issue) bool {
	if iss.GroupKind == GroupEpic {
		return true          // ← フィルタに関わらず常に見せる
	}
	return f.shows(iss.Status)
}
```

その結果、バッジが `○`（open のみ）でも、group を展開すると `✓` / `⏸` の行が並ぶ:

```
[next 0] [All 3] [human 0] [feat 3]        ○     ← バッジは ○ だけ
→ ▾ alpha (3 ✓1)
  710 ○ feat  open
  709 ✓ feat  done      ← 既定でも見えている
  708 ⏸ feat  held
```

- **バッジは「どこまで見えているか」を表さなくなった**（表すのは「group の外で」どこまで見えているか）
- `a: +⏸` も **epic の子には何も起きない**（既に見えている）ので、案内としても正確でない

## 対処の前例が同じファイルに在る

`emptyMessage` は番号フィルタのときに同じ理由で `a:` の案内を落としている。
**hint 側も「epic の子が見えている状態か」で出し分ける**のが素直で、前例と揃う。

## やること

1. hint の `a: +⏸` を、epic の子が展開されている文脈では出さない（または語を変える）
2. **コメントを直す**。「バッジが示すので二重に説明しない」は現在の契約と矛盾している
   （[`claude-md-maintenance.md`](../../_claude/rules/claude-md-maintenance.md) の意味的乖離: 根拠が
   仕様変更で崩れた警告はその場で直す）
3. 直したら幅ゲートを通す。issues viewer は全画面ビューアなので `fullScreenCases` 経由で
   自動的に対象になっている（[289](289-refactor-glogx-hintline-two-layer-structure.md) で
   非全画面ぶんもレジストリ駆動にしたが、**viewer 側の hint は意図的に別経路のまま**）

## なぜ 289 に混ぜないか

289 は「hintLine の非全画面の分岐をレジストリへ寄せる」範囲で合意しており、
**全画面ビューア 4 枚の早期 return は意図どおり残す**と決めている
（viewer の hint に前置を混ぜると末尾の抜ける手段が切れる、という文書化された設計判断）。
これは viewer 側の hint の**中身**の問題なので別件として切る。

## 対応 (2026-09-06)

**バッジ側を直した**。`issues.VisibleBadges` を足して、段階が見せているもの (`○` / `○⏸` / `○⏸✓`) の
後ろに**迂回して見えているもの**を括弧で足す:

```
○        段階は open のみ、迂回も無い
○(✓)     段階は open のみ。だが done が見えている (epic の子 / 番号フィルタ)
○⏸(✓)   a を 1 段進めた。pending は段階の側へ移り、done は括弧のまま
○⏸✓     全部見せる段階 (括弧は出ない)
```

- **迂回は 2 つある**: epic group の子 (状態フィルタの対象外。issue 291) と、番号フィルタ (`/`。
  状態を問わず拾う)。後者の嘘は 291 より前から在った
- **括弧の外と中を分けたのは `a` の手応えを残すため**。全部を 1 列に混ぜると、epic の子が既に `✓` を
  出している一覧では `a` を押してもバッジが変わらず「効かなかった」と読める
- **hint (`a: +⏸`) は段階の話に留めた**（やること 1 の「出さない / 語を変える」は採らなかった）。
  `a` は group の外には効くので出さないのは正しくなく、「group には効かない」と書き足す幅が hint 行
  (1 行) に無い。代わりにコメントへ射程を書いた
- やること 2 (コメントの修正) と 3 (幅ゲート) は実施済み。バッジは最大 5 桁 (`○⏸(✓)`) に増えるが、
  `tabLine` は組んだ後に `clipToWidth` する既存の規律に乗っている

変異検証: W1 配線を `v.filter.Badges()` へ戻す → tabLine のテストが red / W2 括弧を外す → 2 本とも red /
W3 段階の skip を外す → 純関数のテストが red。W4 (`StatusNext` / `StatusUnknown` を対象に足す) は
**等価変異** — `shows()` がこの 2 つに常に true を返すのでループ本体へ到達しない (parse.go の `shows`)。

実体: `src/glogx/issues/parse.go` の `VisibleBadges` / `src/glogx/issues_view.go` の `tabLine` /
契約は `docs/issues-viewer-spec.md` 4 節。
