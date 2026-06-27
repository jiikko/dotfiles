#!/usr/bin/env zsh

set -euo pipefail
unset CDPATH

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
SOCKET_NAME="dotfiles-test-$$"
log_file="$TMUX_TMPDIR/tmux.log"

# resurrect / debounce 保存を実データから隔離する。
# _tmux.conf には window-linked hook（scripts/tmux_resurrect_debounced_save.sh を
# 走らせる debounce 保存）と continuum autosave があり、テストで conf をロードして
# セッションを作ると保存が走り得る。resurrect の保存先（helpers.sh）は
#   1) @resurrect-dir / 2) ~/.tmux/resurrect が在ればそれ / 3) $XDG_DATA_HOME/...
# の順で解決されるため、XDG_DATA_HOME だけ差し替えても実 HOME に ~/.tmux/resurrect が
# 在る環境では本物を触り得る。そこで HOME ごと temp に隔離して全候補を temp に倒す。
# DOTFILES_DIR は明示固定する（conf の plugin パスは ${DOTFILES_DIR:-$HOME/dotfiles}
# なので HOME を temp にすると $HOME/dotfiles が壊れるため）。
export HOME="$TMUX_TMPDIR/home"
export DOTFILES_DIR="$ROOT_DIR"
export XDG_DATA_HOME="$HOME/.local/share"
export TT_DEBOUNCE_STATE_DIR="$HOME/.cache/tt-debounce"
mkdir -p "$HOME" "$XDG_DATA_HOME" "$TT_DEBOUNCE_STATE_DIR"

if ! command -v "$TMUX_BIN_PATH" >/dev/null 2>&1; then
  print -u2 "Error: tmux binary not found. Install tmux or set \$TMUX_BIN."
  exit 1
fi

if [[ ! -f "$CONF_FILE" ]]; then
  print -u2 "Error: tmux config $CONF_FILE not found."
  exit 1
fi

TMUX_CMD=("$TMUX_BIN_PATH" -L "$SOCKET_NAME" -f "$CONF_FILE")

cleanup() {
  "$TMUX_BIN_PATH" -L "$SOCKET_NAME" kill-server >/dev/null 2>&1 || true
  rm -rf "$TMUX_TMPDIR"
}
trap cleanup EXIT

handle_result() {
  local log="$1"
  local desc="$2"
  local allow_skip="$3"
  if grep -qiE "operation not permitted|permission denied" "$log"; then
    if [[ "$allow_skip" == "skip" ]]; then
      print -u2 "[test-tmux:zsh] skipped: tmux cannot create sockets in this environment"
      cat "$log" >&2
      exit 0
    fi
  fi
  print -u2 "[test-tmux:zsh] $desc"
  cat "$log" >&2
  exit 1
}

run_with_check() {
  local log="$1"
  local desc="$2"
  local allow_skip="$3"
  shift 3
  if ! "$@" >"$log" 2>&1 || grep -qi "error" "$log"; then
    handle_result "$log" "$desc" "$allow_skip"
  fi
}

probe_dir=$(mktemp -d)
probe_log="$probe_dir/probe.log"
probe_socket="dotfiles-probe-$$"
run_with_check "$probe_log" "probe session failed" "skip" \
  env TMUX_TMPDIR="$probe_dir" "$TMUX_BIN_PATH" -L "$probe_socket" new-session -d -s dotfiles_probe "tail -f /dev/null"
"$TMUX_BIN_PATH" -L "$probe_socket" kill-server >/dev/null 2>&1 || true
rm -rf "$probe_dir"

print "[test-tmux:zsh] starting server with $CONF_FILE"
run_with_check "$log_file" "failed to create test session" "skip" \
  "${TMUX_CMD[@]}" new-session -d -s dotfiles_test "tail -f /dev/null"

# conf ロード時に tmux が出す設定警告 (invalid option / unknown ...) を検出する。
# これらは run_with_check の grep "error" では拾えない
# (例: "invalid option: pane-scrollbars" は "error" を含まない)。
# また -f での起動時ロード (上の new-session) は警告を呼び出し元の stderr へ返さない
# (サーバ側に記録されるだけ) ため、source-file を明示実行して呼び出しの出力に警告を
# 捕捉する (実測: source-file は呼び出し元へ返す / -f 起動は返さない)。
# 古い tmux でバージョンガード無しに新オプションを足すと壊れる回帰
# (pane-scrollbars は tmux 3.6+ 専用) を防ぐのが目的。
print "[test-tmux:zsh] checking config load for tmux warnings"
"${TMUX_CMD[@]}" source-file "$CONF_FILE" >"$log_file" 2>&1 || true
conf_warnings=$(grep -niE 'invalid option|unknown option|unknown command|unknown key|invalid or unknown' "$log_file" || true)
if [[ -n "$conf_warnings" ]]; then
  print -u2 "[test-tmux:zsh] tmux reported config warnings while loading $CONF_FILE:"
  print -u2 "$conf_warnings"
  exit 1
