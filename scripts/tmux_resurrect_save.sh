#!/usr/bin/env bash
# tmux-resurrect の保存を「全経路で単一 lock 直列化」する共通 wrapper。
# @resurrect-save-script-path をこのスクリプトに向けて使う（_tmux.conf 参照）。
#
# 背景（なぜ必要か）:
#   保存経路は 3 系統あり、それぞれ別々の（または無い）lock で動いていた:
#     1. window 増減 → scripts/tmux_resurrect_debounced_save.sh（自前 mkdir lock。
#        ただし debounce 同士しか守らない）
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
# NOTE: 保存期間 lock のみを担う薄い shim。@continuum-save-last-timestamp の更新等は
#   呼び出し側（debounce スクリプト / continuum）の責務のまま変えない。
set -uo pipefail
unset CDPATH

# 本 wrapper はリポジトリの scripts/ 配下にある前提。upstream save.sh は ../vendor/...。
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REAL_SAVE="${TT_REAL_SAVE_SCRIPT:-$SCRIPT_DIR/../vendor/tmux-plugins/tmux-resurrect/scripts/save.sh}"

TT_SAVE_STATE_DIR="${TT_SAVE_STATE_DIR:-$HOME/.cache/tt-resurrect-save}"
TT_SAVE_LOCK_DIR="$TT_SAVE_STATE_DIR/lock"
# lock の stale 判定（秒）。これより古い lock はクラッシュ取り残しとみなして解除。
# 実保存は数秒〜十数秒なので 120s で十分。
TT_SAVE_LOCK_STALE_SECONDS="${TT_SAVE_LOCK_STALE_SECONDS:-120}"
# lock を取れないとき待つ上限（秒）。先行保存（数秒〜十数秒）の完了を待ってから保存する。
# 全 caller が background 実行なので待ってよい。0.2s 間隔でポーリングする。
TT_SAVE_LOCK_WAIT_SECONDS="${TT_SAVE_LOCK_WAIT_SECONDS:-15}"

if [ ! -x "$REAL_SAVE" ]; then
  exit 1
fi

mkdir -p "$TT_SAVE_STATE_DIR" 2>/dev/null

tt_save_release_lock() {
  rm -f "$TT_SAVE_LOCK_DIR/pid" 2>/dev/null || true
  rmdir "$TT_SAVE_LOCK_DIR" 2>/dev/null || true
}

# lock が「取り残し（owner プロセス死亡）」かを判定する。
# mtime のみで stale 判定すると、pane contents が巨大等で実保存が stale TTL を超えた場合に
# 「進行中の正当な lock」を取り残しと誤判定して奪い、再び並行 save.sh の競合を招く（codex 指摘）。
# そこで owner PID の生存確認を主とし、PID 不明時のみ mtime を保険に使う。
tt_save_lock_is_stale() {
  [ -d "$TT_SAVE_LOCK_DIR" ] || return 1
  local owner
  owner="$(cat "$TT_SAVE_LOCK_DIR/pid" 2>/dev/null)"
  if [ -n "$owner" ]; then
    # owner 生存中 = 保存進行中（待つ）。死亡 = クラッシュ取り残し（解除可）。
    # PID 再利用で別プロセスが生きている誤検知は「待つ」側（安全）に倒れるだけ。
    if kill -0 "$owner" 2>/dev/null; then return 1; else return 0; fi
  fi
  # PID 不明（mkdir 直後で pid 書き込み前の競合 or 旧形式）→ mtime が stale TTL より古ければ取り残し。
  [ -z "$(find "$TT_SAVE_LOCK_DIR" -maxdepth 0 -mmin "-$(( TT_SAVE_LOCK_STALE_SECONDS / 60 + 1 ))" 2>/dev/null)" ]
}

# lock を bounded-wait で取得する。先行保存が進行中なら完了を待ってから自分が保存する
# （skip を成功扱いにして取りこぼす不変条件破れを避ける。冒頭コメント参照）。
acquired=
wait_until=$(( $(date +%s) + TT_SAVE_LOCK_WAIT_SECONDS ))
while :; do
  if tt_save_lock_is_stale; then
    tt_save_release_lock
  fi
  if mkdir "$TT_SAVE_LOCK_DIR" 2>/dev/null; then
    printf '%s\n' "$$" > "$TT_SAVE_LOCK_DIR/pid" 2>/dev/null || true
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
  exit 1
fi
trap 'tt_save_release_lock' EXIT

# lock を保持したまま upstream save.sh を foreground 同期実行する。
# continuum が本 wrapper を `&` で background 起動しても、wrapper プロセスは
# save.sh 完了まで生きるため、保存期間中ずっと lock が保持される。
"$REAL_SAVE" "$@"
