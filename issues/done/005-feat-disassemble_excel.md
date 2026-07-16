# disassemble_excel: Excel(.xlsm) のシート/数式/VBA を抽出して書き出す

起票日: 2026-06-08
完了日: 2026-06-13 — Phase 1/2 実装済み・テストパス。Phase 3（diff サブコマンド）は任意のため未実装のまま完了とする。
**出力形式・CLI の正は `src/disassemble_excel/README.md`**（実装が issue 起票時の案から拡張されている: `.cells.tsv` / `.values.csv` / `.slim.tsv` の3形態 + `vba_index.tsv`）。

`.xlsm`（Excel + マクロ）を分解し、**シートのセル（数式・値）と VBA マクロのソースコード**を
diff しやすいテキスト群として書き出す個人内部ツール。Go 実装。`~/dotfiles/bin/disassemble_excel` から起動する。

> ※ 当面の主用途はあるレガシー Excel モデルを Go へ移植するための解析だが、
> このツール自体は汎用の Excel 分解器として dotfiles に置く。**解析対象のプロジェクトリポジトリにはコミットしない。**

## 背景 / モチベーション

- 移植対象の `*.xlsm` は同一構造のワークブックが多数（バリアント × バージョン）あり、各 ~70MB、実シート数十枚規模
- 本体の計算ロジックは**巨大シートのセル数式**に埋まっている（VBA は `Application.Calculate` を叩くだけのオーケストレーションで、計算式は持たない）
- これを Go に移植したい。そのために必要なのは:
  1. **バリアント間 diff** — 「複数バリアントで何が同じで、どのセルだけがバリアント固有の入力か」が分かれば、「共通ロジック1本 + バリアント別パラメータ」に落とす根拠になる
  2. **バージョン間 diff** — 更新が来るたびに、何が変わったかを追える
  3. **数式 + キャッシュ値の両方** — formula=ロジック、value=期待値。Go 移植の照合に両方要る
- 今は Excel を手で開いて unzip / olevba で都度抜いている。**新バージョンが来るたびに繰り返す常用ツール**にしたい

## ゴール / 非ゴール

### ゴール
- `.xlsm` / `.xlsx` 1 ファイルを食って、シート・数式・値・定義名・VBA ソースを**安定した順序のテキスト**で書き出す
- 出力が `git diff` / `diff` でそのまま比較できる（バリアント間・バージョン間）
- 新バージョンが来たら 1 コマンドで再抽出できる（再実行耐性・エラー処理を持つ）

### 非ゴール
- 数式の**評価（再計算）はしない**。あくまで「書き出す」だけ。モンテカルロの再現は移植側（Go 本体）の仕事
- Excel の書式・スタイル・チャート・ピボットは抽出しない（ロジック移植に不要）
- バッチ実行・並列実行はツールに持たせない。**1 ファイル 1 起動**に徹する（複数ファイルは呼び出し側のシェルで回す）

---

## 実行したら何が出るか（成果物）

（起票時の案は削除。実装で `.cells.tsv` / `.values.csv` / `.slim.tsv` + `vba_index.tsv` に拡張されたため、出力レイアウトとサンプルは `src/disassemble_excel/README.md` の「Output layout」を参照）

---

## 抽出スコープ（確定）

| 対象 | 抽出するもの | 手段 |
|------|------------|------|
| ワークシート | 各セルの (アドレス, 型, 数式, キャッシュ値) | excelize |
| 定義名 | LAMBDA 引数・名前付き範囲 (name, scope, refers_to) | excelize |
| VBA マクロ | モジュール単位のソースコード | mscfb + 自前 MS-OVBA 解凍 |

## 置き場所・配線

dotfiles 既存の Go ツールと**同じ型**にする（`bin/` の薄い zsh ラッパーが `src/` 配下のバイナリを自動ビルドして `exec` する。ラッパー実装は既存の `bin/parallel-each` を踏襲）。

```
~/dotfiles/
  bin/disassemble_excel           # zsh ラッパー（初回 go build / ソースが新しければ再ビルド → exec）
  src/disassemble_excel/
    go.mod                        # module disassemble_excel （dotfiles root には go.mod を置かない）
    .gitignore                    # /disassemble_excel （ビルド済みバイナリを除外）
    main.go                       # CLI・フラグ・オーケストレーションのみ
    ...（下記パッケージ構成）
```

- **依存は `src/disassemble_excel/go.mod` に閉じ込める**。dotfiles root も解析対象プロジェクトも汚さない
- ビルド済みバイナリは `src/disassemble_excel/disassemble_excel` に置き、`.gitignore` で除外（= ソースだけコミット対象）

