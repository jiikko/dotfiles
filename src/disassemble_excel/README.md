# disassemble_excel

Excel ワークブック（`.xlsx` / `.xlsm`）を、diff しやすいプレーンテキスト群の
ディレクトリに**解体（分解）**するツール。各シートを 1 セル 1 行の TSV（数式
**と**キャッシュ値の両方）、定義名、VBA マクロのソース（モジュールごとに 1 ファイル）
として書き出す。

バージョン管理上でスプレッドシートのロジックを読み・**diff** するため、そして
数式＋マクロを持つ重い Excel モデルを実コードへ移植するために作られた。

## なぜ

スプレッドシートは XML の ZIP アーカイブ＋マクロ用 OLE2 バイナリの塊なので、
そのままでは diff も grep もできない。`disassemble_excel` は 1 つのワークブックを、
`diff` / `grep` / レビューがそのまま効く安定した行指向テキストに変換する:

- **セルごとに数式 *と* 値。** 数式がロジック、キャッシュ値が期待される出力。
  両方を並べて出力する。
- **共有数式を展開する。** ワークブックには、生 XML 上では数式テキストが空の
  共有数式の子セルが数万個含まれることがある。各セルの実際の数式を解決するので、
  どのセルが何を計算しているかが見える。
- **VBA マクロを純 Go で解凍。** `olevba` も Python も外部ツールも不要。モジュール
  ソースは `vbaProject.bin` から直接取り出す。
- **完全オフライン。** `.xlsx` / `.xlsm` の解体は入力ファイルの読み取りと出力
  ファイルの書き込みだけ — ネットワークもサブプロセスも使わない。（唯一の例外が
  `.xlsb` 入力で、これは LibreOffice のサブプロセスでまず `.xlsm` に変換する。
  後述の *バイナリ形式 `.xlsb` のワークブック* を参照。）

## 機能

- シート → `sheets/<name>.cells.tsv` — `cell  type  formula  value`、(行, 列) で
  ソート、空セルは省略。
- シート → `sheets/<name>.values.csv` — 目視確認用のキャッシュ値のプレーンなグリッド。
- 巨大シート → `sheets/<name>.slim.tsv` — 数式セルのみ＋値だけのセルの要約
  （セル数のしきい値を超えたシート向け）。
- VBA → `vba/<module>.bas` / `.cls`、モジュールごとに 1 ファイル、加えて
  `vba_index.tsv`（module / proc / kind / line）で各 `Sub`/`Function` へ辿れる。
- 定義名（名前付き範囲・LAMBDA 引数）→ `defined_names.tsv`。
- マクロ割り当てオブジェクト → `objects.tsv` — シート上の画像・図形・ボタンに登録され、
  クリック時に実行されるマクロ。列は `sheet  object  kind  anchor  macro`。
  画像自体（見た目）は復元しないが、「クリックで何が走るか」は分かる。
- `manifest.json` — 元ファイルの SHA-256、シートごとの寸法とカウント、モジュール一覧、
  マクロ割り当てオブジェクト一覧。
- `README.md` — 出力ディレクトリに書き出される人間向けの来歴ファイル:
  元ワークブックの絶対パス、SHA-256、出力された各ファイルの意味を記す。
- 共有文字列は実テキストに解決される。非 ASCII（例: 日本語。プロジェクトのコード
  ページ経由）はセル文字列・VBA ソースの両方でデコードされる。

## インストール

Go 1.23 以上が必要。

最新版を直接インストール:

```sh
go install github.com/jiikko/disassemble_excel@latest
```

またはソースからビルド:

```sh
git clone https://github.com/jiikko/disassemble_excel
cd disassemble_excel
go build -o disassemble_excel .
# 必要なら ./disassemble_excel を PATH に移動する
```

初回ビルドで Go モジュールの依存をダウンロードする。以降は完全オフラインで
ビルド・実行できる。

## 使い方

```sh
disassemble_excel <file.xlsx|.xlsm|.xlsb> [options]
# フラグはファイル引数の前後どちらに置いてもよい
```

| フラグ | 意味 |
|------|---------|
| `-o DIR` | 出力先ディレクトリ（既定: 拡張子を除いたファイル名） |
| `-sheets a,b,c` | 指定したシートだけを抽出（既定: 全シート） |
| `-max-cells N` | セル数が N を超えるシートには `.slim.tsv` も出力（既定 100000、0 で無効） |
| `-no-grid` | グリッドの `values.csv` を出力しない（`cells.tsv` は常に出力） |
| `-no-vba` | VBA 抽出をしない |
| `-f` | 出力先ディレクトリが既存でも上書きする |

実行例:

