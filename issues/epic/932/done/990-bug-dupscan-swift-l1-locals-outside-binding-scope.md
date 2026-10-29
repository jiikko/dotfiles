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

- [x] 上の形 (未確定のものを除く) をテストにして赤を確かめてから直す。変異で固定する (scope を出たら宣言を外す処理を消す変異を含む)
- [x] 対照を 2 つ固定する: 局所の名前だけが違う写しは組のまま (974 の `TestL1StillNumbersSwiftRenamedLocals` と、guard の後ろの対照) /
  プロパティを読むだけで読むプロパティが違う写しは組にならない (各形のテストがそれ)
- [x] 変更前後で、Swift の repo (flickorder-client・DualNoteApp) の組の集合と L1 の hash の変わる単位を数えて書く (下の「結果」)。
  減った組は 0 で、目で見る対象は無かった
- [x] 直した範囲に合わせて、`-v` の L1 の行 (`internal/report/compare_hint.go`) と README の断り書きを見直す

## 関連

- [974](974-bug-dupscan-l1-numbers-shadowed-outer-names.md): Go は名前解決、Swift は近似にした。その残り

## 結果 (2026-10-30、dotfiles-31)

commit「fix(dupscan): Swift の L1 で、束縛の届かない所の同じ綴りのプロパティを番号にしない (issue 990)」。

- `markOuter` に scope を入れた。`statements`・`lambda_literal`・for / while・`switch_entry`・`catch_block`・if / guard・入れ子の
  `function_declaration`・局所の型の本体 (`class_body`・`enum_class_body`) を出るとき、中で効いた宣言を外す。if / guard は並びの中の `else` で
  切り替える (if は else から先で外し、guard は else の本体でだけ外して後ろでは効かせる)
- 未確定だった入れ子の func の引数の形も直った (テストに入れた。組になるのは入れ子の関数そのもので、render は組にならない)
- 入れなかったもの: `init_declaration`・`subscript_declaration`・`repeat_while_statement` は、変異で効き目が無い (囲む型の本体・本体の statements で外れる) と分かった
- 前後の比較:
  | repo | 組 (前 → 後) | L1 の hash の変わる単位 |
  |---|---|---|
  | flickorder-client (Swift) | 183 → 183 | 52 / 7777 |
  | DualNoteApp (Swift) | 104 → 104 | 18 / 2225 |
  | dotfiles `src` (Go) | 55 → 55 | 0 / 8976 |
  | pj_energy_matching (Go) | 41 → 41 | (未計測。Go は変えていない) |
  - 所要は前後で差なし (flickorder-client で交互に 5 回ずつ: 0.60 / 0.63 秒、0.54 / 0.51 秒)
  - 測る途中で、974 の Go の hash の数の測り方の誤り (鍵の改行) に気づき、974 の done の issue を訂正した
- テスト: `src/dupscan/l1_swift_scope_test.go` (15 形と guard の対照)。変異 9 本で red
- 反証レビュー (sonnet) と敵対的レビュー 2 周 (opus)。1 周目: 局所の型のメンバー (class_body)・入れ子の関数のテストの穴。
  2 周目: 局所の enum の本体 (enum_class_body)・テストの冒頭のコメント。どれも直した。偽陰性 (正しい写しを落とす形) は作れなかった
- 残る近似: tree-sitter の節の種類で scope を区切る構文の近似。ここに入れていない構文で scope が区切られる所は漏れうる (README に書いた)
