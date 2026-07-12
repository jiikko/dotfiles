#!/usr/bin/env bash
# Centralized settings

SRC_DIR="$(dirname "$0")"

# Speed: 0-1000 scale (lower = faster)
config__speed() {
    local speed="$(tmux show-option -gqv "@smooth-scroll-speed")"
    echo "${speed:-100}"
}

# Scroll distances
config__normal_lines() {
    local lines="$(tmux show-option -gqv "@smooth-scroll-normal")"
    echo "${lines:-3}"
}

config__halfpage_lines() {
    local lines="$(tmux show-option -gqv "@smooth-scroll-halfpage")"
    local pane_height="${1:-$(tmux display-message -p '#{pane_height}')}"
    echo "${lines:-$((pane_height / 2))}"
}

config__fullpage_lines() {
    local lines="$(tmux show-option -gqv "@smooth-scroll-fullpage")"
    local pane_height="${1:-$(tmux display-message -p '#{pane_height}')}"
    echo "${lines:-$pane_height}"
}

# Mouse wheel scrolling enabled (default: true)
config__mouse_scroll() {
    local mouse="$(tmux show-option -gqv "@smooth-scroll-mouse")"
    echo "${mouse:-true}"
}

# [dotfiles patch] Key-repeat passthrough threshold in ms (scroll.sh の素通し判定で使用)
config__repeat_ms() {
    local ms="$(tmux show-option -gqv "@smooth-scroll-repeat-ms")"
    echo "${ms:-150}"
}

# [dotfiles patch] アニメ対象のスクロール種別 (space 区切り: normal halfpage fullpage)。
# init.sh が rebind 対象の絞り込みに使う。normal (C-e/C-y 等) を外すと native の
# 1 行スクロールのまま維持できる
config__scopes() {
    local scopes="$(tmux show-option -gqv "@smooth-scroll-scopes")"
    echo "${scopes:-normal halfpage fullpage}"
}

# [dotfiles patch] 1 アニメの最大描画ステップ数 (0 = 行ごと = upstream 挙動)。
# chunk 化しても easing の時間配分は保存される (animator.pl)。描画が遅い端末向け
config__max_steps() {
    local n="$(tmux show-option -gqv "@smooth-scroll-max-steps")"
    echo "${n:-0}"
}

# Easing mode: linear, sine, quad
config__easing_mode() {
    local mode="$(tmux show-option -gqv "@smooth-scroll-easing")"
    echo "${mode:-sine}"
}

# Auto-exit copy mode when scrolling past bottom (default: true)
config__exit_copy_mode_at_bottom() {
    local val="$(tmux show-option -gqv "@smooth-scroll-exit-copy-mode-at-bottom")"
    echo "${val:-true}"
}
