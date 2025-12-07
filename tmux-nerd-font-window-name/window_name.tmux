#!/usr/bin/env bash

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Use pane_id so the helper can look up pane-local variables (e.g. @ai_name)
plugin_format="#($CURRENT_DIR/bin/tmux-nerd-font-window-name #{pane_id} #{pane_current_command} #{window_panes})"

user_format="$(tmux show-option -gv automatic-rename-format 2>/dev/null)"
placeholder="#{window_icon}"

# If user format contains the placeholder, substitute it. Otherwise just use the plugin format.
if [[ -n "$user_format" && "$user_format" == *"$placeholder"* ]]; then
    new_format="${user_format//$placeholder/$plugin_format}"
else
    new_format="$plugin_format"
fi

tmux set-option -g automatic-rename on
# Avoid clobbering if same value
current="$(tmux show-option -gv automatic-rename-format 2>/dev/null)"
if [[ "$current" != "$new_format" ]]; then
    tmux set-option -g automatic-rename-format "$new_format"
fi
