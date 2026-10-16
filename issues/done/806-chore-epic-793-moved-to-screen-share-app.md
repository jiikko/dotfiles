# 806 (chore): epic 793 (793〜806) は repo screen-share-app へ移した

起票日: 2026-10-16

## 概要

macOS 専用の画面共有クライアントの epic 793 (親 793 と子 794〜806 の 14 本) を、ユーザーの指示 (2026-10-16「dotfiles の epic issues も移動して」) で
private repo `jiikko/screen-share-app` (`~/src/screen-share-app`) の `issues/` 直下へ、番号を変えずに移した。以後の正本はそちら。

## この issue が在る理由

dotfiles の採番は「issues/ 配下の最大の番号 + 1」で決まる。793〜806 を抜くと最大が 792 に戻り、793 以降が再利用される。
番号は再利用しない規約 (`_claude/issue-rules.md`) を守るため、使用済みの最大の番号 806 でこの目印を残す。
**dotfiles の 793〜806 は screen-share-app の issue を指す** (dotfiles の中で 793〜806 を参照するときは「screen-share-app 793」と書く)。

## 移した先

- screen-share-app の commit「issues: dotfiles の epic 793 (793〜806) をそのまま移す」(中身を変えずに移した) と、その次の注記の commit
- 移す直前の dotfiles の状態は、この commit の親の `issues/epic/793/`
