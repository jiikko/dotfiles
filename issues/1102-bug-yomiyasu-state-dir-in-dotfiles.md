# 1102 (bug): 状態フォルダを渡さずに check を dotfiles の中で動かすと ~/dotfiles/.yomiyasu ができる

起票日: 2026-11-21

## 概要

ja-writing の check (と本家の yomiyasu_check.py) を状態フォルダの指定なしで dotfiles の中から動かすと、git の最上位の `~/dotfiles/.yomiyasu` に記録を書く。本文は後で書く。
