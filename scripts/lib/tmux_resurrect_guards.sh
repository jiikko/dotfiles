# shellcheck shell=bash
# tmux-resurrect 保存ガードの共有ライブラリ (source して使う。実行ファイルではないので shebang なし)。
# 利用者: scripts/tmux_resurrect_debounced_save.sh (イベント駆動 debounce 経路) /
#         scripts/tmux_resurrect_save.sh (全保存経路の choke point wrapper)。
# かつて両スクリプトに同じ判定式と TTL が二重定義され「変えるなら両方揃えること」
# 運用だったのを、ここに一本化した (2026-07-05)。
#
# 前提: 呼び出し元は bash。tmux コマンドが PATH にあること (テストはスタブで差し替える)。

# hold セッション (bootstrap 中の一時セッション) の名前接頭辞。
# zshlib/_tmux_session.zsh の bootstrap と一致させること。
TT_HOLD_PREFIX="${TT_HOLD_PREFIX:-__tt_hold_}"

# 復元中フラグ (@tt-restore-in-progress) の有効期限 (秒)。pre-restore-all が立てた
# フラグを post-restore-all が降ろし損ねた (復元途中のクラッシュ / kill / server 停止)
# 場合、フラグが永久に残ると保存が二度と走らなくなる。TTL を超えたフラグは
# 「降り損ね」とみなして無効化し、保存を再開させる。実復元は数秒〜十数秒なので 120s で十分。
TT_RESTORE_INPROGRESS_TTL="${TT_RESTORE_INPROGRESS_TTL:-120}"

# 復元中か。@tt-restore-in-progress には pre-restore-all が復元開始の epoch(date +%s) を
# 格納し、post-restore-all が 0 に戻す。TTL 内の epoch のときだけ「復元中」とみなす。
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

# bootstrap 状態か（hold セッション「だけ」が存在し、実セッションが 1 つも無い）。
# 「hold が 1 つでもある」ではなく「hold 以外が 1 つも無い」を見るのが肝。
# 実セッションが存在すれば保存は安全なので抑止しない（rc=1/2 の hold 残置で永久抑止しない）。
tt_only_hold_sessions() {
  local sessions
  sessions="$(tmux list-sessions -F '#{session_name}' 2>/dev/null)"
  # hold 以外（実セッション）が 1 行でもあれば bootstrap ではない → 抑止しない。
  # セッション皆無（list-sessions 失敗/空）のときも here-string の空行がここにマッチし
  # 「実セッションあり」と同じ経路で return 1 する（= 抑止しない。意図どおり）。
  #
  # ⚠️ ここを `printf … | grep -q` のパイプに戻さないこと（here-string 必須）。この lib は
  #   pipefail 下で source される（tmux_resurrect_save.sh:49 の set -uo pipefail）。パイプにすると
  #   grep -q が非マッチ行を見つけた瞬間に exit してパイプを閉じ、まだ書いている printf が
  #   SIGPIPE(141) で死ぬ。pipefail がその 141 を拾ってパイプライン全体を偽の非 0 にするため、
  #   この early-return が高負荷時に稀に素通りし、実セッションがあるのに「only-hold」と誤判定して
  #   保存を抑止する（CI flake 2026-07-11 / 2026-07-15 run 29382449580 で観測・根治）。
  #   here-string は単一コマンドで pipefail の対象外なので SIGPIPE レースの影響を受けない。
  if grep -qv "^${TT_HOLD_PREFIX}" <<<"$sessions"; then
    return 1
  fi
  # ここに来るのは「全行が hold」のときだけ。防御的に hold の存在を確認して bootstrap 判定。
  grep -q "^${TT_HOLD_PREFIX}" <<<"$sessions"
}

# default socket のサーバか（単一環境 gate）。
# 期待値は「継承した TMUX_TMPDIR」ではなく canonical な /tmp 基準で組む: hook の
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
