#!/usr/bin/env bash
# bin/tmux-toast の unit テスト (PATH stub 方式。実 tmux には触れない)。固定する不変条件:
#   - 再入ガード: 直近 2 秒以内に toast 済みなら new-pane を呼ばず exit 0
#     (new-pane 自体が after-split-window hook を再発火するため、これが無いと
#      hook 経由の呼び出しで toast が toast を生む無限増殖になる。2026-07-19 実測)
#   - floating pane 経路: new-pane に -d (フォーカス非奪取) と、メッセージの表示セル幅
#     (東アジア文字=2セル) から計算した右下座標が渡ること
#   - fallback 経路 (floating 非対応 tmux): クライアント tty へ直接描画し、
#     表示終了後に refresh-client で消すこと
#   - tmux 外では非 0 で終わる / メッセージ無しは usage (exit 2)
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/bin/tmux-toast"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

[[ -x "$SCRIPT" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"; exit 1; }

CALLS="$TMP_DIR/calls.log"
export CALLS

# stub tmux: 呼び出しを記録し、応答は環境変数で制御する
#   STUB_FLOATING=0 → list-commands の出力に -X が無い (floating panes 非対応 tmux)
#   STUB_LAST=<epoch> → @tmux_toast_last_epoch の値 (再入ガードの状態)
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  list-commands)
    if [ "${STUB_FLOATING:-1}" = 1 ]; then
      echo 'new-pane (newp) [-bdefhIklPvZ] [-X x-position] [-Y y-position] [shell-command]'
    else
      echo 'new-pane (newp) [-bdfhvP] [-l size] [shell-command]'
    fi ;;
  display-message)
    case "$*" in
      *client_tty*) echo "${STUB_TTY:?} 200 50" ;;
      *window_width*) echo "200 50" ;;
    esac ;;
  show-option) echo "${STUB_LAST:-}" ;;
esac
exit 0
EOS
chmod +x "$TMP_DIR/bin/tmux"
export PATH="$TMP_DIR/bin:$PATH"

fail=0
ok()   { printf '✓ %s\n' "$1"; }
ng()   { printf '✗ %s\n' "$1"; fail=1; }
reset_calls() { : > "$CALLS"; }

run_toast() { TMUX=stub "$SCRIPT" "$@"; }

# --- tmux 外では実行しない -------------------------------------------------
reset_calls
if env -u TMUX "$SCRIPT" "msg" 2>/dev/null; then
  ng "tmux 外なのに成功した"
else
  ok "tmux 外では非 0 で終わる"
fi

# --- メッセージ無しは usage ------------------------------------------------
if run_toast 2>/dev/null; then
  ng "メッセージ無しなのに成功した"
else
  ok "メッセージ無しは usage (非 0)"
fi

# --- 再入ガード: 直近の toast があれば何もしない ---------------------------
reset_calls
STUB_LAST="$(date +%s)" run_toast "🪟 二発目"
if grep -q '^tmux new-pane' "$CALLS"; then
  ng "再入ガード: 直近 toast ありでも new-pane が呼ばれた (hook 無限増殖の再発)"
else
  ok "再入ガード: 直近 2 秒以内なら new-pane を呼ばない"
fi

# --- floating pane 経路: -d と右下座標 (セル幅計算込み) --------------------
# 「あ b」= あ(2) + space(1) + b(1) = 4 セル → box_w=8, x=200-8=192, y=50-3=47
reset_calls
run_toast "あ b"
if grep -q -- 'new-pane -d -x 8 -y 3 -X 192 -Y 47' "$CALLS"; then
  ok "floating: -d + セル幅からの右下座標で new-pane を呼ぶ"
else
  ng "floating: new-pane の引数が期待と違う: $(grep new-pane "$CALLS" || echo '(呼ばれていない)')"
fi
if grep -q -- 'set-option -g @tmux_toast_last_epoch' "$CALLS"; then
  ok "floating: 再入ガード用の epoch を記録する"
else
  ng "floating: epoch が記録されていない (ガードが効かなくなる)"
fi

# --- fallback 経路: tty へ直接描画し refresh-client で消す ------------------
reset_calls
STUB_TTY="$TMP_DIR/fake_tty"
: > "$STUB_TTY"
export STUB_TTY
STUB_FLOATING=0 run_toast -d 0.2 "fallback msg"
# 描画ループは background なので refresh-client (終了処理) を最大 2 秒待つ
for _ in $(seq 1 20); do
  grep -q 'refresh-client' "$CALLS" && break
  sleep 0.1
done
if grep -q '^tmux new-pane' "$CALLS"; then
  ng "fallback: floating 非対応なのに new-pane が呼ばれた"
else
  ok "fallback: floating 非対応では new-pane を呼ばない"
fi
if grep -q 'fallback msg' "$STUB_TTY" && grep -q $'\e\[48;5;' "$STUB_TTY"; then
  ok "fallback: クライアント tty に色付きでメッセージを描画する"
else
  ng "fallback: tty への描画が無い/色指定が無い"
fi
if grep -q 'refresh-client' "$CALLS"; then
  ok "fallback: 表示終了後に refresh-client で消す"
else
  ng "fallback: refresh-client が呼ばれない (toast が残留する)"
fi

exit "$fail"
