# shellcheck shell=bash
# tmux ウィンドウ名を入力コマンドに基づいて設定する
# YAML でコマンド名 -> 表示名のマッピングを定義可能

_TMUX_WINDOW_NAME_YAML="$HOME/dotfiles/tmux-window-name.yaml"

# 連想配列でマッピングをキャッシュ（起動時に1回だけ読み込み）
typeset -gA _TMUX_WINDOW_NAMES

_tmux_load_yaml() {
  [[ -f "$_TMUX_WINDOW_NAME_YAML" ]] || return
  local line key value
  while IFS= read -r line; do
    # コメント行と空行をスキップ
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ -z "$line" ]] && continue
    # key: value の形式をパース
    key="${line%%:*}"
    value="${line#*:}"
    # 先頭の空白とクォートを除去
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value#\"}"
    value="${value%\"}"
    _TMUX_WINDOW_NAMES[$key]="$value"
  done < "$_TMUX_WINDOW_NAME_YAML"
}
_tmux_load_yaml

# YAML から表示名を取得（連想配列から O(1) で取得）
_tmux_get_display_name() {
  local cmd="$1"
  if [[ -n "${_TMUX_WINDOW_NAMES[$cmd]}" ]]; then
    echo "${_TMUX_WINDOW_NAMES[$cmd]}"
  else
    echo "${_TMUX_WINDOW_NAMES[_default]}${cmd}"
  fi
}

# エイリアスを展開してコマンド名を取得
_resolve_alias() {
  local cmd="$1"
  local resolved
  resolved=$(alias "$cmd" 2>/dev/null) || { echo "$cmd"; return; }
  resolved=${resolved#*=}
  resolved=${resolved#\'}
  resolved=${resolved%\'}
  echo "${resolved%% *}"
}

if [[ -n "$TMUX" ]]; then
  _tmux_preexec() {
    local cmd="${1%% *}"
    cmd=$(_resolve_alias "$cmd")
    printf "\033k%s\033\\" "$(_tmux_get_display_name "$cmd")"
  }

  _tmux_precmd() {
    printf "\033k%s\033\\" "${_TMUX_WINDOW_NAMES[zsh]}"
  }

  autoload -Uz add-zsh-hook
  add-zsh-hook preexec _tmux_preexec
  add-zsh-hook precmd _tmux_precmd
fi
