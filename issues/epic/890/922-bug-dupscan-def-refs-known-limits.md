# 922 (bug): dupscan の def / refs の分かっている限界を直す

> 🚨 **担当中: M4Pro-MacBook-Pro (glogx)**（2026-10-26〜）

起票日: 2026-10-26

## 概要

921 で README の「分かっている限界」に書いた def / refs の誤り 5 つと、obaket で使って気づいた 1 つを直す。ユーザー指示 (2026-10-26)「dupscan にフィードバックできることがあったらやって」。

## 直すもの (再現)

1. **refs で型の直下の宣言の行が「(単位の外)」と出る**: obaket の `refs StorageRequestStatsRecorder` で `DropboxClient.swift:67 (単位の外) [型の位置] | private let statsRecorder: …`。
   関数の外でも型の中なら、囲む型の名前 (`DropboxClient`) を出す (top-level のときだけ「(単位の外)」)
2. **`def count()` (空のラベル) が関数以外にも当たる**: 括弧を書いたら、関数・初期化子・subscript・protocol の関数の要件だけに当てる (property・case・typealias には当てない)
3. **`var x = 0, y = 0` の 2 つ目以降の名前を def で引けない**: 1 つの property 宣言の束縛を全部、定義として集める
4. **複数行の enum の case が 1 行目しか出ない**: `case foo(\n a: Int\n)` の EndLine を case の終わりまでにする
5. **`if let foo {` (Swift 5.7 の省略形) で外側の foo への参照を数えない**: 省略形は外側の同名の値への参照でもあるので、refs で数える (宣言と参照の両方の性質を持つ。数えて [式の位置] と出す)
6. **parameter pack の宣言 (`<each T>` の T) を refs で参照に数える**: 宣言名として除く

## 受け入れ条件

- [x] 上の 6 つを直し、それぞれをテストで固定する (変異で赤を確かめる)
- [x] report / gate / index / callers の出力と、単位の鍵・Name・ハッシュが変わらない (obaket で変更前後の report の組が同じ)
- [x] README の「分かっている限界」から直したものを消す

## 進捗

- 2026-10-26: codex (gpt-6-astra・medium、codex-drive の書く run) が実装し、Claude が検閲した (commit「fix(dupscan): def / refs の分かっている限界 6 つを直す」)
  - テスト: `src/dupscan/known_limits_test.go` (6 本)。`make lint test` 緑
  - 変異 (規則を外して赤を確認): 型名への fallback → `TestRefsTypeScopeOutsideUnits` / 括弧の種別制限 → `TestDefParenthesesOnlyCallables` /
    全束縛の収集 → `TestDefAllPropertyBindings` / case の終わりの行 → `TestDefMultilineEnumCaseRange` / 省略形の参照 → `TestRefsOptionalBindingShorthand` /
    pack の宣言名の除外 → `TestRefsPackDeclarationExcludedUsesKept`
  - 実装の run が足した `isNameField` の property_declaration の分岐は、外しても全テスト緑だった (2 つ目以降の束縛名は binding の文脈で元から宣言として落ちる)。効果の無い変更なので戻した
  - obaket で変更前 (PATH の dupscan) と後の `report -format json -limit 0 .` は JSON 全体が一致 (238 組)
  - obaket の `refs StorageRequestStatsRecorder` は `DropboxClient.swift:67 DropboxClient [型の位置]` のように型名を出す
- 敵対的レビュー 1 周目 (gpt-6.1-sol・high、読み取りのみ): P2 2 件・P3 2 件。4 件とも作った dupscan で再現した
  - P2 `if let foo: Int = x` と `if case let foo? = x` を省略形と読み、宣言を参照に数えた → 省略形は名前の直後が `,` / `{` / `else` のときだけにした
  - P2 `let (label: a, other: b) = …` の tuple のラベルを定義に数えた → 直後が `:` の名前を飛ばした
  - P3 `case foo(…)\n , bar` の EndLine が次の case の行まで伸びた → 次の `,` の手前の子の終わりで切った
  - 上の 3 つはテストに足し、直しを旧の形へ戻す変異で各テストが赤になることを確かめた
  - P3 `let (a, b): (Int, String)` の def の見出しに型が付かない → 直さず README の限界に書いた。変更前の dupscan は tuple の束縛を def で引けなかった (`def a` が「定義なし」。2026-10-26 に PATH の dupscan で測った) ので、引けるようになった分の改善で、退行ではない。直すなら tuple の要素の位置で型を割り当てる
- 敵対的レビュー 2 周目 (gpt-6.1-sol・high。sandbox で実行できず静的な予測だけ): 3 件。3 件とも作った dupscan で再現した
  - P2 `if let foo /* gap */ {` の省略形を落とした (1 周目の直しの退行。隣の子を見るときにコメントの節を飛ばしていなかった) / P2 `(inner /* gap */: a)` のラベルを定義に数えた /
    P3 `case foo = 1\n /* gap */\n , bar` の EndLine がコメントの行まで伸びた → コメントを飛ばす helper (`isComment` / `nextNonComment`) に 3 か所とも通した。テストに足し、helper を外す変異で赤を確かめた
- 敵対的レビュー 3 周目 (opus のサブエージェント。作った版を実際に動かした): P3 5 件
  - 退行 `let a, b: String` の a の署名から型が消えた → 名前の後ろを走査して最初の型注釈を共有する (`=` が先なら推論なので付けない)。変更前の版の `var g = 0, h: Int` の g に h の型を付ける誤りもこれで直った
  - 退行 `def deinit()` が「定義なし」になった → 括弧つきの問い合わせに deinit も当てる
  - 前からの穴 `if let /* c */ foo {` → 前側もコメントを飛ばす (`prevNonComment`)
  - 言い方 `extension Foo: Bar` の Foo・Bar を Foo の中と出した → 型の囲む単位は本体 (`body`) の子にだけ掛ける
  - 前からの穴 2 つ目以降の束縛の def が宣言の先頭行を指す → 直さず README の限界に書いた
  - 直した 4 つはテストに足し、1 つずつ戻す変異で各テストが赤になることを確かめた
- 敵対的レビュー 4 周目 (opus。直した 4 つに絞った。約 60 入力): 4 つとも壊せなかった。P3 3 件はどれも変更前の版でも同じ結果の前からの穴なので、README の限界に書いた
  (囲む単位を行の範囲で引くので、型の宣言と同じ行に関数があると継承節の参照もその関数の中と出る / 束縛の列の `,` の直前のコメントで 2 つ目以降を引けない / ラベルつき関連値の case と macro を def で引けない)。
  行の範囲の件は Claude も変更前の版で同じ表示になることを確かめた
- 最終: `make lint test` 緑、obaket の `report -format json -limit 0 .` は変更前と JSON 全体が一致 (238 組)

## 評価

- def / refs の 6 つの誤りが直り、敵対的レビューで出た退行 (計 3 件) も直した。report / gate など重複検出の出力は変わらない (obaket で JSON 全体が一致)
- 増えた複雑さ: `isOptionalBindingShorthand` と `boundNames`、コメントを飛ばす helper 2 つ。判定は木の隣の子を見る局所的なもの
- 残る制約は README の「分かっている限界」に 5 行 (tuple の型注釈・2 つ目以降の束縛の行・行の範囲で引く囲む単位・`,` の前のコメント・ラベルつき case と macro)
