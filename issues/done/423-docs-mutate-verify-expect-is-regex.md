# 423 (docs): bin/mutate-verify の --expect が正規表現だと usage に書かれていない

起票日: 2026-09-24

## 概要

`bin/mutate-verify` の `--expect` / `--baseline-expect` は `grep -E` として照合される。
`ctrl+h` のようなテスト名の `+` が量指定子になり、照合に失敗する (2026-09-24 に pro-con の変異検証で rc=7 を踏んだ)。
スクリプト冒頭の使い方にはこのことが書かれていない。

## 対応方針

- 照合は ERE のまま残す (usage の例が `^ok ` のアンカーを使っており、固定文字列に倒すと表現力が落ちる)
- usage に「ERE で照合する。`+` `.` `(` はエスケープ」を書く
- 正規表現としては外れ、固定文字列 (`grep -F`) としては出力に在るとき、rc=7 (`--expect`) / rc=3 (`--baseline-expect`)
  の失敗メッセージでエスケープ漏れを疑うよう案内する (`hint_fixed_match`)

## 関連ファイル

- `bin/mutate-verify`

## 進捗

- [x] 起票
- [x] usage 追記と案内 (fix(mutate-verify): --expect が正規表現であることを usage に書き、エスケープ漏れを案内する)
  - `tests/bin/test_mutate_verify.sh` の case 5b: `(got=accepted)` の括弧をエスケープし忘れた `--expect` で rc=7 + 案内。
    case 5 (本当に別の検査が落ちた) では案内を出さないことも固定
  - 変異: `--expect` 側の `hint_fixed_match` 呼び出しを消すと case 5b が red (巻き添えなし)
  - 未検証: `--baseline-expect` 側の案内はテストが無い (fixture の出力にメタ文字が無く、固定文字列では一致・正規表現では不一致の形を作れない)
