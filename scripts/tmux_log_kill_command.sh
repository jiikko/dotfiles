#!/usr/bin/env bash
# kill-server / kill-session の command-alias shim (発行元の記録 + 直前セーフティ保存)。
#
# _tmux.conf の command-alias[101]/[102] から同期 run-shell で呼ばれ、本物の kill-* は
# このスクリプト完了後に実行される (tmux の alias 展開は 1 パスで、alias 本体内の同名
# コマンドは組み込みに直接解決されるため再帰しない。tmux 3.7b 実測 2026-07-30)。
#
# なぜ: サーバが "[server exited]" で死んだとき「誰が kill したか」は事後特定できない
#   (macOS はシグナル送信元を記録しない。tmux の #{client_pid} も「発行元」ではなく
#   attach 中の best client に解決されるため使えない。どちらも 2026-07-30 実測)。
#   発行元は、この shim の実行中はまだ生きている「kill-server / kill-session を argv に
#   持つ tmux クライアントプロセス」として ps から同定し、親子関係 (ppid chain) ごと
#   ~/.cache/tt-restore-trigger.log に記録する。2026-07-30 の本番サーバ誤殺
#   (_claude/rules/tmux-probe-requires-socket-isolation.md) の再発を即日特定するための観測。
#
# セーフティ保存: kill-server (および「最後の 1 セッション」への kill-session = exit-empty
#   でサーバごと落ちる) の直前に resurrect 保存を試みる。これにより「kill 経路」の損失窓は
#   構造的にゼロになる (シグナル直撃経路は保存できないため、そちらは continuum 周期保存が
#   カバーする)。保存は choke point wrapper (@resurrect-save-script-path) 経由なので、
#   復元中ガード・hold ガード・退行ガード・lock 直列化が全て効く。bounded-wait で待ち、
#   超過したら保存を待たずに kill へ進む (部分保存は wrapper の退行ガードが reject する)。
#
# 無音契約: kill コマンドの実行経路上にあるため、いかなる失敗でも stdout/stderr を汚さず
#   exit 0 する (非 0 や出力は run-shell エラーとして pane の view-mode に積まれる。
#   scripts/CLAUDE.md「hook から呼ばれるスクリプトの無音契約」と同じ)。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# tt_on_default_server を共有ライブラリから読む (判定式の二重定義を避ける)
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"
# 保存完了を待つ上限秒。wrapper の lock bounded-wait (15s) + 実保存 (数秒) を覆う値。
# 超過時は保存を諦めて kill に進む (kill-server の体感遅延の上限でもある)。
TT_KILL_SAVE_WAIT_SECONDS="${TT_KILL_SAVE_WAIT_SECONDS:-20}"

kind="${1:-unknown}"

# default socket のサーバ以外 (テスト/scratch の隔離サーバ) では何もしない。
# ログを共有ファイルに書くと watchdog の死因分類を汚し、保存は wrapper 側ガードで
# どのみち落ちる。静かに素通しして本物の kill-* だけ実行させる。
tt_on_default_server || exit 0


# 発行元の同定: argv に「tmux ... kill-server/kill-session」を持つプロセスを ps から探す。
# 自分のプロセスツリーとサーバ本体は除外する。複数 (稀) は先頭 3 件まで。
# 🚨 実行ファイル名は basename で判定する。`$2 == "tmux"` の厳密一致だとフルパス起動
#   (/opt/homebrew/bin/tmux kill-server。スクリプト経由では普通) を取り逃し、この装置の
#   主目的である「誰が殺したか」が not-found になる (2026-07-30 セルフレビューで検出)。
find_issuers() {
  local server_pid self_pid kind_re
  server_pid="$(tmux display-message -p '#{pid}' 2>/dev/null)"
  self_pid=$$
  # 発行元の argv は略記形かもしれない (tmux は曖昧でない前方一致を受理し、alias も略記に
  # 張ってある)。正式名で完全一致すると `tmux kill-sessio` の発行元を取り逃すため、
  # 曖昧でない最短接頭辞 (kill-ser / kill-ses) で照合する。
  case "$kind" in
    kill-server)  kind_re=' kill-ser' ;;
    kill-session) kind_re=' kill-ses' ;;
    *)            kind_re=" $kind" ;;
  esac
  ps -axo pid=,command= 2>/dev/null | awk -v kre="$kind_re" -v srv="${server_pid:-0}" -v self="$self_pid" '
    $2 ~ /(^|\/)tmux$/ && index($0, kre) > 0 {
      if ($1 == srv || $1 == self) next
      print $1
      if (++n >= 3) exit
    }'
}

