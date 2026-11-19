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

- [x] `mutate-verify`・`mutate-verify-list` を `devtool/bin/` へ移した (git mv)。`bin/` には `devtool-run` を `"$BASH"` で起こす呼び口を置いた (名前で呼ぶ 36 ファイルは変えずに済む)
- [x] lib は **方針を変えた**: `uint.sh`・`git_location_env.sh`・`worktree_scratch.sh` の実体を `devtool/lib/` へ移し、元の `bin/lib/`・`scripts/lib/` に symlink を置いた。
  利用者 (15 / 21 / 5 ファイル) を書き換えずに済み、向きは dotfiles → devtool のまま。`runtimeout.sh` は Go の `src/runtimeout` に依存するので移さず、本体は `DEVTOOL_RUNTIMEOUT` を使う
- [x] 本体は devtool の root を `${bin のディレクトリ%/bin}` で決める (`..` を使わない)。skill-tmpdir は `DEVTOOL_SKILL_TMPDIR`、無ければ旧と同じく `$TMPDIR` へ
- [x] テスト 2 本を `tests/devtool/` へ移した (中身は変えず、呼び口を起こす)。`test_mutate_verify.sh` は移す前の HEAD でも移した後でも 51 ケース。CI の `src_codexevents.yml` の paths filter に `devtool/lib/uint.sh` を足した
- [x] devtool-run を直した: runtimeout の解決はサブシェルで git の場所の変数を外して行う (外せなければ解決しない。道具には継承した値と、`RUNTIMEOUT` の無い環境を渡す)。
  shebang が `#!/bin/bash` / `#!/usr/bin/env bash` の道具は `"$BASH"` で起こす (呼び口が選んだ bash を本体まで保つ)。app の呼び口も `"$BASH"` で起こす。
  変異: `"$BASH"` を外す → bash の選択のケースが RED (dotfiles・vgp)、git の場所の変数を外さない → RED

### codex の敵対的レビュー (2026-11-19、2 周。gpt-6.2-sol)

- 1 周目: 不正な `GIT_DIR` を継承すると冷えた checkout で runtimeout のビルドが落ちる・`bash5 bin/mutate-verify` でも本体が /bin/bash で走る・`RUNTIMEOUT` が LIST に漏れる・CI の paths filter が旧パスだけ → 直した
- 2 周目: 外せないのに解決する・`head` が無いと選択が落ちる・bash のテストが区別できていない → 直した (この修正は 3 周目に通していない)
- 受け入れ (記録): `--help` や引数の誤りでも runtimeout の解決が走る (ビルド済みなら何もしない)。`DEVTOOL_*` が LIST・検証コマンドに見える。本体を devtool-run を通さずに起こすと判定不能
  (README の約束)。別名の symlink で診断の名前が `mutate-verify` になる (rc は同じ)。`core.symlinks=false` の checkout では symlink の lib が読めない (macOS だけの dotfiles では使わない)。
  list が既定の mutate-verify を直接起こし bash の選択が落ちるのは、移す前と同じ挙動。継承した `GIT_DIR` で別の checkout を対象にするのも、app の旧 `bin/mutate` と同じ挙動
