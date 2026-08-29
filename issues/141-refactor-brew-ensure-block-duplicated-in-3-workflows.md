# 141 refactor: brew の依存導入ブロックが 3 つの workflow に重複している

起票日: 2026-08-29 / 種別: refactor / 優先度: P3 / 出典: 自分の後始末

## 事実

CI を macOS へ移した (issue 133) とき、「存在を確かめ → 無いものだけ brew で入れ → 最後に
もう一度確かめる」というブロックを **3 箇所に書いた**:

- `.github/workflows/tests.yml` (CI_COMMANDS_* を読む版)
- `.github/workflows/lint.yml` (zsh / jq / ruby / yamllint)
- `.github/workflows/bench.yml` (tmux / zsh)

加えて「brew の bash 5 を PATH 先頭に出す」ブロックも tests.yml / lint.yml /
`.github/actions/run-bench/action.yml` の 3 箇所にある。

CLAUDE.md「コード変更時の自律改善」は**同じ変更を 2 箇所にコピペするのを禁止**しており、
これは規約違反を自分で作った形。

## なぜ直すか (行数ではなく複雑性で)

単なる行数の重複ではなく、**変更時の touch 箇所が 3 になっている**のが問題。実際に想定される
変更: formula 名の写像を足す (`bats → bats-core` / `gtimeout → coreutils` が既にある) /
検証の仕方を変える / brew の環境変数を足す (`HOMEBREW_NO_AUTO_UPDATE` 等、issue 133 の
敵対レビュー R3 で未対応として残っている)。今はどれも 3 箇所を直す必要がある。

## 対応案

`.github/actions/` に composite を 1 本切り出す (`run-bench` が既にこの形)。入力は
「必要なコマンドの空白区切り」だけにし、**formula 名の写像も composite 側に集約**する。

## ⚠️ 注意

- bash 5 の導入は `run-bench` composite に既に入っている。**二重に入れない**
- `tests.yml` だけは対象コマンドを Makefile (`CI_COMMANDS_*`) から sed で読む step を持つ。
  そこは残し、**導入部分だけ**を共通化する (出典を Makefile に保つ設計は issue 073 の結論)

## 受け入れ条件

- [ ] formula 名の写像が 1 箇所になる
- [ ] 3 つの workflow すべてが CI で緑
- [ ] **実行された証拠**を確認する (composite の出力が各 job のログに出ること。
      `verify-execution-not-just-exit-code.md`)
