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

## 進捗 (2026-09-19): 完了

### 全数勘定 (「それ以外の tests/ 配下は未調査」への回答)

`tests/` 配下で `setopt err_exit` と `FAIL_COUNT` を両方持つファイルは **2 本だけ**
(`grep -rl err_exit tests/` の全件を `FAIL_COUNT` で絞った結果。`tests/CLAUDE.md` と
`FAIL_COUNT` を持たないテスト本体は対象外):

| ファイル | 状態 |
|---|---|
| `tests/zshrc/av1ify/test_helper.sh` | **既に修正済み** (別セッションが issue 399 で対応。assert 4 つが `bad()` + `return 0`、規範もコメント化済み) |
| `tests/zshrc/concat/test_helper.sh` | **本 issue で修正** (assert 6 つ。`assert_exit_code` / `assert_in_mock_trash` を含む) |

つまり同型は**この 2 本で全部**で、他に取りこぼしは無い。

### concat 側の実態 (本文の「同じ挙動かは未確認」への回答)

同じだった。加えて concat の assert は `bad()` を**一度も呼んでいなかった** (素の `printf` +
`return 1`) ので、`FAIL_COUNT` は常に 0 のまま。rc が非 0 になっていたのは
**err_exit が止めていたから**で、EXIT trap の格上げは効いていなかった。
🚨 false green ではない: 318 箇所の呼び出しを数えたが、条件文脈 (`if` / `||` / `&&`) で
assert を呼んでいる箇所は **0 件**なので、失敗は必ず err_exit に拾われていた。

### 受け入れ条件 (実測)

- [x] 変異で 2 件以上落ちる状態を作り、✗ が全件出て、最後に `失敗 N 件` と rc=1 が出る
      — `test_concat_basic.sh` に「help から `--force` / `--output-info` を消す」変異:
      **✗ 3 件すべて表示 + 完走 (✓ 37 件) + rc=1**
- [x] 失敗 0 件のとき rc=0 のまま — 変異なしで ✓ 40 件 / ✗ 0 件 / rc=0
- [x] concat 側の同型を確認し、同じなので直した

### A-B (直したこと自体の変異検証)

| 版 | 同じ変異での出力 |
|---|---|
| 旧挙動 (`printf` + `return 1` へ戻す) | ✗ **1 件** / ✓ 3 件でファイルごと終了 |
| 新 (`bad()` + `return 0`) | ✗ **3 件** / ✓ 37 件 / 末尾に「失敗 3 件」/ rc=1 |

### 結果

- `tests/zshrc/concat/test_concat_*.sh` **17 本すべて rc=0**（✗ 0 件）
- 「前提が崩れたらそこで止める」ための `|| exit 1` は**足していない**。
  条件文脈での呼び出しが 0 件で、既存の呼び出しはすべて素の assert だったため
  (止めたい箇所が出てきたら、その呼び出し側に明示的に書く。規範はヘッダに記載)

## 残タスク

- なし (全数勘定・受け入れ条件・A-B すべて実測済み)