```sh
# すべてを ./report/ に抽出する
disassemble_excel report.xlsm

# 出力先を指定し、2 シートだけ抽出する
disassemble_excel report.xlsm -o out/report -sheets Summary,Data

# 複数ファイルを 1 ファイル 1 プロセスで処理する
for f in *.xlsm; do disassemble_excel "$f" -o "out/${f%.xlsm}" -f; done
```

## 出力レイアウト

```
<out>/
  README.md                      # 来歴: 元ファイルのパス・SHA-256・各ファイルの意味
  manifest.json
  defined_names.tsv              # name  scope  refers_to
  sheets/
    Summary.cells.tsv            # cell  type  formula  value   （1 セル 1 行）
    Summary.values.csv           # キャッシュ値のグリッド
    Data.cells.tsv
    Data.values.csv
    Big.cells.tsv                # 巨大シートはフルダンプを保持しつつ…
    Big.slim.tsv                 # …数式のみの圧縮ビューも追加
  vba/
    Module1.bas                  # 標準モジュール
    ThisWorkbook.cls             # クラス/ドキュメントモジュール
  vba_index.tsv                  # module  proc  kind  module_line
  objects.tsv                    # sheet  object  kind  anchor  macro（マクロ割り当てオブジェクト）
```

`cells.tsv` の例:

```
cell	type	formula	value
A1	n		1
E3	n	=Inputs!C2	46054
J12	n	=SUMIF($D$20:$II$20,J11,$D$59:$II$59)/SUMIF($D$20:$II$20,J11,$D$42:$II$42)	8.71798
C1	s		Start date
```

value はワークブックに保存されたキャッシュ値。値の中のタブ・改行は各セルが
1 行に収まるようエスケープされる。

## 仕組み

- **セル・値・型・インライン数式** はワークシート XML を直接パースして読む
  （高速・シングルパス・低メモリ）。
- **共有数式・配列数式** は [excelize](https://github.com/xuri/excelize) で展開する。
  展開が必要なのは共有数式の子セルだけで、値だけのセルには不要。
- **VBA** は純 Go のパイプラインで抽出する。[mscfb](https://github.com/richardlehane/mscfb)
  が OLE2 コンテナを開き、同梱の `ovba` パッケージが [MS-OVBA] の解凍と
  `dir` ストリームの解析を実装する。モジュールソースはプロジェクトのコードページで
  デコードする。

## バイナリ形式 `.xlsb` のワークブック

`.xlsb`（Excel バイナリブック）は BIFF12 の**バイナリ**レコードを収めた ZIP
コンテナであり、このツールが読む OOXML（ZIP＋XML）パーツではないため、直接は
解体できない。[LibreOffice](https://www.libreoffice.org/)（`soffice`）が `PATH`
または macOS の既定の場所
（`/Applications/LibreOffice.app/Contents/MacOS/soffice`）に見つかれば、ツールは
まず `.xlsb` を `.xlsm` に変換する — マクロを残すため *Calc MS Excel 2007 VBA*
エクスポートフィルタを使う — その上で結果を解体する。変換後のファイルは一時
ディレクトリに置かれ、処理後に削除される。`manifest.json` には元の `.xlsb` の
ファイル名と SHA-256 を記録する。

`soffice` が未インストールの場合は、インストール方法
（`brew install --cask libreoffice`）または手動変換の手順を表示する。VBA を
落とさないため `.xlsx` ではなく `.xlsm` に変換すること。

## 制限事項

- 数式は**抽出するだけで評価（再計算）はしない**。ツールは数式テキストと Excel が
  最後にキャッシュした値を報告するだけで、何も再計算しない。
- 書式・チャート・ピボットテーブル・条件付き書式、および画像・図形の見た目（ピクセルや
  描画）は抽出しない（焦点はプレゼンテーションではなくロジック）。ただし画像・図形・ボタンに
  **割り当てられたマクロ**は `objects.tsv` に抽出する（クリック時に実行されるマクロ）。
- グループ化された図形（`grpSp`）の内部にある子オブジェクトに個別にマクロが割り当てられて
  いる場合は拾わない（グループ自体に割り当てられたマクロは拾う）。フォームコントロールの
  ボタン（VML）は仕様準拠で実装しているが、実ファイルでの検証は未実施。
- ワークブックはメモリに読み込む。~70 MB のマクロ付きワークブックでおよそ 1 GB の
  RAM を要し、数秒で処理する。

## ライセンス

[MIT](LICENSE)

## 謝辞

- [xuri/excelize](https://github.com/xuri/excelize)
- [richardlehane/mscfb](https://github.com/richardlehane/mscfb)
- VBA 抽出は [MS-OVBA] 仕様に従う。

[MS-OVBA]: https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-ovba/
