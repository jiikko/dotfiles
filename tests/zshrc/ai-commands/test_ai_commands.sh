#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_ZDOTDIR="$(mktemp -d)"
TMP_HOME="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_ZDOTDIR" "$TMP_HOME"
}
trap cleanup EXIT

cat <<EOF > "$TMP_ZDOTDIR/.zshrc"
source "$ROOT_DIR/_zshrc"
EOF

# mimic expected home layout
ln -s "$ROOT_DIR" "$TMP_HOME/dotfiles"
mkdir -p "$TMP_HOME/.rbenv/bin"
mkdir -p "$TMP_HOME/.rbenv/shims"
mkdir -p "$TMP_HOME/.nodebrew/current/bin"

run_zsh() {
  local cmd="$1"
  HOME="$TMP_HOME" ZDOTDIR="$TMP_ZDOTDIR" zsh -i -c "$cmd"
}

assert_function_exists() {
  local func="$1"
  local message="$2"
  if run_zsh "type $func >/dev/null 2>&1"; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s\n' "$message"
    exit 1
  fi
}

assert_is_function() {
  local func="$1"
  local message="$2"
  local output
  output=$(run_zsh "type $func 2>&1" || echo "")
  if [[ "$output" == *"shell function"* ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s (got: %s)\n' "$message" "$output"
    exit 1
  fi
}

printf '\n=== AI Commands Tests ===\n\n'

# Test 1: _wrap_ai_command_with_tmux 関数が定義されている
printf '## Test 1: _wrap_ai_command_with_tmux function exists\n'
assert_function_exists "_wrap_ai_command_with_tmux" "_wrap_ai_command_with_tmux function is defined"

# Test 2: claude 関数が定義されている
printf '\n## Test 2: claude function exists\n'
assert_is_function "claude" "claude is defined as a function"

# Test 3: gemini 関数が定義されている
printf '\n## Test 3: gemini function exists\n'
assert_is_function "gemini" "gemini is defined as a function"

# Test 4: codex 関数が定義されている
printf '\n## Test 4: codex function exists\n'
assert_is_function "codex" "codex is defined as a function"

# Test 5: Functions are callable (basic smoke test)
printf '\n## Test 5: Functions are callable\n'

# 実際のコマンドが存在する場合のみテスト
if command -v claude >/dev/null 2>&1; then
  if run_zsh "type claude | grep -q function"; then
    printf '✓ claude function is callable\n'
  else
    printf '✗ claude function check failed\n'
    exit 1
  fi
else
  printf '↷ claude command not installed; skipping claude test\n'
fi

if command -v gemini >/dev/null 2>&1; then
  if run_zsh "type gemini | grep -q function"; then
    printf '✓ gemini function is callable\n'
  else
    printf '✗ gemini function check failed\n'
    exit 1
  fi
else
  printf '↷ gemini command not installed; skipping gemini test\n'
fi

if command -v codex >/dev/null 2>&1; then
  if run_zsh "type codex | grep -q function"; then
    printf '✓ codex function is callable\n'
  else
    printf '✗ codex function check failed\n'
    exit 1
  fi
else
  printf '↷ codex command not installed; skipping codex test\n'
fi

printf '\n=== All Tests Completed ===\n'
printf 'All AI command tests passed successfully!\n'
