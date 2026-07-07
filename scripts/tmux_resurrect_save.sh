#!/usr/bin/env bash
# tmux-resurrect の保存を「全経路で単一 lock 直列化」する共通 wrapper。
# @resurrect-save-script-path をこのスクリプトに向けて使う（_tmux.conf 参照）。
#
# 背景（なぜ必要か）:
#   保存経路は 3 系統あり、それぞれ別々の（または無い）lock で動いていた:
#     1. window 増減 → scripts/tmux_resurrect_debounced_save.sh（かつて自前 mkdir lock を
#        持っていたが skip-as-success の取りこぼしバグがあり撤去済み。直列化は本 wrapper に一本化）
#     2. continuum の周期 autosave（continuum_save.sh）。これは save を
#        `save.sh quiet &` と background 起動し、自前の世代 lock を即 trap 解放するため
#        「保存期間」を保護していない（vendor/.../continuum_save.sh の acquire_lock/
#        fetch_and_run_tmux_resurrect_save_script 参照）。
#     3. 手動・その他
#   この結果、continuum 保存と debounce 保存が同時に upstream save.sh を起動しうる。
#   upstream save.sh（vendor/.../tmux-resurrect/scripts/save.sh の save_all）は
#   共有の save/pane_contents/ ディレクトリ・共有の pane_contents.tar.gz・秒精度の
#   layout ファイル名を lock 無しで使い、最後に `rm save/*` する。2 つが重なると
#   片方の `rm save/*` が他方の dump を消し、空/部分的な pane_contents.tar.gz が
#   last の実体になる（= 復元時に window は作られるが中身が空になる degrade）。
#   layout ファイル自体も同一秒なら同一パスに `>`/`>>` され混線しうる。
#
# 不変条件:
#   - 同時に複数の upstream save.sh を走らせない（単一 mkdir lock）。debounce も
#     continuum も手動(C-s)も同じ @resurrect-save-script-path/キーバインド = 本 wrapper を
#     叩くため、ここに lock を集約すれば全経路が直列化される。
#   - lock はクラッシュ取り残し対策に mtime ベースで stale 自動解除する
#     （tmux_resurrect_debounced_save.sh の lock と同方針）。
#   - lock が取れない（他の保存が進行中）ときは「skip して成功扱い」にしてはいけない。
#     skip を成功扱いにすると、呼び出し側 debounce の tt_run_resurrect_save が保存成功と
#     誤認して @continuum-save-last-timestamp を進め、(a) 今回のイベント(新 window 等)が
#     保存されないまま (b) continuum も次周期(既定 15 分)まで抑止される、という「秒オーダー
#     保存」不変条件の破れが起きる（先行保存はイベント前の状態を採取済みのため新 window を
#     含まない）。よって lock は bounded-wait で待ってから保存する。自動経路は background
#     実行（debounce: run-shell -b / continuum: save.sh quiet &）なので数秒待ってよい。
#     手動 C-s だけは run-shell（非 -b。_tmux.conf の bind C-s）で、lock 競合時は当該
#     クライアントの command queue を最大 TT_SAVE_LOCK_WAIT_SECONDS+保存時間ブロックするが、
#     発生は競合時のみ・上限 ~15s なので「skip して取りこぼす」より待つ方を取る。
#     待っても取れない異常時のみ非 0 で返し、呼び出し側に「保存せず」を伝える。
#
# 依存（変わったら追従が必要）:
#   - upstream save.sh のパス（vendor/tmux-plugins/tmux-resurrect/scripts/save.sh）。
#     本 wrapper はリポジトリルート相対で解決する（DOTFILES_DIR ではなく自身の位置基準）。
#   - 引数（"quiet" 等）はそのまま upstream save.sh に渡す。
#
# NOTE: 本 wrapper の責務は「保存期間 lock による直列化」+「全経路に効かせるべき保存ガード
#   （復元中チェック・Fix B 退行ガード）」。choke point なのでここに置くと continuum 周期保存・
#   手動 C-s・debounce の 3 経路すべてに一度で効く。@continuum-save-last-timestamp の更新等は
#   呼び出し側（debounce スクリプト / continuum）の責務のまま変えない。
set -uo pipefail
unset CDPATH

