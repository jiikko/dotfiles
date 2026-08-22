#!/usr/bin/env bash
# tmux_agent_panel.sh — エージェント常駐パネル (herdr 風ダッシュボードの tmux 版)。
#
# 全セッション横断で @claude_state (⚙ working / 🔔 input / 🔕 seen / ✓ idle) を持つ
# pane を、現在 window の右上に floating pane (tmux 3.7+) で常時表示する。
# floating pane はフォーカスを奪わない (-d) ため作業を邪魔しない。
#
# モード:
#   toggle <window_id>   パネルの表示/非表示を切り替える (bind から)
#   follow <window_id>   window 切替時にパネルを新 window へ作り直す (hook から)
#   render               パネル pane 内で走る描画ループ (内部用)
#
# 構造上の制約 (tmux 3.7b 実測):
#   - floating pane は window 所属 (pane の一般則)。window を跨いで居続けられない
#     ため、after-select-window / client-session-changed hook の follow で
#     「旧 window の panel を kill → 新 window に作成」して追従する。
#   - move-pane は floating 属性を剥がして通常 split に降格させるため使えない
#     (kill + create が唯一の追従手段)。
#
# hook ノイズ抑止 (@agent_panel_busy):
#   panel の kill/create 自体が pane-exited / after-split-window hook を発火させ、
#   放置すると window 切替のたびに (1) tmux-toast の分割/閉じ通知 (2) resurrect の
#   debounce 保存 (1 回 ~20MB のダンプ) が走る。kill/create の直前に
#   @agent_panel_busy へ epoch を書き、bin/tmux-toast と
#   scripts/tmux_resurrect_debounced_save.sh が AGENT_PANEL_QUIET_SECS 秒以内の
#   イベントを無視する。副作用として、その窓の間の実イベント (人間の split 等) の
#   toast / debounce 保存も落ちるが、toast は装飾のみ・保存は次のイベントの
#   フルスナップショットが取り返すため損失は「窓内イベントが最後 + その後クラッシュ」
#   に限られる (周期保存と kill shim が残余をカバー)。
#
# KNOWN LIMITATION:
#   - resurrect のスナップショットに panel pane が写り込む。サーバ復元後は
#     「render コマンドが復元されない素の shell の floating pane」として 1 個残る
#     ことがある (toggle か kill-pane で消す。復元は稀なので許容)。
#   - panel が居る window の pane 数バッジ [N] は +1 される (実 pane なので仕様)。
#   - select-pane の方向移動でパネルにフォーカスが当たった場合は after-select-pane
#     hook (_tmux.conf) の unfocus が即座に弾き返す。入力も無効化してある
#     (create_panel の select-pane -d)。
set -uo pipefail
unset CDPATH

SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"

# render pane の pane_id を列挙する。⚠️ 同一性は**スクリプト名**で見る (絶対パス完全一致に
# しないこと)。worktree や tmp のコピーから起動した panel はパスが違うため、完全一致では
# 自分のものと認識できず掃討も save-hide も掴めない。実測 2026-08-21: 本番 window に孤児
# panel が 6 枚・6〜7 日累積し、最新スナップショットに pane 行 6 本 + floating layout が
# 写り込んでいた (worktree 分離は repo の規約自体が要求しているので、必ず踏む形だった)。
# 判定の出典をここ 1 箇所に集約する (以前は同じ awk が 2 箇所へ逐語コピーされていた)。
render_panes() {
  tmux list-panes -a -F '#{pane_id} #{pane_start_command}' 2>/dev/null |
    awk '$2 ~ /\/tmux_agent_panel\.sh$/ && $3 == "render" {print $1}'
}


# 復元中 / bootstrap 判定 (tt_restore_in_progress / tt_only_hold_sessions)。
# デフォルト表示 (@agent_panel_on を conf が立てる) のため、サーバ起動直後の hook でも
# follow が走る。復元前に panel pane を作ると「総 pane 数 = 1」を破って resurrect の
# restore_from_scratch (スクロールバック復元) を不発にするため、guards で抑止する
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/tmux_resurrect_guards.sh"
# popup 専用セッション (scratch 等) の除外パターン TT_POPUP_SESSION_RE
# shellcheck source=scripts/lib/tmux_popup_sessions.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/tmux_popup_sessions.sh"

