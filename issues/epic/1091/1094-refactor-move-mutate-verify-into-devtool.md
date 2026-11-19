# 1094 (refactor): mutate-verify と mutate-verify-list を devtool/bin/ へ移す

> 🚨 **担当中: air5 (glogx)**（2026-11-19〜）

起票日: 2026-11-19

## 概要

1 本のスクリプトにはせず (理由は 1093)、置き場所だけを `devtool/bin/` に揃える。呼び名は `bin/` の symlink で保つ (1092)。機能は変えない。

## 移すときに引きずるもの (2026-11-19 に確認)

- `bin/mutate-verify` は `_self=$(readlink -f …)` の隣から `lib/uint.sh`・`lib/git_location_env.sh` を、
  `_tool_root` (bin の親 = dotfiles の root) から `scripts/lib/worktree_scratch.sh`・`bin/lib/runtimeout.sh` (その先で `bin/lib/go_tool.sh`) を source し、
  `$_tool_root/bin/skill-tmpdir` を起こす。移すと `_tool_root` の意味が変わる
- `bin/mutate-verify-list` は `lib/git_location_env.sh` を source し、隣の `mutate-verify` と `skill-tmpdir` を兄弟のパスで起こす

方針: source する lib (`uint.sh`・`git_location_env.sh`・`worktree_scratch.sh`) は `devtool/lib/` へ移し、dotfiles 側の他の利用者は `devtool/lib/` を読む (dotfiles → devtool の向き)。
runtimeout と skill-tmpdir は 1092 の起動役から受け取る。`_tool_root` は devtool の root を指すように直す。

## 参照

dotfiles の中で `mutate-verify` を名指ししているのは 36 ファイル (2026-11-19、issues/・tmp/ を除く)。
symlink で呼び名を保つので、コマンド名で呼ぶものは変えなくてよい。着手時に `rg 'bin/mutate-verify'` でパスで指すもの (`_claude/hooks/serial-run-wrap.sh`・`tests/claude/test_serial_run_wrap.sh` など) の件数を先に確定し、全部を見る。
テスト `tests/bin/test_mutate_verify.sh`・`test_mutate_verify_list.sh` は `devtool/tests/` へ移す (1092 の発見の拡張が先)。

## 進捗

(未着手。1093 の後)
