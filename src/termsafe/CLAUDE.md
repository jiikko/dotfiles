# termsafe

外部由来の文字列を端末へ出す前に無害化する単一の関門。`glogx` と `doctor` が replace で取り込む共有 module。使い方・関数の使い分け・通し忘れ検査の一次情報は README.md、仕様・トレードオフは `termsafe.go` のパッケージ doc。

## ファイルの地図

- `termsafe.go` — 唯一の実装ファイル。無害化関数群 (`DetailLine` / `LineKeepTabs` / `PlainLine` / `PlainLineKeepTabs` / `PlainBlock` / `IsPlain` / `DropEmojiVS16`) と内部の `sanitize` / `needsSanitize`
- `ctlprobe/` — **テスト専用**の独立オラクル (「制御文字が残っているか」を termsafe 実装とは別系統で判定する。production からの import 禁止で、import cycle により構造的に強制)
- `termsafe_bench_test.go` — ベンチマーク

## 入口

- 表の形で使い分けが決まっている (README.md「使い分け」表を見る)。迷ったら `PlainLine` が既定
- 消費者: `glogx` (TUI 描画)・`doctor` (`bin/diskdoctor` / `bin/svcdoctor` が stdout へ直接書く)

## ビルド・テスト

- `make -C src/termsafe lint` / `test`

## 詳しくは

- README.md 全体 (なぜ独立 module か・使い分け表・通し忘れを機械で止める仕組み・検出しない形・型で持つ案を採らなかった理由)
