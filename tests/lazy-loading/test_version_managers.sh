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
mkdir -p "$TMP_HOME/.nodebrew/current/bin"
mkdir -p "$TMP_HOME/.anyenv/envs"

create_fake_env() {
  local tool="$1"
  local root="$2"
  mkdir -p "$root/bin" "$root/shims"
  local tool_bin="$root/bin/$tool"
  local upper_tool
  upper_tool="$(printf '%s' "$tool" | tr '[:lower:]' '[:upper:]')"
  cat <<EOF_TOOL > "$tool_bin"
#!/usr/bin/env bash
if [[ "\$1" == "init" && "\$2" == "-" ]]; then
  cat <<SCRIPT
export ${upper_tool}_INIT_CALLED=1
${tool}() {
  printf '${tool} real: %s\n' "\$*"
}
SCRIPT
else
  printf '${tool} binary invoked %s\n' "\$*" >&2
fi
EOF_TOOL
  chmod +x "$tool_bin"
}

create_fake_env "rbenv" "$TMP_HOME/.rbenv"
create_fake_env "nodenv" "$TMP_HOME/.nodenv"
create_fake_env "goenv" "$TMP_HOME/.anyenv/envs/goenv"

run_zsh() {
  local cmd="$1"
  HOME="$TMP_HOME" ZDOTDIR="$TMP_ZDOTDIR" zsh -i -c "$cmd"
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

assert_root_variable() {
  local tool="$1"
  local expected="$2"
  local upper
  upper="$(printf '%s' "$tool" | tr '[:lower:]' '[:upper:]')"
  local actual
  actual=$(run_zsh "print -r -- \${${upper}_ROOT:-}" | tr -d '\r')
  if [[ "$actual" == "$expected" ]]; then
    printf '✓ %s_ROOT is set to %s\n' "$upper" "$expected"
  else
    printf '✗ %s_ROOT expected %s but got %s\n' "$upper" "$expected" "${actual:-<empty>}"
    exit 1
  fi
}

assert_path_priority() {
  local dir="$1"
  local reference="$2"
  local description="$3"
  local listing
  listing=$(run_zsh 'for d in $path; do echo $d; done')
  local idx_dir idx_ref idx=1
  while IFS= read -r line; do
    if [[ "$line" == "$dir" ]]; then
      idx_dir=$idx
    fi
    if [[ "$line" == "$reference" ]]; then
      idx_ref=$idx
    fi
    ((idx++))
  done <<< "$listing"

  if [[ -z "${idx_dir:-}" ]]; then
    printf '✗ PATH does not include %s\n' "$dir"
    exit 1
  fi
  if [[ -z "${idx_ref:-}" ]]; then
    printf '✗ PATH does not include %s\n' "$reference"
    exit 1
  fi

  if (( idx_dir < idx_ref )); then
    printf '✓ %s\n' "$description"
  else
    printf '✗ %s (expected index %d < %d)\n' "$description" "$idx_dir" "$idx_ref"
    exit 1
  fi
}

printf '\n=== _lazy_anyenv_manager Tests ===\n\n'

for tool in rbenv nodenv goenv; do
  printf '## Testing %s lazy loading\n' "$tool"
  assert_is_function "$tool" "$tool is defined as a lazy-loading function"

  if [[ "$tool" == "goenv" ]]; then
    expected_root="$TMP_HOME/.anyenv/envs/goenv"
  else
    expected_root="$TMP_HOME/.${tool}"
  fi
  assert_root_variable "$tool" "$expected_root"

  assert_path_priority "$expected_root/shims" "/usr/bin" "$tool shims precede /usr/bin"
  assert_path_priority "$expected_root/bin" "/usr/bin" "$tool bin precedes /usr/bin"
  printf '\n'
done

printf 'All _lazy_anyenv_manager tests passed successfully!\n'
