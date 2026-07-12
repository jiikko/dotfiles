#!/usr/bin/env bash
# Tmux smooth-scroll plugin initialization
source "$(dirname "$0")/config.sh"

MODE_KEYS="$(tmux show-option -gwq mode-keys)"
TABLE="copy-mode-vi"
[ "$MODE_KEYS" = "emacs" ] && TABLE="copy-mode"

# Find keys bound to scroll commands and rebind
tmux list-keys -T "$TABLE" | while IFS= read -r line; do
    # Skip mouse wheel events if explicitly disabled
    if [ "$(config__mouse_scroll)" = "false" ]; then
        case "$line" in
            *Wheel*) continue ;;
        esac
    fi
    
    case "$line" in
        *send-keys*scroll-up|*scroll.sh*up*normal*)         params="up normal" ;;
        *send-keys*scroll-down|*scroll.sh*down*normal*)     params="down normal" ;;
        *send-keys*halfpage-up|*scroll.sh*up*halfpage*)     params="up halfpage" ;;
        *send-keys*halfpage-down|*scroll.sh*down*halfpage*) params="down halfpage" ;;
        *send-keys*page-up|*scroll.sh*up*fullpage*)         params="up fullpage" ;;
        *send-keys*page-down|*scroll.sh*down*fullpage*)     params="down fullpage" ;;
        *) continue ;;
    esac
    
    key=$(echo "$line" | awk '{print $4}')
    [ -z "$key" ] && continue

    case "$key" in
        Wheel*Pane)
            # Wheel bindings need the mouse pane passed into scroll.sh.
            tmux bind-key -T "$TABLE" "$key" run-shell -b -t = "TMUX_PANE=#{pane_id} $SRC_DIR/scroll.sh $params"
            ;;
        *)
            # [dotfiles patch] keyboard も TMUX_PANE=#{pane_id} を明示する。#{pane_id} は
            # キー発火時に tmux が展開するため「押下時点の pane」が正確に確定する。
            # 渡さないと scroll.sh 側の fallback (display -p '#{pane_id}') が run-shell
            # 起動後の非同期タイミングで解決され、押下直後の pane 切替で移動先を誤爆する。
            # 継承 env の stale な TMUX_PANE (tmux 内から起動したサーバ等) の上書きも兼ねる。
            tmux bind-key -T "$TABLE" "$key" run-shell -b "TMUX_PANE=#{pane_id} $SRC_DIR/scroll.sh $params"
            ;;
    esac
done
