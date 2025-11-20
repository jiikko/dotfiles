#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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

assert_command_available() {
  local cmd="$1"
  local message="$2"
  if run_zsh "command -v $cmd >/dev/null 2>&1"; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s\n' "$message"
    exit 1
  fi
}

assert_is_function() {
  local cmd="$1"
  local message="$2"
  local output
  output=$(run_zsh "type $cmd 2>&1" || echo "")
  if [[ "$output" == *"shell function"* ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s (got: %s)\n' "$message" "$output"
    exit 1
  fi
}

printf '\n=== rbenv Lazy Loading Tests ===\n\n'

# Test 1: rbenv コマンドが使える
if command -v rbenv >/dev/null 2>&1; then
  printf '## Test 1: rbenv command availability\n'
  assert_command_available "rbenv" "rbenv command is available"

  # Test 2: rbenv が関数として定義されている（遅延読み込み）
  printf '\n## Test 2: rbenv lazy loading function\n'
  assert_is_function "rbenv" "rbenv is defined as a lazy-loading function"
else
  printf '↷ rbenv not installed; skipping rbenv tests\n'
fi

# Test 3: nodenv コマンドが使える
if command -v nodenv >/dev/null 2>&1; then
  printf '\n## Test 3: nodenv command availability\n'
  assert_command_available "nodenv" "nodenv command is available"

  # Test 4: nodenv が関数として定義されている
  printf '\n## Test 4: nodenv lazy loading function\n'
  assert_is_function "nodenv" "nodenv is defined as a lazy-loading function"
else
  printf '↷ nodenv not installed; skipping nodenv tests\n'
fi

# Test 5: pyenv コマンドが使える
if command -v pyenv >/dev/null 2>&1; then
  printf '\n## Test 5: pyenv command availability\n'
  assert_command_available "pyenv" "pyenv command is available"

  # Test 6: pyenv が関数として定義されている
  printf '\n## Test 6: pyenv lazy loading function\n'
  assert_is_function "pyenv" "pyenv is defined as a lazy-loading function"
else
  printf '↷ pyenv not installed; skipping pyenv tests\n'
fi

# Test 7: anyenv コマンドが使える
if command -v anyenv >/dev/null 2>&1; then
  printf '\n## Test 7: anyenv command availability\n'
  assert_command_available "anyenv" "anyenv command is available"

  # Test 8: anyenv が関数として定義されている
  printf '\n## Test 8: anyenv lazy loading function\n'
  assert_is_function "anyenv" "anyenv is defined as a lazy-loading function"
else
  printf '↷ anyenv not installed; skipping anyenv tests\n'
fi

printf '\n=== All Tests Completed ===\n'
printf 'All version manager tests passed successfully!\n'
