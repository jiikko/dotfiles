# 994 (bug): Swift の setter の引数を宣言として扱い、宣言を作る節の一覧を nameRole と 1 つにする

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-30〜）

起票日: 2026-10-30

## 概要

[993](done/993-test-dupscan-swift-scope-kinds-cover-grammar.md) で「守れていないもの」として記録した残りを片付ける。

1. computed property の `set(v)` の v が宣言 (引数) として扱われず、引数の名前だけが違う setter の写しが L1 の組にならない
2. 宣言を作る節の一覧 (`scope_kinds_test.go` の `declMakers`) が、`swift.go` の `nameRole` と結ばれていない (足し忘れても緑)
3. 局所の関数名・局所の型名・enum の case が宣言として扱われない。広く hash が変わりうるので、影響を測ってから決める

## 受け入れ条件

- [x] 1 を直し、テストと変異で固定する
- [x] 2 を、nameRole と同じ表から確かめる形にする
- [x] 3 の影響を測り、採るかを決めて書く

## 結果 (2026-10-30、dotfiles-31)

commit「fix(dupscan): Swift の setter の引数を局所の名前にし、宣言を作る節の表を nameRole と 1 つにする (issue 994)」。

- 1: `set(v)` の v を引数にした (`declParentKinds` に `computed_setter`)。setter を scope にして、引数が後ろの getter へ漏れないようにした。
  テストは「引数の名前だけ違う setter は組になる」「setter を先に書き、getter が同じ綴りの別のプロパティを読む写しは組にならない」の 2 本
- 2: nameRole が直下の名前を引数にする親の節を `declParentKinds` にまとめ、`declMakers` がそれを全部含むことをテストで見る。
  条件つきで宣言を作る親 (capture_list_item・pattern) は nameRole の switch にあるので、テストに手で並べた
- 3: 局所の関数名だけ試した (`function_declaration` の name を宣言にする試しの build)。入れ子の関数名は番号になったが、
  flickorder-client 183 組・DualNoteApp 104 組とも組の集合は変わらず、利点 (名前だけを変えた局所の関数の写しを拾う) が観測できない。
  **採らない**。局所の型名は型の位置の名前 (`type_identifier`) の扱いから、enum の case は `.a` のメンバーの参照と結ぶ所から変える必要があり、
  効果が見えない今は着手しない。再開の条件: 名前だけを変えた局所の関数・局所の型の写しを、実際の課題で見逃したと分かったとき
- 993 の版と比べて、Swift の 2 repo の組の集合は同じ。変異 4 本で red。敵対的レビュー 1 周 (opus) は退行を見つけず、
  setter の scope を振る舞いで守るテストの穴を指摘したので足した

### 前からの挙動 (記録。この issue では直さない)

- setter の引数を書くか省くか (`set(newValue)` と `set`) で、同じ本体の写しが組にならない (省略形の newValue は暗黙で宣言が無い)
- 局所の名前は単位全体で綴りごとに番号を振るので、getter の `let x` と setter の引数が同じ綴りの写しと、違う綴りの写しは組にならない
