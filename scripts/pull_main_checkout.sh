#!/bin/bash
# 本体の checkout (~/dotfiles) を pull する。同時に pull すると、片方がファイルを書き出している途中をもう片方が見て
# 「untracked working tree files would be overwritten」で止まる (issue 544。2026-09-26 に 2 つの session が 1.2 秒差で pull した)。
# 本体を pull する人 (取り込みの係・worktree の後始末をする session・人) は全員ここを通し、決まった lock で 1 つずつにする。
#
#   scripts/pull_main_checkout.sh            # ~/dotfiles (DOTFILES_DIR があればそこ) を pull --rebase する
#
# 止まったら何もせずに rc と git の出力を返す。🚨 git が案内する `git reset --hard` はしない (他の session の作業を消しうる)。
set -u
main=${DOTFILES_DIR:-$HOME/dotfiles}
lockman=${PULL_MAIN_LOCKMAN:-$main/bin/lockman} # テストは差し替える (bin/lockman は初回にビルドする)
common=$(git -C "$main" rev-parse --path-format=absolute --git-common-dir) || { echo "pull_main_checkout: $main は git の checkout ではない" >&2; exit 2; }
dir="$common/dotfiles-locks/pull-main"
mkdir -p "$dir" || exit 2
"$lockman" with "$dir" --wait "${PULL_MAIN_WAIT:-2m}" --ttl 10m --label "pull $main" -- git -C "$main" pull --rebase "$@"
rc=$?
case $rc in
  0) ;;
  121) echo "pull_main_checkout: ほかの pull が lock を持ったまま (${PULL_MAIN_WAIT:-2m} 待った)。何もしていない" >&2 ;;
  *) echo "pull_main_checkout: pull が rc=$rc で止まった。reset はせず、git status を見てから判断する" >&2 ;;
esac
exit $rc
