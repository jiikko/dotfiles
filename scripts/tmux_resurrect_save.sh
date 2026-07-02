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
#     含まない）。よって lock は bounded-wait で待ってから保存する。全 caller は background
#     実行（debounce: run-shell -b / continuum: save.sh quiet & / 手動: run-shell）なので
#     数秒待ってよい。待っても取れない異常時のみ非 0 で返し、呼び出し側に「保存せず」を伝える。
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
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REAL_SAVE="${TT_REAL_SAVE_SCRIPT:-$SCRIPT_DIR/../vendor/tmux-plugins/tmux-resurrect/scripts/save.sh}"

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
# 全 caller が background 実行なので待ってよい。0.2s 間隔でポーリングする。
TT_SAVE_LOCK_WAIT_SECONDS="${TT_SAVE_LOCK_WAIT_SECONDS:-15}"
# 復元中フラグ(@tt-restore-in-progress)の有効期限（秒）。
# tmux_resurrect_debounced_save.sh の TT_RESTORE_INPROGRESS_TTL と同セマンティクス
# （変えるなら両方揃えること。grep: TT_RESTORE_INPROGRESS_TTL）。
TT_RESTORE_INPROGRESS_TTL="${TT_RESTORE_INPROGRESS_TTL:-120}"

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
#   周期 autosave が走り、upstream save.sh:252-253 の無条件 `ln -fs last` が last を貧弱保存へ
#   前進させ、直前の完全状態保存を孤立させる（実害: 7セッション→2セッションで last が上書きされ
#   復元不能になった事例）。restore 不発の根治（tests/tmux/test_tmux.sh の probe leak 修正 +
#   @continuum-restore-max-delay 拡大）に加え、万一 restore が走らなくても完全状態 last を守る
#   最後の砦としてここで壊滅的退行を弾く。bypass: 正当な大量 kill 直後は TT_SAVE_ALLOW_REGRESSION=1。

# 復元中か（「復元中は絶対に保存しない」不変条件の choke point 検査）。
# tmux_resurrect_debounced_save.sh の tt_restore_in_progress と同じ判定式（epoch + TTL。
# あちらのコメント参照。変えるなら両方揃えること。grep: TT_RESTORE_INPROGRESS_TTL）。
# debounce 入口のガードは sleep + wrapper の bounded-wait の間に stale になるし、
# continuum 周期保存・手動 C-s はそもそもガードを通らない。全経路が必ず通る本 wrapper で
# 保存直前に再検査することで、復元途中の部分状態を last に焼き付ける窓を「save.sh 実行時間
# のみ」まで縮める（完全閉鎖には restore 側の lock 参加が必要だが、変更規模に対して残存窓が
# 小さいため defense-in-depth のここまでとする）。
tt_save_restore_in_progress() {
  local v now
  v="$(tmux show -gqv @tt-restore-in-progress 2>/dev/null)"
  case "$v" in
    ''|0)     return 1 ;;
    *[!0-9]*) return 1 ;;
  esac
  now="$(date +%s)"
  [ "$(( now - v ))" -lt "$TT_RESTORE_INPROGRESS_TTL" ]
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

# Fix C: 退行ガードが last 前進を抑止したことを観測ログに残す（_tmux.conf の startup 観測と同じファイル）。
tt_save_log_guard() {
  { mkdir -p "$HOME/.cache" && printf '%s\tregression-blocked prev_sessions=%s new_sessions=%s kept=%s rejected=%s\n' \
      "$(date +%FT%T)" "$1" "$2" "$3" "$4" >> "$HOME/.cache/tt-restore-trigger.log"; } 2>/dev/null || true
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

  # Fix B: 保存前に現 last のターゲットとセッション数を控える（save.sh が last を前進させる前に）。
  local tt_rdir='' tt_last_link='' tt_prev_target='' tt_prev_n=0
  # Fix B2: pane_contents.tar.gz の退避先（退行を戻すとき last symlink だけでなく共有 archive も
  # 戻さないと、window は復元されるが大半の pane でスクロールバックが失われる。@resurrect-capture-
  # pane-contents on の主目的が退行保存 1 回で silent に消える）。退行が起こりうる prev_n>=4 の
  # ときだけ退避してコストを限定する。upstream は `gzip > file` で同一 inode を truncate 上書き
  # するため hardlink 退避は不可＝実コピーする。
  local tt_archive='' tt_archive_bak=''
  if [ "${TT_SAVE_ALLOW_REGRESSION:-}" != "1" ]; then
    tt_rdir="$(tt_resurrect_dir)"
    tt_last_link="$tt_rdir/last"
    tt_prev_target="$(readlink "$tt_last_link" 2>/dev/null || true)"
    [ -n "$tt_prev_target" ] && tt_prev_n="$(tt_count_sessions_in_file "$tt_rdir/$tt_prev_target")"
    tt_archive="$tt_rdir/pane_contents.tar.gz"
    if [ "${tt_prev_n:-0}" -ge 4 ] && [ -f "$tt_archive" ]; then
      tt_archive_bak="$tt_rdir/.pane_contents.ttguard.$$.tar.gz"
      cp "$tt_archive" "$tt_archive_bak" 2>/dev/null || tt_archive_bak=''
    fi
  fi

  # lock を保持したまま upstream save.sh を foreground 同期実行する。
  # continuum が本 wrapper を `&` で background 起動しても、wrapper プロセスは
  # save.sh 完了まで生きるため、保存期間中ずっと lock が保持される。
  "$REAL_SAVE" "$@"
  local tt_rc=$?

  # Fix B: 壊滅的なセッション数退行なら last を完全状態へ戻す（新ファイルは archive として残す）。
  # 旧 last が 4 セッション以上あり、新保存がその 1/3 以下（=2/3 以上喪失）のときだけ弾く。
  # 通常のセッション増減（1〜数個 kill）は誤抑止しない保守的しきい値。bypass は TT_SAVE_ALLOW_REGRESSION=1。
  if [ "$tt_rc" -eq 0 ] && [ "${TT_SAVE_ALLOW_REGRESSION:-}" != "1" ] && [ -n "$tt_prev_target" ]; then
    local tt_new_target tt_new_n
    tt_new_target="$(readlink "$tt_last_link" 2>/dev/null || true)"
    if [ -n "$tt_new_target" ] && [ "$tt_new_target" != "$tt_prev_target" ]; then
      tt_new_n="$(tt_count_sessions_in_file "$tt_rdir/$tt_new_target")"
      if [ "${tt_prev_n:-0}" -ge 4 ] && [ "$(( tt_new_n * 3 ))" -le "${tt_prev_n:-0}" ]; then
        ln -sf "$tt_prev_target" "$tt_last_link"
        # Fix B2: last と一緒に共有 pane_contents.tar.gz も退行前の内容へ戻す。
        # 復元側が本 lock を持たずに読むため、temp へ書いてから mv でアトミックに差し替える。
        if [ -n "$tt_archive_bak" ] && [ -f "$tt_archive_bak" ]; then
          mv -f "$tt_archive_bak" "$tt_archive" 2>/dev/null || true
        fi
        tt_save_log_guard "$tt_prev_n" "$tt_new_n" "$tt_prev_target" "$tt_new_target"
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
