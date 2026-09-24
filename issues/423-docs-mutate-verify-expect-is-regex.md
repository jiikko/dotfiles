# 423 (docs): bin/mutate-verify の --expect が正規表現だと usage に書かれていない

起票日: 2026-09-24

## 概要

`bin/mutate-verify` の `--expect` / `--baseline-expect` は `grep -E` として照合される。
`ctrl+h` のようなテスト名の `+` が量指定子になり、照合に失敗する (2026-09-24 に pro-con の変異検証で rc=7 を踏んだ)。
スクリプト冒頭の使い方にはこのことが書かれていない。

## 対応方針

(調査後に書く)

## 関連ファイル

- `bin/mutate-verify`

## 進捗

- [ ] 起票