# 本 wrapper はリポジトリの scripts/ 配下にある前提。upstream save.sh は ../vendor/...。
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
REAL_SAVE="${TT_REAL_SAVE_SCRIPT:-$SCRIPT_DIR/../vendor/tmux-plugins/tmux-resurrect/scripts/save.sh}"

# 保存ガード (復元中 / hold のみ / default サーバ) は debounce 経路と共有のライブラリから読む
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

TT_SAVE_STATE_DIR="${TT_SAVE_STATE_DIR:-$HOME/.cache/tt-resurrect-save}"
TT_SAVE_LOCK_DIR="$TT_SAVE_STATE_DIR/lock"
# lock の stale 判定（秒）の mtime フォールバック。通常の生死判定は owner の PID+起動時刻で行い
# （下記 tt_save_owner_is_stale 参照）、この mtime TTL は「pid ファイルに owner が書かれていない
# 稀な取り残し」（mkdir 直後・pid 書き込み前に死亡）だけに使う。実保存は数秒〜十数秒なので 120s で十分。
TT_SAVE_LOCK_STALE_SECONDS="${TT_SAVE_LOCK_STALE_SECONDS:-120}"
# owner の起動時刻で同定できない（旧形式 PID-only の lock / ps -o lstart= 非対応環境）ときの
# mtime backstop TTL（秒）。通常は owner の PID+起動時刻で生死を厳密に同定し生存 owner を絶対に
# 奪わないが、起動時刻が無いと「PID 再利用の取り残し」と「正当な長時間保存」を区別できない。その場合
# だけ、mtime がこの TTL を超えた生存 owner を取り残しとみなして解除し、保存の永久停止を防ぐ。実保存
# 最大時間より十分大きく（誤って奪う確率を最小化）、continuum interval（既定 900s）より小さく保つこと
# （既定 600s）。起動時刻が取れる通常環境ではこの TTL は使われない。
TT_SAVE_LOCK_HARD_STALE_SECONDS="${TT_SAVE_LOCK_HARD_STALE_SECONDS:-600}"
# lock を取れないとき待つ上限（秒）。先行保存（数秒〜十数秒）の完了を待ってから保存する。
# 自動経路は background 実行なので待ってよい（手動 C-s のみ同期。冒頭コメント参照）。
# 0.2s 間隔でポーリングする。
TT_SAVE_LOCK_WAIT_SECONDS="${TT_SAVE_LOCK_WAIT_SECONDS:-15}"

tt_save_release_lock() {
  rm -f "$TT_SAVE_LOCK_DIR/pid" 2>/dev/null || true
  rmdir "$TT_SAVE_LOCK_DIR" 2>/dev/null || true
}

# owner 行が expected と一致するときだけ解除する conditional release。
# stale lock の横取り（acquire ループ）と自分の lock 解放（EXIT trap）の両方で使う。
# 無条件 rmdir だと「判定〜削除の間に別プロセスが取得した新 lock」を消して並行 save.sh を招く
# （codex 指摘）。owner 一致確認でその窓を大幅に縮める。観測〜削除間の微小 TOCTOU は mkdir lock の
# 原理上完全には消せないが、万一すり抜けて並行 save が起きても upstream save.sh は直近 5 世代を
# 保持するため次回保存で回復する（= 残存リスクは許容範囲）。
tt_save_release_lock_if_owner() {
  local expected="$1" cur=''
  cur="$(cat "$TT_SAVE_LOCK_DIR/pid" 2>/dev/null || true)"
  [ "$cur" = "$expected" ] && tt_save_release_lock
}

# 自分が現 owner のときだけ lock を解放する（EXIT trap 用）。横取り誤検知などで別プロセスが
# 再取得した lock を、後から終了した自分の trap が消す連鎖を防ぐ。
tt_save_release_own_lock() {
  tt_save_release_lock_if_owner "$$ $(tt_save_proc_starttime "$$")"
}

