# 537 (ux): issue の一覧で、既にカードになっている issue にその旨を出す

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-27): 「pro-con で issue 一覧を見ているときに、すでにカードとして追加済みなら、その旨を一覧に書いて欲しい」。
今は画面の issue の一覧 (`i`。`ui/picker.go`) に、その issue を担当しているカードが出ないので、同じ issue を二重に積みうる。

## 作り

- カードは紐づいた issue を `card.Card.Issues` (`card.IssueRef`: repo + 番号) に持つ。一覧の各行 (`pickRow`) の issue と、Snapshot のカードの `Issues` を突き合わせる
  (画面が既に持っている Snapshot だけで足りる。外を読みに行かない)
- 行に出すもの: 紐づいたカードの ID と列 (例: `C-081 作業中`)。複数あれば全部。完了のカードは記録 (画面の Snapshot) に在る間 (完了から 24 時間か、x で片付けて書庫へ移るまで) だけ、薄く `[完了 C-xxx]`
  (書庫 = 完了から 1 週間 (497) までは読まない。「外を読みに行かない」を優先。2026-09-27 のユーザーの決定)
- epic の見出し行は、子の issue のどれかにカードがあれば数を出すか (見本で決める)
- 既にカードがある issue を Enter で選んだら、そのまま積む前に「C-081 が担当中。それでも足すか」を確かめる (二重に積まないため。y/N)
- 見た目 (印・色・位置) は見本を出して人に選んでもらう

## 注意

- 紐づけは PM の `card plan --issue` の時点か、511 の後は issue の一覧から足した時点で付く。それより前に紐づけずに作られたカードは突き合わせられない (題名の番号から推すことはしない)

## 受け入れ条件

- [x] 一覧の issue の行に、紐づいたカードの ID と列が出る (完了は薄く)
- [x] カードがある issue を選ぶと、足す前に確かめる
- [x] 見た目は人が見本から選んだもの

## 関連ファイル

- `src/pro-con/ui/picker.go` (`pickRow` / `loadPicker`) / `src/pro-con/card/card.go` (`IssueRef`) / 511 (足した時点で issue を紐づける)

## 進捗

- 2026-09-27 (C-090): 見本 (`src/pro-con/samples/537-picker-cards/`) の 3 案からユーザーが選んだ形で実装した
  - 行: 案 C = 番号と題名の間に、列の色の地に黒字の札 `C-081 作業中` (複数なら並べる)。完了は薄く `[完了 C-xxx]`。epic の親の issue にも出す
  - epic の見出し: `(未完了の子 4 · カードあり 3)` (完了でないカードがある子の数)
  - 確かめ: 案 X = 一覧の下の 1 行 `#537 には既にカード C-090 (作業中) がある。それでも足す? y/N`。**y だけで足し、Enter は取り消し**
    (選んだ Enter の 2 度押しで越えないため。docs/glogx-ui-guide.md §8)。epic は子にカードがあるときも確かめる
  - 突き合わせは `setSnap` で片付けたカードを落とす前の Snapshot から作る (`ui/picker.go` の `issueLinks`)。削除中のカードは出さない
  - テスト: `ui/picker_test.go` の `TestPickerShowsLinkedCards` / `TestPickerAsksBeforeAddingLinkedIssue` / `TestPickerAsksForEpicWithLinkedChildren`
