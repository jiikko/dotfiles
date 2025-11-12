#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_ZDOTDIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_ZDOTDIR"
}
trap cleanup EXIT

cat <<EOF > "$TMP_ZDOTDIR/.zshrc"
source "$ROOT_DIR/_zshrc"
EOF

run_zsh() {
  ZDOTDIR="$TMP_ZDOTDIR" zsh -i -c "$1"
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    printf '✗ %s\n' "$message"
    printf '  expected to find: %s\n' "$needle"
    exit 1
  else
    printf '✓ %s\n' "$message"
  fi
}

# 1. fzf-history-widget is available
if run_zsh 'type fzf-history-widget >/dev/null'; then
  printf '✓ fzf-history-widget is defined\n'
else
  printf '✗ fzf-history-widget is not defined\n'
  exit 1
fi

# 2. Ctrl-R is bound to fzf-history-widget
bind_output="$(run_zsh 'bindkey "^R"')"
assert_contains "$bind_output" "fzf-history-widget" "Ctrl-R is bound to fzf-history-widget"

# 3. Base FZF options stay intact
fzf_opts="$(run_zsh 'print -r -- $FZF_DEFAULT_OPTS')"
assert_contains "$fzf_opts" "--height 80%" "FZF_DEFAULT_OPTS preserves height setting"
assert_contains "$fzf_opts" "--reverse" "FZF_DEFAULT_OPTS preserves reverse setting"

printf 'All zshrc tests passed.\n'
