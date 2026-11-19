# 1097 (test): devtool の mutate が読む obaket の出力の形と、dotfiles の fixture・テストのずれを検出する

起票日: 2026-11-19

## 概要

mutate が読む obaket の run-logged-command・run-with-test-profile-residue の出力の形が変わっても、dotfiles 側の写しのテストは緑のまま通る。ずれを機械で検出する。
