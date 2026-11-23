#!/usr/bin/env bash
# prefix f (_tmux.conf の bind f) の popup の中身。usage: tmux_treefiler_popup.sh (引数なし)
# treefiler を開き、q で閉じたら、popup を開いたペインの zsh をそのフォルダへ cd させる。
#
# popup の中のプロセスはペインのシェルの cwd を変えられないので、行き先をペインの option (@treefiler_cd) に置き、
# ペインへ widget のキー列 (TREEFILER_CD_KEYS) だけを送る。cd するのはペインの zsh の widget (zshlib/_treefiler_cd.zsh の
# _treefiler_cd_widget)。`cd ...` の文字列と Enter を送らないのは、打ちかけのコマンド行を壊し、押した後の Enter で実行してしまうため。
#
# 🚨 送るのは、閉じた時点で、widget を読み込んだ zsh (@treefiler_cd_ready に書いた pid の一覧) がペインの前面で入力を待っているときだけ。
# 開いている間にペインで vim などを起こしていたら、キー列はそのアプリへの入力になる。widget の無い zsh (dotfiles を pull する前に
# 起動したシェル・zsh -f) では、キー列が知らないキーとしてコマンド行に入る。コピーモードと同期中の window にも送らない。
# 見分けられないのは、その zsh の組み込み (read・vared・select) が入力を待っている間と、受け口を読んだ zsh から `exec zsh -f` した後
# (pid が同じ)、抜けた zsh の pid が後で zsh のスクリプトに再利用された形 (一覧の pid は次に zsh が起動するまで残る)。
set -uo pipefail

# TREEFILER_POPUP_BIN はテストが偽の treefiler を渡すための差し込み口
bin="${TREEFILER_POPUP_BIN:-${DOTFILES_DIR:-$HOME/dotfiles}/bin/treefiler}"

# 開いたペインは bind が @treefiler_pane に控える (popup のコマンドも -e も format を展開しないので #{pane_id} を渡せない)。
# 読むのは treefiler を開く前 (開いている間に別のペインで prefix f を押すと上書きされる)
pane=$(tmux show -gv @treefiler_pane 2>/dev/null)
if [ -z "$pane" ]; then
  tmux display-message -d 4000 "treefiler: 開いたペインが分からないので cd しません (@treefiler_pane が空)"
  exec "$bin"
fi
keys=$'\e[9119~' # zshlib/_treefiler_cd.zsh の TREEFILER_CD_KEYS と同じ値

work=$(mktemp -d "${TMPDIR:-/tmp}/treefiler-popup.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT

"$bin" --cwd-file "$work/cd"
rc=$?
[ -s "$work/cd" ] || exit "$rc"
IFS= read -r -d '' dest < "$work/cd"

# 送る直前のペインを読む (開いている間にペインで別のコマンドを起こしたかもしれない)
info=$(tmux display -p -t "$pane" '#{@treefiler_cd_ready}|#{pane_tty}|#{pane_in_mode}|#{pane_synchronized}|#{pane_current_command}') || exit "$rc"
IFS='|' read -r ready tty in_mode synced front <<< "$info"
skip() {
  tmux display-message -d 4000 "treefiler: $1"
  exit "$rc"
}
[ "$in_mode" = 1 ] && skip "ペインがコピーモードなので cd しません"
# 同期中の window では send-keys が他のペインにも届く (vim には大文字と小文字の入れ替えのような操作として入る)
[ "$synced" = 1 ] && skip "window がペインの同期 (synchronize-panes) 中なので cd しません"
# 受け口を読み込んだ zsh (@treefiler_cd_ready の pid の一覧。zshlib/_treefiler_cd.zsh) のどれかが、そのペインの端末の前面の
# プロセスグループで入力を待っているときだけ送る。pane_current_command の「zsh」では足りない: zsh のスクリプト (read -q の確認など)
# も zsh と出て、キー列をその入力として読む。対話の zsh は自分のプロセスグループを作るので、コマンドを走らせている間は前面 (tpgid)
# が子のグループに移る
[ -n "$ready" ] || skip "このペインのシェルは cd の受け口を読み込んでいません (zsh を起動し直すと使えます)"
procs=$(ps -o pgid=,tpgid=,comm= -t "${tty#/dev/}" 2>/dev/null)
fg_pgid=$(awk 'NR == 1 { print $2 }' <<< "$procs")
[ -n "$fg_pgid" ] || skip "ペインの端末の前面が分からないので cd しません"
case " $ready " in
*" $fg_pgid "*) ;;
*) skip "ペインで ${front:-別のコマンド} が動いているので cd しません" ;;
esac
# 一覧には抜けた zsh の pid も残る (次に zsh が起動して足すときまで)。その pid が後のジョブに再利用されたときに送らないよう、
# 前面のプロセスが zsh であることも見る
case "$(ps -o comm= -p "$fg_pgid" 2>/dev/null)" in
*zsh) ;;
*) skip "ペインで ${front:-別のコマンド} が動いているので cd しません" ;;
esac
# widget の中のコマンド置換 (^R の fzf-history-widget など) は zsh と同じプロセスグループで端末から読むので、前面は zsh のままになる。
# 前面のグループに zsh のほかのプロセスがいたら送らない。裏で起こすもの (& のジョブ・zsh-autosuggestions の取得) は job control で別のグループになる
in_fg=$(awk -v g="$fg_pgid" '$1 == g { n++ } END { print n + 0 }' <<< "$procs")
[ "$in_fg" = 1 ] || skip "ペインの zsh が別のコマンド (fzf など) に端末を読ませているので cd しません"
tmux set -p -t "$pane" @treefiler_cd "$dest" && tmux send-keys -t "$pane" -l "$keys"
exit "$rc"
