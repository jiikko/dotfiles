# test: VS16 ゲートが `parser.ParseDir`（非再帰）で、サブパッケージを黙って対象外にしている

起票日: 2026-09-06
カテゴリ: test
優先度: 中（除外の実態が doc の記述と違い、0 件ガードが構造的に発火しない）

## 何が起きているか

`vs16_literal_test.go:TestOwnStringLiteralsHaveNoVS16` は `go/parser.ParseDir(fset, ".", …)` を使う。
**`ParseDir` は非再帰**なので、走査対象は `src/glogx/*.go` の `package main` **だけ**。
`usage/` `issues/` `termwidth/` `widthenv/` `subproc/` `sgr/` は全て黙って対象外。

doc コメントは「`tools/` は幅そのものを測る道具なので VS16 を書いてよい（対象外）」と書いており、
**除外は `tools/` だけだと読ませる**が、実態は全サブパッケージ。

さらに `len(pkgs) == 0` の 0 件ガードは **`package main` が常に在るため構造的に発火せず**、
走査範囲の縮小を検出できない。

## 実測（変異検証）

- `usage/banner.go` に VS16 入り文字列リテラルを追加 → `go build` OK →
  `TestOwnStringLiteralsHaveNoVS16` は **PASS**（ログ「検査した package 数=1 / VS16 を含むリテラル=0 件」）
- 陽性対照として**同じリテラルを `box.go`（main）に置く**と **FAIL**

## 現時点の実違反

**0 件**（`usage/` 0 件・`issues/` 0 件）。`termwidth/termwidth.go` の 9 件は全てコメント内で、
AST の `BasicLit` 検査には元々かからない。よって**潜在的な穴**。

## 発火条件

- サブパッケージの文字列リテラルに VS16（U+FE0F）が入ったとき、検査が素通しする
- **silent に壊れる**。VS16 は端末によって表示幅が 1 桁 / 2 桁で揺れ、行の右端がフレームごとに動く
  （`src/glogx/CLAUDE.md` が 🚨 を使う理由そのもの）

## 推奨対応

**同 repo 内に正解がある**: `width_test.go:TestNoSecondWidthEngine` は
`filepath.WalkDir` + 各ファイル読みで**再帰**し、`tools/` と `testdata/` を**明示除外**し、
`checked == 0` で fail する。

VS16 ゲートも `WalkDir` + `parser.ParseFile` へ寄せ、除外は `tools/` を**理由つきで明示**、
下限は package 数でなく **ファイル数**で置く。

## 反証の試み

`src/glogx/CLAUDE.md` / `vs16_literal_test.go` の doc / `issues/` と `issues/done/`（136 / 124 / 201）/
テストファイルを探したが、**サブパッケージを意図的に対象外にしたという記述は無い**。
doc は逆に `tools/` **だけ**を除外対象として挙げている。

## 関連

- `width_test.go:TestNoSecondWidthEngine`（同じ repo にある正しい形）
