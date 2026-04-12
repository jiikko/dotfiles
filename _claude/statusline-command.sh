#!/bin/sh
# Claude Code statusLine command
# Mirrors the zsh PROMPT configuration from ~/.zshrc

input=$(cat)

cwd=$(echo "$input" | jq -r '.workspace.current_dir // .cwd')

# Shorten the path: replace $HOME with ~, truncate to 50 chars with leading ..
home="$HOME"
short_cwd="${cwd/#$home/\~}"
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

# Build the status line: bold path + optional git branch + file counts
if [ -n "$branch" ]; then
  git_info="${branch}"
  if [ "$changed_count" -gt 0 ] || [ "$untracked_count" -gt 0 ]; then
    git_info="${git_info} ~${changed_count} ?${untracked_count}"
  fi
  path_part="${bold}${short_cwd}${reset} ${black_fg}${green_bg}[${git_info}]${reset}"
else
  path_part="${bold}${short_cwd}${reset}"
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

rate_part=""
five_pct=$(echo "$input" | jq -r '.rate_limits.five_hour.used_percentage // empty' 2>/dev/null)
seven_pct=$(echo "$input" | jq -r '.rate_limits.seven_day.used_percentage // empty' 2>/dev/null)

if [ -n "$five_pct" ] || [ -n "$seven_pct" ]; then
  parts=""
  if [ -n "$five_pct" ]; then
    p=${five_pct%.*}
    c=$(rate_color "$p")
    b=$(rate_bar "$p")
    parts="${c}5h:[${b}]${p}%${reset}"
  fi
  if [ -n "$seven_pct" ]; then
    p=${seven_pct%.*}
    c=$(rate_color "$p")
    b=$(rate_bar "$p")
    [ -n "$parts" ] && parts="$parts " || true
    parts="${parts}${c}7d:[${b}]${p}%${reset}"
  fi
  rate_part=" ${parts}"
fi

# Right-align rate_part using terminal width
if [ -n "$rate_part" ]; then
  cols=$(tput cols 2>/dev/null || echo 80)
  # Calculate visible (non-ANSI) lengths
  left_len=$(printf "%b" "$path_part" | sed 's/\x1b\[[0-9;]*m//g' | wc -m | tr -d ' ')
  right_len=$(printf "%b" "$rate_part" | sed 's/\x1b\[[0-9;]*m//g' | wc -m | tr -d ' ')
  gap=$(( cols - left_len - right_len ))
  [ "$gap" -lt 1 ] && gap=1
  padding=$(printf "%${gap}s" "")
  printf "%b%s%b" "$path_part" "$padding" "$rate_part"
else
  printf "%b" "$path_part"
fi
