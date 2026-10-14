# 756 (bug): 幅の環境 (RUNEWIDTH_EASTASIAN) の警告を、restartable・schedkeys・ratelimit が起動時に出さない

起票日: 2026-10-14

## 概要

`src/tuikit/widthenv/widthenv.go` のパッケージの doc は「使う側は起動直後に Message で警告を出す」と約束している。
これを守っているのは glogx (`main.go`)・pro-con (`main.go`)・treefiler (`main.go`) の 3 つだけで、どれも同じ if 文を手で書いている。
restartable・schedkeys・ratelimit の本体は `widthenv` を一度も呼ばない (使う側 6 module の非テストのコードで `widthenv.` を grep)。

## 詳細

- 発火条件: `RUNEWIDTH_EASTASIAN=1` のまま restartable を起動する。restartable の `internal/ui/view.go` は `confirm.Box` と `confirm.Dialog` を使う
- 実測 (main が scratchpad の複製で、`confirm.Box("─ t ", {"a"}, 40, false)` の各行を `ansi.StringWidth` で測った):
  - 環境変数なし: 40 / 40 / 40 / 40
  - `RUNEWIDTH_EASTASIAN=1`: 74 / 42 / 78 / 77 (枠が崩れる)
- 警告も compile error も出ないので、利用者には「なぜか枠が崩れる」としか見えない
- schedkeys の本体で幅が 2 になりうる字形 (罫線) は grep で 0 件。ratelimit は `usage/dial.go` に罫線がある。どちらも実害は未確認 (監査体の調べ)
- 経緯: issue 701 の 2 はテスト側 (`TestMain`) を全部に揃え、`tests/scripts/test_widthenv_guarded.sh` がテストの漏れを止めている。本体の警告は pro-con と treefiler に足しただけで、残り 3 つは扱っておらず、理由の記述も無い

## 対応方針

- `widthenv` に警告の関数 (例: `Warn(io.Writer)`) を足し、3 つの写しを置き換える
- restartable・schedkeys・ratelimit の main から呼ぶ。restartable は tuikit を版で固定している (go.mod の擬似版 99b256e39f08) ので、版を上げる必要がある (759 の 1 と一緒に)
- 漏れを止めるなら、`tests/scripts/test_widthenv_guarded.sh` と同じ形で main 側も見る

## 関連ファイル

- `src/tuikit/widthenv/widthenv.go`・`src/{glogx,pro-con,treefiler}/main.go`・`src/restartable/`・`src/schedkeys/`・`src/ratelimit/`・`tests/scripts/test_widthenv_guarded.sh`
- 前回: 701。監査の記録: 760

## 進捗

- [ ] 未着手
