#!/usr/bin/env bash
# Tmux smooth-scroll plugin initialization
source "$(dirname "$0")/config.sh"

MODE_KEYS="$(tmux show-option -gwq mode-keys)"
TABLE="copy-mode-vi"
[ "$MODE_KEYS" = "emacs" ] && TABLE="copy-mode"

# [dotfiles patch] 設定はループ外で 1 回だけ読む。元実装は list-keys の全行 (~88 行) ごとに
# config__mouse_scroll (= tmux fork) を呼び、conf ロード 1 回に ~0.69s かかっていた (実測)。
MOUSE_SCROLL="$(config__mouse_scroll)"
SCOPES=" $(config__scopes) "

# [dotfiles patch] bind-key も 1 キーずつ fork せず ';' 区切りで 1 クライアントにバッチする
# (vendor resurrect/continuum の boot 高速化と同じ手法)。
BATCH=()

# Find keys bound to scroll commands and rebind
while IFS= read -r line; do
    # Skip mouse wheel events if explicitly disabled
    if [ "$MOUSE_SCROLL" = "false" ]; then
        case "$line" in
            *Wheel*) continue ;;
        esac
    fi

    case "$line" in
        *send-keys*scroll-up|*scroll.sh*up*normal*)         params="up normal";     scope="normal";   native="scroll-up" ;;
        *send-keys*scroll-down|*scroll.sh*down*normal*)     params="down normal";   scope="normal";   native="scroll-down" ;;
        *send-keys*halfpage-up|*scroll.sh*up*halfpage*)     params="up halfpage";   scope="halfpage"; native="halfpage-up" ;;
        *send-keys*halfpage-down|*scroll.sh*down*halfpage*) params="down halfpage"; scope="halfpage"; native="halfpage-down" ;;
        *send-keys*page-up|*scroll.sh*up*fullpage*)         params="up fullpage";   scope="fullpage"; native="page-up" ;;
        *send-keys*page-down|*scroll.sh*down*fullpage*)     params="down fullpage"; scope="fullpage"; native="page-down" ;;
        *) continue ;;
    esac

    # キー名は 4 カラム目 (glob 展開を止めて word split のみ行う。awk fork の削減)
    set -f
    set -- $line
    set +f
    key=$4
    [ -z "$key" ] && continue

    # [dotfiles patch] @smooth-scroll-scopes に無い種別は rebind しない。過去の load で
    # rebind 済み (行に scroll.sh を含む) のキーは native へ戻し、source-file だけで scope の
    # 変更を反映できるようにする (Wheel 系だけは native の既定が -N 付き等で単純復元できない
    # ため対象外 = mouse を有効にしたまま scope から外した場合は要サーバ再起動)。
    case "$SCOPES" in
        *" $scope "*) ;;
        *)
            case "$key" in Wheel*Pane) continue ;; esac
            case "$line" in
                *scroll.sh*) BATCH+=(bind-key -T "$TABLE" "$key" send-keys -X "$native" ';') ;;
            esac
            continue
            ;;
    esac

    case "$key" in
        Wheel*Pane)
            # Wheel bindings need the mouse pane passed into scroll.sh.
            BATCH+=(bind-key -T "$TABLE" "$key" run-shell -b -t = "TMUX_PANE=#{pane_id} $SRC_DIR/scroll.sh $params" ';')
            ;;
        *)
            # [dotfiles patch] keyboard も TMUX_PANE=#{pane_id} を明示する。#{pane_id} は
            # キー発火時に tmux が展開するため「押下時点の pane」が正確に確定する。
            # 渡さないと scroll.sh 側の fallback (display -p '#{pane_id}') が run-shell
            # 起動後の非同期タイミングで解決され、押下直後の pane 切替で移動先を誤爆する。
            # 継承 env の stale な TMUX_PANE (tmux 内から起動したサーバ等) の上書きも兼ねる。
            BATCH+=(bind-key -T "$TABLE" "$key" run-shell -b "TMUX_PANE=#{pane_id} $SRC_DIR/scroll.sh $params" ';')
            ;;
    esac
done < <(tmux list-keys -T "$TABLE")

if [ "${#BATCH[@]}" -gt 0 ]; then
    # 末尾の ';' を除いて 1 回の tmux 呼び出しで全 bind を適用する
    tmux "${BATCH[@]:0:${#BATCH[@]}-1}"
fi