PANEL_W=150         # パネル幅 (セル)。行の組み立て (list_agents の切り詰め幅) はこの幅に収まる前提
PANEL_MAX_H=14      # 高さ上限 (超過分は +N more に畳む)
REFRESH_SECS=2      # 描画ループの更新間隔
# busy 窓の秒数 (3 秒) は読み手側 (bin/tmux-toast / tmux_resurrect_debounced_save.sh の
# AGENT_PANEL_QUIET_SECS) が持つ。ここは書くだけ (epoch)

# now_epoch は現在の epoch を REPLY へ入れる。⚠️ echo で返さない / date を呼ばない:
# `$(date +%s)` は 1 回ごとに fork+exec になり、描画 tick (行数に比例) と window 切替 hook の
# 両方に乗る (rules/zsh-hook-return-via-reply.md と同思想)。bash 5+ の $EPOCHSECONDS は
# 組み込みなので 0 fork。素の macOS /bin/bash (3.2) だけ date へ落ちる。
if [ -n "${EPOCHSECONDS+x}" ]; then
  now_epoch() { REPLY=$EPOCHSECONDS; }
else
  now_epoch() { REPLY="$(date +%s)"; }
fi

mark_busy() { now_epoch; tmux set-option -g @agent_panel_busy "$REPLY" 2>/dev/null || true; }

panel_pane() { tmux show-option -gqv @agent_panel_pane 2>/dev/null; }
panel_on()   { [ "$(tmux show-option -gqv @agent_panel_on 2>/dev/null)" = "1" ]; }

# ⚠️ exit code でなく出力で判定する: display-message -p -t <消滅した pane> は
# stderr に "can't find pane" を吐きつつ exit 0 で空行を返す (tmux 3.7b 実測)
pane_alive() { [ -n "${1:-}" ] && [ -n "$(tmux display-message -p -t "$1" '#{pane_id}' 2>/dev/null)" ]; }

pane_window() { tmux display-message -p -t "$1" '#{window_id}' 2>/dev/null; }

# @claude_state 持ち pane の一覧 (tab 区切り: state, session:index, pane_title)。
# ⚠️ 名前は window_name でなく pane_title を使う: window_name は「その window の
# アクティブ pane のタイトル」なので、同一 window に複数エージェントが居ると
# 全行が同じ名前になる (全部 "Auth0" 表示になった実発 2026-08-08)。pane_title は
# pane 単位 (claude が ✳ 付きで自セッション名をセットする) なので区別できる
list_agents() {
  # 場所は session:window.pane まで出す (同一 window に複数エージェントが居るため
  # pane まで無いと特定できない)。切り詰め幅はセルでなく文字数 (CJK は 1 文字 2 セル)。
  # タイトルが全部 CJK でも loc(27) + state(~10) + since(~5) + title(50×2=100) ≈ 144
  # ≤ PANEL_W で折り返さない上界にしてある (これ以上増やすなら PANEL_W と対で)。
  # 第 4 フィールド @claude_state_since (epoch。_claude/hooks/tmux-pane-state.sh が書く)
  # は経過時間表示用。未設定 (旧 hook が書いた状態が残っている間) は空。
  # 第 5 フィールドは「その pane を今まさに表示中か」(pane_active かつ window_active かつ
  # attach 中のセッション)。draw_once が該当行をハイライトする (どの行が「いま見ている
  # エージェント」かを一目にする。複数 client attach 時は client ごとの表示 pane が
  # それぞれ 1 になる = 複数行ハイライトされ得るが、どれも実際に表示中なので正)
  tmux list-panes -a -F $'#{?@claude_state,#{@claude_state}\t#{=20:session_name}:#{window_index}.#{pane_index}\t#{=50:pane_title}\t#{@claude_state_since}\t#{?#{&&:#{pane_active},#{&&:#{window_active},#{session_attached}}},1,0},}' 2>/dev/null |
    awk -F'\t' 'NF >= 3'
}

