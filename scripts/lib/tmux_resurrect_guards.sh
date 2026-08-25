# shellcheck shell=bash
# tmux-resurrect 保存ガードの共有ライブラリ (source して使う。実行ファイルではないので shebang なし)。
# 利用者: resurrect 系スクリプト全般 (保存経路の choke point / 観測ログの書き手など。
#         `grep -rl tmux_resurrect_guards.sh scripts/` が実際の一覧)。
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

# ---- プロセス同一性 / lock owner 判定 (常駐プロセスの lock で共有) --------------------
# pid だけの生存判定は pid 再利用で誤る。「記録時と同一のプロセスか」を起動時刻で照合する。
# 実害の実証 (2026-07-30 監査): watchdog の残骸 lock の watcher pid が別プロセスに再利用されると、
# 新世代の watchdog が「先任が生きている」と誤認して無音で退き、**watchdog が 1 つも張られない**
# (サーバが死んでも死亡記録が残らない = 観測装置が丸ごと不発)。同型の穴が periodic_save の
# ループ脱出条件と restore_runner の単一実行 lock にもある。3 箇所で同じ判定が要るのでここに集約する。
# プロセスの起動時刻を「空白を含まない単一トークン」に正規化して返す (存在しなければ空)。
# PID だけでは再起動跨ぎ / PID 再利用で別プロセスを同一 owner と誤認するため、指紋として併用する。
# `ps -o lstart=` は BSD(macOS)/GNU(Linux) 双方で同一プロセスに安定な文字列を返す。
#
# ⚠️ **空白を潰すのは必須**。owner 行を `read -r pid start` のように空白分割で読む書き手が
#   いるため、生の `ps` 出力 (例 "火  8/25 17:09:37 2026") を入れると先頭語だけが記録され、
#   比較が壊れる。ここが**指紋の唯一の出典**で、`scripts/tmux_resurrect_save.sh` にあった
#   独立実装 (同じ処理を別書式で持っていた) は削除済み (issue 078。二重化は issue 068 の
#   drift の直接原因だった)。
# 起動時刻トークンの正規化: 空白を `_` に潰し、**前後の `_` を落とす**。
# ⚠️ 前後を落とすのは必須。`ps -o lstart=` の末尾パディングは platform 依存で、macOS は
#   空白で埋めるが Linux は埋めない。落とさないと同じプロセスの指紋が OS によって別物になり、
#   下の移行ガード (記録側の正規化) が Linux でだけ破れて**生存 owner を奪う**。
#   実測 2026-08-25: macOS 手元は緑・Linux CI だけ赤、で発覚した。
tt_norm_fp() {
  local s
  s="$(printf '%s' "$1" | tr -s '[:space:]' '_')"
  s="${s#_}"; s="${s%_}"
  printf '%s' "$s"
}
tt_proc_starttime() { tt_norm_fp "$(ps -o lstart= -p "$1" 2>/dev/null)"; }

# ファイルの mtime (epoch)。取れなければ空を返す。
# ⚠️ GNU を先に試すこと。`stat -f` は macOS では「書式指定」だが GNU ではファイルシステム情報の
# 表示で、Linux では成功して別物 (mount point 等) を返すため `||` のフォールバックが発動しない。
# GNU に無い `-c` を先に試せば macOS では invalid option で失敗して `-f` へ落ちる。
# (実測 2026-07: Ubuntu 24.04 で `[: File: ... integer expression expected` で死んだ)
tt_mtime_of() { stat -c '%Y' "$1" 2>/dev/null || stat -f '%m' "$1" 2>/dev/null; }

# $1=pid $2=記録した起動時刻 (空なら pid 生存のみで判定)
tt_same_proc() {
  local pid="$1" want="${2:-}" cur
  [ -n "$pid" ] || return 1
  case "$pid" in *[!0-9]*) return 1 ;; esac
  kill -0 "$pid" 2>/dev/null || return 1
  [ -n "$want" ] || return 0
  cur="$(tt_proc_starttime "$pid")"
  # 起動時刻が取れない環境 (ps 制限等) は pid 生存のみで判定する (fail-open)
  [ -n "$cur" ] || return 0
  # ⚠️ 記録側も同じ正規化を通してから比較する。指紋の書式を変えた移行期に、旧書式で書かれた
  #   lock を「別プロセス」と誤判定して**生存 owner を奪う**のを防ぐ (issue 078)。
  want="$(tt_norm_fp "$want")"
  [ "$cur" = "$want" ]
}