# lock dir の mtime が指定秒数より「古い」とき真（stale 判定の共通部品）。
# BSD/GNU 双方で -mmin -N = 「過去 N 分以内に変更=新しい」。find が空（=N 分以内に該当なし）= それより古い。
# 秒→分の整数除算 +1 で切り上げるため、実効しきい値は名目より最大 ~1 分長い（安全側＝早すぎる解除をしない）。
tt_save_lock_older_than() {
  local secs="$1"
  [ -z "$(find "$TT_SAVE_LOCK_DIR" -maxdepth 0 -mmin "-$(( secs / 60 + 1 ))" 2>/dev/null)" ]
}

# プロセス PID の起動時刻を空白無しの単一トークンに正規化して返す（存在しなければ空）。
# PID だけでは再起動跨ぎ / PID 再利用で「別プロセスを同一 owner と誤認」するため、起動時刻を
# 指紋として併用する。ps -o lstart= は BSD(macOS)/GNU(Linux) 双方で同一プロセスに安定な文字列を返す。
tt_save_proc_starttime() {
  ps -o lstart= -p "$1" 2>/dev/null | tr -s '[:space:]' '_'
}

# 渡された owner 行（"PID 起動時刻トークン"）が「取り残し」かを判定する。owner 行は呼び出し側が
# lock の pid から一度だけ読んで渡す。判定と解除で別々に pid を読むと、判定後に別プロセスが取得した
# 新 owner を解除対象として拾い、その生存 lock を誤って消してしまう（codex 指摘）。
# 単純な PID 生存(kill -0)だけだと二律背反に陥る:
#   (a) 生存中の正当な長時間保存（巨大 pane contents 等）を mtime TTL で奪うと並行 save.sh の競合、
#   (b) ~/.cache の lock が再起動を跨いで残置し PID が無関係な長命プロセスに再利用されると
#       kill -0 が真のままで永久に解除できず、全保存経路が exit 1 を繰り返して保存が止まる。
# 方針: 生死は kill -0 を主判定にして「生きている owner は絶対に奪わない」((a) を回避)。その上で
# PID 再利用 ((b)) だけを「記録と現在の起動時刻が両方取得でき、かつ食い違う」ときに限り取り残し扱い
# にする。起動時刻が取れない/取得失敗の環境では fail-safe で生存 owner を尊重する（誤って奪う方には
# 倒さない）。pid ファイルに PID が無い稀な取り残し（mkdir 直後で書き込み前に死亡）だけ mtime を保険に使う。
tt_save_owner_is_stale() {
  [ -d "$TT_SAVE_LOCK_DIR" ] || return 1
  local owner_pid='' owner_start='' cur_start=''
  read -r owner_pid owner_start <<<"${1:-}"
  if [ -n "$owner_pid" ]; then
    # 生死は kill -0 が主判定。死亡していれば取り残し（解除可）。
    kill -0 "$owner_pid" 2>/dev/null || return 0
    cur_start="$(tt_save_proc_starttime "$owner_pid")"
    if [ -n "$cur_start" ] && [ -n "$owner_start" ]; then
      # 起動時刻が両方取れた: 一致 = 同一プロセスが進行中（どれだけ長くても奪わない）。
      # 不一致 = 別プロセスが同 PID を再利用（再起動跨ぎ等）→ 取り残し（解除可）。
      [ "$cur_start" = "$owner_start" ] && return 1
      return 0
    fi
    # 起動時刻で同定できない（旧形式 PID-only / ps -o lstart= 非対応）。生存 owner を即奪うと正当な
    # 保存を壊すが、永久に待つと PID 再利用で保存が止まる。妥協として mtime hard TTL を backstop に:
    # hard TTL 内なら進行中とみなし待ち、超過なら再利用とみなして取り残し扱いにする（永久停止を防ぐ）。
    tt_save_lock_older_than "$TT_SAVE_LOCK_HARD_STALE_SECONDS" && return 0
    return 1
  fi
  # PID 不明（mkdir 直後で pid 書き込み前の競合 or 旧形式）→ mtime が soft stale TTL より古ければ取り残し。
  tt_save_lock_older_than "$TT_SAVE_LOCK_STALE_SECONDS"
}