# epoch → 短い相対時間 ("45s"/"12m"/"3h"。不正/未設定は空) を REPLY へ。
# ⚠️ 出力で返さない (呼び出しごとに $( ) = fork。行数に比例して増える)。現在時刻も引数で
# 受ける: tick あたり 1 回だけ取れば済むものを行ごとに測り直さない。
rel_time() {
  local since="$1" now="$2" d
  REPLY=""
  case "$since" in ''|*[!0-9]*) return 0 ;; esac
  d=$(( now - since ))
  [ "$d" -lt 0 ] && d=0
  if   [ "$d" -lt 60 ];    then REPLY="${d}s"
  elif [ "$d" -lt 3600 ];  then REPLY="$((d / 60))m"
  else                          REPLY="$((d / 3600))h"
  fi
}

# panel の一意性は @agent_panel_pane の記録でなく「render を実行中の pane を全部消す」
# 掃討で強制する。⚠️ 記録だけを kill する実装に戻さないこと: 並走した follow
# (client-attached / client-session-changed / after-select-window は popup 開閉で同時に
# 発火する) が panel を二重作成すると、記録から漏れた孤児が prefix+a で消せず残る
# (scratch:5 に孤児が残った実発 2026-08-08)
kill_panel() {
  local p killed=0
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    [ "$killed" = 0 ] && { mark_busy; killed=1; }
    tmux kill-pane -t "$p" 2>/dev/null || true
  done < <(render_panes)
  tmux set-option -gu @agent_panel_pane 2>/dev/null || true
}

create_panel() {
  local win="$1" n h w win_w x pid
  n="$(list_agents | wc -l | tr -d ' ')"
  h=$((n + 2))
  [ "$h" -lt 3 ] && h=3
  [ "$h" -gt "$PANEL_MAX_H" ] && h=$PANEL_MAX_H
  win_w="$(tmux display-message -p -t "$win" '#{window_width}' 2>/dev/null)" || return 1
  case "$win_w" in ''|*[!0-9]*) return 1 ;; esac
  w=$PANEL_W
  [ "$w" -gt "$win_w" ] && w=$win_w   # 狭い window では window 幅に clamp (行は折り返る)
  x=$((win_w - w))
  [ "$x" -lt 0 ] && x=0
  mark_busy
  pid="$(tmux new-pane -d -P -F '#{pane_id}' -t "$win" \
    -x "$w" -y "$h" -X "$x" -Y 0 \
    -s 'fg=colour252,bg=colour233' -R 'fg=colour240' \
    -- "$SELF" render)" || return 1
  tmux set-option -g @agent_panel_pane "$pid"
  # 入力を無効化 (select-pane -d = input off)。M-hjkl の方向移動でパネルに乗っても
  # 誤タイプ・C-d での誤 kill が起きない置物にする (表示は render プロセスが続ける)
  tmux select-pane -d -t "$pid" 2>/dev/null || true
}

cmd_toggle() {
  local win="${1:-}"
  [ -n "$win" ] || win="$(tmux display-message -p '#{window_id}')"
  if panel_on; then
    tmux set-option -gu @agent_panel_on 2>/dev/null || true
    kill_panel
  else
    tmux set-option -g @agent_panel_on 1
    # resurrect 保存中 (save-hide が退避した窓) は状態だけ立てて作成しない: ここで作ると
    # 実行中の save.sh のダンプに写り込み、D 節が防いだ残骸バグが再発する (敵対レビュー
    # 指摘 2026-08-08)。保存後の save-show / 次の follow が panel_on を見て作る
    panel_saving && return 0
    kill_panel   # 迷子の旧パネルがあれば掃除してから作る
    # popup 専用セッション (scratch 等) の中で on にした場合は作成だけ遅延する
    # (popup 内に作ると覆い被さり + 取り残しの温床。次の window 切替の follow が作る)
    if ! tmux display-message -p -t "$win" '#{session_name}:' 2>/dev/null |
         grep -Eq "$TT_POPUP_SESSION_RE"; then
      create_panel "$win"
    fi
  fi
}

