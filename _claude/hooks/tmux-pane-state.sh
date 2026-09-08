#!/usr/bin/env bash
# Claude Code hook: tmux ペイン境界 (@claude_state) に作業状態を反映する。
#
# 使い方: tmux-pane-state.sh working|input|idle|start|clear
#   working : "⚙ working" を表示 (UserPromptSubmit / PostToolUse=承認後の自動復帰)
#             + @claude_bg を落とす (Claude が動いている = bg 待機ではない)
#   input   : "🔔 input" を表示   (Notification — permission 承認待ち・質問への回答待ち)
#             + ペインが画面に見えていなければ macOS 通知 (音あり)
#             + tmux ベル (window-status のシアン反転)
#             ただし次のどちらかなら状態を変えず通知もベルも出さない:
#             (a) stdin (hook JSON) の notification_type が入力不要の種別
#                 (auth_success / agent_completed 等)
#             (b) @claude_bg が立っている = bg 待機中 (下記)
#   idle    : "✓ idle" を表示     (Stop = 応答完了)
#             + ペインが画面に見えていなければ macOS 通知 (音なし)
#             + tmux ベル (bg タスクが残っているときは鳴らさない — 下記)
#             ただし stdin (hook JSON) の background_tasks に実行中タスクが残っている
#             場合 (バックグラウンド Bash / subagent / codex レビュー待ち等) は、
#             入力待ちではないので "⚙ working (bg)" を表示して通知もしない。
#             タスク完了で再開すると PostToolUse / 次の Stop が状態を上書きする。
#             表示側 (_tmux.conf / tmux_agent_panel.sh / tmux_agent_jump.sh) は
#             "*working*" の部分一致で色分けするため、このラベルも working 色になる
#   start   : "✓ idle" を表示     (SessionStart — 起動直後なので通知しない)
#   clear   : 状態を消す          (SessionEnd — Claude 終了後は通常シェルへ戻す)
#
# @claude_bg (pane option) = 「直近の Stop 時点で bg タスクが in-flight だった」
# = Claude が bg の完了待ちで止まっている。input 分岐がこれを読んでベルを抑制する。
# 🚨 Notification hook の stdin には background_tasks が無い (claude 2.1.263 の
# バイナリ内スキーマ: Notification = 共通ベース + {message, title?, notification_type}。
# background_tasks は Stop / SubagentStop 限定) ので、Stop が書いたものを input が
# 読む形でしか bg の有無を渡せない。
# 落とすのは working (UserPromptSubmit / PostToolUse) — Claude が動いているなら
# 待機ではない。この意味づけにより「bg 完了 → Claude 再開 → permission 承認待ち」は
# 再開時の PostToolUse が @claude_bg を落とすのでちゃんと鳴る。逆に「bg を起こした
# のと同じターン内の承認待ち」も鳴る (bg があっても人が答えないと前へ進まないため)。
# 状態は pane 単位のユーザーオプション @claude_state に書き込む。未設定なら
# pane-border-format 側の #{?@claude_state,...,} が空に展開されるため、
# Claude を起動していないペイン (通常シェル等) には一切影響しない。
# tmux 外で起動された場合 ($TMUX_PANE 未設定) は何もしない。
# 🚨 その「何もしない」は今は通知の欠落を意味する: 内蔵通知を settings.json の
# preferredNotifChannel=notifications_disabled で切っており (これはユーザー設定なので
# 全プロジェクト・全起動に効く。ベルだけでなく OSC 9 / 99 / 777 も止まる)、代わりに
# 鳴らすのがこのフックだから。tmux 外の claude はベルも OSC 通知も出ない。
# フックには制御端末が無く /dev/tty へ書けないので (実測 2026-09-05)、この経路の
# 代替はここには置けない。tmux 外でも通知が要るなら内蔵チャネルを戻すしかない。
#
# 通知条件 = 「画面で状態表示が見えていないとき」だけ:
# - そのペインのウィンドウがどのクライアントでも前面でない (window_active_clients=0)
# NOTE: かつては「ターミナル自体が非アクティブ (@term_unfocused)」も条件だったが、
# 供給元の client-focus-in/out フックを誤判定のため削除した (_tmux.conf 側の
# NOTE 参照、2026-06-10) のに伴い本条件も撤去。ターミナルが背面でも claude
# ウィンドウが tmux 内で前面なら通知は出ない (許容)
set -uo pipefail
unset CDPATH

[ -n "${TMUX_PANE:-}" ] || exit 0
command -v tmux >/dev/null 2>&1 || exit 0

