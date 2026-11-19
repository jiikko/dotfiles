# 1091 (epic): devtool — 別の repo へ切り出す前提で、開発の道具を dotfiles/devtool/ に置く

起票日: 2026-11-19

## 概要

repo や CI から呼ばれる開発の道具を、この Mac の設定 (zsh・tmux・nvim・Claude) と分けて `devtool/` に置く。
いずれ別の repo (仮称 toolbox) へ切り出す。今すぐ分けないのは、参照の張り替えが大きいから (ユーザー決定 2026-11-19)。
それまでの間、`devtool/` は「`devtool/` の外を参照しない」形を保つ。

**機能は今と変えない**。各 repo の呼び口は wrapper として残し、判定の本体を devtool の 1 か所にまとめる (ユーザー決定 2026-11-19)。

## 子 issue

- [1092](done/1092-chore-devtool-dir-and-boundary-check.md): 置き場・PATH・境界の検査 (最初にやる)
- [1093](done/1093-refactor-unify-app-mutate-into-devtool.md): app の `bin/mutate` の統合 (最初に入る道具)
- [1094](done/1094-refactor-move-mutate-verify-into-devtool.md): `mutate-verify` / `mutate-verify-list` の移設
- [1095](pending/1095-task-split-devtool-into-own-repo.md): 別の repo への切り出し (pending)

## 反証レビュー (2026-11-19、観点を分けた read-only のサブエージェント 2 体: 事実 / 計画の穴)

採った指摘は 1092〜1094 の本文に反映済み (起票の時点の本文は骨組みだけで、レビューは本文を commit する前に通した):

- runtimeout は PATH のコマンドではなく source する lib で、バイナリを 1 回だけ解決している → 1092 の起動役で解決して渡す
- devtool/tests は `make test`・bats・lint・test_changed の発見に入らない → 1092 で足し、出力で確かめる
- 境界の検査が `readlink -f` / `_tool_root` 経由の参照・`shellcheck source=`・symlink を見ない → 1092 の止める形に足し、変数で組んだ形は「検出しない形」と明記
- obaket の `run-logged-command-test` が `bin/mutate --summarize` と fixture を実行時に使う → wrapper を残すので呼び出しは通る。fixture の読み先と契約の持ち主を 1093 に書いた
- obaket の reap を「在れば呼ぶ」に弱めると、helper が消えたときに黙る → wrapper から明示して渡す
- PATH は `_zshrc` にしか無く非対話のシェルは読まない → PATH に頼らず、wrapper が絶対パスで起動役を起こす
- 61 行は diff の変更行、fixture は `bin/testdata/mutate/` → 訂正

壊せなかった (記録): `mutate` の名前の衝突は無い (`which -a mutate`)、mutate の `$root` は対象 repo の root なので置き場を移しても変わらない、app の CI に mutate を回す job は無い。