# resurrect 保存中か (@agent_panel_saving に save-hide が epoch を書く)。TTL 120s は
# save wrapper の lock TTL 系と同じ思想 (途中クラッシュで永久に follow が止まらないように)
panel_saving() {
  local v
  v="$(tmux show-option -gqv @agent_panel_saving 2>/dev/null)"
  case "$v" in ''|*[!0-9]*) return 1 ;; esac
  now_epoch
  [ $(( REPLY - v )) -lt 120 ]
}

cmd_follow() {
  local win="${1:-}" p sess
  panel_on || exit 0
  # 復元中 / bootstrap (hold のみ) は作らない (冒頭の guards コメント参照)。
  # 復元完了後の最初の window 切替 / attach で作られる
  if tt_restore_in_progress || tt_only_hold_sessions; then
    exit 0
  fi
  # resurrect 保存中は作らない (save-hide がスナップショットから panel を退避している間に
  # follow が作り直すと写り込みが復活する。save-show が保存後に復帰させる)
  panel_saving && exit 0
  [ -n "$win" ] || win="$(tmux display-message -p '#{window_id}')"
  # popup 専用セッション (scratch 等) へは追従しない: popup 開閉は client-attached /
  # client-session-changed を発火させるが、そこへ panel を作ると popup の小画面に
  # 覆い被さる上、popup を閉じた後も panel がそのセッションに取り残される
  sess="$(tmux display-message -p -t "$win" '#{session_name}:' 2>/dev/null)"
  if grep -Eq "$TT_POPUP_SESSION_RE" <<< "$sess"; then
    exit 0
  fi
  p="$(panel_pane)"
  if pane_alive "$p" && [ "$(pane_window "$p")" = "$win" ]; then
    exit 0   # すでに今の window に居る (同一 window 内の pane 移動等)
  fi
  # 並走する follow (popup 開閉で attached / session-changed / select-window が同時発火)
  # の kill+create を lock で直列化する。取れなければ何もしない (勝者が正しい位置に作る。
  # 位置がずれても次の window 切替で追従する)。stale lock は TTL で奪う
  local lock="${TT_AGENT_PANEL_LOCK:-$HOME/.cache/tt-agent-panel.lock}"
  mkdir -p "$(dirname "$lock")" 2>/dev/null
  if ! mkdir "$lock" 2>/dev/null; then
    local age
    # mtime は tt_mtime_of (guards.sh。GNU/BSD 方言差を吸収) 経由。素の `stat -f %m` は
    # GNU stat では mount point を返し、算術式が構文エラーになって hook 経由の無音契約を破る
    local lock_mtime; lock_mtime="$(tt_mtime_of "$lock")"
    case "${lock_mtime:-}" in ''|*[!0-9]*) lock_mtime=0 ;; esac
    now_epoch
    age=$(( REPLY - lock_mtime ))
    [ "$age" -lt 5 ] && exit 0
    rmdir "$lock" 2>/dev/null; mkdir "$lock" 2>/dev/null || exit 0
  fi
  kill_panel
  # hook (run-shell -b) 経由のため無音契約 (scripts/CLAUDE.md): 作成失敗 (window が
  # 狭すぎる等) でも stderr / 非 0 を返さない。失敗しても次の window 切替で再試行される
  create_panel "$win" >/dev/null 2>&1 || true
  rmdir "$lock" 2>/dev/null
  exit 0
}

# パネルにフォーカスが乗っていたら別 pane へ弾き返す (乗れない pane 化)。
# アクティブ pane が閉じると tmux が次のアクティブとしてパネルを選ぶことがあり、
# その瞬間に他の全 pane が非アクティブ dim (window-style bg=colour234) に沈んで
# 「メイン画面が暗い」状態になる (別マシンで実発 2026-08-09)。
# 弾き先は直前のアクティブ (!) を優先し、それが死んでいれば window 内の別 pane。
# パネルしか残っていない window (最後の実 pane が死んだ) はパネルごと畳む
# (次の window 選択の follow が作り直す)
ensure_unfocused() {
  local p="$1" win n
  [ "$(tmux display-message -p -t "$p" '#{pane_active}' 2>/dev/null)" = "1" ] || return 0
  win="$(pane_window "$p")"
  [ -n "$win" ] || return 0
  n="$(tmux list-panes -t "$win" -F 'x' 2>/dev/null | wc -l | tr -d ' ')"
  if [ "$n" -le 1 ]; then
    mark_busy
    tmux kill-pane -t "$p" 2>/dev/null || true
    tmux set-option -gu @agent_panel_pane 2>/dev/null || true
    return 0
  fi
  tmux select-pane -t "${win}.!" 2>/dev/null ||
    tmux select-pane -t "$(tmux list-panes -t "$win" -F '#{pane_id}' | grep -v "^${p}\$" | head -1)" 2>/dev/null || true
}

