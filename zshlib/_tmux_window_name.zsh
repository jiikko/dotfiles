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
# サブコマンドも window 名に出すコマンドの set (whitelist)。YAML の `_subcommands`
# から _tmux_load_yaml が構築する。make/git 等の「第2語が意味を持つ」コマンド用。
typeset -gA _TMUX_SUBCOMMAND_CMDS

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

  # whitelist (_subcommands) を set 化する。空白区切りの値を取り込み、無ければ空 set。
  local _subs="${_TMUX_WINDOW_NAMES[_subcommands]:-}"
  _TMUX_SUBCOMMAND_CMDS=()
  local _sub
  for _sub in ${=_subs}; do
    _TMUX_SUBCOMMAND_CMDS[$_sub]=1
  done

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

# wrapper コマンド (sudo 等)。それ自身は「実コマンド」ではないので読み飛ばす。
# NOTE: `sudo -u postgres psql` のように値を取るフラグでは値 (postgres) を拾うが、
#       これは稀なので簡易判定に留める (完全な引数解析はしない)。
_tmux_is_wrapper() {
  case "$1" in
    sudo|doas|command|builtin|env|exec|noglob|nohup|nice|time) return 0 ;;
  esac
  return 1
}

# 先頭の代入 (FOO=bar) と付随フラグ (sudo -E / make -j4 等)。コマンド名・サブコマンド名
# のどちらを探すときも読み飛ばす対象。フラグを飛ばさないと `sudo -E git` で `-E` を、
# `make -j4 test` で `-j4` をコマンド名/サブコマンドと誤認する。
_tmux_is_skippable_arg() {
  [[ "$1" == *=* || "$1" == -* ]]
}

# シェルの制御演算子・リダイレクトトークン (${(z)} が独立トークンとして返す)。
# サブコマンド探索でこれに当たったら、その先は別コマンド or リダイレクト先であり
# base コマンドの引数ではないので打ち切る (`make && echo` を `make &&` と出さない)。
_tmux_is_shell_operator() {
  case "$1" in
    '|'|'||'|'&'|'&&'|';'|';;'|'|&'|'&|'|'('|')'|'{'|'}') return 0 ;;
  esac
  # `<`/`>` 始まり、`&>`系リダイレクト (&> / &>| 等)、または fd 番号付きリダイレクト
  # (2>&1 → 2>& 等。<-> = 数字列)。`&>>` は ${(z)} が `>>&` を返すため [\<\>]* で捕捉済み。
  [[ "$1" == [\<\>]* || "$1" == '&>'* || "$1" == <->[\<\>]* ]]
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
    _tmux_is_skippable_arg "$word" && continue
    _tmux_is_wrapper "$word" && continue
    cmd="$word"
    break
  done

  [[ -z "$cmd" ]] && cmd="${words[1]:-}"
  [[ "$cmd" == */* ]] && cmd="${cmd:t}"
  print -r -- "$cmd"
}

# whitelist コマンド (make/git 等) のサブコマンド = base コマンドの次に来る最初の
# 素の語を返す (フラグ・代入は読み飛ばす)。無ければ空文字。
# NOTE: `git -C /path status` のように値を取るグローバルフラグでは値 (/path) を拾い
#       うるが、対話入力では稀なため extract_command と同じく簡易判定に留める。
_tmux_extract_subcommand() {
  emulate -L zsh
  # shellcheck disable=SC2034
  local input="$1"
  local -a words
  # shellcheck disable=SC2206,SC2296
  words=(${(z)input})

  local word
  local -i found_cmd=0
  for word in "${words[@]}"; do
    [[ -z "$word" ]] && continue
    if (( ! found_cmd )); then
      # base コマンドを見つけるまでは extract_command と同じ規則で読み飛ばす
      _tmux_is_skippable_arg "$word" && continue
      _tmux_is_wrapper "$word" && continue
      found_cmd=1
      continue
    fi
    # base コマンドの後: フラグ・代入を飛ばし、最初の素の語をサブコマンドとする。
    # 演算子/リダイレクトに当たったら引数は無いと判断して打ち切る。
    _tmux_is_skippable_arg "$word" && continue
    _tmux_is_shell_operator "$word" && break
    print -r -- "$word"
    return
  done
  print -r -- ""
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
    # whitelist 判定で親シェルの _TMUX_SUBCOMMAND_CMDS を参照する。$(...) 経由の
    # 遅延ロードはサブシェル内でしか set を構築しないため、ここで親シェルにロードを
    # 保証する (precmd が先に走る前提に依存しない)。
    (( _TMUX_WINDOW_NAMES_LOADED )) || _tmux_load_yaml
    local cmd title
    cmd=$(_tmux_extract_command "$1")
    # alias は展開せず、タイプした名前のまま表示する (意図的な仕様)。
    # 2d68f3c (2025-12-12) で alias 展開を廃止した。`v` は `nvim` ではなく `v` と出る。
    # 実コマンド名を出したくなったら alias 解決を再導入することになるが、その判断は
    # この経緯を踏まえてから行うこと (安易に戻すと過去の決定を覆すことになる)
    title=$(_tmux_get_display_name "$cmd")
    # whitelist (_subcommands) のコマンドは第2語 (サブコマンド) も付けて
    # `make test` / `git commit` のように出す (一覧でコマンドの何かが分かるように)。
    if (( ${+_TMUX_SUBCOMMAND_CMDS[$cmd]} )); then
      local sub
      sub=$(_tmux_extract_subcommand "$1")
      [[ -n "$sub" ]] && title+=" $sub"
    fi
    _tmux_set_pane_title "$title"
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
