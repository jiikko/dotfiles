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
  local mode="${2:-interactive}"
  local opts=(-i)
  if [[ "$mode" == "login" ]]; then
    opts=(-i -l)
  fi
  HOME="$TMP_HOME" ZDOTDIR="$TMP_ZDOTDIR" zsh "${opts[@]}" -c "$cmd"
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

# 4. rbenv initialization hooks are present (if rbenv exists)
if command -v rbenv >/dev/null 2>&1; then
  rbenv_location="$(run_zsh 'command -v rbenv')"
  assert_contains "$rbenv_location" "rbenv" "rbenv is available in interactive shells"
else
  printf '↷ rbenv not installed; skipping rbenv test\n'
fi

# 5. PATH includes expected local bins
path_output="$(run_zsh 'print -r -- $PATH' | awk 'END{print}')"
expected_home="$TMP_HOME"
assert_path_contains() {
  local path_str="$1"
  shift
  local -a parts
  local IFS=':'
  read -r -a parts <<< "$path_str"
  local target
  for target in "$@"; do
    local found=0
    for part in "${parts[@]}"; do
      if [[ "$part" == "$target" ]]; then
        found=1
        break
      fi
    done
    if (( found == 0 )); then
      printf '✗ PATH is missing expected entry: %s\n' "$target"
      exit 1
    fi
  done
  printf '✓ PATH contains required entries\n'
}
assert_path_contains "$path_output" \
  "$expected_home/.rbenv/shims" \
  "$expected_home/.nodebrew/current/bin" \
  "$expected_home/dotfiles/bin"

# 6. Login shells include pyenv shims early (if pyenv exists)
if command -v pyenv >/dev/null 2>&1; then
  pyenv_shims="$(pyenv root)/shims"
  login_path="$(run_zsh 'print -r -- $PATH' login | awk 'END{print}')"
  assert_contains "$login_path" "$pyenv_shims" "pyenv shims present in login shell PATH"
else
  printf '↷ pyenv not installed; skipping pyenv login test\n'
fi

# 7. Git-branch picker key binding is present
git_branch_binding="$(run_zsh 'bindkey "^g^b"')"
assert_contains "$git_branch_binding" "select-git-branch-friendly" "Ctrl-g Ctrl-b is bound to branch selector"

# 8. zcompile command exists (used by zshrc)
if run_zsh 'command -v zcompile >/dev/null'; then
  printf '✓ zcompile is available in zsh\n'
else
  printf '✗ zcompile is missing; required for cached compdump\n'
  exit 1
fi

printf 'All zshrc tests passed.\n'