# @claude_state_since は「claude hook が状態を書いた時刻」(epoch)。agent パネル /
# ジャンプ (scripts/tmux_agent_panel.sh / tmux_agent_jump.sh) が経過時間表示に使う。
# mark-seen の input→seen 降格では更新しない (「🔕 seen 12m = 12 分前に入力待ちに
# なった (見たが未応答)」の方が放置時間として有用なため。意図的)
set_state() {
  tmux set -p -t "$TMUX_PANE" @claude_state "$1" 2>/dev/null
  tmux set -p -t "$TMUX_PANE" @claude_state_since "$(date +%s)" 2>/dev/null
}

# bg 待機フラグの書き / 読み。0 を渡すと消す (unset)。
set_bg() {
  if [ "${1:-0}" -gt 0 ] 2>/dev/null; then
    tmux set -p -t "$TMUX_PANE" @claude_bg "$1" 2>/dev/null
  else
    tmux set -p -u -t "$TMUX_PANE" @claude_bg 2>/dev/null
  fi
}

# 🚨 数字判定は範囲式 [0-9] でなく明示列挙で書く (ja_JP.UTF-8 では [0-9] が全角数字を
# 通す。_claude/rules/shell-numeric-gate-explicit-digits.md)。桁数上限も併せて課す —
# 19 桁以上は [ -gt ] が integer expected で常に偽になり、判定が無音で死ぬ。
# 判定不能 (未設定・非数値・異常長) は「bg 待機ではない」= 鳴らす側へ倒す。input の
# 危険な失敗方向は「入力待ちなのに気づけない」なので、ここは過検知側に落とす。
bg_waiting() {
  local n
  n=$(tmux show -p -v -t "$TMUX_PANE" @claude_bg 2>/dev/null) || return 1
  case "$n" in ''|*[!0123456789]*) return 1 ;; esac
  [ "${#n}" -le 9 ] || return 1
  [ "$n" -gt 0 ]
}

# そのペインのウィンドウが、どのクライアントでも前面でないとき真。
# 判定不能 (tmux が答えない) は「見えている」に倒す — 通知もベルも「見ていないときの
# 代替手段」なので、判定できないまま鳴らすと画面を見ている最中に割り込む側に転ぶ。
pane_hidden() {
  local visible
  visible=$(tmux display -p -t "$TMUX_PANE" '#{window_active_clients}' 2>/dev/null) || return 1
  [ "${visible:-1}" = "0" ]
}

notify_if_hidden() {
  local message="$1" sound="${2:-}"
  command -v terminal-notifier >/dev/null 2>&1 || return 0
  local target
  if pane_hidden; then
    target=$(tmux display -p -t "$TMUX_PANE" '#{session_name}:#{window_index}' 2>/dev/null)
    # -group で同一ペインの通知を上書き (通知センターに溜めない)。
    # バックグラウンド起動で hook の応答を遅らせない
    terminal-notifier -title "Claude Code (${target:-tmux})" -message "$message" \
      -group "tmux-claude-$TMUX_PANE" ${sound:+-sound "$sound"} >/dev/null 2>&1 &
  fi
}

# tmux の bell フラグ (window-status のシアン反転 = 見るまで残る印) を立てる。
# 🚨 hook の stdout は Claude Code が捕まえるので printf '\a' では端末へ届かない。
# ペインの pane_tty へ直接書くと tmux が出力として読み、window_bell_flag=1 になる
# (隔離サーバ -L で実測 2026-09-05)。
#
# 🚨 見えているウィンドウで鳴らさないのは flag の問題ではない。flag は tmux が即座に
# 落とすが、_tmux.conf の `set-hook -g alert-bell` は落ちる前に発火し、800ms の
# display-message (-N なのでキー入力でも消えない) が被さる (pty で client を attach
# して実測 2026-09-05: flag=0 なのに alert-bell は 1 回発火)。見ている最中の応答完了
# ごとにステータス行が覆われるので、notify_if_hidden と同じ可視性ガードを課す。
#
# 🚨 Claude Code 内蔵のベル (preferredNotifChannel) は必ず切っておくこと
# (_claude/settings.json で notifications_disabled)。内蔵ベルは Stop / Notification
# で無条件に鳴り、bg タスク実行中かどうかを区別しないため、これと二重に鳴ると
# 本ルーチンで絞った意味が消える
ring_bell() {
  local tty
  pane_hidden || return 0
  tty=$(tmux display -p -t "$TMUX_PANE" '#{pane_tty}' 2>/dev/null) || return 0
  [ -n "$tty" ] && [ -w "$tty" ] || return 0
  printf '\a' > "$tty" 2>/dev/null || :
}