# ---- Fix B/C: last の壊滅的セッション数退行ガード（2026-06-28）----
# 背景: continuum の自動 restore が発火しないままサーバが再起動すると（他 tmux サーバ存在で
#   continuum_restore.sh:25 が Gate2 skip する等）、復元前の貧弱なセッション状態で window hook /
#   周期 autosave が走り、upstream save.sh:252-253 が last を貧弱保存へ前進させ、直前の
#   完全状態保存を孤立させる (upstream は files_differ = 「前回と差分があるか」しか見ず、
#   保存内容の健全性は検証しない。差分の有無と中身の妥当性は別物なので Fix B は必要)（実害: 7セッション→2セッションで last が上書きされ
#   復元不能になった事例）。restore 不発の根治（tests/tmux/test_tmux.sh の probe leak 修正 +
#   @continuum-restore-max-delay 拡大）に加え、万一 restore が走らなくても完全状態 last を守る
#   最後の砦としてここで壊滅的退行を弾く。bypass: 正当な大量 kill 直後は TT_SAVE_ALLOW_REGRESSION=1。

# 復元中か（「復元中は絶対に保存しない」不変条件の choke point 検査）。判定の実体は
# 共有ライブラリの tt_restore_in_progress (lib/tmux_resurrect_guards.sh)。
# debounce 入口のガードは sleep + wrapper の bounded-wait の間に stale になるし、
# continuum 周期保存・手動 C-s はそもそもガードを通らない。全経路が必ず通る本 wrapper で
# 保存直前に再検査することで、復元途中の部分状態を last に焼き付ける窓を「save.sh 実行時間
# のみ」まで縮める（完全閉鎖には restore 側の lock 参加が必要だが、変更規模に対して残存窓が
# 小さいため defense-in-depth のここまでとする）。
tt_save_restore_in_progress() {
  tt_restore_in_progress
}

# resurrect の保存先 dir を解決する。vendor helpers.sh:1-7,99-103 と同手順（source 副作用を避け
# wrapper 自己完結）。解決順 @resurrect-dir → ~/.tmux/resurrect → $XDG_DATA_HOME/tmux/resurrect。
# helpers.sh の解決順を変えたらここも追従すること。
tt_resurrect_dir() {
  local d
  d="$(tmux show -gqv @resurrect-dir 2>/dev/null || true)"
  if [ -n "$d" ]; then
    # helpers.sh:103 と同一の展開式 ($HOME / $HOSTNAME / ~)。$HOSTNAME を欠くと
    # マルチホスト設定 (@resurrect-dir に $HOSTNAME) で last を取りこぼし、
    # Fix B 退行ガード全体が silent no-op になる (zshlib/_tmux_session.zsh:80 と同式)。
    printf '%s\n' "$d" | sed "s,\$HOME,$HOME,g; s,\$HOSTNAME,$(hostname),g; s,\~,$HOME,g"
  elif [ -d "$HOME/.tmux/resurrect" ]; then
    printf '%s\n' "$HOME/.tmux/resurrect"
  else
    printf '%s\n' "${XDG_DATA_HOME:-$HOME/.local/share}/tmux/resurrect"
  fi
}

# resurrect 保存ファイルの一意セッション数を数える（window 行 field2=session名, TAB 区切り）。
tt_count_sessions_in_file() {
  [ -f "$1" ] || { printf '0\n'; return 0; }
  awk -F'\t' '$1=="window"{print $2}' "$1" 2>/dev/null | sort -u | grep -c . || true
}

# resurrect 保存ファイルの window 総数を数える。セッション数だけの退行判定では
# 「1 セッション × 多 window」運用 (tt の基本形) で window 壊滅を見逃すため併用する。
tt_count_windows_in_file() {
  [ -f "$1" ] || { printf '0\n'; return 0; }
  awk -F'\t' '$1=="window"' "$1" 2>/dev/null | grep -c . || true
}

