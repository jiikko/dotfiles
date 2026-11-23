#!/usr/bin/env bash
# prefix f の popup (scripts/tmux_treefiler_popup.sh) を q で閉じたら、開いたペインの zsh が cd することを隔離サーバで確かめる。
# 固定する不変条件:
#   - bind f が開いたペインを @treefiler_pane に控えてから popup でスクリプトを開く (popup のコマンドは format を展開しない)
#   - ペインの zsh (zshlib/_treefiler_cd.zsh を読んだもの) は、打ちかけのコマンド行を残したまま cd し、プロンプトを描き直す
#   - 何も書かれない (esc で閉じた) とき・前面が zsh でない / zsh のスクリプトが入力を待つとき・コピーモード・同期中の window・
#     受け口の無い zsh (zsh -f) には、キー列を送らない。入れ子の zsh を抜けた後も外側の zsh には届く
#
# 🚨 popup とキー押下そのものは自動テストで再現できない (display-popup は attach したクライアントが要り、send-keys は
# key table を通らない。tests/tmux/test_ctrl_v_paste.sh と同じ)。bind の登録と、popup の中で走るスクリプトに分けて固定する。
# 🚨 socket 隔離: -L の一意な名前を使い、スクリプトの中の素の tmux は $TMUX に隔離サーバの socket を入れて向ける
# ($TMUX は TMUX_TMPDIR より優先される。_claude/rules/tmux-probe-requires-socket-isolation.md)。
set -uo pipefail
unset CDPATH TMUX TMUX_PANE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONF="$ROOT_DIR/_tmux.conf"
POPUP="$ROOT_DIR/scripts/tmux_treefiler_popup.sh"
# shellcheck source=tests/lib/utf8_locale.sh
. "$ROOT_DIR/tests/lib/utf8_locale.sh"
export DOTFILES_DIR="$ROOT_DIR"
SOCK="tfcd-test-$$"
TMP_DIR="$(cd "$(mktemp -d)" && pwd -P)"

