# tmux ウィンドウ名を入力コマンドに基づいて設定する
# YAML でコマンド名 -> 表示名のマッピングを定義可能

_TMUX_WINDOW_NAME_YAML="$HOME/dotfiles/tmux-window-name.yaml"

# YAML から表示名を取得（シンプルなパーサー）
_tmux_get_display_name() {
  local cmd="$1"
  local yaml_file="$_TMUX_WINDOW_NAME_YAML"

  if [[ -f "$yaml_file" ]]; then
    # コメント行とマッチする行を探す
    local result=$(grep -E "^${cmd}:" "$yaml_file" 2>/dev/null | head -1 | sed 's/^[^:]*:[[:space:]]*//' | sed 's/^"//' | sed 's/"$//')
    if [[ -n "$result" ]]; then
      echo "$result"
      return
    fi

    # _default があれば使う
    local default_icon=$(grep -E "^_default:" "$yaml_file" 2>/dev/null | head -1 | sed 's/^[^:]*:[[:space:]]*//' | sed 's/^"//' | sed 's/"$//')
    if [[ -n "$default_icon" ]]; then
      echo "${default_icon}${cmd}"
      return
    fi
  fi

  # YAML にない場合はコマンド名そのまま
  echo "$cmd"
}

if [[ -n "$TMUX" ]]; then
  _tmux_preexec() {
    # コマンドの最初の単語（コマンド名）を取得
    local cmd="${1%% *}"
    # エイリアスを展開して実際のコマンド名を取得
    local resolved=$(alias "$cmd" 2>/dev/null | sed "s/^${cmd}=//" | sed "s/^'//" | sed "s/'$//" | awk '{print $1}')
    if [[ -n "$resolved" ]]; then
      cmd="$resolved"
    fi
    local display_name=$(_tmux_get_display_name "$cmd")
    printf "\033k%s\033\\" "$display_name"
  }

  _tmux_precmd() {
    # コマンド終了後はシェル名に戻す
    local display_name=$(_tmux_get_display_name "zsh")
    printf "\033k%s\033\\" "$display_name"
  }

  # preexec/precmd フックに追加
  autoload -Uz add-zsh-hook
  add-zsh-hook preexec _tmux_preexec
  add-zsh-hook precmd _tmux_precmd
fi
