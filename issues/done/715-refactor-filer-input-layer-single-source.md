# 715 (refactor): filer の「どの層が入力を握っているか」の判定が 5 か所に散っている

起票日: 2026-10-12

> **保留 (pending)**: 層を足す予定が無いので、実際に層を足すときまで凍結する (`~/.claude/rules/verify-design-intent-before-refactor.md` の
> 「実変更 trigger 待ち」)。**再開の trigger**: filer にキーを全部取る層 (5 つ目) を足すとき。そのときこの issue の対応方針から選ぶ。
> 「ついでに見つけたもの」の 1 桁は trigger を待たずに直してよい。

## 概要

treefiler の filer (`src/treefiler/filer`) には、キーを全部取る層が 4 つある: キー一覧 (`m.help`)・設定の板 (`m.panel.open`)・
検索 (`m.search.active`)・`!` の入力欄 (`m.prompt.active`)。どの層が開いているかの判定を、次の各所が自前の組み合わせ・順序で書いている
(master の版。2026-10-12 に確認。4 つのフラグを読む場所は model.go だけで 9 か所あり、組み合わせと順序を自前で書いているのが次の 5 か所):

| 箇所 | 書いている判定 |
|---|---|
| `model.go` `OwnsKeys` | 4 つの OR (glogx が横断キーを取らずに filer へ渡すかを決める。spec §0.3。`Binds` は OwnsKeys 経由で追従する) |
| `model.go` `HandleInput` | ctrl+c の例外 (検索・`!` の間だけ)、help → panel → search → prompt の順に振り分け |
| `model.go` `inputField` | search → prompt (足元の入力欄と IME のキャレット) |
| `model.go` `draw` の末尾 | search → prompt → help → panel の順に重ねて描く |
| `shell.go` `Paste` | search → prompt |

`OwnsKeys` と `HandleInput` は対でなければならない (`HandleInput` だけ直すと、glogx がその層の間のキーを横断キーとして先に取る)。
今ある 4 層は層ごとのテストが対を確かめている (`filer/search_test.go`・`filer/shell_test.go`・`filer/settings_test.go` `TestPanelOwnsKeys`・
glogx `filer_view_test.go` `TestFilerHelpSwallowsCrossKeys`)。素通りするのは「**新しい層を足して、そのテストを書かなかった**」ときだけ。

## 対応方針 (案。trigger が引かれたら選ぶ)

- 案 A (安い): glogx の `overlay_ownership_test.go` (reflect で ownsKeys の実装を数え上げる) と同じ形の網羅テストを filer に足し、
  「入力を握る層のフラグ」と OwnsKeys の対を機械で固定する
- 案 B: 今開いている層を 1 つ返す `activeLayer()` を置き、`OwnsKeys` / `HandleInput` / `Paste` は switch で読む
  - 🚨 1 つの enum では全部を表せない: 「取る順」(help → panel → search → prompt)・「重ねる順」(search → prompt → help → panel)・
    「入力欄か」(search と prompt だけ。ctrl+c の例外と `Binds` の ctrl+c も同じ問い) は別の情報として残る
  - exhaustive の lint は足し忘れを止めない (`src/treefiler/.golangci.yml` は `default-signifies-exhaustive: true` で、`default:` を書けば通る)
- 2 つの層が同時に開く経路は無い (読んで確認。実行はしていない): 層は treeKey からしか開かず、開いている間は HandleInput の先頭で
  その層が全キーを取る。`,` は help を同時に閉じる (`m.panel.open, m.help = true, false`)

## ついでに見つけたもの (trigger を待たずに直してよい)

- `inputField` の `line.Window(max(m.w-pw-1, 1))` は 1 桁多く引いている。`tuikit/lineedit.Window` は既にキャレットが欄の中に収まるよう
  `w-1` 桁で切る (doc と実装で確認)。glogx の `url_picker.go` `promptWindow` は `width-頭の幅` を渡している。直すと欄が 1 桁広がる
  (見た目が変わるので、直すなら目で確かめる)

## 関連

- issue 716 (入力欄の組み立てを寄せる案は見送り。この issue の `inputField` と同じ所)
- issue 712 (`draw` の末尾と model.go を触った)
- 出典: dotfiles-5f の調査 (2026-10-12、読むだけ)

## 進捗

- 2026-10-12: 起票
- 2026-10-12: 反証レビュー (dotfiles-5f、sonnet) を当てた。「直し忘れはテストも通る」を「新しい層のテストを書かなかったとき」に絞り、
  enum で表せない情報・exhaustive の限界・網羅テストの案 A を足し、trigger 待ちとして pending へ。反証できなかった主張: 層は 4 つで漏れが無い・
  OwnsKeys と HandleInput は対であるべき・`m.w-pw-1` は 1 桁多い
- 2026-10-12: issue 712 は done (model.go の draw の末尾も含めて push 済み。この issue の「712 が入った後に寄せる」の前提は満たした)
- 2026-10-12 (dotfiles-4e): trigger が引かれた (issue 722 で確認の板 = 5 つ目の層を足した)。**案 A を入れた**: Model の層の欄に `layer:"…"` の札を付け、
  `src/treefiler/filer/layers_test.go` が札の集合とテストの表を突き合わせ、各層で OwnsKeys / Binds / 木のキーが届かない / ctrl+c の扱いを確かめる
  (OwnsKeys から層を外す変異で red)。札を付けずに足した層は検出しない (CLAUDE.md に書いた)。案 B (activeLayer) は採らなかった。名前の入力は `!` の欄を使い回したので
  入力欄は増えていない。ついでの 1 桁 (`m.w-pw-1`) は見た目が変わるので触っていない
