# 990 (bug): Swift の L1 が、束縛の届かない所の同じ綴りの外側の名前を番号にする

起票日: 2026-10-30

> 反証レビュー: 2026-10-30 に通した (sonnet)。書き漏れの 4 形・scope の境界の候補・方針の事実の誤り (平らなのは conv でなく木) を反映した。

## 概要

974 で、局所の名前と同じ綴りの外側の名前 (プロパティ) を L1 で番号にしないようにした。Go は parser の名前解決で正確に分けるが、
Swift は名前解決が無いので近似 (`src/dupscan/internal/lang/swift/outer.go` の `markOuter`) にした。この近似は scope を見ず、
「その位置までに一度でも効いた宣言」を以後ずっと局所と読む。そのため、束縛の届かない所で同じ綴りのプロパティを読む 2 つの写しが、
読むプロパティが違っても L1 の組になる (誤検知の向き。`explain -compare` も差 0 を返す)。

## 詳細 (974 の敵対的レビューと 990 の反証レビューで、974 の版で L1 の組になると確かめた形)

いずれも「2 つの class でプロパティ X だけが違い (user / account)、ほかは同じ」写しで、L1 の組になる。

- closure の引数を、closure の外で使う: `let f = { (X: String) in show(X) }; f(X ?? "")` (後ろの X はプロパティ)
- for の変数を、ループの後で使う: `for X in items { … }; save(X)`
- guard let の束縛を、else の中で使う: `guard let X = w else { show(X); return }` (else の中の X はプロパティ)
- if let の束縛を、else の中で使う: `if let X = w { … } else { show(X) }`
- if の条件の束縛を、if の後ろで使う: `if let X = w { … }; show(X)`
- block の中で宣言した綴りを、block の後ろ (兄弟の block・do の block の後ろを含む) で使う
- while let の束縛を、ループの後で使う: `while let X = items.first { show(X); break }; show(X ?? "")`
- switch の `case let X?` の束縛を、switch の後で使う
- `catch let X` の束縛を、catch の後で使う
- else if の連鎖の末尾の else で使う: `if let X { … } else if let Y { … } else { show(X) }`
- 未確定: 入れ子の func の引数 X を、その func の外で使う形 (組は出たが、組になったのが内側の func の単位で、外側の本体の組かは確かめていない)

`-v` の L1 の行と README の「分かっている限界」に、この残りを断り書きとして書いてある (974)。

## 対応方針 (候補)

- `markOuter` に scope の stack を持たせる。宣言は、それを囲む scope (block・closure・for の本体・if の then) の中だけで効かせ、
  scope を出たら外す。guard の束縛は、else を除く、囲む block の残りで効かせる
- tree-sitter の木の節の種類 (`statements`・`lambda_literal`・`for_statement`・`while_statement`・`if_statement`・`guard_statement`・
  switch の case・catch の block・入れ子の `function_declaration`) から scope の境界を決める。tree-sitter-swift の木は if / guard の条件・
  then / else を同じ並びに平らに置く (conv はそのまま写すだけ。974 で `,` の括弧の深さを数えたのと同じ事情) ので、then / else の
  位置を `{` `}` `else` の token と、else if の入れ子の if_statement で見分ける必要がある (木のダンプは未確認)
- 直すと、Swift の L1 の hash が変わる単位がまた出る (974 の README の除外の節の付け直し方で足りるか、件数を測る)

## 受け入れ条件

- [ ] 上の形 (未確定のものを除く) をテストにして赤を確かめてから直す。変異で固定する (scope を出たら宣言を外す処理を消す変異を含む)
- [ ] 対照を 2 つ固定する: 局所の名前だけが違う写しは組のまま / 局所を使わずプロパティを読むだけの写しは、読むプロパティが違えば組にならない
- [ ] 変更前後で、Swift の repo (flickorder-client・DualNoteApp) の組の集合と L1 の hash の変わる単位を数えて書く。減った組は数件を目で見て、
  本当に別のプロパティを読む誤検知だったかを確かめる
- [ ] 直した範囲に合わせて、`-v` の L1 の行 (`internal/report/compare_hint.go`) と README の断り書きを見直す

## 関連

- [974](done/974-bug-dupscan-l1-numbers-shadowed-outer-names.md): Go は名前解決、Swift は近似にした。その残り
