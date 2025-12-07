# shellcheck shell=bash
# tmux ウィンドウ名を実行コマンドに基づいて設定する
# YAML でコマンド名 -> 表示名のマッピングを定義可能

: "${_TMUX_WINDOW_NAME_YAML:=$HOME/dotfiles/tmux-window-name.yaml}"

# 連想配列でマッピングをキャッシュ（起動時に1回だけ読み込み）
typeset -gA _TMUX_WINDOW_NAMES
typeset -gi _TMUX_WINDOW_NAMES_LOADED=0

_tmux_window_name_trim() {
  local str="$1"
  str="${str#"${str%%[![:space:]]*}"}"
  str="${str%"${str##*[![:space:]]}"}"
  print -r -- "$str"
}

_tmux_load_yaml() {
  (( _TMUX_WINDOW_NAMES_LOADED )) && return
  _TMUX_WINDOW_NAMES_LOADED=1
  _TMUX_WINDOW_NAMES=()

  [[ -f "$_TMUX_WINDOW_NAME_YAML" ]] || return

  local line key value
  while IFS= read -r line || [[ -n "$line" ]]; do
    # コメント行と空行をスキップ
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ -z "${line//[[:space:]]/}" ]] && continue
    [[ "$line" != *:* ]] && continue

    # key: value の形式をパース
    key="${line%%:*}"
    value="${line#*:}"

    key="$(_tmux_window_name_trim "$key")"
    value="$(_tmux_window_name_trim "$value")"
    value="${value#\"}"
    value="${value%\"}"

    [[ -n "$key" ]] && _TMUX_WINDOW_NAMES[$key]="$value"
  done < "$_TMUX_WINDOW_NAME_YAML"

  # _default が未定義でも安全に使えるようにする
  (( ${+_TMUX_WINDOW_NAMES[_default]} )) || _TMUX_WINDOW_NAMES[_default]=""
}

_tmux_reload_window_names() {
  _TMUX_WINDOW_NAMES_LOADED=0
  _tmux_load_yaml
}

# YAML から表示名を取得（連想配列から O(1) で取得）
_tmux_get_display_name() {
  (( _TMUX_WINDOW_NAMES_LOADED )) || _tmux_load_yaml
  local cmd="$1"
  if (( ${+_TMUX_WINDOW_NAMES[$cmd]} )); then
    print -r -- "${_TMUX_WINDOW_NAMES[$cmd]}"
  else
    print -r -- "${_TMUX_WINDOW_NAMES[_default]}${cmd}"
  fi
}

# エイリアスを展開してコマンド名を取得
_tmux_resolve_alias() {
  emulate -L zsh
  local cmd="$1"
  [[ -z "$cmd" ]] && { print -r -- ""; return; }
  local resolved
  resolved=$(alias "$cmd" 2>/dev/null) || { print -r -- "$cmd"; return; }
  resolved=${resolved#*=}
  resolved=${resolved#\'}
  resolved=${resolved%\'}
  local -a words
  # shellcheck disable=SC2206,SC2296
  words=(${(z)resolved})
  print -r -- "${words[1]:-$cmd}"
}

_tmux_extract_command() {
  emulate -L zsh
  # shellcheck disable=SC2034
  local input="$1"
  local -a words
  # shellcheck disable=SC2206,SC2296
  words=(${(z)input})

  local word cmd=""
  for word in "${words[@]}"; do
    [[ -z "$word" ]] && continue
    [[ "$word" == *=* ]] && continue
    case "$word" in
      sudo|command|builtin|env) continue ;;
    esac
    cmd="$word"
    break
  done

  [[ -z "$cmd" ]] && cmd="${words[1]:-}"
  [[ "$cmd" == */* ]] && cmd="${cmd:t}"
  print -r -- "$cmd"
}

_tmux_set_window_title() {
  local title="$1"
  [[ -z "$title" ]] && return
  printf "\033k%s\033\\" "$title"
}

if [[ -n "$TMUX" ]]; then
  _tmux_preexec() {
    local cmd
    cmd=$(_tmux_extract_command "$1")
    cmd=$(_tmux_resolve_alias "$cmd")
    _tmux_set_window_title "$(_tmux_get_display_name "$cmd")"
  }

  _tmux_precmd() {
    _tmux_set_window_title "$(_tmux_get_display_name zsh)"
  }

  autoload -Uz add-zsh-hook
  add-zsh-hook preexec _tmux_preexec
  add-zsh-hook precmd _tmux_precmd
fi
