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
#   Ctrl-B      … push (clean 画面では p。確認なしの一発 push はユーザー指定)
#                 ⚠️ C-p にしない: fzf のカーソル上移動の定番キーで誤 push した実績あり (2026-07-18)
#   Ctrl-D      … フォーカス中ファイルの diff を全画面 (less)
#   Esc         … 閉じる
#
# fzf の execute/reload からはこのスクリプト自身をサブコマンド付きで再入する
# ($0 list/toggle/preview/commit)。シェル関数を fzf に渡せないことへの定石。
# パスの空白は git status --short の quote を剥いで対応する (改行入り等の病的なパスは
# 対象外 = tests/ の発見規約と同じ前提)。
set -eu

self="$0"

# 配色定数 (単一ソース theme/colors.yml から生成。役割の対応は docs/theme-colors.md)
# shellcheck source=scripts/lib/theme_colors.sh
. "$(dirname "$0")/lib/theme_colors.sh"

sgr() { printf '\033[%sm' "$1"; }
RESET=$(sgr 0)
DIM=$(sgr 2)
FG_GREEN=$(sgr "38;5;$THEME_ACTIVE_GREEN")
FG_CYAN=$(sgr "38;5;$THEME_INFO_CYAN")
FG_ORANGE=$(sgr "38;5;$THEME_MARKER_ORANGE")
BADGE_ON=$(sgr "1;38;5;16;48;5;$THEME_ACTIVE_GREEN")
# dots の色語彙 (ユーザー指定 2026-07-18): 未 push = 灰 (まだ確定していない/消灯)・
# push 済み = 緑 (確定済み)。「未 push を橙で目立たせる」逆案は直感と逆と却下済み
DOT_UNPUSHED=$(sgr "38;5;$THEME_COLD_GRAY")●$RESET
DOT_PUSHED=$(sgr "38;5;$THEME_ACTIVE_GREEN")●$RESET

# working tree が clean なときのサマリ画面 (空の fzf リストは寂しいので、反転バッジ +
# ブランチ同期状態 + 未 push ドットグラフ + 直近コミットを出す)。素の ANSI 256 色のみで
# 描く (gum 不要 = degrade 分岐なし)。配色は冒頭で source した theme/colors.yml 由来の定数。
show_clean() {
  branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || printf 'detached')
  ahead=0
  behind=0
  sync="${DIM}upstream なし${RESET}"
  upstream=$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || :)
  if [ -n "$upstream" ]; then
    counts=$(git rev-list --left-right --count "$upstream...HEAD" 2>/dev/null || printf '0 0')
    behind=$(printf '%s' "$counts" | awk '{print $1}')
    ahead=$(printf '%s' "$counts" | awk '{print $2}')
    ahead=${ahead:-0}
    behind=${behind:-0}
    if [ "$ahead" = 0 ] && [ "$behind" = 0 ]; then
      sync="${FG_GREEN}✔ $upstream と同期${RESET}"
    elif [ "$behind" = 0 ]; then
      sync="${FG_ORANGE}↑$ahead${RESET} ${DIM}push 待ち ($upstream)${RESET}"
    elif [ "$ahead" = 0 ]; then
      sync="${FG_CYAN}↓$behind${RESET} ${DIM}pull 待ち ($upstream)${RESET}"
    else
      sync="${FG_ORANGE}↑$ahead${RESET} ${FG_CYAN}↓$behind${RESET} ${DIM}$upstream と分岐${RESET}"
    fi
  fi
  printf '\n\n   %s  ✔ CLEAN  %s  %s⎇ %s%s %s·%s %s\n\n' \
    "$BADGE_ON" "$RESET" "$FG_CYAN" "$branch" "$RESET" "$DIM" "$RESET" "$sync"
  # 未 push があるときだけ、直近 20 commit を dots で可視化 (灰 = 未 push・緑 = push 済み)
  if [ "$ahead" -gt 0 ] 2>/dev/null; then
    total=$(git rev-list --count --max-count=20 HEAD 2>/dev/null || printf '0')
    dots=''
    i=0
    while [ "$i" -lt "${total:-0}" ]; do
      if [ "$i" -lt "$ahead" ]; then
        dots="$dots$DOT_UNPUSHED"
      else
        dots="$dots$DOT_PUSHED"
      fi
      i=$((i + 1))
    done
    printf '   %s  %s← 未 push %s / 最新 %s commit%s\n\n' "$dots" "$DIM" "$ahead" "$total" "$RESET"
  fi
  # --no-pager 必須: popup 内は stdout が tty なので、素の git log は less の alternate
  # screen を開いてしまい、直前に描いたバッジ/ドット行が画面ごと消える (実機で再現済み)
  git --no-pager log -5 --color=always --date=format:'%H:%M' \
    --format="   %C($THEME_QUANTITY_YELLOW)%h%Creset %C(dim)%cd%Creset %<(58,trunc)%s" 2>/dev/null || :
  if [ -n "$upstream" ]; then
    printf '\n   %s(p: push / 他キー: 閉じる)%s\n' "$DIM" "$RESET"
  else
    printf '\n   %s(何かキーで閉じる)%s\n' "$DIM" "$RESET"
  fi
  key=$(wait_key)
  if [ "$key" = p ] && [ -n "$upstream" ]; then
    push_current
    clear 2>/dev/null || :
    show_clean   # push 後の同期状態を再描画 (↑N → ✔ 同期 が見える)
  fi
}

# 1 キー待ち。押されたキーを stdout へ返す (canonical mode だと Enter まで待ってしまう
# ので raw に落とす)。非 tty では待たず空を返す。
wait_key() {
  [ -t 0 ] || return 0
  old=$(stty -g 2>/dev/null) || return 0
  stty -icanon -echo min 1 time 0
  dd bs=1 count=1 2>/dev/null || :
  stty "$old"
}

# 現在ブランチを upstream へ push する (clean 画面の p / fzf の C-p から)。
# 確認なしの一発 push はユーザー指定 (「p とか押すと push されるといい」)。
push_current() {
  printf '\n   %spushing...%s\n' "$DIM" "$RESET"
  if git push; then
    sleep 1
  else
    printf '   %spush 失敗 (何かキーで戻る)%s\n' "$DIM" "$RESET"
    wait_key >/dev/null
  fi
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
push)
  push_current
  exit 0
  ;;
"") ;;  # 引数なし = メイン (fzf 起動) へ
*)
  printf 'usage: tmux_git_popup.sh [list|toggle|preview|commit|push]\n' >&2
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
  --header="[$branch] Tab/Enter: stage⇄unstage  C-a: 全add  C-o: commit  C-b: push  C-d: diff全画面  Esc: 閉じる" \
  --preview="\"$self\" preview {}" \
  --preview-window='right:55%:wrap' \
  --bind "tab:execute-silent(\"$self\" toggle {})+reload(\"$self\" list)" \
  --bind "enter:execute-silent(\"$self\" toggle {})+reload(\"$self\" list)" \
  --bind "ctrl-a:execute-silent(git add -A)+reload(\"$self\" list)" \
  --bind "ctrl-o:execute(\"$self\" commit)+reload(\"$self\" list)" \
  --bind "ctrl-b:execute(\"$self\" push)+reload(\"$self\" list)" \
  --bind "ctrl-d:execute(\"$self\" preview {} | less -R)" \
  || :
