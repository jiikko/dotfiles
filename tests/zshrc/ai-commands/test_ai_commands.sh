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

# Test 1: gemini 関数が定義されている (brew auto-install wrapper)
printf '## Test 1: gemini function exists\n'
assert_is_function "gemini" "gemini is defined as a function"

# Test 2: codex 関数が定義されている (brew auto-install wrapper)
printf '\n## Test 2: codex function exists\n'
assert_is_function "codex" "codex is defined as a function"

# Test 3: claude はコマンドとして存在するか確認 (ラッパー関数は不要になった)
printf '\n## Test 3: claude command check\n'
if run_zsh "command -v claude >/dev/null 2>&1"; then
  printf '✓ claude command is available\n'
else
  printf '↷ claude command not installed; skipping\n'
fi

# Test 4: tmux window name の YAML に AI コマンドが定義されている
printf '\n## Test 4: AI commands defined in tmux-window-name.yaml\n'
yaml_file="$ROOT_DIR/tmux-window-name.yaml"
if grep -q "^claude:" "$yaml_file"; then
  printf '✓ claude is defined in YAML\n'
else
  printf '✗ claude is not defined in YAML\n'
  exit 1
fi

if grep -q "^gemini:" "$yaml_file"; then
  printf '✓ gemini is defined in YAML\n'
else
  printf '✗ gemini is not defined in YAML\n'
  exit 1
fi

if grep -q "^codex:" "$yaml_file"; then
  printf '✓ codex is defined in YAML\n'
else
  printf '✗ codex is not defined in YAML\n'
  exit 1
fi

printf '\n=== All Tests Completed ===\n'
printf 'All AI command tests passed successfully!\n'