# lock ディレクトリに owner を記録する ($1=lock dir, $2=pid。省略時は自分)。
# 形式: "<pid>\t<lstart>"。旧形式 (pid のみ) も読み手が受け付ける。
# ⚠️ $2 はテストが「他プロセスを owner にした lock」を production と同じ書式で作るための seam。
# 書式をテスト側へ写すと書式変更に追従できず、実物とずれた fixture で常に緑になる
# (実例 2026-08-20: 読み手が cat|kill -0 で書式を無視しており誤報していたのに気づけなかった)
tt_lock_write_owner() {
  local dir="$1" pid="${2:-$$}"
  # ⚠️ `{ ...; } 2>/dev/null` で括ること。`printf ... > "$dir/pid" 2>/dev/null` の形だと、
  #   **リダイレクト先を open できない失敗はシェル自身が報告する**ので抑止できない
  #   (lock dir が競合で消えている間に stderr が漏れる。実測 2026-08-25)。
  { printf '%s\t%s\n' "$pid" "$(tt_proc_starttime "$pid")" > "$dir/pid"; } 2>/dev/null || true
}

# lock の owner が「記録時と同一のプロセスとして」生きているか ($1=lock dir)
tt_lock_owner_alive() {
  local line pid start
  line="$(cat "$1/pid" 2>/dev/null)" || return 1
  [ -n "$line" ] || return 1
  pid="${line%%	*}"
  start="${line#*	}"
  [ "$start" = "$line" ] && start=''   # 旧形式 (pid のみ) は pid 生存のみで判定
  tt_same_proc "$pid" "$start"
}

# resurrect の保存先 dir を解決する。vendor helpers.sh:1-7,99-103 と同手順（source 副作用を避け
# 自己完結）。解決順 @resurrect-dir → ~/.tmux/resurrect → $XDG_DATA_HOME/tmux/resurrect。
# helpers.sh の解決順を変えたらここも追従すること。
# 利用者: tmux_resurrect_save.sh (Fix B 退行ガード) / tmux_snapshot_health.sh (鮮度判定)。
# 2026-07-30 に save wrapper からここへ移動（健全性チェックが同じ解決を必要とし、二重定義に
# なると片方の更新漏れで last を取りこぼす。この lib が集約点である理由そのもの）。
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