# 同一秒再保存ガード: last の実体名が「今この秒」のファイル名と一致する間、次の秒まで待つ。
# upstream save.sh は秒精度ファイル名（vendor helpers.sh resurrect_file_path:
# tmux_resurrect_<%Y%m%dT%H%M%S>.txt）で保存し、内容が last と同一なら新ファイルを rm する
# （save.sh save_all: files_differ → else rm）。直前の保存と同一秒に再保存すると、生成パスが
# 「last が指す実体そのもの」になり、`>` の truncate で旧内容を破壊 → cmp が同一ファイル比較で
# 必ず「差分なし」→ rm で last が dangling になる（実体は truncate 済みで復旧不能。次の保存まで
# 復元が silent に不発）。lock 直列化は並行を防ぐが、逐次の同一秒 2 連保存（debounce 完了直後の
# continuum 周期/手動 C-s）は防げないため、lock 保持中のここで秒をずらす。
# 依存（変わったら追従）: upstream のファイル名形式（helpers.sh の RESURRECT_FILE_PREFIX /
# RESURRECT_FILE_EXTENSION / resurrect_file_path の date フォーマット）。
tt_save_avoid_same_second_target() {
  local cur
  cur="$(readlink "$(tt_resurrect_dir)/last" 2>/dev/null || true)"
  if [ -n "$cur" ] && [ "$cur" = "tmux_resurrect_$(date +%Y%m%dT%H%M%S).txt" ]; then
    sleep 1
  fi
}

# Fix C: 退行ガードが last 前進を抑止したことを観測ログに残す（_tmux.conf の startup 観測と同じファイル）。
tt_save_log_guard() {
  { mkdir -p "$HOME/.cache" && printf '%s\tregression-blocked prev_sessions=%s new_sessions=%s prev_windows=%s new_windows=%s kept=%s rejected=%s\n' \
      "$(date +%FT%T)" "$1" "$2" "$3" "$4" "$5" "$6" >> "$HOME/.cache/tt-restore-trigger.log"; } 2>/dev/null || true
}

