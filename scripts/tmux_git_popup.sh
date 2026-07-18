#!/bin/sh
# tmux_git_popup.sh — いま見ているペインの repo に対する git 操作 popup (fzf)。
# _tmux.conf の bind a (display-popup -E -d '#{pane_current_path}') から起動される。
# popup は対象ペインの cwd で開くため、nvim / claude のペインからでも「その repo」に
# 対して操作でき、zsh の window を探す必要をなくすのが目的。
# 操作感は zsh の C-r と同じインクリメンタル絞り込み。対象 = status / diff /
# add (stage toggle) / commit。hunk 単位 stage 等の深い操作は意図的にスコープ外
# (必要になったら lazygit の popup 併用を検討する)。
#
# キー:
#   タイプ      … ファイル絞り込み (fzf)
#   Tab / Enter … stage ⇄ unstage トグル (worktree 側に変更があれば add、staged のみなら unstage)
#   Ctrl-A      … 全部 add (git add -A)
#   Ctrl-O      … commit (gum input でメッセージ。未導入なら素の read)
#   Ctrl-D      … フォーカス中ファイルの diff を全画面 (less)
#   Esc         … 閉じる
#
# fzf の execute/reload からはこのスクリプト自身をサブコマンド付きで再入する
# ($0 list/toggle/preview/commit)。シェル関数を fzf に渡せないことへの定石。
# パスの空白は git status --short の quote を剥いで対応する (改行入り等の病的なパスは
# 対象外 = tests/ の発見規約と同じ前提)。
set -eu

self="$0"

# working tree が clean なときのサマリ画面 (空の fzf リストは寂しいので、ブランチの
# 同期状態と直近コミットを出す)。gum があれば枠付き、無ければ素の色付きテキスト。
show_clean() {
  branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || printf 'detached')
  sync="upstream なし"
  upstream=$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || :)
  if [ -n "$upstream" ]; then
    counts=$(git rev-list --left-right --count "$upstream...HEAD" 2>/dev/null || printf '0 0')
    behind=$(printf '%s' "$counts" | awk '{print $1}')
    ahead=$(printf '%s' "$counts" | awk '{print $2}')
    if [ "${ahead:-0}" = 0 ] && [ "${behind:-0}" = 0 ]; then
      sync="$upstream と同期済み"
    elif [ "${behind:-0}" = 0 ]; then
      sync="$upstream より ↑$ahead 先行 (push 待ち)"
    elif [ "${ahead:-0}" = 0 ]; then
      sync="$upstream より ↓$behind 遅れ (pull 待ち)"
    else
      sync="$upstream と分岐 (↑$ahead ↓$behind)"
    fi
  fi
  body=$(printf '\033[1;38;5;46m   ✔ working tree clean\033[0m\n\n   \033[38;5;51m⎇ %s\033[0m — %s\n\n   \033[2m── 直近のコミット ──\033[0m\n%s' \
    "$branch" "$sync" \
    "$(git log -5 --color=always --format='   %C(yellow)%h%Creset %<(60,trunc)%s %C(dim)%cr%Creset' 2>/dev/null || :)")
  if command -v gum >/dev/null 2>&1; then
    printf '%s\n' "$body" | gum style --border rounded --border-foreground 46 --padding "1 2" --margin "1 1"
  else
    printf '\n%s\n\n' "$body"
  fi
  printf '   \033[2m(何かキーで閉じる)\033[0m\n'
  wait_key
}

# 1 キー待ち (canonical mode だと Enter まで待ってしまうので raw に落とす)。非 tty では待たない。
wait_key() {
  [ -t 0 ] || return 0
  old=$(stty -g 2>/dev/null) || return 0
  stty -icanon -echo min 1 time 0
  dd bs=1 count=1 >/dev/null 2>&1 || :
  stty "$old"
}

# status --short の 1 行からパスを取り出す ("XY PATH" / "XY OLD -> NEW" / quote 付き)。
line_path() {
  p=${1#???}                     # 先頭の "XY " を落とす
  case "$p" in *" -> "*) p=${p#* -> } ;; esac # rename は新パス側
  case "$p" in \"*\") p=${p#\"}; p=${p%\"} ;; esac
  printf '%s\n' "$p"
}

case "${1:-}" in
list)
  # 色は fzf --ansi が解釈する。clean なら空リスト (header だけ残る)
  exec git -c color.status=always status --short
  ;;
toggle)
  [ -n "${2:-}" ] || exit 0
  path=$(line_path "$2")
  worktree=$(printf '%s' "$2" | cut -c2)
  if [ "$worktree" = " " ]; then
    # staged のみ → unstage
    exec git restore --staged -- "$path"
  fi
  # worktree 側に変更あり (?? / M / D 等) → stage (削除も add で stage される)
  exec git add -- "$path"
  ;;
preview)
  [ -n "${2:-}" ] || exit 0
  path=$(line_path "$2")
  index=$(printf '%s' "$2" | cut -c1)
  worktree=$(printf '%s' "$2" | cut -c2)
  if [ "$index" = "?" ]; then
    # untracked は /dev/null との diff で中身を diff 形式表示 (差分ありで exit 1 が正常)
    git diff --color --no-index -- /dev/null "$path" || :
    exit 0
  fi
  if [ "$index" != " " ]; then
    printf '\033[2m── staged ──\033[0m\n'
    git diff --color --cached -- "$path"
  fi
  if [ "$worktree" != " " ]; then
    printf '\033[2m── unstaged ──\033[0m\n'
    git diff --color -- "$path"
  fi
  exit 0
  ;;
commit)
  if git diff --cached --quiet; then
    printf 'staged な変更がありません (Tab で stage してから)\n'
    sleep 1
    exit 0
  fi
  if command -v gum >/dev/null 2>&1; then
    msg=$(gum input --placeholder "commit message" --width 100 || :)
  else
    printf 'commit message: '
    IFS= read -r msg
  fi
  [ -n "$msg" ] || exit 0
  git commit -m "$msg"
  sleep 1  # 結果 (sha / 行数) を一瞬見せてから一覧へ戻る
  exit 0
  ;;
"") ;;  # 引数なし = メイン (fzf 起動) へ
*)
  printf 'usage: tmux_git_popup.sh [list|toggle|preview|commit]\n' >&2
  exit 2
  ;;
esac

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'ここは git repo ではありません: %s\n' "$(pwd)"
  sleep 1.2
  exit 1
fi

branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || printf 'detached')

# clean ならサマリ画面 (fzf 内での add/commit 後に clean になったケースは対象外 —
# その場合は fzf の空リストのまま Esc で閉じる運用)
entries=$("$self" list)
if [ -z "$entries" ]; then
  show_clean
  exit 0
fi

printf '%s\n' "$entries" | fzf --ansi --no-sort --layout=reverse \
  --prompt='git> ' \
  --header="[$branch] Tab/Enter: stage⇄unstage  C-a: 全add  C-o: commit  C-d: diff全画面  Esc: 閉じる" \
  --preview="\"$self\" preview {}" \
  --preview-window='right:55%:wrap' \
  --bind "tab:execute-silent(\"$self\" toggle {})+reload(\"$self\" list)" \
  --bind "enter:execute-silent(\"$self\" toggle {})+reload(\"$self\" list)" \
  --bind "ctrl-a:execute-silent(git add -A)+reload(\"$self\" list)" \
  --bind "ctrl-o:execute(\"$self\" commit)+reload(\"$self\" list)" \
  --bind "ctrl-d:execute(\"$self\" preview {} | less -R)" \
  || :
