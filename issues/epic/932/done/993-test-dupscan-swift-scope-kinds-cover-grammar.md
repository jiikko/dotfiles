# 993 (test): Swift の scope の節の種類を、文法の全部の節と突き合わせて固定する

起票日: 2026-10-30

## 概要

990 で Swift の `markOuter` (`src/dupscan/internal/lang/swift/outer.go`) に scope を入れたが、その節の種類の一覧 (`scopeKinds`) は
レビューで見つけた形から積み上げたもので、文法の節を網羅した保証が無かった。tree-sitter-swift の node-types.json の節を全部判定し、
新しい節の種類が黙って素通りしないテストにする。

## 受け入れ条件

- [x] 中に宣言を持ちうる節を文法から全部挙げ、scope / 素通り / 関数の外に分類する
- [x] 分類をテストにする (分類の無い節・scopeKinds との食い違いで赤)
- [x] 突き合わせで見つかった取りこぼしを直す (振る舞いのテストと変異で固定)

## 結果 (2026-10-30、dotfiles-31)

commit「test(dupscan): Swift の scope の節の種類を、文法の全部の節と突き合わせて固定する (issue 993)」。

- 中に宣言を持ちうる節は 79 種類。式の節 (中の closure しか宣言を持たない) を除いて、全部を `scope_kinds_test.go` の
  `scopeKindDecisions` で分類した。分類の無い節が出るか、表と `scopeKinds` が食い違うと赤
- 見つかった取りこぼし (直した): 局所の変数の didSet / willSet の引数が外へ漏れる・局所の型の init / subscript の引数が同じ型の後ろのメソッドへ
  漏れる (990 で「効き目が無い」と外したのは、型の外で使う形しか試していなかったため)・宣言を作る節の一覧に capture_list_item と
  lambda_function_type_parameters が無かった
- 990 の版と比べて、flickorder-client 183 組・DualNoteApp 104 組とも組の集合は同じ
- 変異 6 本で red。敵対的レビュー 1 周 (opus)

### 守れていないもの (記録)

- 表が守るのは「宣言を持つ節が分類されていること」まで。分類の正しさ (scope にすべき節を素通りにしていないか) は守らない。
  表で scope を pass に変え、scopeKinds からも消すと緑のまま。意味の正しさは `l1_swift_scope_test.go` の振る舞いのテストに頼る
- `declMakers` は手で書いた一覧で、`swift.go` の `nameRole` が RoleDecl を返す親の節と結んでいない (足し忘れると緑のまま)。
  一覧のコメントに、nameRole に足したらここにも足すと書いた
- 式の節を分類から免除するのは「中の closure しか宣言を持たない」ときだけ。if / switch の式は、if_statement が scope・
  switch_statement が switch_entry 経由でしか宣言を持たない間は無害
- 前からの挙動 (この issue の範囲外): computed property の `set(v)` の v・局所の関数名・局所の型名・enum の case は RoleDecl にならず、
  名前を変えただけの写しを取りこぼす (番号にならない側。誤検知ではなく見逃しの向き)
