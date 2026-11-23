# treefiler を q で閉じたら、カーソルのフォルダ (ファイルの上ならその親) へ cd する。esc・Ctrl-C で閉じたら cd しない。
# 子プロセスは親のシェルの cwd を変えられないので、treefiler は行き先を --cwd-file に書くだけで、cd はシェルの側で行う
# (src/treefiler/main.go の parseArgs と cwdFile)。入口は 2 つ:
#   - tf: ペインで直接開く。閉じた後にこの関数が読んで cd する。関数を通さない呼び方 (command tf・bin/tf) は cd しない
#   - tmux の prefix f: popup の中の scripts/tmux_treefiler_popup.sh が、行き先をペインの option (@treefiler_cd) に置いて
#     TREEFILER_CD_KEYS をペインへ送り、下の widget が cd する

tf() {
  local f dir rc
  f=$(mktemp "${TMPDIR:-/tmp}/treefiler-cd.XXXXXX") || return 1
  {
    command tf --cwd-file "$f" "$@"
    rc=$?
    # 書かれていなければ空 (esc で閉じた・起動に失敗した)。treefiler は改行を付けないので、$(<f) で末尾の改行を落とす形にしない
    IFS= read -r -d '' dir < "$f"
  } always {
    command rm -f -- "$f"
  }
  _treefiler_cd_to "$dir" || return 1
  return rc
}

# _treefiler_cd_to は行き先へ cd する ("" なら何もしない)。行き先が消えていたら知らせて 1 を返す。
_treefiler_cd_to() {
  [[ -n $1 ]] || return 0
  if [[ ! -d $1 ]]; then
    print -ru2 -- "treefiler: cd 先がありません: $1"
    return 1
  fi
  [[ $1 == "$PWD" ]] || cd -- "$1"
}

# popup から送られるキー列。scripts/tmux_treefiler_popup.sh の keys と同じ値にする (人が打たない、端末も出さない CSI)
typeset -g TREEFILER_CD_KEYS=$'\e[9119~'

# _treefiler_cd_widget は popup が置いた行き先を読んで cd し、プロンプトを描き直す。打ちかけのコマンド行 (BUFFER) は触らない。
# `cd ...` と Enter を送らずに widget にするのは、打ちかけの行を壊さないため (scripts/tmux_treefiler_popup.sh 冒頭)。
_treefiler_cd_widget() {
  [[ -n $TMUX_PANE ]] || return 0
  local dir fn
  dir=$(command tmux show -pqv -t "$TMUX_PANE" @treefiler_cd 2>/dev/null)
  command tmux set -pu -t "$TMUX_PANE" @treefiler_cd 2>/dev/null
  if [[ -n $dir ]]; then
    zle -I # 知らせを出すとき、コマンド行の上に出す
    _treefiler_cd_to "$dir"
    # プロンプトの中身 (git のブランチなど) は precmd が作るので、cd の後に回し直す。回した関数の rc はプロンプトの %? に出ない
    # (zle は widget の間も直前のコマンドの終了コードを保つ。tests/tmux/test_treefiler_popup_cd.sh が %? を照合する)
    (( ${+functions[precmd]} )) && precmd
    for fn in $precmd_functions; do
      "$fn"
    done
  fi
  zle reset-prompt
}

_treefiler_cd_mark_ready() {
  local p
  local -a live
  for p in ${=$(command tmux show -pqv -t "$TMUX_PANE" @treefiler_cd_ready)}; do
    [[ $p == <-> && $p != $$ ]] && kill -0 $p 2>/dev/null && live+=($p)
  done
  command tmux set -p -t "$TMUX_PANE" @treefiler_cd_ready "${(j: :)live} $$"
}

if [[ -o interactive && -n $TMUX_PANE ]]; then
  zle -N _treefiler_cd_widget
  bindkey -M emacs "$TREEFILER_CD_KEYS" _treefiler_cd_widget
  bindkey -M viins "$TREEFILER_CD_KEYS" _treefiler_cd_widget
  bindkey -M vicmd "$TREEFILER_CD_KEYS" _treefiler_cd_widget # 無いと vi のコマンドモードでは 1 字ずつ解釈され、~ が行の大文字と小文字を入れ替える
  # popup が「このペインのこの zsh はキー列を受けられる」と分かるように、自分の pid を一覧に足す (popup は、一覧の pid が端末の前面で
  # 入力を待っているときだけ送る。scripts/tmux_treefiler_popup.sh)。上書きにしないのは、入れ子の zsh を抜けた後も外側の zsh が
  # 受けられるようにするため。抜けた zsh の pid は次に足すときに外す。シェルの起動を待たせないよう裏で打つ
  (_treefiler_cd_mark_ready >/dev/null 2>&1 &)
fi
