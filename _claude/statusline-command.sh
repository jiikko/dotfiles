#!/bin/sh
# Claude Code statusLine command
# Mirrors the zsh PROMPT configuration from ~/.zshrc

input=$(cat)

# TEMP (remove after capturing): dump one raw statusline input so we can
# discover the rate_limits reset field names. Negligible cost; overwrites
# the same file each render.
printf '%s' "$input" > "$HOME/.claude/.statusline-input-debug.json" 2>/dev/null

cwd=$(echo "$input" | jq -r '.workspace.current_dir // .cwd')

# Model display name (e.g. "Opus 4.8"). Provided by Claude Code via stdin JSON.
model_name=$(echo "$input" | jq -r '.model.display_name // empty')

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

# Model segment (leading space, empty when not provided).
model_part=""
if [ -n "$model_name" ]; then
  model_part=" ${cyan_fg}[${model_name}]${reset}"
fi

# Context window usage segment (sits to the right of the model).
# Claude Code does not pass the live context token count on stdin (only the
# boolean .exceeds_200k_tokens), so we read the most recent main-thread
# assistant `usage` from the session transcript JSONL and sum the tokens that
# occupy the context window: input + cache_read + cache_creation.
#
# CTX_LIMIT is only the denominator for the fullness color; the displayed
# value is the absolute token count. Default 1,000,000 for the 1M-context
# Opus models; drop to 200000 for standard 200k-context sessions.
CTX_LIMIT=1000000
ctx_part=""
transcript=$(echo "$input" | jq -r '.transcript_path // empty')
if [ -z "$transcript" ] || [ ! -f "$transcript" ]; then
  # Fallback: derive the transcript path from cwd + session_id when stdin
  # omits transcript_path. Claude encodes the project dir by replacing
  # '/' and '.' with '-'.
  sid=$(echo "$input" | jq -r '.session_id // empty')
  if [ -n "$sid" ]; then
    enc=$(printf '%s' "$cwd" | sed -e 's#/#-#g' -e 's#\.#-#g')
    cand="$HOME/.claude/projects/${enc}/${sid}.jsonl"
    [ -f "$cand" ] && transcript="$cand"
  fi
fi
if [ -n "$transcript" ] && [ -f "$transcript" ]; then
  # tail bounds the cost on large transcripts; the latest assistant message
  # (and thus its usage) is always within the final handful of lines.
  ctx_tokens=$(tail -n 80 "$transcript" 2>/dev/null | jq -rs '
    [ .[]
      | select(.isSidechain != true)
      | .message.usage // empty
      | (.input_tokens // 0) + (.cache_read_input_tokens // 0) + (.cache_creation_input_tokens // 0) ]
    | last // empty' 2>/dev/null)
  if [ -n "$ctx_tokens" ] && [ "$ctx_tokens" -gt 0 ] 2>/dev/null; then
    ctx_pct=$(( ctx_tokens * 100 / CTX_LIMIT ))
    cc=$(rate_color "$ctx_pct")
    used_label=$(human_tokens "$ctx_tokens")
    limit_label=$(human_tokens "$CTX_LIMIT")
    ctx_part=" ${cc}[ctx:${used_label}/${limit_label}]${reset}"
  fi
fi

# Session cost segment (USD, leading space). cost.total_cost_usd is provided
# directly on stdin by Claude Code.
cost_part=""
cost_usd=$(echo "$input" | jq -r '.cost.total_cost_usd // empty')
if [ -n "$cost_usd" ]; then
  cost_fmt=$(printf '%.2f' "$cost_usd" 2>/dev/null)
  [ -n "$cost_fmt" ] && cost_part=" ${magenta_fg}[\$${cost_fmt}]${reset}"
fi

# Order: directory, branch, model, context, cost, rate limits. Each non-first
# segment carries its own leading space. (No right-alignment: the statusLine
# command runs without a controlling TTY so `tput cols` reports the wrong
# width and the rate part would overflow past the right edge.)
printf "%b%b%b%b%b%b" "$dir_part" "$branch_part" "$model_part" "$ctx_part" "$cost_part" "$rate_part"
