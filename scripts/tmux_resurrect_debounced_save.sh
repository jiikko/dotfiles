#!/usr/bin/env bash
# tmux の window 作成/削除を契機に tmux-resurrect の保存を debounce して走らせる。
#
# 背景:
#   tmux-continuum の周期 autosave（既定 15 分）だけだと、最後の保存以降に
#   増やした window は OS 再起動で失われる（snapshot staleness）。クラッシュ /
#   強制再起動 / 電源断では save-on-shutdown が無いため、最大 15 分ぶんの作業
#   （window 構成）が丸ごと失われ得る。
#   そこで window-linked / window-unlinked hook からこのスクリプトを呼び、
#   「最後のイベントから DEBOUNCE 秒後に一度だけ」resurrect 保存を走らせて
#   損失窓を秒オーダーに縮める。
#
# 不変条件（Invariant）— ここが本スクリプトの肝。崩すと last を破壊する:
#   1. 復元中（@tt-restore-in-progress=1）は絶対に保存しない。
#      復元は new-window/move-window で大量の window-linked を発火させるため、
#      ガード無しだと「部分復元状態」を last に焼き付け、次回復元が壊れる。
#      フラグは _tmux.conf の @resurrect-hook-pre/post-restore-all が立て/降ろす。
#   2. bootstrap 用の hold セッション（_tmux_session.zsh の _tt_impl が作る
#      __tt_hold_* 。下記「依存」参照）「だけ」が存在する間は保存しない。
#      fresh boot 直後は「空の hold だけ」の状態であり、ここで保存すると
#      復元前に last（唯一の良いスナップショット）を空状態で上書きしてしまう。
#      ただし抑止するのは「hold が唯一のセッション」のときだけにする。実セッションが
#      1 つでも存在すれば（= 復元が進んで実体が出来た / 平常運用）保存は安全であり、
#      抑止しない。これにより _tt_impl が rc=1/2（復元 timeout・フック未設定）で hold を
#      畳まず残置しても、実セッションがある限り保存が永久停止しない。
#      復元の「最中」（hold + 部分的な実セッション）は不変条件 1（@tt-restore-in-progress）が
#      抑止する（pre-restore-all がセッション生成より前にフラグを立てるため）。
#   3. 保存の直列化（同時に複数の save.sh を走らせない）は本スクリプトでは行わず、
#      全保存経路の choke point である wrapper（scripts/tmux_resurrect_save.sh、
#      @resurrect-save-script-path 経由）の bounded-wait lock に委ねる。
#      かつて自前 mkdir lock を持っていたが、lock 競合時に「skip して成功扱い」で
#      イベントを取りこぼし損失窓が最大 15 分に退行するバグがあった（wrapper 冒頭
#      コメントが明文で禁じたのと同一パターン）。撤去により、先行保存と重なった
#      後発 debouncer は wrapper 内で完了を待ってから最新状態を保存する（取りこぼし無し）。
#   4. イベント連打で保存を多発させない（debounce token: 自分が最後の
#      イベントでなければ何もしない。最後の 1 つだけが実際に保存する）。
#   5. default socket のサーバ以外からは保存しない（単一環境 gate）。conf は tmux -L 等の
#      第 2 サーバでも source され本 hook が付くが、resurrect の保存先は HOME 共有のため、
#      無ガードだと第 2 サーバの状態が main サーバの last を上書きする（continuum は
#      another_tmux_server_running で同じ理由の gate を持つ。自前 hook にも同じ判断を写す）。
#
# 依存（変わったら追従が必要）:
#   - hold セッション名のプレフィックス __tt_hold_ は zshlib/_tmux_session.zsh の
#     _tt_impl が決める。あちらの命名を変えたら本スクリプトの TT_HOLD_PREFIX も
#     合わせること（grep: __tt_hold_）。
#   - @tt-restore-in-progress / @tt-restore-complete フラグは _tmux.conf の
#     @resurrect-hook-pre-restore-all / @resurrect-hook-post-restore-all が制御する。
#   - 実際の保存は resurrect 本体の save.sh（option @resurrect-save-script-path）。
#
# テスト容易性:
#   tmux / sleep を直接呼ぶことで、tests/zshrc/tmux-session/test_debounced_save.sh
#   からスタブ注入して分岐ロジックを検証できる（tt の unit テストと同方式）。
set -uo pipefail
unset CDPATH

