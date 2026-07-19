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
#   Ctrl-B      … push (clean 画面では p)。gum confirm (デフォルト No) を挟む
#                 ⚠️ C-p にしない: fzf のカーソル上移動の定番キーで誤 push した実績あり (2026-07-18)
#   Ctrl-D      … フォーカス中ファイルの diff を全画面 (less)
#   Ctrl-L      … log ⇄ changes を切り替え
#   Ctrl-G/Esc  … 閉じる (fzf)
#   l           … clean 画面から log 画面へ
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
FG_RED=$(sgr "38;5;$THEME_ERROR_RED")
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
  if [ -n "$upstream" ]; then
    printf '\n   %s(l: log / p: push / 他キー: 閉じる)%s\n' "$DIM" "$RESET"
  else
    printf '\n   %s(l: log / 何かキーで閉じる)%s\n' "$DIM" "$RESET"
  fi
  key=$(wait_key)
  if [ "$key" = p ] && [ -n "$upstream" ]; then
    push_current
    clear 2>/dev/null || :
    show_clean; return $?   # push 後の同期状態を再描画 (↑N → ✔ 同期)。再帰先の l/閉じるの合図を伝播
  elif [ "$key" = l ]; then
    return 10
  fi
  return 0
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

# 現在ブランチを upstream へ push する (clean 画面の p / fzf の C-b から)。
# 実行前に確認を挟む (当初は確認なし一発 push のユーザー指定だったが、誤爆を受けて
# confirm 追加へ変更 2026-07-18)。gum confirm は kill 確認と同じ --default=false。
# push できない状態 (upstream なし / 未 push コミットなし) では git を黙って走らせず、
# 明示メッセージ + キー待ちで返す (無言・一瞬表示で「何が起きたか分からない」の防止)。
push_current() {
  up=$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || :)
  if [ -z "$up" ]; then
    printf '\n   upstream がありません (git push -u origin <branch> で設定してから)\n   %s(何かキーで戻る)%s\n' "$DIM" "$RESET"
    wait_key >/dev/null
    return 0
  fi
  n=$(git rev-list --count "$up..HEAD" 2>/dev/null || printf '0')
  if [ "${n:-0}" = 0 ]; then
    printf '\n   未 push のコミットはありません (%s と同期済み)\n   %s(何かキーで戻る)%s\n' "$up" "$DIM" "$RESET"
    wait_key >/dev/null
    return 0
  fi
  if command -v gum >/dev/null 2>&1; then
    gum confirm --default=false "↑$n commit を $up へ push しますか?" || return 0
  else
    printf '\n   ↑%s commit を %s へ push しますか? [y/N] ' "$n" "$up"
    IFS= read -r ans || ans=''
    case "$ans" in y|Y|yes) ;; *) return 0 ;; esac
  fi
  printf '\n   %spushing... (↑%s → %s)%s\n' "$DIM" "$n" "$up" "$RESET"
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

# 選択コミット SHA の CI job 状態を glog 風 (✓/✗/●/○ + job 名) に出力する best-effort ヘルパー。
# 主目的は diff プレビューで、CI は「あれば添える」。以下では静かに何も出さず抜ける:
#   gh 未導入 / 非 GitHub remote / オフライン / CI 結果が無いコミット。
# gh 本体 (glog) は read-only ツールとして拡張しない方針のため、CI 取得は popup 側で gh を
# 直接叩く。カーソル移動ごとに gh を叩くと重いので sha 単位で 60 秒ディスクキャッシュする
# (走行中 job は 60 秒で最新化)。glog とは別キャッシュ (フォーマット結合を避ける)。
ci_status_lines() {
  sha=$1
  command -v gh >/dev/null 2>&1 || return 0
  cache_dir="${XDG_CACHE_HOME:-$HOME/.cache}/tmux_git_popup"
  cache="$cache_dir/ci-$sha"
  # 60 秒以内の cache が無ければ取得し直す (空ファイルも「取得済み」として 60 秒は再取得しない)
  if ! find "$cache" -mmin -1 2>/dev/null | grep -q .; then
    mkdir -p "$cache_dir" 2>/dev/null || return 0
    # 一時ファイルへ書いて成功/失敗どちらでも atomic に mv する。fzf はカーソル移動で
    # preview を kill するため、cache を直接 truncate/write すると中断で壊れた内容が 60 秒
    # 有効化されうる。gh 失敗時は空ファイルを置いて 60 秒は再取得を抑止する (degrade 維持)。
    # kill されて mv 前に死んだ場合は cache 未更新のまま (tmp が孤児化するのみ)。
    tmp="$cache.$$"
    gh api "repos/{owner}/{repo}/commits/$sha/check-runs" \
      --jq '.check_runs[] | "\(.conclusion // .status)\t\(.name)"' >"$tmp" 2>/dev/null || :
    mv -f "$tmp" "$cache" 2>/dev/null || rm -f "$tmp"
  fi
  [ -s "$cache" ] || return 0
  printf '%s─── CI ───%s\n' "$DIM" "$RESET"
  tab=$(printf '\t')
  while IFS="$tab" read -r state name; do
    case "$state" in
      success) sym="${FG_GREEN}✓${RESET}" ;;
      failure|cancelled|timed_out|action_required|startup_failure) sym="${FG_RED}✗${RESET}" ;;
      skipped|neutral|stale) sym="${DIM}○${RESET}" ;;
      *) sym="${FG_ORANGE}●${RESET}" ;;  # in_progress / queued / pending 等の進行中
    esac
    printf '  %s %s\n' "$sym" "$name"
  done < "$cache"
  printf '%s──────────%s\n\n' "$DIM" "$RESET"
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
logpreview)
  # log mode の preview: 選択コミットの CI job (glog 風) を上に添え、下に git show の diff。
  sha="${2:-}"
  [ -n "$sha" ] || exit 0
  ci_status_lines "$sha"
  git show --color=always "$sha"
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
-h|--help|help)
  cat <<'EOF'
