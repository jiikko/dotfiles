#!/bin/sh
#
# 孤児化した tmux サーバ（listening socket ファイルが消えたのにプロセスだけ生存）を回収する。
#
# 背景 (2026-06-28 診断):
#   TMUX_TMPDIR=$(mktemp -d) で起こした tmux サーバが、その後 `rm -rf "$TMUX_TMPDIR"` で
#   socket ファイルだけ消され、プロセスは kill されずに残る（launchd に里子化, ppid=1）。
#   この孤児は `ps` に `^tmux` として残り続けるため、tmux-continuum の自動復元 Gate2
#   を恒久的に破る:
#     vendor/tmux-plugins/tmux-continuum/scripts/helpers.sh の all_tmux_processes は
#     `ps | grep '^tmux'` を socket 生存に関係なく数え、number_tmux_processes_except_current_server
#     が 1 を超えると another_tmux_server_running_on_startup が真になり、
#     continuum_restore.sh:25 が auto-restore を丸ごと skip する。
#   実害: 2026-06-11〜06-28 の 17 日間 restore が不発（tt-restore-duration.log が凍結）。
#   原因は mktemp socket 上の孤児 scratch サーバ 2 台が ^tmux 数を押し上げていたこと。
#
# 不変条件:
#   - 生存 socket を 1 つでも持つプロセス（実サーバ・接続中 client）は絶対に kill しない。
#   - socket ファイルが消滅していても、accept 済み接続 fd を持つ（= client が attach して
#     使用中の）サーバは kill しない。unix socket は unlink 後も接続が維持されるため、
#     「ファイル消滅 = 未使用」ではない（実測: rm 後も attach 中の操作は正常に通る）。
#   - 判定の主キーは「socket ファイルの実在 + 接続 fd の有無」であり、argv やセッション名ではない。
#   - TERM 送信後は実 exit を bounded-wait で確認してから戻る（呼び出し元 _tt_impl は直後に
#     サーバを起動し、conf source 中の continuum Gate2 が ps を数える。TERM 処理中の孤児が
#     ps に残ると Gate2 が再び破れ、reap の目的そのものが不発になる）。
#
# 呼び出し位置: tmux サーバ起動より前（= continuum の Gate2 判定より前）に実行すること。
#   zshlib/_tmux_session.zsh の _tt_impl がサーバ未起動を検知した直後に呼ぶ。
#   手動でも安全に実行できる（孤児が無ければ何もしない）。
#
# 現況 (2026-07-30): continuum の Gate2 は「default socket のサーバか」判定へ置換済み
#   (vendor/.../tmux-continuum/scripts/helpers.sh のパッチ) のため、本スクリプトは復元の
#   成否には効かなくなった。socket が rm された孤児プロセスの掃除役 (衛生) として存続する。
#   なお「socket が生きたまま放置されたテストサーバ」は本スクリプトの対象外であり (下記
#   不変条件のとおり意図的)、それらの後片付けはテスト/probe 側の責務
#   (_claude/rules/tmux-probe-requires-socket-isolation.md)。
#
# DRY_RUN=1 で「kill せず対象だけ列挙」する（検証用）。

set -u

# 共有観測ログ。seam は他の書き手と同じ (テストが差し替える)
TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"

# lsof が無い環境では socket 生存を判定できない。誤って実サーバを kill しないため何もしない。
if ! command -v lsof >/dev/null 2>&1; then
  exit 0
fi

uid=$(id -u)