# shellcheck source=tests/tmux/lib/kill_socket.sh
. "$ROOT_DIR/tests/tmux/lib/kill_socket.sh"
cleanup() {
  tt_tmux_kill_socket "$SOCK" || {
    printf '✗ 隔離サーバを止められなかった (上の stderr の回収手順を実行すること)\n' >&2
    rm -rf "$TMP_DIR"
    exit 1
  }
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

fail=0
ok()  { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1" >&2; fail=1; }
# shellcheck source=tests/lib/wait_until.sh
. "$ROOT_DIR/tests/lib/wait_until.sh"

t() { tmux -L "$SOCK" "$@"; }
pane_opt() { t show -pqv -t "$1" "$2" 2>/dev/null; }
pane_path_is() { [ "$(t display -p -t "$1" '#{pane_current_path}' 2>/dev/null)" = "$2" ]; }
pane_cmd_is() { [ "$(t display -p -t "$1" '#{pane_current_command}' 2>/dev/null)" = "$2" ]; }
# 一覧は空白で区切った pid なので、語に割って数える
# shellcheck disable=SC2086
ready_count() { local r; r=$(pane_opt "$1" @treefiler_cd_ready); set -- $r; echo $#; }
pane_ready_n() { [ "$(ready_count "$1")" = "$2" ]; }
pane_has() { grep -qF -- "$2" <<< "$(t capture-pane -p -t "$1" 2>/dev/null)"; }

if t ls >/dev/null 2>&1; then
  bad "隔離できていない (このソケットに既存サーバがある)"
  exit 1
fi

mkdir -p "$TMP_DIR/zd" "$TMP_DIR/start" "$TMP_DIR/dest dir/日本" "$TMP_DIR/ptmp"
DEST="$TMP_DIR/dest dir/日本"
# keymap は本番の _zshrc と同じく emacs を明示する (指定しないと EDITOR に vi を含むかで決まり、開発機と CI で変わる)。
# TF_VI があれば vi (viins) で試す
cat > "$TMP_DIR/zd/.zshrc" <<EOF
if [[ -n \$TF_VI ]]; then bindkey -v; else bindkey -e; fi
source "$ROOT_DIR/zshlib/_treefiler_cd.zsh"
setopt prompt_subst
PROMPT='%1~ \${_t_dir} %?> '
# 本番の _zshrc の git のプロンプトと同じく、プロンプトの一部を precmd で作る (widget が cd の後に回し直したかを _t_dir で見る。
# 0 を返しても、%? は直前のコマンドの値のまま出ることも照合する)
_t_precmd() { _t_dir=\${PWD:t}; return 0; }
precmd_functions+=(_t_precmd)
# ^R の fzf-history-widget と同じ形: widget の中のコマンド置換で、端末から読むコマンドを動かす (C-x c で起こし、cat が C-d まで読む)。
# widget の中の stdin は端末でないので、fzf と同じく /dev/tty を開いて読ませる
_t_comsub() { local x; x=\$(cat </dev/tty); BUFFER=\$x; }
zle -N _t_comsub
bindkey '^Xc' _t_comsub
EOF
# 偽の treefiler: --cwd-file の先に FAKE_TF_WRITE を書く (unset なら書かない = esc で閉じた)
cat > "$TMP_DIR/fake-treefiler" <<'EOF'
#!/bin/sh
[ "$1" = --cwd-file ] || exit 97
[ -n "${FAKE_TF_WRITE+x}" ] && printf '%s' "$FAKE_TF_WRITE" > "$2"
exit 0
EOF
chmod +x "$TMP_DIR/fake-treefiler"

zsh_cmd="env ZDOTDIR=$TMP_DIR/zd zsh -i"
t -f "$CONF" new-session -d -s main -c "$TMP_DIR/start" -x 120 -y 30 "$zsh_cmd" 2>/dev/null
SOCK_PATH="$(t display -p '#{socket_path}')"

# popup の中で走るのと同じく、スクリプトの中の素の tmux を隔離サーバへ向けて呼ぶ。ペインは本番と同じく bind が控える
# @treefiler_pane から読ませる (引数で渡すと、本番の読む経路を通らない)。一時 dir は ptmp に作らせ、残っていないかを最後に見る
run_popup() {  # $1=ペイン ("" なら @treefiler_pane を消して呼ぶ)
  if [ -n "$1" ]; then t set -g @treefiler_pane "$1"; else t set -gu @treefiler_pane; fi
  TMUX="$SOCK_PATH,0,0" TMPDIR="$TMP_DIR/ptmp" TREEFILER_POPUP_BIN="$TMP_DIR/fake-treefiler" "$POPUP"
}
fg_pgid_of() {  # $1=ペイン
  ps -o tpgid= -t "$(t display -p -t "$1" '#{pane_tty}' | sed 's,^/dev/,,')" 2>/dev/null | awk 'NR == 1 { print $1 }'
}
pane_fg_is() { [ "$(fg_pgid_of "$1")" = "$2" ]; }
pane_fg_not() { local g; g=$(fg_pgid_of "$1"); [ -n "$g" ] && [ "$g" != "$2" ]; }

# --- 1. bind f の登録 ---
binds="$(t list-keys -T prefix 2>/dev/null)"
# list-keys は書いた形でなく tmux の正規の形 (-Fg・引用符つき) で出す
if grep -q ' f  *set-option -Fg @treefiler_pane "#{pane_id}" \\; display-popup .*tmux_treefiler_popup.sh' <<< "$binds"; then
  ok "bind f が @treefiler_pane を控えてから popup でスクリプトを開く"
else
  bad "bind f の形が違う: $(grep ' f ' <<< "$binds")"
fi

# 送っていないことの判定: 行き先の option が置かれず、キー列の文字が画面に出ていない
not_sent() { [ -z "$(pane_opt "$1" @treefiler_cd)" ] && ! pane_has "$1" '9119'; }

# --- 2. q で閉じた: 打ちかけの行と直前の終了コードを残したまま cd する ---
P1="$(t display -p -t main '#{pane_id}')"
tt_wait_until pane_ready_n "$P1" 1 || bad "zsh が @treefiler_cd_ready を立てない"
outer=$(fg_pgid_of "$P1")
t send-keys -t "$P1" -l 'false' && t send-keys -t "$P1" Enter
# capture-pane は行末の空白を落とすので、プロンプトの末尾の空白までは照合しない
tt_wait_until pane_has "$P1" 'start start 1>' || bad "false の後のプロンプトが出ない"
t send-keys -t "$P1" -l 'echo partial'
tt_wait_until pane_has "$P1" 'echo partial' || bad "打ちかけの文字が出ない"
FAKE_TF_WRITE="$DEST" run_popup "$P1"
if tt_wait_until pane_path_is "$P1" "$DEST"; then ok "q で閉じるとペインの zsh が cd する (空白・日本語入りのパス)"; else bad "cd しない: $(t display -p -t "$P1" '#{pane_current_path}')"; fi
if tt_wait_until pane_has "$P1" '日本 日本 1> echo partial'; then ok "打ちかけの行と %? を残し、precmd を回してプロンプトを描き直す"; else bad "行かプロンプトが違う: $(t capture-pane -p -t "$P1" | grep '>' | tail -2)"; fi
if [ -z "$(pane_opt "$P1" @treefiler_cd)" ]; then ok "読んだ行き先を消す"; else bad "@treefiler_cd が残った"; fi
t send-keys -t "$P1" C-u

# --- 3. esc で閉じた (何も書かない): 何もしない ---
run_popup "$P1"
if pane_path_is "$P1" "$DEST" && not_sent "$P1"; then ok "書かれていなければ何もしない"; else bad "esc で何かした"; fi

# --- 4. 前面が zsh でない (cat): 送らない ---
t send-keys -t "$P1" -l 'cat' && t send-keys -t "$P1" Enter
tt_wait_until pane_cmd_is "$P1" cat || bad "cat が前面にならない"
FAKE_TF_WRITE="$TMP_DIR/start" run_popup "$P1"
if not_sent "$P1"; then ok "前面が zsh でなければ送らない"; else bad "cat にキー列を送った"; fi
# 抜けた zsh の pid が後のジョブ (ここでは cat) に再利用された形: 前面の pgid が一覧にあっても、zsh でなければ送らない
saved=$(pane_opt "$P1" @treefiler_cd_ready)
t set -p -t "$P1" @treefiler_cd_ready "$saved $(fg_pgid_of "$P1")"
FAKE_TF_WRITE="$TMP_DIR/start" run_popup "$P1"
if not_sent "$P1"; then ok "一覧の pid が zsh でないプロセスなら送らない (pid の再利用)"; else bad "pid を再利用した cat にキー列を送った"; fi
t set -p -t "$P1" @treefiler_cd_ready "$saved"
t send-keys -t "$P1" C-d
tt_wait_until pane_cmd_is "$P1" zsh || bad "cat が終わらない"

# --- 5. zsh のスクリプトが入力を待っている (前面の名前は zsh): 送らない ---
t send-keys -t "$P1" -l "zsh -fc 'read -r line; print -r got=\$line'" && t send-keys -t "$P1" Enter
# 画面や前面の名前では待たない (外側の zsh も zsh)。端末の前面が外側の zsh から移ったことを待つ
tt_wait_until pane_fg_not "$P1" "$outer" || bad "スクリプトの zsh が前面にならない"
FAKE_TF_WRITE="$TMP_DIR/start" run_popup "$P1"
t send-keys -t "$P1" -l 'typed' && t send-keys -t "$P1" Enter
if tt_wait_until pane_has "$P1" 'got=typed' && not_sent "$P1"; then ok "入力を待つ zsh のスクリプトには送らない"; else bad "スクリプトにキー列を送った: $(t capture-pane -p -t "$P1" | grep got= | tail -1)"; fi

# --- 5b. widget の中のコマンド置換が端末から読んでいる (^R の fzf と同じ形): 送らない ---
# macOS の ps の comm はフルパス (/bin/cat) のことがあるので末尾で照合する
tty_has_cat() { grep -q '/cat$\|^cat$' <<< "$(ps -o comm= -t "$(t display -p -t "$1" '#{pane_tty}' | sed 's,^/dev/,,')" 2>/dev/null)"; }
t send-keys -t "$P1" C-x c
if tt_wait_until tty_has_cat "$P1"; then
  FAKE_TF_WRITE="$TMP_DIR/start" run_popup "$P1"
  if [ -z "$(pane_opt "$P1" @treefiler_cd)" ]; then ok "widget のコマンド置換が読んでいる間は送らない"; else bad "widget のコマンド置換にキー列を送った"; fi
  # 自分が起こした cat を pid で止める。C-d は送らない: zle が端末を raw にしているので EOF にならず、cat が読み続けて後のケースの
  # キーを全部吸う (前面は初めから zsh のグループなので、前面の戻りでは終わったことを待てない)
  cat_pid=$(ps -o pid=,comm= -t "$(t display -p -t "$P1" '#{pane_tty}' | sed 's,^/dev/,,')" | awk '$2 ~ /(^|\/)cat$/ { print $1 }')
  for p in $cat_pid; do kill "$p"; done
  tty_no_cat() { ! tty_has_cat "$1"; }
  tt_wait_until tty_no_cat "$P1" || bad "widget の cat が終わらない"
else
  bad "widget の中の cat が起きない"
fi
t send-keys -t "$P1" C-u
t set -pu -t "$P1" @treefiler_cd

# --- 6. コピーモード・同期中の window: 送らない ---
t copy-mode -t "$P1"
FAKE_TF_WRITE="$TMP_DIR/start" run_popup "$P1"
if [ -z "$(pane_opt "$P1" @treefiler_cd)" ] && [ "$(t display -p -t "$P1" '#{pane_in_mode}')" = 1 ]; then ok "コピーモードには送らない"; else bad "コピーモードに送った"; fi
t send-keys -t "$P1" -X cancel
t set -w -t "$P1" synchronize-panes on
FAKE_TF_WRITE="$TMP_DIR/start" run_popup "$P1"
if not_sent "$P1" && pane_path_is "$P1" "$DEST"; then ok "同期中の window には送らない"; else bad "同期中の window に送った"; fi
t set -w -t "$P1" synchronize-panes off

# --- 7. 入れ子の zsh を抜けた後も、外側の zsh に届く ---
outer=$(fg_pgid_of "$P1")
t send-keys -t "$P1" -l "$zsh_cmd" && t send-keys -t "$P1" Enter
tt_wait_until pane_ready_n "$P1" 2 || bad "入れ子の zsh が一覧に足さない: $(pane_opt "$P1" @treefiler_cd_ready)"
t send-keys -t "$P1" -l 'exit' && t send-keys -t "$P1" Enter
# 画面の行で待たない (同じプロンプトが前のケースで出ている)。端末の前面が外側の zsh に戻ったことを待つ
tt_wait_until pane_fg_is "$P1" "$outer" || bad "外側の zsh に戻らない"
FAKE_TF_WRITE="$TMP_DIR/start" run_popup "$P1"
if tt_wait_until pane_path_is "$P1" "$TMP_DIR/start"; then ok "入れ子の zsh を抜けた後も外側の zsh が cd する"; else bad "外側の zsh に届かない"; fi

# --- 8. 受け口の無い zsh (zsh -f): 送らない ---
t new-window -d -t main -n plain -c "$TMP_DIR/start" "zsh -f"
P2="$(t display -p -t main:plain '#{pane_id}')"
tt_wait_until pane_cmd_is "$P2" zsh || bad "zsh -f が起きない"
FAKE_TF_WRITE="$DEST" run_popup "$P2"
if not_sent "$P2" && pane_path_is "$P2" "$TMP_DIR/start"; then
  ok "受け口の無い zsh には送らない"
else
  bad "zsh -f にキー列を送った: $(t capture-pane -p -t "$P2" | tail -2)"
fi

# --- 9. vi の keymap の zsh にも届く ---
t new-window -d -t main -n vi -c "$TMP_DIR/start" "env TF_VI=1 $zsh_cmd"
P3="$(t display -p -t main:vi '#{pane_id}')"
tt_wait_until pane_ready_n "$P3" 1 || bad "vi の zsh が @treefiler_cd_ready を立てない"
FAKE_TF_WRITE="$DEST" run_popup "$P3"
if tt_wait_until pane_path_is "$P3" "$DEST"; then ok "vi の keymap の zsh も cd する"; else bad "vi の keymap の zsh が cd しない"; fi
# コマンドモード (vicmd) でも届く。打ちかけの行は壊さない
t send-keys -t "$P3" -l 'echo Vi' && t send-keys -t "$P3" Escape
FAKE_TF_WRITE="$TMP_DIR/start" run_popup "$P3"
if tt_wait_until pane_path_is "$P3" "$TMP_DIR/start" && tt_wait_until pane_has "$P3" 'start start 0> echo Vi'; then
  ok "vi のコマンドモードでも cd し、打ちかけの行を残す"
else
  bad "vi のコマンドモードで cd しないか行が変わった: $(t capture-pane -p -t "$P3" | grep '>' | tail -1)"
fi

# --- 10. 開いたペインが分からない (@treefiler_pane が空): treefiler だけ開いて何も送らない ---
before=$(t display -p -t "$P3" '#{pane_current_path}')
FAKE_TF_WRITE="$DEST" run_popup ""
if pane_path_is "$P3" "$before" && [ -z "$(pane_opt "$P3" @treefiler_cd)" ]; then ok "@treefiler_pane が空なら送らない"; else bad "@treefiler_pane が空でも何かした"; fi

leftover=$(ls -A "$TMP_DIR/ptmp")
if [ -z "$leftover" ]; then ok "popup の一時 dir を残さない"; else bad "popup の一時 dir が残った: $leftover"; fi

exit "$fail"