case "${1:-}" in
  working) set_state "⚙ working"; set_bg 0 ;;
  input)
    # Notification hook は入力待ち以外の種別 (auth_success / agent_completed 等) でも
    # 発火する。stdin JSON の notification_type
    # (実測 2026-08-13: {"hook_event_name":"Notification","notification_type":"idle_prompt",...})
    # を見て「入力不要と分かっている種別」だけ状態を変えない (denylist)。
    # 未知の種別・jq 不在・フィールド不在は 🔔 input に倒す — このインジケータの
    # 危険な失敗方向は「入力待ちなのに気づけない」なので、判定不能は過検知側に落とす。
    #
    # denylist の中身は claude 2.1.263 のバイナリが持つ notification_type の enum
    # (14 値) と突き合わせて選んだ。鳴らす側に残すのは「人が答えないと前へ進まない」
    # 6 値 = permission_prompt / idle_prompt / elicitation_dialog /
    # elicitation_url_dialog / agent_needs_input / worker_permission_prompt と、
    # 人の対処が要る quota_auto_resume_disabled。
    # 🚨 かつて並んでいた elicitation_complete / elicitation_response は enum に無く、
    # 何も除外していなかった (実在するのは elicitation_dialog / elicitation_url_dialog で、
    # どちらも入力が要る = 鳴らす側)。新しい値を足すときは同じ enum を引き直すこと
    # (バイナリの strings に `notification_type` の配列がそのまま出る)。
    ntype=""
    if command -v jq >/dev/null 2>&1 && [ ! -t 0 ]; then
      ntype=$(jq -r '.notification_type // ""' 2>/dev/null) || ntype=""
    fi
    case "$ntype" in
      auth_success|agent_completed|push_notification|\
      computer_use_enter|computer_use_exit|\
      quota_auto_resume_fired|quota_auto_resume_stale) : ;;
      *)
        # bg 待機中 (Claude は bg タスクの完了を待って止まっている) は、入力待ちの
        # 催促が来ても人がやることは無いので鳴らさず表示も変えない
        # (@claude_state は "⚙ working (bg:N)" のまま = ベルマークを出さない)
        if bg_waiting; then
          :
        else
          set_state "🔔 input"; ring_bell; notify_if_hidden "🔔 入力待ち (承認 or 回答が必要)" "default"
        fi
        ;;
    esac
    ;;
  idle)
    # Stop hook の stdin JSON から実行中バックグラウンドタスクを数える。
    # フィールド名は background_tasks (実測 2026-08-13:
    # {"hook_event_name":"Stop","background_tasks":[{"id":...,"status":"running",...}]})。
    # 🚨 status は 6 値 ("pending" "running" "completed" "failed" "killed" "paused"。
    # claude バイナリの enum を実測 2026-09-05) で、Claude Code 自身が
    # background_tasks を "In-flight background work (running/pending + backgrounded)"
    # と定義している。**終端の 3 値を除く denylist で数えること** — "running" だけの
    # allowlist にすると pending / paused を「手が空いた」と誤判定する
    # jq 不在 / stdin が JSON でない / フィールド不在は 0 扱い (= 従来どおり idle)。
    # input 分岐と fail 方向が非対称なのは意図的: bg タスクは完了すればハーネスが
    # セッションを再起動して次の Stop が状態を上書きする (実測 2026-08-13) ため
    # 誤 idle は自己回復するが、input の見逃しは人が来るまで誰も直せない
    pending=0
    if command -v jq >/dev/null 2>&1 && [ ! -t 0 ]; then
      pending=$(jq -r '[.background_tasks[]? | select(.status == "completed" or .status == "failed" or .status == "killed" | not)] | length' 2>/dev/null) || pending=0
      case "$pending" in ''|*[!0-9]*) pending=0 ;; esac
    fi
    set_bg "$pending"
    if [ "$pending" -gt 0 ]; then
      set_state "⚙ working (bg:$pending)"
    else
      set_state "✓ idle"
      ring_bell
      notify_if_hidden "✓ 応答完了"
    fi
    ;;
  start)   set_state "✓ idle"; set_bg 0 ;;
  clear)   tmux set -p -u -t "$TMUX_PANE" @claude_state 2>/dev/null
           tmux set -p -u -t "$TMUX_PANE" @claude_state_since 2>/dev/null
           set_bg 0 ;;
esac

# hook が状態を返さないよう常に成功で抜ける (Stop/UserPromptSubmit を block しない)
exit 0