# --- 設定（環境変数で上書き可能。テストはここを差し替える）---------------------
TT_DEBOUNCE_STATE_DIR="${TT_DEBOUNCE_STATE_DIR:-$HOME/.cache/tt-resurrect-debounce}"
TT_DEBOUNCE_TOKEN_FILE="$TT_DEBOUNCE_STATE_DIR/token"
# hold セッション名プレフィックス（_tmux_session.zsh と一致させる。上記「依存」参照）
TT_HOLD_PREFIX="${TT_HOLD_PREFIX:-__tt_hold_}"
# 復元中フラグ(@tt-restore-in-progress)の有効期限（秒）。pre-restore-all が立てた
# フラグを post-restore-all が降ろし損ねた（復元途中のクラッシュ / kill / server 停止）
# 場合、フラグが永久に残ると debounce 保存が二度と走らなくなる。TTL を超えた
# フラグは「降り損ね」とみなして無効化し、保存を再開させる（_tt_wait_for_restore の
# タイムアウトと同じ degrade 思想）。実復元は数秒〜十数秒なので 120s で十分。
TT_RESTORE_INPROGRESS_TTL="${TT_RESTORE_INPROGRESS_TTL:-120}"

# debounce 秒数。@tt-debounce-save-seconds で上書き、無ければ既定 10 秒。
tt_debounce_seconds() {
  local v
  v="$(tmux show -gqv @tt-debounce-save-seconds 2>/dev/null)"
  case "$v" in
    ''|*[!0-9]*) echo 10 ;;   # 未設定 / 非数値 → 既定
    *)           echo "$v" ;;
  esac
}

# 復元中か（不変条件 1）。@tt-restore-in-progress には pre-restore-all が復元開始の
# epoch(date +%s)を格納し、post-restore-all が 0 に戻す。TTL 内の epoch のときだけ
# 「復元中」とみなす（降り損ねた古いフラグは無視して保存を再開＝不変条件の TTL）。
tt_restore_in_progress() {
  local v now
  v="$(tmux show -gqv @tt-restore-in-progress 2>/dev/null)"
  case "$v" in
    ''|0)     return 1 ;;   # 未設定 / クリア済み → 復元中でない
    *[!0-9]*) return 1 ;;   # 非数値（不正値）→ 安全側で復元中でない扱い
  esac
  now="$(date +%s)"
  [ "$(( now - v ))" -lt "$TT_RESTORE_INPROGRESS_TTL" ]
}

# bootstrap 状態か（hold セッション「だけ」が存在し、実セッションが 1 つも無い）（不変条件 2）。
# 「hold が 1 つでもある」ではなく「hold 以外が 1 つも無い」を見るのが肝。
# 実セッションが存在すれば保存は安全なので抑止しない（rc=1/2 の hold 残置で永久抑止しない）。
tt_only_hold_sessions() {
  local sessions
  sessions="$(tmux list-sessions -F '#{session_name}' 2>/dev/null)"
  # hold 以外（実セッション）が 1 行でもあれば bootstrap ではない → 抑止しない
  if printf '%s\n' "$sessions" | grep -qv "^${TT_HOLD_PREFIX}"; then
    return 1
  fi
  # ここに来るのは「全行が hold」or「セッション皆無」。hold が最低 1 つあるときだけ bootstrap。
  printf '%s\n' "$sessions" | grep -q "^${TT_HOLD_PREFIX}"
}

# default socket のサーバか（不変条件 5・単一環境 gate）。
# 期待値は「継承した TMUX_TMPDIR」ではなく canonical な /tmp 基準で組む: 本 hook の
# run-shell 子プロセスは第 2 サーバの TMUX_TMPDIR を継承するため、それで期待値を組むと
# 比較が自己正当化して素通りする（過去事故の scratch 第 2 サーバがまさにこの形態だった）。
# /tmp 決め打ちは正規の TMUX_TMPDIR 利用者には tmux の文書化挙動から逸れるが、この環境は
# scratch popup (bind t) が TMUX_TMPDIR を明示 unset して実 default socket を強制する方針
# (_tmux.conf / scripts/tmux_scratch_popup.sh) なので整合する。
# macOS の /tmp は /private/tmp への symlink で、tmux の #{socket_path} は解決済みパスを
# 返す（実測: /private/tmp/tmux-501/default）ため、期待値側も realpath で解決して比較する。
# socket_path が取れない環境（古い tmux / テストスタブ）は fail-open で保存を殺さない。
tt_on_default_server() {
  local actual expected
  actual="$(tmux display-message -p '#{socket_path}' 2>/dev/null)"
  [ -n "$actual" ] || return 0
  expected="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"
  [ "$actual" = "$expected" ]
}

