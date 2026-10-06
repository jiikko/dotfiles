# 643 (chore): 時間の上限付き実行を runtimeout 以外で新しく書いたら lint で落とす

> 🚨 **担当中: dotfiles-4d**（2026-10-07〜）

起票日: 2026-10-07

## 概要

issue 640 で、上限付きの実行 (coreutils の `timeout` / `gtimeout`、背景起動 + kill の手組み) を `bin/runtimeout` に寄せた。
新しく同じ形を書かれるのを止めているのは CLAUDE.md の 1 行だけで、機械の検査が無い。
`make test-lint` に入る検査 `scripts/check_handrolled_timeout.sh` を足す。

## 対応方針

- 検出する形 (脅威モデル: うっかり昔の書き方で書くこと。意図的な迂回は対象外):
  - `timeout` / `gtimeout` をコマンドとして呼ぶ・`command -v` で探す (macOS に無く、無いと黙って上限なしになる。bench_zsh.sh がこの形だった)
  - `( sleep N; kill … ) &` の見張り (scripts/check_assert_reaches_exit.sh の旧 run_probe の形。pid の再利用で別のプロセスを撃つ)
- 検出しない形はスクリプトの冒頭に書く。意図的な例外は行内の `handrolled-timeout: allow` (理由を添える)
- 対象 0 件・発見の失敗は緑にしない (既存の check_pipefail_grep_q.sh と同じ規律)。fixture のテストで検出することを固定する

## 関連ファイル

- `scripts/check_handrolled_timeout.sh` (新設) / `tests/scripts/test_check_handrolled_timeout.sh` (新設) / `Makefile` (test-lint)
- issue 640

## 進捗

- [ ] 検査の実装と test-lint への配線
- [ ] fixture のテストと変異
