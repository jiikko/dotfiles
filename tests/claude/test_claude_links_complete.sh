#!/usr/bin/env bash
#
# ~/dotfiles/_claude/{agents,commands,rules,hooks}/* ・ skills/*/ ・ workflows/*.js が、
# ~/.claude/<dir>/ に 1 対 1 で symlink されていることを検証する (setup.sh の per-file リンク
# 方式の「漏れ」側。test_dangling_symlinks.sh は「消えたのに残る」側)。
#
# なぜ: setup.sh はファイル単位で ln -sfn するため、新しい rule / hook / skill を repo に足しても
# setup.sh を再実行するまで ~/.claude 側に現れず、Claude Code は silent にそれを読まない。
# 2026-08-27 に rules 7 本 (8/21 以降に追加) が未リンクで、CLAUDE.md から参照されているのに
# 一度も読み込まれていなかった。
#
# 対象は「この checkout が ~/dotfiles として setup.sh 済みの環境」だけ。CI 等で ~/dotfiles が
# この checkout でない / ~/.claude/rules が無い場合は検査対象が無いので、skipped を明示して exit 0
# する (無言で pass にしない)。リンク先の一致は -ef (実体の同一性) で見る。
#
# 期待するリンク集合は scripts/claude_links.sh (setup.sh と SessionStart hook が呼ぶ実装) と対応させる
# (agents/commands/rules/hooks = 全ファイル、skills = ディレクトリ、workflows = *.js のみ)。あちらの方針を
# 変えたらここも直す。⚠️ このテストは意図的にあのスクリプトを呼ばない (実装が自分を検査する形にしない)。
#
# ⚠️ hooks だけは「未リンク = 読まれない」が成り立たない。hook は _claude/settings.json の command
# (dotfiles の実体パス) から起動されるので、link が無くても動く (issue 142 の実測)。ここで hooks を
# 検査しているのは link 集合を setup.sh と一致させるためで、hooks の FAIL は機能停止を意味しない。

set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
CLAUDE_HOME="$HOME/.claude"

expected_root="$(cd "$HOME/dotfiles" 2>/dev/null && pwd -P || true)"
if [ "$expected_root" != "$ROOT_DIR" ]; then
  echo "skipped: ~/dotfiles がこの checkout ($ROOT_DIR) ではない (setup.sh 未適用の環境)"
  exit 77
fi
if [ ! -d "$CLAUDE_HOME/rules" ]; then
  echo "skipped: $CLAUDE_HOME/rules が無い (setup.sh 未実行の環境)"
  exit 77
fi

fail=0
checked=0

check_link() {
  # $1 = 期待するリンク, $2 = 実体
  checked=$((checked + 1))
  if [ ! -L "$1" ]; then
    echo "FAIL: $1 が symlink として存在しない (実体: $2)" >&2
    fail=1
  elif [ ! "$1" -ef "$2" ]; then
    echo "FAIL: $1 -> $(readlink "$1") が $2 を指していない" >&2
    fail=1
  fi
}

for dir in agents commands rules hooks; do
  for f in "$ROOT_DIR/_claude/$dir"/*; do
    [ -e "$f" ] || continue
    check_link "$CLAUDE_HOME/$dir/$(basename "$f")" "$f"
  done
done
for d in "$ROOT_DIR/_claude/skills"/*/; do
  [ -d "$d" ] || continue
  check_link "$CLAUDE_HOME/skills/$(basename "$d")" "$d"
done
for f in "$ROOT_DIR/_claude/workflows"/*.js; do
  [ -e "$f" ] || continue
  check_link "$CLAUDE_HOME/workflows/$(basename "$f")" "$f"
done

if [ "$checked" -eq 0 ]; then
  echo "FAIL: 検査対象が 0 件 (_claude/ 配下の発見が壊れている)" >&2
  exit 1
fi
if [ "$fail" -ne 0 ]; then
  echo "→ scripts/claude_links.sh apply (または ./setup.sh) でリンクを張る。通常は次のセッション起動時に" >&2
  echo "   _claude/hooks/claude-links-sync.sh が自動で補う (Claude Code は未リンクのファイルを読まない。" >&2
  echo "   ただし hooks は settings.json の実体パス起動なので link 無しでも動く — issue 142)" >&2
  exit 1
fi
echo "OK: _claude/ の $checked 個が ~/.claude に link 済み"
