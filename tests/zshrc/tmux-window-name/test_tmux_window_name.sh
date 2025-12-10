#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
YAML_FILE="$ROOT_DIR/tmux-window-name.yaml"
ZSH_LIB="$ROOT_DIR/zshlib/_tmux_window_name.zsh"

TMP_ZDOTDIR="$(mktemp -d)"
TMP_HOME="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_ZDOTDIR" "$TMP_HOME"
}
trap cleanup EXIT

# mimic expected home layout
ln -s "$ROOT_DIR" "$TMP_HOME/dotfiles"
mkdir -p "$TMP_HOME/.rbenv/bin"
mkdir -p "$TMP_HOME/.rbenv/shims"
mkdir -p "$TMP_HOME/.nodebrew/current/bin"

cat <<EOF > "$TMP_ZDOTDIR/.zshrc"
source "$ROOT_DIR/_zshrc"
EOF

run_zsh() {
  local cmd="$1"
  HOME="$TMP_HOME" ZDOTDIR="$TMP_ZDOTDIR" zsh -i -c "$cmd" 2>/dev/null
}

assert_equals() {
  local expected="$1"
  local actual="$2"
  local message="$3"
  if [[ "$expected" == "$actual" ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s\n  expected: "%s"\n  actual:   "%s"\n' "$message" "$expected" "$actual"
    exit 1
  fi
}

assert_contains() {
  local expected="$1"
  local actual="$2"
  local message="$3"
  if [[ "$actual" == *"$expected"* ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s\n  expected to contain: "%s"\n  actual: "%s"\n' "$message" "$expected" "$actual"
    exit 1
  fi
}

assert_file_exists() {
  local file="$1"
  local message="$2"
  if [[ -f "$file" ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s\n' "$message"
    exit 1
  fi
}

printf '\n=== Tmux Window Name Tests ===\n\n'

# Test 1: YAML ファイルが存在する
printf '## Test 1: YAML file exists\n'
assert_file_exists "$YAML_FILE" "tmux-window-name.yaml exists"

# Test 2: zsh ライブラリが存在する
printf '\n## Test 2: Zsh library exists\n'
assert_file_exists "$ZSH_LIB" "_tmux_window_name.zsh exists"

# Test 3: YAML に必須エントリが含まれている
printf '\n## Test 3: YAML contains required entries\n'
assert_contains "zsh:" "$(cat "$YAML_FILE")" "YAML contains zsh entry"
assert_contains "nvim:" "$(cat "$YAML_FILE")" "YAML contains nvim entry"
assert_contains "brew:" "$(cat "$YAML_FILE")" "YAML contains brew entry"
assert_contains "claude:" "$(cat "$YAML_FILE")" "YAML contains claude entry"
assert_contains "_default:" "$(cat "$YAML_FILE")" "YAML contains _default entry"

# Test 4: _tmux_get_display_name 関数が定義されている
printf '\n## Test 4: _tmux_get_display_name function exists\n'
result=$(run_zsh "type _tmux_get_display_name" || echo "not found")
assert_contains "function" "$result" "_tmux_get_display_name is a function"

# Test 5: _tmux_get_display_name が YAML からコマンドを取得できる
printf '\n## Test 5: _tmux_get_display_name resolves commands from YAML\n'

# nvim のテスト
result=$(run_zsh '_tmux_get_display_name nvim')
assert_contains "nvim" "$result" "_tmux_get_display_name returns nvim entry"

# brew のテスト
result=$(run_zsh '_tmux_get_display_name brew')
assert_contains "brew" "$result" "_tmux_get_display_name returns brew entry"

# claude のテスト
result=$(run_zsh '_tmux_get_display_name claude')
assert_contains "claude" "$result" "_tmux_get_display_name returns claude entry"

# Test 6: _default が未定義コマンドに適用される
printf '\n## Test 6: _default is applied to unknown commands\n'
result=$(run_zsh '_tmux_get_display_name unknowncommand123')
assert_contains "unknowncommand123" "$result" "_tmux_get_display_name returns unknown command with default icon"

# Test 7: preexec/precmd フックが登録されている (TMUX 環境外ではスキップ)
printf '\n## Test 7: Hooks are defined\n'
result=$(run_zsh "type _tmux_preexec" || echo "not found")
if [[ "$result" == *"function"* ]]; then
  printf '✓ _tmux_preexec is defined\n'
else
  printf '↷ _tmux_preexec not defined (expected outside TMUX)\n'
fi

result=$(run_zsh "type _tmux_precmd" || echo "not found")
if [[ "$result" == *"function"* ]]; then
  printf '✓ _tmux_precmd is defined\n'
else
  printf '↷ _tmux_precmd not defined (expected outside TMUX)\n'
fi

# Test 8: エイリアス展開のテスト
printf '\n## Test 8: Alias resolution in preexec\n'
# v -> nvim のエイリアスが定義されているかテスト
result=$(run_zsh "alias v" || echo "")
if [[ "$result" == *"nvim"* ]]; then
  printf '✓ alias v=nvim is defined\n'
else
  printf '✗ alias v=nvim is not defined\n'
  exit 1
fi

# Test 9: コマンド抽出がプレフィックスや代入をスキップする
printf '\n## Test 9: Command extraction skips wrappers\n'
result=$(run_zsh '_tmux_extract_command "sudo env FOO=1 /usr/bin/nvim file.txt"')
assert_equals "nvim" "$result" "_tmux_extract_command strips sudo/env/assignments"

result=$(run_zsh '_tmux_extract_command "FOO=bar brew install fzf"')
assert_equals "brew" "$result" "_tmux_extract_command ignores leading assignments"

result=$(run_zsh '_tmux_resolve_alias v')
assert_equals "nvim" "$result" "_tmux_resolve_alias returns aliased command"

printf '\n=== All Tests Completed ===\n'
printf 'All tmux window name tests passed successfully!\n'
