#!/usr/bin/env bash
#
# _claude/CLAUDE.md の「スキルファイル参照」テーブルと _claude/skills/ の実体を突き合わせる。
#
# なぜ: テーブルは手動メンテのため、スキルの追加・削除と乖離する
# (実例: smoke-test スキル削除後もテーブルに参照が残っていた)。
# 乖離を「読んだ人が気づく」から「テストが落ちる」に格上げする。
#
# 検出する乖離 (両方向):
#   1. テーブルが参照する ~/.claude/skills/<name>/SKILL.md が実在しない (削除残り)
#   2. _claude/skills/ に存在するのにテーブルに載っていないスキル (登録漏れ)
#
# 意図的にテーブルへ載せないスキルができたら EXEMPT_SKILLS に追加すること。

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CLAUDE_MD="$ROOT_DIR/_claude/CLAUDE.md"
SKILLS_DIR="$ROOT_DIR/_claude/skills"

# テーブルに載せない意図的な例外 (スキル名を空白区切りで列挙)
EXEMPT_SKILLS=""

fail=0

# テーブルが参照するスキル名を抽出 (~/.claude/skills/<name>/SKILL.md 形式)
referenced=$(grep -o '~/\.claude/skills/[A-Za-z0-9_-]*/SKILL\.md' "$CLAUDE_MD" \
  | sed 's|.*/skills/||; s|/SKILL\.md||' | sort -u)

if [ -z "$referenced" ]; then
  echo "FAIL: $CLAUDE_MD からスキル参照を 1 件も抽出できない (テーブル形式が変わった?)" >&2
  exit 1
fi

# 方向1: 参照先の実在チェック (削除残り検出)
while IFS= read -r name; do
  if [ ! -f "$SKILLS_DIR/$name/SKILL.md" ]; then
    echo "FAIL: CLAUDE.md が参照する skills/$name/SKILL.md が存在しない (スキル削除後のテーブル更新漏れ)" >&2
    fail=1
  fi
done <<< "$referenced"

# 方向2: 実在スキルの登録チェック (登録漏れ検出)
for dir in "$SKILLS_DIR"/*/; do
  name=$(basename "$dir")
  [ -f "$dir/SKILL.md" ] || continue
  case " $EXEMPT_SKILLS " in *" $name "*) continue ;; esac
  if ! grep -qx "$name" <<< "$referenced"; then
    echo "FAIL: skills/$name が CLAUDE.md のスキルファイル参照テーブルに未登録 (追加するか EXEMPT_SKILLS へ)" >&2
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "[test-skill-trigger-table] OK: 参照 $(wc -l <<< "$referenced" | tr -d ' ') 件すべて実在・実在スキルすべて登録済み"