# ppid chain を辿って「pid:コマンド先頭 40 文字」を最大 6 段連結する
ancestry_of() {
  local pid="$1" depth=0 out="" line ppid cmd
  while [ -n "$pid" ] && [ "$pid" -gt 1 ] 2>/dev/null && [ "$depth" -lt 6 ]; do
    line="$(ps -o ppid=,command= -p "$pid" 2>/dev/null)" || break
    [ -n "$line" ] || break
    ppid="$(printf '%s' "$line" | awk '{print $1}')"
    cmd="$(printf '%s' "$line" | awk '{ $1=""; sub(/^ /,""); print substr($0, 1, 40) }')"
    out="${out:+$out <- }${pid}:${cmd}"
    pid="$ppid"
    depth=$((depth + 1))
  done
  printf '%s' "${out:-unknown}"
}

sessions="$(tmux list-sessions 2>/dev/null | grep -c .)" || sessions=0

# kill-server は常に、kill-session は「残り 1 セッション以下」(= exit-empty でサーバごと
# 落ちる) のときだけ直前保存する。wrapper のガード群 (hold のみ/復元中/退行) が中身の
# 妥当性を守るので、ここでは発火条件だけ判断する。
do_save=0
case "$kind" in
  kill-server)  do_save=1 ;;
  kill-session) [ "${sessions:-0}" -le 1 ] && do_save=1 ;;
esac

# hold セッション (bootstrap 中の一時セッション) しか無い状態では保存しない。
# wrapper の only-hold ガードは lock の bounded-wait (15s) より後段にあるため、ここで発火させると
# tt bootstrap の hold 掃除が最大 15s (shim の cap で 20s) 待たされる。どのみち wrapper は
# reject するので、待たせる価値がない (レビュー指摘 2026-07-30)。
if [ "$do_save" -eq 1 ] && tt_only_hold_sessions; then
  do_save=0
  hold_only=1
fi

save_result=skipped
[ "${hold_only:-0}" = 1 ] && save_result=skipped-hold-only
if [ "$do_save" -eq 1 ]; then
  save_script="$(tmux show -gqv @resurrect-save-script-path 2>/dev/null)"
  if [ -n "$save_script" ] && [ -x "$save_script" ]; then
    "$save_script" quiet >/dev/null 2>&1 &
    save_pid=$!
    i=0
    limit=$((TT_KILL_SAVE_WAIT_SECONDS * 5))
    while kill -0 "$save_pid" 2>/dev/null && [ "$i" -lt "$limit" ]; do
      sleep 0.2
      i=$((i + 1))
    done
    if kill -0 "$save_pid" 2>/dev/null; then
      save_result=timeout   # 保存は走ったまま kill へ進む (部分保存は退行ガードが弾く)
    else
      # 🚨 終了コードを必ず収集する。捨てると「wrapper がガードで reject した (= 何も保存
      # されていない)」ケースまで save=ok と記録され、ログが「保存できたつもり」の嘘をつく
      # (レビューで実証: @tt-restore-in-progress 残置中の kill-server が save=ok になった)。
      wait "$save_pid"; save_rc=$?
      if [ "$save_rc" -eq 0 ]; then
        save_result=ok
      else
        save_result="rejected-rc$save_rc"
      fi
    fi
  else
    save_result=no-save-script
  fi
fi

issuer_info=""
for ipid in $(find_issuers); do
  issuer_info="${issuer_info:+$issuer_info | }$(ancestry_of "$ipid")"
done

# server pid を必ず刻む。watchdog の死因分類は共有ログを時間窓だけで相関するため、pid が無いと
# 「前世代のサーバへの kill-cmd」が新サーバの外因死に誤って結び付く (レビューで実証: 外因死が
# verdict=kill-server-command になった)。
tt_trigger_log "kill-cmd cmd=$kind pid=$(tmux display-message -p '#{pid}' 2>/dev/null) sessions=$sessions save=$save_result epoch=$(date +%s) issuer=${issuer_info:-not-found}"

exit 0
