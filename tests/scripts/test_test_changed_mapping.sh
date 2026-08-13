#!/usr/bin/env bash
# scripts/test_changed.sh の写像 (パス → テストターゲット) のゴールデンテスト。
#
# なぜ: 写像の腕は「実在するテストとの対応」を人手で維持しており、テスト側の移動や
# 腕の編集で黙って腐る (issue 060: _claude/skills 等が「テスト対象なし」と誤断定され、
# tests/claude が回らなかった)。ここで代表パスごとに期待ターゲットを固定し、
# 写像が痩せたら red になるようにする。
# 網羅ではなく「腐ると実害が大きい対応」だけを pin する (全列挙は写像と二重管理になる)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TC="$ROOT_DIR/scripts/test_changed.sh"
cd "$ROOT_DIR"

fail=0

# expect <説明> <期待パターン(grep -E)> <パス>...
expect() {
  local desc="$1" pattern="$2"; shift 2
  local out
  if ! out=$("$TC" --dry-run "$@" 2>&1); then
    printf '✗ %s: exit 非 0\n%s\n' "$desc" "$out"; fail=1; return
  fi
  if ! grep -qE "$pattern" <<<"$out"; then
    printf '✗ %s: 期待パターン "%s" が出力に無い\n%s\n' "$desc" "$pattern" "$out"; fail=1; return
  fi
  printf '✓ %s\n' "$desc"
}

# issue 060 で実際に腐っていた対応 (回帰の本丸)
expect "_claude/CLAUDE.md -> tests/claude" 'tests: .*tests/claude' _claude/CLAUDE.md
expect "_claude/skills/*.md -> tests/claude" 'tests: .*tests/claude' _claude/skills/fable/SKILL.md
expect "_claude/rules/*.md -> tests/claude" 'tests: .*tests/claude' _claude/rules/commit-with-pathspec.md
expect "_claude/hooks/*.sh -> shell lint + tests/claude" 'test-shellcheck.*tests: .*tests/claude' _claude/hooks/deny-bare-tmux-kill.sh
expect "_claude/statusline-command.sh -> shell lint + tests/claude" 'test-shellcheck.*tests: .*tests/claude' _claude/statusline-command.sh

# 主要な腕の代表 1 例ずつ
expect "src/<proj> -> go lint+test" 'go: .*src/glogx' src/glogx/tui.go
expect "tests/<dir> -> test-dir + lint-tests" 'test-lint-tests.*tests: .*tests/claude' tests/claude/test_statusline.sh
expect "zshlib -> shell 系" 'test-zshrc' zshlib/_concat.zsh
expect "json -> test-json" 'test-json' _claude/settings.json

# 素通り防止側: 未知パスは fail する / notest は明示報告する
if "$TC" --dry-run path/that/should/never/match.xyz >/dev/null 2>&1; then
  printf '✗ 未知パスが fail しない (素通り防止が壊れている)\n'; fail=1
else
  printf '✓ 未知パスは fail する\n'
fi
out=$("$TC" --dry-run docs/foo.md)
if grep -q "テスト対象なし" <<<"$out"; then
  printf '✓ notest は明示報告される\n'
else
  printf '✗ notest の明示報告が消えている\n%s\n' "$out"; fail=1
fi

# dry-run が「実行していない」ことを明示する契約 (DRY_RUN=0 事故の再発防止と対)
out=$("$TC" --dry-run docs/foo.md)
if grep -q "dry-run: テストは実行していません" <<<"$out"; then
  printf '✓ dry-run の明示メッセージがある\n'
else
  printf '✗ dry-run の明示メッセージが消えている\n'; fail=1
fi

[ "$fail" -eq 0 ] || exit 1
printf '=== test_changed 写像テスト: すべて成功 ===\n'
