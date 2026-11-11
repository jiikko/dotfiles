# 1067 (bug): setup.sh が ~/.claude の外 (~/.config・$HOME 直下) の dotfiles のリンク切れを掃除しない

起票日: 2026-11-11

## 概要

`tests/claude/test_dangling_symlinks.sh` が見つけるリンク切れのうち、`~/.claude` の外のものは `./setup.sh` を回しても消えない (本文は反証レビューの後に書く)。
