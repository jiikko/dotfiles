# 715 (refactor): filer の「どの層が入力を握っているか」の判定が 5 か所に散っている

起票日: 2026-10-12

## 概要

treefiler の filer (`src/treefiler/filer`) には、キーを全部取る層が 4 つある: キー一覧 (`m.help`)・設定の板 (`m.panel.open`)・
検索 (`m.search.active`)・`!` の入力欄 (`m.prompt.active`)。どの層が開いているかの判定を、次の各所が自前の組み合わせ・優先順位で書いている
(master の版。2026-10-12 に確認):

| 箇所 | 書いている判定 |
|---|---|
| `model.go` `OwnsKeys` | 4 つの OR (glogx が横断キーを取らずに filer へ渡すかを決める。spec §0.3) |
| `model.go` `HandleInput` | ctrl+c の例外 (検索・`!` の間だけ)、help → panel → search → prompt の順に振り分け |
| `model.go` `inputField` | search → prompt (足元の入力欄と IME のキャレット) |
| `model.go` `draw` の末尾 | search → prompt → help → panel の順に重ねて描く |
| `shell.go` `Paste` | search → prompt |

🚨 `OwnsKeys` と `HandleInput` は対でなければならない。層を足して `HandleInput` だけを直すと、glogx がその層の間のキーを横断キーとして
先に取ってしまう。これは compile もテストも通る (対を固定しているのは人の注意だけ)。

## 対応方針 (案)

- 今開いている層を 1 つ返す `activeLayer()` (enum。キー一覧 / 設定の板 / 検索 / `!` / なし) を置き、上の 5 か所はそれを読む
  - `OwnsKeys` = `activeLayer() != none`、`HandleInput` / `Paste` / `inputField` は switch で振り分ける (exhaustive の lint で層の足し忘れを止める)
- `draw` の末尾は 712 で動いた所なので、712 が入った後に寄せる
- 良くなる点: 層を足すときに直す場所が 1 つになり、`OwnsKeys` と `HandleInput` の食い違いが構造的に起きなくなる
- 🚨 寄せる前に、2 つの層が同時に開く経路が無いかを確かめる (今の優先順位は「同時には開かない」前提で書かれているように読めるが未確認)

## ついでに見つけたもの (同じ変更で直してよい)

- `inputField` の `line.Window(max(m.w-pw-1, 1))` は 1 桁多く引いている。`tuikit/lineedit.Window` は既にキャレットが欄の中に収まるよう
  `w-1` 桁で切る (doc と実装で確認)。glogx の `url_picker.go` `promptWindow` は `width-頭の幅` を渡している。直すと欄が 1 桁広がる
  (見た目が変わるので、直すなら目で確かめる)

## 関連

- issue 716 (入力欄の「頭 + Window + キャレット」を tuikit へ寄せる案。この issue の `inputField` と同じ所を触る)
- 出典: dotfiles-5f の調査 (2026-10-12、読むだけ)

## 進捗

- 2026-10-12: 起票