### 依存ライブラリ
- `github.com/xuri/excelize/v2` — シート・数式・値・定義名・`GetVBAProject()`(生 vbaProject.bin)。文字列の sharedStrings 解決もこれが担う
- `github.com/richardlehane/mscfb` — OLE2/CFB コンテナ（vbaProject.bin の中身）の読み取り
- MS-OVBA 解凍 + `VBA/dir` 解析は**自前実装**（Go に turnkey な olevba 相当は無い）

## 内部設計（Godファイル回避・責務分離）

```
src/disassemble_excel/
  main.go        # CLI / フラグ / 1ファイル処理のオーケストレーションのみ
  model.go       # Cell / Sheet / Module / Manifest 構造体
  sheets.go      # SheetExtractor: excelize で (cell, type, formula, value) を吐く
  defnames.go    # DefinedNameExtractor: 定義名
  vba.go         # VBAExtractor: GetVBAProject → mscfb → ovba で各モジュールのソースを取り出す
  ovba/          # MS-OVBA の純アルゴリズム（外部依存なし・単体テスト可能）
    decompress.go  #   CompressedContainer の解凍（[MS-OVBA] 2.4.1）
    dir.go         #   VBA/dir の解凍 + MODULE レコード解析（offset/種別/名前/コードページ）
  writer.go      # シリアライザ（tsv / json / bas）。抽出と出力を分離
```

- 2 つの抽出器（シート系 / VBA）は互いに独立。片方が落ちても他方は出せる設計
- `ovba/` は I/O も excelize も知らない純ロジックにして、`[MS-OVBA]` 仕様のテストベクタで単体テストする

### VBA 抽出の処理フロー（[MS-OVBA] 準拠）
1. `excelize.GetVBAProject()` で生の `vbaProject.bin`（OLE2/CFB）を取得
2. `mscfb` で CFB を開き、`VBA/dir` ストリームと各モジュールストリームを得る
3. `VBA/dir` を MS-OVBA 解凍 → `MODULE` レコード群を解析:
   - `MODULENAME` / `MODULESTREAMNAME`（ストリーム名）
   - `MODULEOFFSET`（= TextOffset。モジュールストリーム内でソースが始まるバイト位置）
   - `MODULETYPE`（標準 / クラス）
   - `PROJECTCODEPAGE`（モジュール名・ストリーム名の MBCS 解釈に必要。日本語名対策）
4. 各モジュールストリームの `[TextOffset:]` を MS-OVBA 解凍 → VBA ソース文字列
5. コードページに従って文字列をデコードし `vba/<name>.<ext>` に書き出す

## CLI 仕様

（起票時の案は削除。確定したフラグ一覧は `src/disassemble_excel/README.md` の「Usage」を参照。1 ファイル 1 起動・1 プロセス方針は維持）

## 技術的リスクと Phase 0 検証（推測で確定しない）

実装本格化の前に、**実物 1 ファイル**で以下を計測してから設計を確定する。

1. **excelize で数式がスケールして取れるか**
   - `GetCellFormula` は in-memory モデル依存。10MB+ XML のシート 1 枚でメモリ/時間を実測
   - **shared formula**（共有数式）が、子セルで参照のまま残るか展開されるかを確認。
     展開されないなら自前で展開する（移植には各セルの実数式が要る）
2. **~70MB × ファイル単位のメモリピーク**
   - excelize は zip 全体をメモリ展開する。1 ファイル処理時のピーク RSS を実測し、
     1 ファイル 1 プロセス方針で問題ないか確認
3. **VBA 解凍の edge case**
   - 「解凍コア ~100 行」は CompressedContainer 部分だけ。`VBA/dir` のレコード解析は型が多く、
     コードページ（日本語モジュール名）・モジュール種別判定に edge case がある
   - 実物 1 ファイルで `dir` を解析しきって、手抽出済みの VBA リファレンスと
     付き合わせて正しさを確認する

## 実装フェーズ計画

- **Phase 0（spike / 使い捨て）**: 実物 1 ファイルで上記 3 点を検証。excelize の数式取得と VBA 解凍が
  実際に通ることを確認してから本実装に入る
- **Phase 1（本命）**: シート/数式/値 + 定義名の抽出と TSV/JSON 出力。`bin/` ラッパー配線。再実行耐性
- **Phase 2（VBA）**: `ovba/` 実装 + VBA ソース書き出し。手抽出済みリファレンスと突き合わせて検証
- **Phase 3（任意）**: `diff` サブコマンド（バリアント間/バージョン間の差分レポート）

## 決定事項

- **コマンド名**: `disassemble_excel` で確定（「解体＝部品にバラす」ニュアンス。区切りは underscore、末尾 `_excel`）。
  ソースディレクトリ `src/disassemble_excel/`、go.mod の module 名も `disassemble_excel`、bin ラッパーは `bin/disassemble_excel`
