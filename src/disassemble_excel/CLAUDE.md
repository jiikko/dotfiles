# disassemble_excel

Excel ワークブック (`.xlsx` / `.xlsm` / `.xlsb`) を diff・grep できるプレーンテキスト群に解体する CLI。使い方・出力レイアウト・仕組み・制限事項の正本は README.md。

## ファイルの地図

- `main.go` — CLI エントリポイント。フラグ解析と全体の処理フロー
- `model.go` — `Cell` などの共有データ型
- `sheets.go` — ワークシート XML を直接パースしてセル (数式・値・型) を読む。共有数式・配列数式は excelize で展開
- `objects.go` — シート上のマクロ割り当てオブジェクト (画像・図形・ボタン) の抽出
- `writer.go` — 出力ファイル (`cells.tsv` / `values.csv` / `manifest.json` / 出力先 README 等) の書き出し
- `defnames.go` — 定義名 (名前付き範囲・LAMBDA 引数) の抽出
- `vba.go` — VBA マクロ抽出のパイプライン (mscfb で OLE2 を開き、`ovba` で解凍・解析)
- `ovba/` — [MS-OVBA] の最小実装 (純 Go)。`Decompress` (CompressedContainer 展開) と `ParseDir` (dir ストリーム解析)。仕様の一次情報は `ovba/decompress.go` のパッケージ doc

## 入口

- `main()` (`main.go`) — `disassemble_excel <file> [options]` として実行 (`go build -o disassemble_excel .` または `go install`)

## ビルド・テスト

- `make -C src/disassemble_excel lint` / `test`

## 詳しくは

- README.md の「機能」「使い方」(フラグ表)「出力レイアウト」「仕組み」「バイナリ形式 `.xlsb` のワークブック」「制限事項」の各節
