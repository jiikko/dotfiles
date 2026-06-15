# shellcheck shell=bash
# 実行コマンドに基づいて tmux ペインタイトル (pane_title) を設定する
# YAML でコマンド名 -> 表示名のマッピングを定義可能
# ウィンドウ名への反映は tmux 側の automatic-rename-format '#{pane_title}' が行う
# (= アクティブペインのタイトルがウィンドウ名になる。_tmux.conf 参照)
# NOTE: このファイルは source で読み込むこと (autoload 不可: ${0:A:h} に依存)

_TMUX_WINDOW_NAME_YAML="${0:A:h}/tmux-window-name.yaml"

# 連想配列でマッピングをキャッシュ（起動時に1回だけ読み込み）
typeset -gA _TMUX_WINDOW_NAMES
typeset -gi _TMUX_WINDOW_NAMES_LOADED=0
# precmd で毎回参照する zsh の表示名はロード後に不変なので、ここに1回だけ確定させて
# precmd のコマンド置換 (fork) を無くす (_tmux_load_yaml が設定する)
typeset -g _TMUX_ZSH_TITLE=""

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

  # YAML が無い場合も読み込みをスキップするだけで、下の _default guard と
  # _TMUX_ZSH_TITLE キャッシュは必ず通す (不在時に title が空になる退行を防ぐ)
  if [[ -f "$_TMUX_WINDOW_NAME_YAML" ]]; then
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
  fi

  # _default が未定義でも安全に使えるようにする (YAML 不在時も含めて必ず設定)
  (( ${+_TMUX_WINDOW_NAMES[_default]} )) || _TMUX_WINDOW_NAMES[_default]=""

  # precmd 用に zsh の表示名を確定キャッシュ (fork なしで配列から直接引く)
  _TMUX_ZSH_TITLE="${_TMUX_WINDOW_NAMES[zsh]:-${_TMUX_WINDOW_NAMES[_default]}zsh}"
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
    # 先頭の代入 (FOO=bar) と wrapper の付随フラグ (sudo -E / -i 等) を読み飛ばす。
    # フラグを飛ばさないと `sudo -E git` で `-E` をコマンド名と誤認する。
    # NOTE: `sudo -u postgres psql` のように値を取るフラグでは値 (postgres) を拾うが、
    #       これは稀なので簡易判定に留める (完全な sudo 引数解析はしない)。
    [[ "$word" == *=* ]] && continue
    [[ "$word" == -* ]] && continue
    case "$word" in
      sudo|doas|command|builtin|env|exec|noglob|nohup|nice|time) continue ;;
    esac
    cmd="$word"
    break
  done

  [[ -z "$cmd" ]] && cmd="${words[1]:-}"
  [[ "$cmd" == */* ]] && cmd="${cmd:t}"
  print -r -- "$cmd"
}

# OSC 2 で「このペインの」タイトルだけを書く。旧実装の \033k (ウィンドウ名直接
# リネーム) は非アクティブペインの precmd がウィンドウ名を奪う事故があったため廃止
# (allow-rename off で無視される。_tmux.conf 参照)
_tmux_set_pane_title() {
  emulate -L zsh
  local title="$1"
  [[ -z "$title" ]] && return
  # 未知コマンド名は入力行由来のため、貼り付け等で制御文字 (ESC/BEL/改行) が混じると
  # OSC 2 シーケンス自体を壊しうる。制御文字を除去し、暴走防止に長さも制限する。
  title="${title//[[:cntrl:]]/}"
  title="${title[1,64]}"
  [[ -z "$title" ]] && return
  printf "\033]2;%s\033\\" "$title"
}

if [[ -n "$TMUX" ]]; then
  _tmux_preexec() {
    local cmd
    cmd=$(_tmux_extract_command "$1")
    # alias は展開せず、タイプした名前のまま表示する (意図的な仕様)。
    # 2d68f3c (2025-12-12) で alias 展開を廃止した。`v` は `nvim` ではなく `v` と出る。
    # 実コマンド名を出したくなったら alias 解決を再導入することになるが、その判断は
    # この経緯を踏まえてから行うこと (安易に戻すと過去の決定を覆すことになる)
    _tmux_set_pane_title "$(_tmux_get_display_name "$cmd")"
  }

  _tmux_precmd() {
    # 表示名は固定 (zsh) なのでロード時キャッシュ (_TMUX_ZSH_TITLE) を使い、
    # プロンプト毎のコマンド置換 fork を避ける。初回はロードを保証する
    (( _TMUX_WINDOW_NAMES_LOADED )) || _tmux_load_yaml
    _tmux_set_pane_title "$_TMUX_ZSH_TITLE"
  }

  autoload -Uz add-zsh-hook
  add-zsh-hook preexec _tmux_preexec
  add-zsh-hook precmd _tmux_precmd
fi
