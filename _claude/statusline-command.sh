#!/bin/sh
# Claude Code statusLine command
# Mirrors the zsh PROMPT configuration from ~/.zshrc

input=$(cat)

cwd=$(echo "$input" | jq -r '.workspace.current_dir // .cwd')

# Model display name (e.g. "Opus 4.8"). Provided by Claude Code via stdin JSON.
model_name=$(echo "$input" | jq -r '.model.display_name // empty')

# Shorten the path: replace $HOME with ~, truncate to 50 chars with leading ..
# (置換文字列の ~ は変数経由で渡す。リテラル \~ だとバックスラッシュごと表示される)
home="$HOME"
tilde="~"
short_cwd="${cwd/#$home/$tilde}"
if [ ${#short_cwd} -gt 50 ]; then
  short_cwd="..${short_cwd: -48}"
fi

# Git branch via vcs_info equivalent
branch=""
changed_count=0
untracked_count=0
if git -C "$cwd" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  branch=$(git -C "$cwd" symbolic-ref --short HEAD 2>/dev/null || git -C "$cwd" rev-parse --short HEAD 2>/dev/null)
  porcelain=$(git -C "$cwd" status --porcelain 2>/dev/null)
  if [ -n "$porcelain" ]; then
    changed_count=$(printf "%s\n" "$porcelain" | grep -c "^[^?]")
    untracked_count=$(printf "%s\n" "$porcelain" | grep -c "^??")
  fi
fi

# ANSI colors
reset="\033[0m"
bold="\033[1m"
black_fg="\033[30m"
green_bg="\033[42m"
blue_fg="\033[34m"
cyan_fg="\033[36m"
green_fg="\033[32m"
magenta_fg="\033[35m"

# Directory segment (bold path).
dir_part="${bold}${short_cwd}${reset}"

# Branch segment (leading space, empty when not in a git repo).
branch_part=""
if [ -n "$branch" ]; then
  git_info="${branch}"
  if [ "$changed_count" -gt 0 ] || [ "$untracked_count" -gt 0 ]; then
    git_info="${git_info} ~${changed_count} ?${untracked_count}"
  fi
  branch_part=" ${black_fg}${green_bg}[${git_info}]${reset}"
fi

# Rate limits with visual bar
# Usage color: green (<50%) -> yellow (50-79%) -> red (>=80%)
rate_color() {
  pct=$1
  if [ "$pct" -ge 80 ]; then
    printf "\033[31m"  # red
  elif [ "$pct" -ge 50 ]; then
    printf "\033[33m"  # yellow
  else
    printf "\033[32m"  # green
  fi
}

# Build a 4-slot bar: e.g. [||..] for 50%
rate_bar() {
  pct=$1
  filled=$(( (pct + 12) / 25 ))  # 0-24%->0, 25-49%->1, 50-74%->2, 75-99%->3, 100%->4
  [ "$filled" -gt 4 ] && filled=4
  empty=$(( 4 - filled ))
  bar=""
  i=0; while [ $i -lt $filled ]; do bar="${bar}█"; i=$((i+1)); done
  i=0; while [ $i -lt $empty ];  do bar="${bar}░"; i=$((i+1)); done
  printf "%s" "$bar"
}

# Remaining-time label until a reset epoch: "3日2時間" / "1時間23分" / "45分".
fmt_remaining() {
  secs=$1
  [ "$secs" -lt 0 ] && secs=0
  d=$(( secs / 86400 ))
  h=$(( (secs % 86400) / 3600 ))
  m=$(( (secs % 3600) / 60 ))
  if [ "$d" -gt 0 ]; then
    printf "%d日%d時間" "$d" "$h"
  elif [ "$h" -gt 0 ]; then
    printf "%d時間%d分" "$h" "$m"
  else
    printf "%d分" "$m"
  fi
}

# Short human label for a token count: <1M -> "269k", >=1M -> "1M" / "1.5M".
human_tokens() {
  n=$1
  if [ "$n" -ge 1000000 ]; then
    if [ $(( n % 1000000 )) -eq 0 ]; then
      printf "%dM" $(( n / 1000000 ))
    else
      printf "%d.%dM" $(( n / 1000000 )) $(( (n % 1000000) / 100000 ))
    fi
  else
    printf "%dk" $(( (n + 500) / 1000 ))
  fi
}

rate_part=""
five_pct=$(echo "$input" | jq -r '.rate_limits.five_hour.used_percentage // empty' 2>/dev/null)
seven_pct=$(echo "$input" | jq -r '.rate_limits.seven_day.used_percentage // empty' 2>/dev/null)
# resets_at: 各ウィンドウがリセットされる時刻 (Unix epoch 秒)。残り時間表示に使う
five_reset=$(echo "$input" | jq -r '.rate_limits.five_hour.resets_at // empty' 2>/dev/null)
seven_reset=$(echo "$input" | jq -r '.rate_limits.seven_day.resets_at // empty' 2>/dev/null)
# 残り時間ラベルの色。90 (dark gray) は暗すぎたので 37 (light gray) にしている
gray_fg="\033[37m"
now=$(date +%s)

# リセット時刻到達後の強調色。SGR 5 (blink) は端末/tmux の対応に依存するため、
# 再描画ごとに epoch 秒の偶奇で赤/黄を入れ替える擬似点滅を重ねる
# (描画が更新されない間は片方の色で止まる)。
blink_color() {
  if [ $(( now % 2 )) -eq 0 ]; then
    printf "\033[5;1;31m"  # blink bold red
  else
    printf "\033[5;1;33m"  # blink bold yellow
  fi
}

# 1 ウィンドウ分のセグメント: "5h:[████]87%(残:1時間23分)"。
# resets_at を過ぎてもデータが更新されるまでは消さず、"(リセット!)" を点滅表示する。
rate_segment() {
  seg_label=$1; seg_pct=$2; seg_reset_at=$3
  p=${seg_pct%.*}
  printf "%s%s:[%s]%s%%%s" "$(rate_color "$p")" "$seg_label" "$(rate_bar "$p")" "$p" "$reset"
  if [ -n "$seg_reset_at" ] && [ "$seg_reset_at" -gt "$now" ] 2>/dev/null; then
    # date -r は BSD (macOS) 形式。%-m / %-d はゼロ埋めなし
    printf "%s(残:%s / %s)%s" "$gray_fg" "$(fmt_remaining $(( seg_reset_at - now )))" "$(date -r "$seg_reset_at" "+%-m月%-d日%H:%M")" "$reset"
  elif [ -n "$seg_reset_at" ] && [ "$seg_reset_at" -gt 0 ] 2>/dev/null; then
    printf "%s(リセット!)%s" "$(blink_color)" "$reset"
  fi
}

if [ -n "$five_pct" ] || [ -n "$seven_pct" ]; then
  parts=""
  if [ -n "$five_pct" ]; then
    parts="$(rate_segment 5h "$five_pct" "$five_reset")"
  fi
  if [ -n "$seven_pct" ]; then
    [ -n "$parts" ] && parts="$parts " || true
    parts="${parts}$(rate_segment 7d "$seven_pct" "$seven_reset")"
  fi
  rate_part=" ${parts}"
fi

# Model segment (leading space, empty when not provided).
model_part=""
if [ -n "$model_name" ]; then
  model_part=" ${cyan_fg}[${model_name}]${reset}"
fi

# Context window usage segment (sits to the right of the model). Claude Code
# provides the live numbers on stdin under .context_window, so we read them
# directly: total_input_tokens is what occupies the window,
# context_window_size is the model's limit, used_percentage drives the
# fullness color. (No transcript parsing: this is exact and per-render cheap.)
ctx_part=""
ctx_used=$(echo "$input" | jq -r '.context_window.total_input_tokens // empty')
if [ -n "$ctx_used" ] && [ "$ctx_used" -gt 0 ] 2>/dev/null; then
  ctx_size=$(echo "$input" | jq -r '.context_window.context_window_size // empty')
  ctx_pct=$(echo "$input" | jq -r '.context_window.used_percentage // 0')
  used_label=$(human_tokens "$ctx_used")
  if [ -n "$ctx_size" ] && [ "$ctx_size" -gt 0 ] 2>/dev/null; then
    ctx_disp="${used_label}/$(human_tokens "$ctx_size")"
  else
    ctx_disp="$used_label"
  fi
  cc=$(rate_color "${ctx_pct%.*}")
  ctx_part=" ${cc}[ctx:${ctx_disp}]${reset}"
fi

# Effort segment (leading space). .effort.level is the current reasoning
# effort (e.g. low / medium / high / xhigh), provided directly on stdin.
effort_part=""
effort_level=$(echo "$input" | jq -r '.effort.level // empty')
if [ -n "$effort_level" ]; then
  effort_part=" ${magenta_fg}[effort:${effort_level}]${reset}"
fi

# 1 行目: directory, branch, model, context, effort / 2 行目: rate limits。
# statusline は複数行出力をサポートする (公式 docs の Display multiple lines)。
# rate limit が無いとき (Free tier 等) は 2 行目自体を出さない。
# Each non-first segment carries its own leading space. (No right-alignment:
# the statusLine command runs without a controlling TTY so `tput cols` reports
# the wrong width and the line would overflow past the right edge.)
printf "%b%b%b%b%b" "$dir_part" "$branch_part" "$model_part" "$ctx_part" "$effort_part"
if [ -n "$rate_part" ]; then
  printf "\n%b" "${rate_part# }"
fi