# 本番 (OS 既定の default socket) のサーバは、socket ファイルが消えていても絶対に reap しない。
# 発火条件: 何か (掃除スクリプト・tmp cleaner・並行セッション) が default socket ファイルを
# 削除し、その瞬間に client が全 detach していると、下の判定は「socket 全消滅 + 接続なし」=
# 孤児と結論して生きている本番サーバを TERM→KILL する。tmux は exit 時保存をしないため、
# 直前の resurrect 保存 (周期 15 分) 以降が失われる。
# 本スクリプトの目的は冒頭の現況コメントのとおり「mktemp socket 上の孤児の掃除」なので、
# default socket を除外しても目的は達成できる。
# ⚠️ TMUX_TMPDIR で隔離された socket は保護しない (テスト・probe の孤児は掃除対象。後片付けは
# probe 側の責務という既存の不変条件を維持する)。macOS では /tmp が /private/tmp の symlink で
# lsof がどちらを返すか環境依存なので両方を候補に持つ。
# TT_REAP_PROTECT_SOCKS はテスト用の seam (実 default socket を使うテストは本番と衝突する)。
# ⚠️ seam は「追加」であって「置換」ではない。`:-` の置換形にすると、空白 1 個や末尾スラッシュ
# のような**空でないが無意味な値**を渡したときに既定の本番保護が黙って全消えする (実測
# 2026-08-21: `TT_REAP_PROTECT_SOCKS=" "` で default socket が保護対象から外れた)。
# 既定は常に含め、seam の値はそこへ足すだけにする。
protect_socks="/tmp/tmux-$uid/default
/private/tmp/tmux-$uid/default"
[ -n "${TT_REAP_PROTECT_SOCKS:-}" ] && protect_socks="$protect_socks
$TT_REAP_PROTECT_SOCKS"

# `^tmux` にマッチするプロセス（サーバも client も含む）を列挙。
# pgrep -x はプロセス名 == "tmux" の厳密一致（argv 全体ではない）。
pids=$(pgrep -U "$uid" -x tmux 2>/dev/null || true)
[ -n "$pids" ] || exit 0