fi

print "[test-tmux:zsh] dumping global options"
run_with_check "$log_file" "show-options failed" "fail" \
  "${TMUX_CMD[@]}" show-options -g

print "[test-tmux:zsh] verifying custom key bindings can be listed"
run_with_check "$log_file" "list-keys failed" "fail" \
  "${TMUX_CMD[@]}" list-keys

# window-status フォーマットのスタイル指定が壊れていないか検査する。
# #{?cond,#[a#,b],...} のように条件分岐の中で #[...] を使うとき、#[...] 内の
# カンマを #, でエスケープし忘れると、tmux は条件分岐の区切りカンマと誤認し、
# スタイル指定が途中で割れて window 名の前に "fg=colour231]" のようなリテラルが
# 漏れる (zoom 強調色の実装で実際に踏んだ回帰。source-file 警告は出ないため
# 上のロード検査では拾えず、描画時に初めて壊れる)。
# 実 format をズーム/非ズーム両状態で展開し、整形済みの #[...] を除去した残りに
# fg=/bg=/colourN が残っていないか検査する (display-message はパイプ出力時に
# 正規タグも #[...] のまま出すので、まず正規タグを除去してから漏れを判定する)。
assert_no_style_leak() {
  local label="$1" expanded="$2" residual
  residual=$(print -r -- "$expanded" | sed -E 's/#\[[^]]*\]//g')
  if print -r -- "$residual" | grep -qE 'fg=|bg=|colour[0-9]'; then
    print -u2 "[test-tmux:zsh] window-status format leaked a style literal ($label):"
    print -u2 "  expanded: $expanded"
    print -u2 "  residual: $residual"
    exit 1
  fi
}

print "[test-tmux:zsh] checking window-status formats expand without leaked style literals"
"${TMUX_CMD[@]}" split-window -d -t dotfiles_test "tail -f /dev/null" >"$log_file" 2>&1 \
  || handle_result "$log_file" "split-window failed" "fail"
fmt_current=$("${TMUX_CMD[@]}" show-options -gv window-status-current-format)
fmt_other=$("${TMUX_CMD[@]}" show-options -gv window-status-format)
for zoom_state in unzoomed zoomed; do
  if [[ "$zoom_state" == zoomed ]]; then
    "${TMUX_CMD[@]}" resize-pane -t dotfiles_test -Z >"$log_file" 2>&1 \
      || handle_result "$log_file" "resize-pane -Z failed" "fail"
  fi
  assert_no_style_leak "current/$zoom_state" \
    "$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "$fmt_current")"
  assert_no_style_leak "other/$zoom_state" \
    "$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "$fmt_other")"
done

# status-left も同じ「条件分岐内 #[...] のカンマ未エスケープ」回帰の対象
# (scratch セッション検出時のソフト点滅 flash 分岐で #[...] を使う)。
print "[test-tmux:zsh] checking status-left expands without leaked style literals"
fmt_sl=$("${TMUX_CMD[@]}" show-options -gv status-left)
# 通常(非 scratch)分岐
assert_no_style_leak "status-left/normal" \
  "$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "$fmt_sl")"
# scratch の flash 分岐を exercise: 検出条件 session_name==scratch を、このテストセッション名
# (dotfiles_test) に一致するよう差し替えると flash 分岐が選ばれる (条件の差し替えはスタイルの
# エスケープ検査結果に影響しない。点滅 #() は client 非 attach で空展開だが #[...] 崩れは検出可)。
fmt_sl_flash=${fmt_sl/,scratch/,dotfiles_test}
if [[ "$fmt_sl_flash" != "$fmt_sl" ]]; then
  assert_no_style_leak "status-left/scratch-flash" \
    "$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "$fmt_sl_flash")"
else
  print -u2 "[test-tmux:zsh] warning: status-left の scratch 検出条件が見つからず flash 分岐を検査できませんでした (条件式が変わった可能性)"
fi

print "[test-tmux:zsh] done"