# pane-exited / after-select-pane hook から呼ばれる (無音契約)。render の 2 秒 tick も
# 同じ関数で自衛するが、hook 経由は「pane が閉じた / 移動で乗った直後」に即発火するので
# フォーカスがパネルに居る時間を最小化できる。
# ⚠️ 弾き返しの select-pane が after-select-pane を再発火させるが、2 回目はパネルが
# 非アクティブで ensure_unfocused が即 return するため 1 段で収束する
cmd_unfocus() {
  # ⚠️ @agent_panel_pane の記録だけを見ない (kill_panel と同じ掃討方式で render 実行中の
  # pane を全列挙する)。記録依存だと、follow の並走で記録から漏れた孤児 panel に
  # フォーカスが乗ったとき誰も弾かない (敵対レビュー指摘 2026-08-11)。
  # なお resurrect 復元後の「render 無しの素 shell 残骸」はここでも対象外 (KNOWN LIMITATION)
  local p
  while IFS= read -r p; do
    [ -n "$p" ] && ensure_unfocused "$p" >/dev/null 2>&1
  done < <(render_panes)
  exit 0
}

# ---- save-guard (scripts/tmux_resurrect_save.sh から保存の前後に呼ばれる) ----
# resurrect のスナップショットに panel pane が写り込むと、復元後に「render が復元されない
# 素の shell の floating pane」が残骸として残る (実測: pane 行 + layout の floating 節
# <150x5,...> が保存されていた 2026-08-08)。保存の choke point wrapper が保存直前に
# save-hide (退避) / 直後に save-show (復帰) を呼び、スナップショットを panel 無しに保つ。
# 周期保存 (60 分) のたびにパネルが一瞬消えて戻るのは仕様。

cmd_save_hide() {
  local p w
  p="$(panel_pane)"
  now_epoch
  tmux set-option -g @agent_panel_saving "$REPLY" 2>/dev/null || true
  if pane_alive "$p"; then
    w="$(pane_window "$p")"
    [ -n "$w" ] && tmux set-option -g @agent_panel_saved_window "$w" 2>/dev/null
  fi
  kill_panel
  exit 0
}

cmd_save_show() {
  local w
  w="$(tmux show-option -gqv @agent_panel_saved_window 2>/dev/null)"
  tmux set-option -gu @agent_panel_saving 2>/dev/null || true
  tmux set-option -gu @agent_panel_saved_window 2>/dev/null || true
  panel_on || exit 0
  [ -n "$w" ] || exit 0
  # 作る前に必ず kill_panel (toggle/follow と同じ規律): 退避窓の間にユーザーの toggle 連打
  # 等で panel が既に存在していると二重になる (敵対レビュー指摘 2026-08-08)
  kill_panel
  create_panel "$w" >/dev/null 2>&1 || true   # window が消えていたら次の follow に任せる
  exit 0
}

# ---- render (パネル pane 内で走る) -----------------------------------------

# state 文字列 → ソート優先度 / 256 色を REPLY へ。色は _tmux.conf の @claude-state-fg と
# 同じ意味の対応 (input=203 / working=220 / seen=244 / idle=10)。
# ⚠️ echo で返さない: 行ごとに $( ) = fork になり、表示行数に比例して積み上がる
# (この repo は tmux-continuum の status interpolation を「5〜10 fork/秒は基準に合わない」と
# して捨てているので、自分の常駐 panel が同じ形をしていてはいけない)。
state_rank() {
  case "$1" in
    *input*)   REPLY=1 ;;
    *working*) REPLY=2 ;;
    *idle*)    REPLY=3 ;;
    *)         REPLY=4 ;;   # seen ほか
  esac
}
state_color() {
  case "$1" in
    *input*)   REPLY=203 ;;
    *working*) REPLY=220 ;;
    *idle*)    REPLY=10 ;;
    *)         REPLY=244 ;;
  esac
}