reaped=0
reaped_pids=""
for pid in $pids; do
  # この PID が開いている tmux unix socket のパスを取得（fd が複数ありうる）。
  # -F n のフィールド出力 (n<path> 行) からパス全体を行単位で取る。
  # ⚠️ NAME 列を awk '{print $NF}' で取ると空白入りパス (例: TMUX_TMPDIR="~/tmp dir") が
  #    最終フィールドだけに truncate され、[ -S ] が常に偽 → 生きているサーバを孤児と
  #    誤判定して kill する (実測で再現)。-F n はパスを 1 行で返すため壊れない。
  # client 側の接続 fd は NAME が '->0x...' 形式でパスを含まないため grep で落ちる
  # (= client プロセス自体は「socket を開いていない」扱いで continue され、触られない)。
  socks=$(lsof -p "$pid" -a -U -F n 2>/dev/null | sed -n 's/^n//p' | grep "tmux-$uid/" || true)
  # tmux socket を 1 つも開いていない → サーバでも接続中 client でもない（起動途中等）。触らない。
  [ -n "$socks" ] || continue
  alive=0
  npaths=0
  protected=0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    npaths=$((npaths + 1))
    # ⚠️ 相対パスの socket は **[ -S ] で確認できない**。lsof が返すパスは対象プロセスの cwd
    # 基準なので、reaper 自身の cwd で解決すると生きている socket を「無い」と読む。tmux は
    # `-S ./rel/...` の相対パスを絶対化しないため、この形の live サーバが実在しうる
    # (実測 2026-08-21: cwd を変えるだけで同じ生存サーバが would reap に入った / 消えた。
    # 冒頭の不変条件「生存 socket を 1 つでも持つプロセスは絶対に停止しない」に反していた)。
    # 確認できないものは alive 側へ倒す = 停止しない (fail-safe)。
    case "$s" in
      /*) [ -S "$s" ] && alive=1 ;;
      *)  alive=1 ;;
    esac
    # 保護対象 (default socket) を開いているプロセスは、socket が消えていても触らない。
    # ⚠️ lsof のパス表記は環境依存。macOS は /tmp が /private/tmp の symlink なので
    # /private/tmp/... を返す (実測 2026-08-21)。素の文字列比較だと保護が黙って外れるため、
    # 先頭 /private を剥がして正規化してから比べる。
    ns="$s"; case "$ns" in /private/*) ns="${ns#/private}" ;; esac
    while IFS= read -r pguard; do
      [ -n "$pguard" ] || continue
      np="$pguard"; case "$np" in /private/*) np="${np#/private}" ;; esac
      [ "$ns" = "$np" ] && protected=1
    done <<PROTEOF
$protect_socks
PROTEOF
  done <<EOF
$socks
EOF
  if [ "$protected" -eq 1 ]; then
    [ "${DRY_RUN:-0}" = "1" ] && printf 'protected (default socket) pid=%s\n' "$pid"
    continue
  fi
  # 生存 socket を 1 つでも持つ = 実サーバ or 接続中 client。絶対に保護する。
  [ "$alive" -eq 1 ] && continue
  # socket ファイルは全て消滅している。ただしパス付き fd が 2 行以上 = listening fd に加えて
  # accept 済み接続 fd がある = client が attach して使用中 (macOS の lsof は accept 側 fd にも
  # bind パスを NAME 表示する。実測)。unlink 後も接続は生きているため、使用中として保護する。
  # 真の孤児 (client なし) は listening の 1 行だけになる。
  [ "$npaths" -ge 2 ] && continue
  # ここに来た = 開いている tmux socket が全て消滅 + 接続なし = 孤児サーバ。回収する。
  if [ "${DRY_RUN:-0}" = "1" ]; then
    socks_oneline=$(printf '%s' "$socks" | tr '\n' ' ')
    printf 'would reap orphan tmux pid=%s socket(s)=%s\n' "$pid" "$socks_oneline"
  else
    kill -TERM "$pid" 2>/dev/null || true
    reaped_pids="$reaped_pids $pid"
  fi
  reaped=$((reaped + 1))
done

escalated=0
if [ -n "$reaped_pids" ]; then
  # TERM は非同期。実 exit を bounded-wait で確認する (0.05s x 40 = 最大 2s、全 pid 共通)。
  # 通常の idle 孤児は数十 ms で消えるため実質 1〜2 周で抜ける。
  anyalive=0
  i=0
  while [ "$i" -lt 40 ]; do
    anyalive=0
    for pid in $reaped_pids; do
      kill -0 "$pid" 2>/dev/null && anyalive=1
    done
    [ "$anyalive" -eq 0 ] && break
    sleep 0.05
    i=$((i + 1))
  done
  # 2s 待っても残る = TERM に応答しない (stopped/wedged/スワップ復帰待ち)。KILL に昇格する。
  # 対象は socket 全消滅 + 接続なしを確認済みの孤児であり、tmux は exit 時保存をしない
  # (保存は continuum の status interpolation 経由) ため KILL によるデータ喪失は無い。
  if [ "$anyalive" -eq 1 ]; then
    for pid in $reaped_pids; do
      if kill -0 "$pid" 2>/dev/null; then
        kill -KILL "$pid" 2>/dev/null || true
        escalated=$((escalated + 1))
      fi
    done
    # KILL 反映を短く待ち、ps から消えたのを見届けてから戻る (Gate2 判定前の安全余裕)
    sleep 0.1
  fi
fi

# 観測ログに残す（_tmux.conf Fix C / tmux_resurrect_save.sh Fix B と同じファイル）。
if [ "$reaped" -gt 0 ] && [ "${DRY_RUN:-0}" != "1" ]; then
  # ⚠️ ここだけ共有の tt_trigger_log (lib/tmux_resurrect_guards.sh) を使わない。本ファイルは
  #    #!/bin/sh で、guards.sh は herestring (<<<) を使う **bash 専用**のため source すると
  #    /bin/sh が dash の環境 (Linux CI) で構文エラーになる。代わりに seam (TT_TRIGGER_LOG) は
  #    尊重する — issue 079 が問題にしていたのは「seam を迂回してテストから観測できない」ことで、
  #    共有関数を通ること自体ではない。guards.sh が POSIX 化されたらここも寄せる。
  { mkdir -p "$(dirname "$TT_TRIGGER_LOG")" \
      && printf '%s\treaped-orphan-servers n=%s escalated=%s\n' \
         "$(date +%FT%T)" "$reaped" "$escalated" >> "$TT_TRIGGER_LOG"; } 2>/dev/null || true  # trigger-log-writer: allow (#!/bin/sh なので bash 専用の guards.sh を source できない。上の注記が理由の正本)
fi

exit 0
