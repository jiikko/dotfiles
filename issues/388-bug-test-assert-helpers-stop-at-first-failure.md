# av1ify / concat のテストで assert_* が最初の ✗ でスクリプトを終了させ、残りの失敗が見えない

## 症状

`tests/zshrc/av1ify/test_av1ify_audio.sh` に既定値を戻す変異を当てたところ、出た ✗ は
`Exactly at threshold copies` の 1 件だけで rc=1 で終わった。後ろに並ぶ
`Default margin copies 128000bps source` などは**実行されず**、同じ変異で落ちるはずの失敗が見えなかった
(実測 2026-09-17、コミット 70d2c9f3 の変異検証中)。

## 原因

- `tests/zshrc/av1ify/test_helper.sh` は冒頭で `setopt err_exit` している
- 同じファイルの `assert_file_exists` / `assert_file_not_exists` / `assert_contains` /
  `assert_not_contains` の 4 つは、失敗すると `✗` を出して **`return 1`** を返す
- テスト本体は assert をむき出しで呼ぶので、`return 1` が err_exit に拾われて**その場でスクリプトが終わる**

同じヘルパーのコメント (`bad()` の上) は「落ちた assert を全部出すため、最初の 1 件で止めない」と書いており、
**意図と実装が食い違っている**。`bad()` と `FAIL_COUNT` の仕組み (issue 327 / 329) はあるのに、
assert_* がそれを使っていない。

rc は非 0 になるので false green ではない。困るのは次の 2 点:

- 失敗が複数あるとき 1 件ずつしか見えず、直して回し直すことを繰り返す
- 変異検証で「どのテストが red になるか」を一度に読めない。後ろのテストは実行されず、
  red にも green にもならない

## 同型の箇所

- `tests/zshrc/concat/test_helper.sh` も `setopt err_exit` と `bad()` / `FAIL_COUNT` を持ち、assert_*
  (`assert_exit_code` を含む 5 つ) に `return 1` がある。同じ挙動かは**未確認** (grep で形が一致しただけ)
- それ以外の `tests/` 配下は未調査

## 対応方針 (案)

- assert_* の失敗経路を `bad` でカウントして `return 0` にし、rc への格上げは既存の EXIT trap (`cleanup`) に任せる
- 「前提が崩れたらそこで止める」ための呼び出しがあれば、`|| exit 1` を付けて明示する
  (現状は err_exit に頼って暗黙に止まっているので、付け替える前に呼び出し箇所を洗う)
- 受け入れ条件:
  - [ ] 変異で 2 件以上落ちる状態を作り、✗ が全件出て、最後に `失敗 N 件` と rc=1 が出る
  - [ ] 失敗 0 件のとき rc=0 のまま (格上げが誤って効かない)
  - [ ] concat 側の同型を確認し、同じなら同時に直す