# lock を bounded-wait で取得し、保持したまま upstream save.sh を foreground 同期実行する本体。
# TT_SAVE_SOURCE_ONLY=1 で source するとここは実行されず、関数だけをテストから直接呼べる
# （tmux_resurrect_debounced_save.sh と同方式。tests/zshrc/tmux-session/test_resurrect_save_lock.sh）。
tt_save_main() {
  if [ ! -x "$REAL_SAVE" ]; then
    return 1
  fi
  mkdir -p "$TT_SAVE_STATE_DIR" 2>/dev/null

  # lock を bounded-wait で取得する。先行保存が進行中なら完了を待ってから自分が保存する
  # （skip を成功扱いにして取りこぼす不変条件破れを避ける。冒頭コメント参照）。
  local acquired='' wait_until observed
  wait_until=$(( $(date +%s) + TT_SAVE_LOCK_WAIT_SECONDS ))
  while :; do
    # owner 行は一度だけ読み、その同じ行で stale 判定と解除対象の同定を行う（二度読みすると
    # 判定後に別プロセスが取得した新 owner を解除対象に拾い、その生存 lock を消す。codex 指摘）。
    observed="$(cat "$TT_SAVE_LOCK_DIR/pid" 2>/dev/null || true)"
    if tt_save_owner_is_stale "$observed"; then
      tt_save_release_lock_if_owner "$observed"
    fi
    if mkdir "$TT_SAVE_LOCK_DIR" 2>/dev/null; then
      # owner 同定用に PID と起動時刻を記録する（tt_save_owner_is_stale 参照）。
      printf '%s %s\n' "$$" "$(tt_save_proc_starttime "$$")" > "$TT_SAVE_LOCK_DIR/pid" 2>/dev/null || true
      acquired=1
      break
    fi
    [ "$(date +%s)" -ge "$wait_until" ] && break
    sleep 0.2
  done

  # 待っても取れない（異常に長い保持）→ 非 0 で返し、呼び出し側に「保存していない」を伝える。
  # これにより debounce の tt_run_resurrect_save は @continuum-save-last-timestamp を進めず、
  # continuum の周期保存を抑止しない（取りこぼしの連鎖を断つ）。
  # NOTE: continuum 経由は save を background 起動した直後に無条件で last-save-timestamp を
  #   進めるため、本 wrapper の非 0 は continuum 側には伝わらない。しかし「lock を保持して
  #   いる先行保存は必ず現在状態を保存する」ため、continuum が今周期見送っても鮮度は保たれる
  #   （= 実害は軽微）。TT_SAVE_LOCK_WAIT_SECONDS は実保存最大時間より十分長く、continuum
  #   interval より十分短く保つこと（既定 15s: 通常保存<1s, continuum 既定 900s の中間）。
  if [ -z "$acquired" ]; then
    return 1
  fi
  trap 'tt_save_release_own_lock' EXIT

  # 復元中なら保存しない（不変条件の choke point 検査。tt_save_restore_in_progress 参照）。
  # lock 待ちの間に復元が始まっていた場合をここで捕まえる。非 0 = 「保存せず」は既存契約どおり
  # 呼び出し側 debounce が正しく扱う（@continuum-save-last-timestamp を進めない）。
  if tt_save_restore_in_progress; then
    return 1
  fi

  # bootstrap 中 (hold セッションのみ) と第 2 サーバからの保存も choke point で弾く。
  # かつて debounce 経路だけのガードで、continuum 周期保存・手動 C-s には効いていなかった
  # (周期保存が hold のみの瞬間に発火すると貧弱状態を保存しうる非対称。レビュー指摘 2026-07-05)。
  if tt_only_hold_sessions; then
    return 1
  fi
  if ! tt_on_default_server; then
    return 1
  fi

  # Fix B: 保存前に現 last のターゲットとセッション数を控える（save.sh が last を前進させる前に）。
  local tt_rdir='' tt_last_link='' tt_prev_target='' tt_prev_n=0 tt_prev_w=0
  # Fix B2: pane_contents.tar.gz の退避先（退行を戻すとき last symlink だけでなく共有 archive も
  # 戻さないと、window は復元されるが大半の pane でスクロールバックが失われる。@resurrect-capture-
  # pane-contents on の主目的が退行保存 1 回で silent に消える）。全喪失退行 (下記 Fix B の
  # 第 3 条件) は prev の規模に関係なく起こりうるため、prev が空でない限り退避する
  # (実測 ~1MB の cp。upstream 自身が毎保存で全 archive を再生成するのに比べ十分軽い)。
  # upstream は `gzip > file` で同一 inode を truncate 上書きするため hardlink 退避は不可＝実コピーする。
  local tt_archive='' tt_archive_bak='' tt_bak_old='' tt_bak_pid=''
  if [ "${TT_SAVE_ALLOW_REGRESSION:-}" != "1" ]; then
    tt_rdir="$(tt_resurrect_dir)"
    tt_last_link="$tt_rdir/last"
    tt_prev_target="$(readlink "$tt_last_link" 2>/dev/null || true)"
    if [ -n "$tt_prev_target" ]; then
      tt_prev_n="$(tt_count_sessions_in_file "$tt_rdir/$tt_prev_target")"
      tt_prev_w="$(tt_count_windows_in_file "$tt_rdir/$tt_prev_target")"
    fi
    tt_archive="$tt_rdir/pane_contents.tar.gz"
    # 異常終了 (kill / crash は EXIT trap が走らない) で残置された過去の退避コピーを掃除する。
    # 生成主 pid が死んでいるものだけ消す (lock 保持中なので進行中の正当な保存は存在しないが、
    # 万一の lock 強制解除経路と競合しないよう kill -0 で保守的に判定する)。
    for tt_bak_old in "$tt_rdir"/.pane_contents.ttguard.*.tar.gz; do
      [ -f "$tt_bak_old" ] || continue
      tt_bak_pid="${tt_bak_old##*.ttguard.}"
      tt_bak_pid="${tt_bak_pid%.tar.gz}"
      case "$tt_bak_pid" in ''|*[!0-9]*) continue ;; esac
      kill -0 "$tt_bak_pid" 2>/dev/null || rm -f "$tt_bak_old" 2>/dev/null
    done
    if [ "${tt_prev_w:-0}" -ge 1 ] && [ -f "$tt_archive" ]; then
      tt_archive_bak="$tt_rdir/.pane_contents.ttguard.$$.tar.gz"
      cp "$tt_archive" "$tt_archive_bak" 2>/dev/null || tt_archive_bak=''
    fi
  fi

  # 直前の保存と同一秒なら次の秒まで待つ（last dangling 化の遮断。関数コメント参照）。
  # TT_SAVE_ALLOW_REGRESSION=1 でも last の健全性は守るべきなので Fix B の外に置く。
  tt_save_avoid_same_second_target

  # lock を保持したまま upstream save.sh を foreground 同期実行する。
  # continuum が本 wrapper を `&` で background 起動しても、wrapper プロセスは
  # save.sh 完了まで生きるため、保存期間中ずっと lock が保持される。
  "$REAL_SAVE" "$@"
  local tt_rc=$?

  # Fix B: 壊滅的な退行なら last を完全状態へ戻す（新ファイルは archive として残す）。
  # 判定は 3 条件 (いずれかに該当で退行扱い):
  #   - セッション数: 旧 last が 4 以上 かつ 新保存がその 1/3 以下（=2/3 以上喪失）
  #   - window 総数:  旧 last が 8 以上 かつ 新保存がその 1/3 以下
  #     (セッション数だけでは「1 セッション × 多 window」運用で window 壊滅を見逃す。
  #      レビュー指摘 2026-07-05。tt の基本形は cwd 名の 1 セッション多 window なので実運用で刺さる)
  #   - 全喪失: 旧 last が空でないのに新保存の window が 0 件。これは上 2 つのしきい値
  #     (prev 4/8 以上) と独立に「常に」退行扱いにする。window 0 件の保存が正当に発生する
  #     経路は存在しない: 全セッションを kill すると exit-empty でサーバごと落ち、保存自体が
  #     走らない。よって 0 件保存は「サーバ終了レース中の dump / list-sessions 失敗」の
  #     artifact と断定できる (実測 2026-07-04: new_sessions=0 の空保存が発生。あのときは
  #     prev=8 セッションでしきい値に救われたが、prev がセッション 4 未満かつ window 8 未満
  #     だと素通りして last が空になり復元不能だった)。
  # 通常の増減（1〜数個 kill）は誤抑止しない保守的しきい値。bypass は TT_SAVE_ALLOW_REGRESSION=1。
  if [ "$tt_rc" -eq 0 ] && [ "${TT_SAVE_ALLOW_REGRESSION:-}" != "1" ] && [ -n "$tt_prev_target" ]; then
    local tt_new_target tt_new_n tt_new_w
    tt_new_target="$(readlink "$tt_last_link" 2>/dev/null || true)"
    if [ -n "$tt_new_target" ] && [ "$tt_new_target" != "$tt_prev_target" ]; then
      tt_new_n="$(tt_count_sessions_in_file "$tt_rdir/$tt_new_target")"
      tt_new_w="$(tt_count_windows_in_file "$tt_rdir/$tt_new_target")"
      if { [ "${tt_prev_n:-0}" -ge 4 ] && [ "$(( tt_new_n * 3 ))" -le "${tt_prev_n:-0}" ]; } || \
         { [ "${tt_prev_w:-0}" -ge 8 ] && [ "$(( tt_new_w * 3 ))" -le "${tt_prev_w:-0}" ]; } || \
         { [ "${tt_prev_w:-0}" -ge 1 ] && [ "$tt_new_w" -eq 0 ]; }; then
        ln -sf "$tt_prev_target" "$tt_last_link"
        # Fix B2: last と一緒に共有 pane_contents.tar.gz も退行前の内容へ戻す。
        # 復元側が本 lock を持たずに読むため、temp へ書いてから mv でアトミックに差し替える。
        if [ -n "$tt_archive_bak" ] && [ -f "$tt_archive_bak" ]; then
          mv -f "$tt_archive_bak" "$tt_archive" 2>/dev/null || true
        fi
        tt_save_log_guard "$tt_prev_n" "$tt_new_n" "$tt_prev_w" "$tt_new_w" "$tt_prev_target" "$tt_new_target"
      fi
    fi
  fi
  # 退行を戻さなかった場合は退避コピーを掃除する（戻した場合は上で mv 済み）。
  [ -n "$tt_archive_bak" ] && [ -f "$tt_archive_bak" ] && rm -f "$tt_archive_bak" 2>/dev/null
  return "$tt_rc"
}

# 直接実行時のみ本体を走らせる。source（テスト）時は関数定義だけ読み込む。
if [ "${TT_SAVE_SOURCE_ONLY:-}" != "1" ]; then
  tt_save_main "$@"
fi
