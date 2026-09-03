#!/usr/bin/env bash
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_ZDOTDIR="$(mktemp -d)"
TMP_HOME="$(mktemp -d)"
# 🚨 後始末はリトライする。goenv の遅延ロードは GOROOT を取るために実 `go` を呼び、go は
# telemetry のカウンタを $HOME/Library/Application Support/go/telemetry へ**遅れて**書くため、
# rm の走査直後にファイルが生えて "Directory not empty" で失敗する (実測 2026-08-21: 本体を
# 実行する assert を足した途端に毎回再発した)。失敗を握り潰さず、諦めるときは残骸の場所を出す。
cleanup() {
  local _attempt
  for _attempt in 1 2 3; do
    rm -rf "$TMP_ZDOTDIR" "$TMP_HOME" 2>/dev/null && return 0
    sleep 1
  done
  rm -rf "$TMP_ZDOTDIR" "$TMP_HOME" 2>/dev/null ||
    printf '🚨 一時ディレクトリを消せなかった (残骸: %s %s)\n' "$TMP_ZDOTDIR" "$TMP_HOME" >&2
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
  # 🚨 内側の heredoc は quote する (<<'SCRIPT')。unquoted だと emit される関数本体の "$*" が
  # **ツール実行時 (init -)** に展開されて "init -" に固定され、遅延ロード後の再ディスパッチが
  # 引数を渡せているかを観測できなくなる (issue 082: この歪みのせいで assert を足せなかった)。
  cat <<EOF_TOOL > "$tool_bin"
#!/usr/bin/env bash
if [[ "\$1" == "init" && "\$2" == "-" ]]; then
  cat <<'SCRIPT'
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

# 遅延ロード**本体**の検査 (issue 082)。
#
# 🚨 これまでの assert は「関数として定義されているか」「*_ROOT」「PATH 順」の 3 種だけで、
# ラッパーの中身 (unfunction → eval "$(<tool> init -)" → 再ディスパッチ) が一度も実行されて
# いなかった。監査時の実測では _zshrc の eval 文字列を空関数に置き換えても 12 assert 全部が
# 緑だった。ここでは 1 つの zsh セッションの中で実際に呼び、
#   (1) init - が eval されたか (*_INIT_CALLED)
#   (2) 実体へ再ディスパッチされ**引数が渡ったか** (fake が引数を echo する)
#   (3) 2 回目の呼び出しも実体に届くか (unfunction 後にラッパーが復活していない / 無限再帰しない)
# を観測する。
assert_lazy_body_runs() {
  local tool="$1"
  local upper
  upper="$(printf '%s' "$tool" | tr '[:lower:]' '[:upper:]')"
  local out
  # 🚨 1 コマンドにまとめる: 遅延ロードの効果はセッション内の状態なので、別々の zsh -c に
  # 分けると毎回「未ロード」から始まり (2)(3) を観測できない。
  out=$(run_zsh "$tool first-call; print -r -- \"INIT=\${${upper}_INIT_CALLED:-}\"; $tool second-call" 2>&1 || echo "<zsh failed>")
  local want_first="$tool real: first-call"
  local want_second="$tool real: second-call"
  if [[ "$out" != *"$want_first"* ]]; then
    printf '✗ %s: 遅延ロード後に実体へ届いていない (期待 %q):\n%s\n' "$tool" "$want_first" "$out"
    exit 1
  fi
  if [[ "$out" != *"INIT=1"* ]]; then
    printf '✗ %s: init - が eval されていない (%s_INIT_CALLED が立たない):\n%s\n' "$tool" "$upper" "$out"
    exit 1
  fi
  if [[ "$out" != *"$want_second"* ]]; then
    printf '✗ %s: 2 回目の呼び出しが実体に届いていない (ラッパーが残っている / 再帰している):\n%s\n' "$tool" "$out"
    exit 1
  fi
  if [[ "$out" == *"binary invoked"* ]]; then
    printf '✗ %s: ラッパーが実バイナリを直接叩いている (init - 経由になっていない):\n%s\n' "$tool" "$out"
    exit 1
  fi
  printf '✓ %s: init - を eval し、引数つきで実体へ再ディスパッチする (2 回目も実体)\n' "$tool"
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
  assert_lazy_body_runs "$tool"
  printf '\n'
done

printf 'All _lazy_anyenv_manager tests passed successfully!\n'
