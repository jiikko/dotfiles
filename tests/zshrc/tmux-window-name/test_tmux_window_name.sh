#!/usr/bin/env bash
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
YAML_FILE="$ROOT_DIR/zshlib/tmux-window-name.yaml"
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

# Test 8: エイリアスは展開せずタイプ名のまま表示する (2d68f3c の意図的仕様)。
# v=nvim が定義されていても window 名は "v" のまま (resolve しない) ことを確認する。
printf '\n## Test 8: Aliases are shown as-typed (not resolved)\n'
result=$(run_zsh "alias v" || echo "")
if [[ "$result" == *"nvim"* ]]; then
  printf '✓ alias v=nvim is defined\n'
else
  printf '✗ alias v=nvim is not defined\n'
  exit 1
fi
# preexec 経路 (extract → display) が alias 'v' を展開せず "v" を保つこと
result=$(run_zsh '_tmux_get_display_name "$(_tmux_extract_command "v foo.txt")"')
assert_contains "v" "$result" "alias 'v' is displayed as-is, not expanded to nvim"
if [[ "$result" == *"nvim"* ]]; then
  printf '✗ alias was unexpectedly expanded to nvim\n'
  exit 1
fi

# Test 9: コマンド抽出がプレフィックスや代入をスキップする
printf '\n## Test 9: Command extraction skips wrappers\n'
result=$(run_zsh '_tmux_extract_command "sudo env FOO=1 /usr/bin/nvim file.txt"')
assert_equals "nvim" "$result" "_tmux_extract_command strips sudo/env/assignments"

result=$(run_zsh '_tmux_extract_command "FOO=bar brew install fzf"')
assert_equals "brew" "$result" "_tmux_extract_command ignores leading assignments"

# wrapper 直後の付随フラグ (sudo -E 等) を読み飛ばして実コマンドを拾う
result=$(run_zsh '_tmux_extract_command "sudo -E git status"')
assert_equals "git" "$result" "_tmux_extract_command skips leading flags after wrappers"

result=$(run_zsh '_tmux_extract_command "noglob make build"')
assert_equals "make" "$result" "_tmux_extract_command skips noglob wrapper"

# Test 10: OSC 2 タイトルのサニタイズ (制御文字の除去)
printf '\n## Test 10: OSC title sanitization strips control chars\n'
# BEL (0x07) を含むタイトルを渡し、出力 (printf の OSC シーケンス) に 0x07 が
# 残っていないことを確認する。ESC は 0x1b なので干渉しない。
hex=$(run_zsh $'_tmux_set_pane_title "a\abc"' | od -An -tx1 | tr -d ' \n')
if [[ -n "$hex" && "$hex" != *07* ]]; then
  printf '✓ control char (BEL) stripped from OSC title\n'
else
  printf '✗ control char leaked into OSC title (hex: %s)\n' "$hex"
  exit 1
fi

# Test 11: YAML 不在でも _TMUX_ZSH_TITLE が "zsh" にフォールバックする (退行防止)
printf '\n## Test 11: zsh title falls back when YAML is missing\n'
result=$(run_zsh '_TMUX_WINDOW_NAME_YAML=/nonexistent/path.yaml; _tmux_reload_window_names; print -r -- "$_TMUX_ZSH_TITLE"')
assert_equals "zsh" "$result" "_TMUX_ZSH_TITLE falls back to 'zsh' when YAML is absent"

printf '\n=== All Tests Completed ===\n'
printf 'All tmux window name tests passed successfully!\n'