# 保存してよい状態か（復元中でも bootstrap(hold のみ) 中でもなく、default サーバである）
tt_should_save() {
  if tt_restore_in_progress; then
    return 1
  fi
  if tt_only_hold_sessions; then
    return 1
  fi
  if ! tt_on_default_server; then
    return 1
  fi
  return 0
}

# resurrect 本体の保存スクリプトを quiet で走らせる。
# 保存成功後に continuum の最終保存時刻を「今」に進める。これにより continuum の
# 周期 autosave（enough_time_since_last_run_passed）が @continuum-save-interval 分
# gate され、「同一秒に save.sh が二重起動して last/archive を競合更新する」稀な
# レースを実質排除する。本フックで頻繁に保存している間 continuum は idle 時のみ保存。
#
# 依存（変わったら再評価）: continuum の最終保存時刻 option 名
# @continuum-save-last-timestamp（vendor/.../tmux-continuum/scripts/variables.sh の
# last_auto_save_option）と epoch(date +%s)形式（shared.sh current_timestamp）。
# この前提が崩れても last は壊さない（save.sh は世代別ファイル + 直近5世代を保持する
# ため、万一の同秒衝突でも過去スナップショットから復旧可能）のが許容の根拠。
tt_run_resurrect_save() {
  local save_script
  save_script="$(tmux show -gqv @resurrect-save-script-path 2>/dev/null)"
  if [ -z "$save_script" ] || [ ! -x "$save_script" ]; then
    return 1
  fi
  "$save_script" quiet >/dev/null 2>&1 || return 1
  tmux set-option -g @continuum-save-last-timestamp "$(date +%s)" 2>/dev/null || true
}

# debounce 用のユニークトークンを生成（同一イベントバーストでも衝突しない値）
tt_make_token() {
  echo "$(date +%s%N)-$$-${RANDOM}"
}

# 本体: token を立てて DEBOUNCE 秒待ち、自分が最後なら（かつ保存可なら）保存する
tt_debounced_save_main() {
  mkdir -p "$TT_DEBOUNCE_STATE_DIR" 2>/dev/null

  local token tmp
  token="$(tt_make_token)"
  # 原子的に最新トークンを書き込む（同一 fs で mv は atomic、last writer wins）
  tmp="$TT_DEBOUNCE_TOKEN_FILE.tmp.$$"
  printf '%s\n' "$token" > "$tmp" 2>/dev/null || return 0
  mv -f "$tmp" "$TT_DEBOUNCE_TOKEN_FILE" 2>/dev/null || { rm -f "$tmp"; return 0; }

  sleep "$(tt_debounce_seconds)"

  # 自分より後にイベントが来ていたら、そちらに保存を譲って何もしない
  if [ "$(cat "$TT_DEBOUNCE_TOKEN_FILE" 2>/dev/null)" != "$token" ]; then
    return 0
  fi

  # 復元中 / bootstrap 中 / 非 default サーバは保存しない（last を壊さない）
  if ! tt_should_save; then
    return 0
  fi

  # 直列化は wrapper (@resurrect-save-script-path = tmux_resurrect_save.sh) の
  # bounded-wait lock が担う。先行保存と重なってもここで skip せず wrapper 内で
  # 完了を待つ（不変条件 3 のコメント参照。skip すると自イベントを取りこぼす）。
  tt_run_resurrect_save
}

# source 時（テスト）は main を実行しない。直接実行時のみ走らせる。
if [ "${TT_DEBOUNCE_SOURCE_ONLY:-}" != "1" ]; then
  tt_debounced_save_main
fi
