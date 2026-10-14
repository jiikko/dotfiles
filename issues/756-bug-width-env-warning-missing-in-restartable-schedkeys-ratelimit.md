# 756 (bug): restartable が幅の環境 (RUNEWIDTH_EASTASIAN) の警告を実行時に出さない (README に書いた既知の制約。P3)

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
- 経緯: issue 701 の 2 はテスト側 (`TestMain`) を全部に揃え、`tests/scripts/test_widthenv_guarded.sh` がテストの漏れを止めている。本体の警告は pro-con と treefiler に足した
- 🚨 反証レビューの訂正: restartable はこの制約を README の「検出しない形 / 注意」に書いている (「`RUNEWIDTH_EASTASIAN=1` … 進捗板や最下行が端末幅を超える場合があります」)。
  issue 588 の進捗にも「記録のみ」とある。なのでこれは「今壊れている」ではなく、**文書化した既知の制約を実行時の警告へ格上げするか**の判断 (P3)
- schedkeys と ratelimit は対象にしない: schedkeys の本体には曖昧幅の字が 0 件 (コメント以外。レビューが検出の手段を先に較正して確認)。
  ratelimit の本体が呼ぶのは `Fetch` / `FetchCodex` / `RenderLine` だけで、罫線を持つ `RenderDashboard` は glogx が呼ぶ (glogx は警告済み)。
  bin/ratelimit は hook と mod から毎プロンプト呼ばれる CLI なので、stderr へ警告を出すと hook の経路を汚す

## 対応方針

- restartable の main で `widthenv.EastAsianAmbiguous` を見て `widthenv.Message` を出す (固定版 99b256e39f08 にも両方ある。警告だけなら版上げは不要)。
  🚨 alt screen の TUI では stderr の警告は終了後にしか見えない (glogx の main のコメント)。pro-con のように画面の中へ出す形 (`m.Notify`) を検討する
- 写しの共通化 (例: `Warn(io.Writer)`) で置き換えられるのは stderr へ出す glogx と treefiler の 2 つだけ (pro-con は画面の中の通知)。効果は小さい

## 関連ファイル

- `src/tuikit/widthenv/widthenv.go`・`src/{glogx,pro-con,treefiler}/main.go`・`src/restartable/`・`src/schedkeys/`・`src/ratelimit/`・`tests/scripts/test_widthenv_guarded.sh`
- 前回: 701・588。監査の記録: 760

## 進捗

- [ ] 未着手
