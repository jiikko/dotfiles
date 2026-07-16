#!/usr/bin/env bash
#
# $HOME / ~/.claude 配下の「~/dotfiles を指す symlink」に dangling (リンク先消失) が
# 無いことを検証する。
#
# なぜ: dotfiles 側でファイルを削除・rename すると、setup.sh を再実行するまで
# ~/.claude 側に壊れたリンクが残り、Claude Code が skill/agent/hook を silent に
# 見失う (issue 001 項目 11)。setup.sh には掃除ロジックがあるが実行時のみ。
# このテストで「壊れたまま使い続けている」状態を make test が検出する。
#
# 対象は readlink が $HOME/dotfiles/ 配下を指すリンクだけ。ユーザーが手動で張った
# 別由来のリンクは対象外 (setup.sh の掃除ロジックと同じ基準)。
# dotfiles 未 symlink の環境 (CI 等) では対象リンクが 0 件になり素通しで pass する。

set -euo pipefail

fail=0

# $HOME 直下 (~/.zshrc 等) と ~/.claude 配下 (agents/skills/... は depth 2)
while IFS= read -r link; do
  target=$(readlink "$link")
  case "$target" in
    "$HOME"/dotfiles/*)
      if [ ! -e "$link" ]; then
        echo "FAIL: dangling symlink $link -> $target (dotfiles 側で削除/移動済み。setup.sh を再実行して掃除)" >&2
        fail=1
      fi
      ;;
  esac
done < <(
  find "$HOME" -maxdepth 1 -type l 2>/dev/null
  [ -d "$HOME/.claude" ] && find "$HOME/.claude" -maxdepth 2 -type l 2>/dev/null
)

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "OK: dotfiles 由来の dangling symlink なし"