# ── lock の取得 ──────────────────────────────────────────────────────────────
# 死んだサーバ/watcher の stale lock を掃除する (`<dir>/<pid>.lock` の形を前提)。
# ⚠️ owner の判定を pid だけで行わないこと。pid 再利用で「先任が生きている」と誤認すると、
#   新世代が無音で退いて **装置が 1 つも張られない** (サーバが死んでも死亡記録が残らない。
#   2026-07-30 の監査で実証済み。実害は「二重起動」ではなく「装置不在」側に倒れる)。
# ⚠️ この掃除は **lock 名に pid を持つ経路専用**。単一 lock (`<dir>/lock`) の経路
#   (tmux_restore_runner.sh) では呼ばないこと。掃除を後付けすると「今まで掃除しなかった
#   経路が掃除を始める」= 挙動変更で、誤奪の新しい窓を開ける (issue 078 で意図的に分けた)。
tt_lock_sweep_stale() {
  local base="$1" d spid
  for d in "$base"/*.lock; do
    [ -d "$d" ] || continue
    spid="$(basename "$d" .lock)"
    # 掃除するのは **サーバも owner も死んでいる**ときだけ (どちらか生きていれば残す)
    if ! kill -0 "$spid" 2>/dev/null && ! tt_lock_owner_alive "$d"; then
      rm -rf "$d" 2>/dev/null
    fi
  done
  # ⚠️ 掃除は best-effort。必ず 0 を返すこと。ループ末尾の `rm -rf` の rc がそのまま漏れると、
  #   呼び出し側に `set -e` が入った瞬間に「掃除に失敗したら装置を張らずに死ぬ」へ化ける
  #   (掃除できない = 装置を張らない理由にはならない)。
  return 0
}

# lock ディレクトリを取り、成功したら owner (pid + 起動時刻) を記録する。
#   戻り値: 0 = 取得した / 1 = 先任が同一プロセスとして生きている / 2 = 取得に失敗した
#
# ⚠️ **政策は呼び出し側に残す**。「先任が生きていたら何をログに出してどう終わるか」は経路ごとに
#   違う (periodic_save / watchdog は無音で exit 0、restore_runner は理由を観測ログへ残す)。
#   ここが返すのは「取れたか / なぜ取れなかったか」だけで、判断は呼び出し側が持つ。
#   同じ理由で `tmux_resurrect_save.sh` の stale 判定 (mtime hard TTL backstop を持つ) は
#   この関数へ寄せていない — 政策が違うものを 1 つにすると保存停止か誤奪を作る (issue 078)。
# ⚠️ 戻り値を読む側は `rc=0; tt_lock_acquire "$d" || rc=$?` の形にすること。素の `rc=$?` は
#   後から `set -e` が入った瞬間に「rc を読む前にスクリプトごと死ぬ」へ変わる (無音で機能停止)。
tt_lock_acquire() {
  local dir="$1"
  if ! mkdir "$dir" 2>/dev/null; then
    tt_lock_owner_alive "$dir" && return 1
    # owner 不在 = 前回の取り残し。奪って続行する
    # ⚠️ 既知のレース (issue 103): 「owner の生存確認 → 奪う」が非アトミックなので、取り残しの
    #   lock を 2 プロセスが 1〜5ms 差で掴むと**両方が取得に成功する**。実測で restore.sh が
    #   40/40 二重実行された。これは統合前 (3 本にコピーされていた頃) から同じ穴で、統合で
    #   新たに開いたものではない。閉じるには acquire/release を対で作り替える必要があり、
    #   単純な `mv` への差し替えでは窓が残る (issue 103 に反証つきで記録)。
    rm -rf "$dir" 2>/dev/null
    mkdir "$dir" 2>/dev/null || return 2
  fi
  tt_lock_write_owner "$dir"
  return 0
}

# ── 共有観測ログ (tt-restore-trigger.log) ────────────────────────────────────
# 復元・保存・kill の因果を 1 本の時系列で読むための共有ログ。**書き手はこの関数だけ**にする。
#
# ⚠️ 直接 `>> "$HOME/.cache/tt-restore-trigger.log"` と書かないこと。seam
#   (`TT_TRIGGER_LOG`) を迂回した経路はテストから観測できず、テストが緑でも実際には
#   書けていない/別の場所に書いている状態を作れてしまう (issue 079。実際に
#   tmux_resurrect_save.sh と tmux_reap_orphan_servers.sh が迂回していた)。
# ⚠️ 行書式は「ISO8601 <TAB> 本文」。読み手 (tmux_server_watchdog.sh の verdict 判定など) が
#   この形に依存しているので、変えるならこの 1 箇所を変えて読み手も同時に直す。
#
# rotation はここでは**やらない**。上限 (TT_TRIGGER_LOG_MAX_LINES) の適用は
# scripts/tmux_periodic_save.sh の prune_trigger_log が担当する。
#   理由: 書き込みのたびに刈ると 1 行ごとに `wc -l` の fork が乗る。periodic_save は元々
#   周期実行されていて追加 fork ゼロで刈れる。増加は実測 96 行/日 ≒ 8KB/日で、上限は
#   forensics の保持期間を決めるものであって安全機構ではないため、periodic_save が
#   止まっている間に上限を超えても実害は無い (ディスクを食う速度が 8KB/日)。
#   ⚠️ この「暗黙の依存」を明示にするのがこのコメントの役目。prune を別の場所へ移すなら
#   ここも直すこと。
tt_trigger_log() {
  { mkdir -p "$(dirname "$TT_TRIGGER_LOG")" \
      && printf '%s\t%s\n' "$(date +%FT%T)" "$1" >> "$TT_TRIGGER_LOG"; } 2>/dev/null || true
}
