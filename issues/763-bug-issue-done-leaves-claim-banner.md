# 763 (bug): scripts/issue_done.sh が担当者バナーを外さず、done/ に「担当中」が残る

> 🚨 **担当中: claude (opus, dotfiles 762 のセッション)**（2026-10-15〜）

起票日: 2026-10-15

## 概要

glogx の viewer で done へ送ると担当者バナーが外れるが、`scripts/issue_done.sh` は外さない。本文は反証レビュー後に書く。