tmux_git_popup.sh — git 操作 popup (fzf)。tmux の C-g / C-t g から開くが、直接実行も可 (cwd の repo が対象)

使い方: tmux_git_popup.sh              # fzf UI を起動 (clean ならサマリ画面)
        tmux_git_popup.sh --help

キー (ファイル一覧):
  タイプ      ファイル絞り込み
  Tab/Enter   stage ⇄ unstage トグル
  Ctrl-A      全部 add
  Ctrl-O      commit (メッセージ入力)
  Ctrl-B      push (確認あり)
  Ctrl-D      diff 全画面 (less)
  Esc         閉じる

clean 画面 (変更なしのとき):
  l           log に戻る
  p           push (確認あり)
  他キー      閉じる

changes 画面 (変更ありのとき):
  Ctrl-L      log に戻る

log 画面 (初期画面):
  プレビュー   選択コミットの CI job (gh 取得・glog 風の ✓/✗/●) + git show の diff
  Ctrl-L      changes に切り替え
  Ctrl-B      push (確認あり)
  Enter       選択コミットの diff を全画面表示
  Ctrl-G/Esc  閉じる

内部サブコマンド (fzf の execute/reload から再入する用): list toggle preview commit push
EOF
  exit 0
  ;;
"") ;;  # 引数なし = メイン (fzf 起動) へ
*)
  printf 'usage: tmux_git_popup.sh [--help]\n' >&2
  exit 2
  ;;
esac

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'ここは git repo ではありません: %s\n' "$(pwd)"
  sleep 1.2
  exit 1
fi

branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || printf 'detached')

# header は 2 行に分ける (1 行だと popup 幅で末尾が見切れる。実機報告 2026-07-18)。
# 末尾の「C-l: log  C-g/Esc: 閉じる」は changes fzf 起動時に付け足す (下記)
header="[$branch] Tab/Enter: stage⇄unstage  C-a: 全add  C-o: commit
C-b: push  C-d: diff全画面"

mode=log
while :; do
  if [ "$mode" = log ]; then
    out=$(git log --oneline --color=always | fzf --ansi --no-sort --layout=reverse \
      --prompt='log> ' \
      --header="[$branch] log  C-l: changes  C-b: push  Enter: 全画面diff  C-g/Esc: 閉じる" \
      --expect=ctrl-l \
      --preview="\"$self\" logpreview {1}" \
      --preview-window='right:55%:wrap' \
      --bind ctrl-g:abort \
      --bind "ctrl-b:execute(\"$self\" push)" \
      --bind 'enter:execute(git show --color {1} | less -R)' \
      || :)
    key=$(printf '%s' "$out" | head -n 1)
    if [ "$key" = ctrl-l ]; then
      mode=changes
      continue
    fi
    break
  fi

  entries=$("$self" list)
  if [ -z "$entries" ]; then
    rc=0
    show_clean || rc=$?
    if [ "$rc" = 10 ]; then
      mode=log
      continue
    fi
    break
  fi

  out=$(printf '%s\n' "$entries" | fzf --expect=ctrl-l --ansi --no-sort --layout=reverse \
    --prompt='git> ' \
    --header="$header  C-l: log  C-g/Esc: 閉じる" \
    --preview="\"$self\" preview {}" \
    --preview-window='right:55%:wrap' \
    --bind ctrl-g:abort \
    --bind "tab:execute-silent(\"$self\" toggle {})+reload(\"$self\" list)" \
    --bind "enter:execute-silent(\"$self\" toggle {})+reload(\"$self\" list)" \
    --bind "ctrl-a:execute-silent(git add -A)+reload(\"$self\" list)" \
    --bind "ctrl-o:execute(\"$self\" commit)+reload(\"$self\" list)" \
    --bind "ctrl-b:execute(\"$self\" push)+reload(\"$self\" list)" \
    --bind "ctrl-d:execute(\"$self\" preview {} | less -R)" \
    || :)
  key=$(printf '%s' "$out" | head -n 1)
  if [ "$key" = ctrl-l ]; then
    mode=log
    continue
  fi
  break
done
