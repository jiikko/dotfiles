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
if git -C "$cwd" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  branch=$(git -C "$cwd" symbolic-ref --short HEAD 2>/dev/null || git -C "$cwd" rev-parse --short HEAD 2>/dev/null)
fi

# ANSI colors
reset="\033[0m"
bold="\033[1m"
black_fg="\033[30m"
green_bg="\033[42m"
blue_fg="\033[34m"
cyan_fg="\033[36m"
green_fg="\033[32m"

# Build the status line: bold path + optional git branch
if [ -n "$branch" ]; then
  printf "${bold}${short_cwd}${reset} ${black_fg}${green_bg}[${branch}]${reset}"
else
  printf "${bold}${short_cwd}${reset}"
fi