draw_once() {
  local rows now h active body_max shown=0 total=0 n_input=0 n_working=0
  rows="$(list_agents)"
  read -r h active < <(tmux display-message -p '#{pane_height} #{pane_active}' 2>/dev/null)
  case "$h" in ''|*[!0-9]*) h=$PANEL_MAX_H ;; esac
  now_epoch; now=$REPLY   # tick あたり 1 回だけ (行ごとに測らない)
  # フォーカス自衛 (hook 経由の即時版が取りこぼした経路の保険。ensure_unfocused 参照)
  [ "${active:-0}" = "1" ] && [ -n "${TMUX_PANE:-}" ] && ensure_unfocused "$TMUX_PANE"

  # ⚠️ この関数は 2 秒ごとに回る常駐ループなので、行ごとに $( ) を作らない (fork が表示行数に
  # 比例して積み上がる。issue 083)。件数の集計・ソート用の feed 作り・行の組み立ては
  # 1 パスの bash 組み込み (printf -v / REPLY 返しのヘルパー) だけで済ませる。
  local feed="" state loc name since cur
  if [ -n "$rows" ]; then
    while IFS=$'\t' read -r state loc name since cur; do
      [ -n "$state$loc$name" ] || continue   # 空行は数えない (旧 awk 'NF' 相当)
      total=$((total + 1))
      case "$state" in *input*) n_input=$((n_input + 1)) ;; *working*) n_working=$((n_working + 1)) ;; esac
      state_rank "$state"
      feed+="$REPLY"$'\t'"$state"$'\t'"$loc"$'\t'"$name"$'\t'"$since"$'\t'"$cur"$'\n'
    done <<< "$rows"
  fi
  # body はヘッダー 1 行を除いた残り。全件が収まらないときは「+N more」行の分を
  # さらに 1 行予約する (予約しないと合計が pane 高さを 1 行超過してスクロールし、
  # ヘッダーが欠ける off-by-one。セルフレビューで検出 2026-08-08)
  body_max=$((h - 1))
  [ "$total" -gt "$body_max" ] && body_max=$((h - 2))

  local out line color rel _rank
  out=""
  if [ "$total" -gt 0 ]; then
    printf -v line '\e[1m 🤖 AGENTS %d  ⚙%d 🔔%d\e[0m\e[38;5;240m   C-t a: 非表示 / C-t A: ジャンプ\e[0m' "$total" "$n_working" "$n_input"
    out+="$line"$'\n'
    while IFS=$'\t' read -r _rank state loc name since cur; do
      [ "$shown" -ge "$body_max" ] && break
      state_color "$state"; color=$REPLY
      rel_time "$since" "$now"; rel=$REPLY
      # 場所を行頭・固定幅・シアンに置く (「どこに居るエージェントか」が縦に揃って
      # 一目で読めるように。loc は ASCII 前提なので printf の桁揃えがセル幅と一致する)。
      # 経過時間 (状態が claude hook に書かれてからの時間) は %-4s で state の直後。
      # 今表示中の pane (cur=1) は行全体に背景を敷いて強調する。⚠️ この行は途中で
      # \e[0m を使わない (フル reset は背景も消す。\e[22m/\e[39m で bold/fg だけ戻す)。
      # 行末も reset しない: 出力ループの \e[K が背景色のまま行末まで塗ってから
      # \e[0m する (reset を先にすると強調がテキスト幅で切れる)
      if [ "${cur:-0}" = "1" ]; then
        printf -v line '\e[48;5;237m \e[1;38;5;51m%-27s\e[22m \e[38;5;%sm%s \e[38;5;240m%-4s\e[39m %s' \
          "$loc" "$color" "$state" "$rel" "$name"
      else
        printf -v line ' \e[38;5;51m%-27s\e[0m \e[38;5;%sm%s\e[0m \e[38;5;240m%-4s\e[0m %s' \
          "$loc" "$color" "$state" "$rel" "$name"
      fi
      out+="$line"$'\n'
      shown=$((shown + 1))
    done < <(sort -t $'\t' -k1,1 -k3,3 <<< "${feed%$'\n'}")
    if [ "$total" -gt "$shown" ]; then
      printf -v line '\e[38;5;240m  +%d more\e[0m' $((total - shown))
      out+="$line"$'\n'
    fi
  else
    out+=$'\e[1m 🤖 AGENTS 0\e[0m\e[38;5;240m   C-t a: 非表示 / C-t A: ジャンプ\e[0m\n'
    out+=$'\e[38;5;240m  (no agents)\e[0m\n'
  fi

  # \e[H で先頭へ、各行 \e[K で残骸消去、末尾 \e[J で下を掃除 (全消し clear より無点滅)。
  # \e[K → \e[0m の順が重要: ハイライト行 (cur=1) は行末に背景色を残したまま来るので、
  # \e[K が背景色で行末まで塗った後に reset して次行へ漏らさない
  printf '\e[H'
  # ⚠️ `printf '%s' "$out" | while read` にしないこと (パイプがサブシェル = 1 fork)。
  # here-string はこのシェル内で回る。末尾改行は落とす (残すと空行を 1 本余計に描く)
  while IFS= read -r line; do
    printf '%s\e[K\e[0m\n' "$line"
  done <<< "${out%$'\n'}"
  printf '\e[J'

  # 🔔 input が 1 件でもあればパネル自体の背景を暗赤にして周辺視で気づけるようにする
  # (bell シアン反転 / zoom 暗赤と同じ「色面で知らせる」思想)。fork+tmux 呼び出しなので
  # 毎 tick ではなく変化したときだけ叩く。
  # ⚠️ select-pane -P で書かないこと: select-pane はたとえ -P 目的でも対象 pane を
  # アクティブにする (3.7b 実測 2026-08-11)。input 遷移のたびにパネルがフォーカスを
  # 奪い全 pane が dim する実発バグの原因だった。set-option -p window-style は
  # フォーカスに一切触れない (同実測で style 適用とアクティブ不変を確認済み)
  local want_bg='colour233'
  [ "$n_input" -gt 0 ] && want_bg='colour52'
  if [ "$want_bg" != "$PANEL_BG_CURRENT" ] && [ -n "${TMUX_PANE:-}" ]; then
    tmux set-option -p -t "$TMUX_PANE" window-style "fg=colour252,bg=$want_bg" 2>/dev/null || true
    PANEL_BG_CURRENT="$want_bg"
  fi
}

cmd_render() {
  PANEL_BG_CURRENT='colour233'   # 作成時スタイル (create_panel の -s) と揃えた初期値
  printf '\e[?25l'   # カーソル非表示
  trap 'printf "\e[?25h"' EXIT
  while :; do
    draw_once
    sleep "$REFRESH_SECS"
  done
}

# ---- main -------------------------------------------------------------------

# ⚠️ source されたときは関数を定義するだけで dispatch しない。tests/tmux/bench_tmux.sh の
# agent_panel_tick_forks が draw_once を**同じプロセスで**呼んで子プロセス数 (fork) を数えるため
# (別プロセスで実行すると親からは子 1 個にしか見えず、tick 内の fork が観測できない)。
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  [ -n "${TMUX:-}" ] || { echo "tmux_agent_panel.sh: not inside tmux" >&2; exit 1; }

  case "${1:-}" in
    toggle)    cmd_toggle "${2:-}" ;;
    follow)    cmd_follow "${2:-}" ;;
    render)    cmd_render ;;
    save-hide) cmd_save_hide ;;
    save-show) cmd_save_show ;;
    unfocus)   cmd_unfocus ;;
    *) echo "usage: tmux_agent_panel.sh toggle|follow [window_id] | render | save-hide | save-show | unfocus" >&2; exit 2 ;;
  esac
fi
