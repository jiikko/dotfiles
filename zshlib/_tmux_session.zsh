# tmux セッションの起動 / attach / 自動復元待ち。
#
# - t  : 新規に 5 窓のセッションを作って attach する（明示的に「まっさらな作業場が欲しい」用途）
# - tt : カレントディレクトリ名（or 引数）のセッションに attach。無ければ作成。
#        OS 再起動直後（サーバ未起動）は、空セッションを先に作らず tmux-continuum /
#        tmux-resurrect の自動復元を完了させてから attach し、スクロールバック（バッファ・
#        ログ）まで復元されるようにする。
#
# 公開コマンド t / tt は薄いラッパーで、呼ばれるたびにこの lib を source し直してから
# 実体（_t_impl / _tt_impl）を呼ぶ。これにより「lib を編集したら zshrc を読み直さなくても
# 次回の t / tt 実行で最新ロジックが反映される」（開発時の即時反映）。
#
# 復元レースの詳細は _tt_wait_for_restore のコメントおよび _tmux.conf の
# @resurrect-hook-post-restore-all を参照。

# この lib 自身の絶対パス。%x は「いま source 中のファイル」を指す（関数名を返す %N とは別物）。
# ラッパーはこのパスを再 source する。グローバルに置く（ラッパー内 source はローカルスコープ）。
typeset -g _TMUX_SESSION_LIB="${${(%):-%x}:A}"

# 非TTY/サンドボックスから最初の tmux コマンドを叩くと、その文脈で tmux サーバが
# 起動され、後続の本物の端末 attach まで壊れる。発生源であるサーバ起動系コマンドに
# 到達する前に止める。TT_ASSUME_TTY は非TTYの zsh -c で実体を直接検証するテスト用の
# 抜け穴で、通常利用では設定しない。
_tt_require_tty () {
  [[ -n "${TT_ASSUME_TTY:-}" ]] && return 0
  if [[ ! -t 0 || ! -t 1 ]]; then
    print -r -- "tt: 対話端末ではありません（tmux サーバを起動しません）。素のターミナルで実行してください。" >&2
    return 1
  fi
  return 0
}

# 新規に 5 窓のセッションを作って attach する実体。
_t_impl () {
  _tt_require_tty || return 1

  local name
  if [ -z "${1-}" ]; then
    name="s$(date +%s)"  # セッション名が重複しないように一意の名前を生成
  else
    name="$1"
  fi
  tmux new-session -d -s "$name"
  tmux new-window -t "$name"
  tmux new-window -t "$name"
  tmux new-window -t "$name"
  tmux new-window -t "$name"

  tmux select-window -t "$name":0
  tmux attach-session -t "$name"
}

# 自動復元の完了フラグ(@tt-restore-complete)が立つまで待つ。
# 戻り値: 0=完了確認 / 1=タイムアウト / 2=完了検知フック未設定 / 3=復元が走らない（保存なし or 無効）。
#
# フラグは _tmux.conf の @resurrect-hook-post-restore-all（post-restore-all フック）が
# 復元の全工程後に立てる。これを待つことで「復元途中での attach（部分復元）」を避ける。
# 復元は server 起動直後に「別サーバ判定 → sleep 1 → 復元」と background 実行される
# （vendor/tmux-plugins/tmux-continuum/scripts/continuum_restore.sh）。その別サーバ判定
# (another_tmux_server_running_on_startup) は tmux プロセス数で行うため、起動直後に
# tmux を叩くと誤検知で復元が skip されうる。よって最初の ~1.2s は tmux に触れない
# （= 保存先や各オプションの確認もこの待機の後に行う）。
_tt_wait_for_restore () {
  sleep 1.2

  # resurrect 本体（helpers.sh の resurrect_dir）と同じ規則で保存先を解決し、last の
  # 有無を判定する。両候補を OR で見ると本体の選択とズレて「走らない復元」を待ち続けうる。
  # 規則: @resurrect-dir があればそれ / 無ければ ~/.tmux/resurrect が在ればそちら、無ければ XDG。
  local rdir
  rdir="$(tmux show -gqv @resurrect-dir 2>/dev/null)"
  if [ -z "$rdir" ]; then
    if [ -d "$HOME/.tmux/resurrect" ]; then
      rdir="$HOME/.tmux/resurrect"
    else
      rdir="${XDG_DATA_HOME:-$HOME/.local/share}/tmux/resurrect"
    fi
  fi
  # @resurrect-dir 内の $HOME / $HOSTNAME / ~ を本体（helpers.sh resurrect_dir）と
  # 同じ規則で展開する。展開がズレると last を取りこぼし、復元中でも誤って rc=3 になる。
  rdir="$(printf '%s' "$rdir" | sed "s,\$HOME,$HOME,g; s,\$HOSTNAME,$(hostname),g; s,\~,$HOME,g")"
  [ -f "$rdir/last" ] || return 3      # 保存が無い → 復元は走らない

  # 自動復元が有効か（continuum_restore.sh の auto_restore_enabled 相当:
  # @continuum-restore=on かつ halt file 不在）。無効なら復元は走らずフラグも立たない。
  if [ -f "$HOME/tmux_no_auto_restore" ] \
     || [ "$(tmux show -gqv @continuum-restore 2>/dev/null)" != "on" ]; then
    return 3
  fi
  # 完了検知は post-restore-all フック依存。未設定だと永遠にフラグが立たず無駄に
  # 待つだけなので、未設定は専用の戻り値で呼び出し側に degrade させる。
  if [ -z "$(tmux show -gqv @resurrect-hook-post-restore-all 2>/dev/null)" ]; then
    return 2
  fi
  local i=0
  while [ "$i" -lt 100 ]; do          # 最大 ~20s（大きいアーカイブ展開も許容）
    [ -n "$(tmux show -gqv @tt-restore-complete 2>/dev/null)" ] && return 0
    sleep 0.2
    i=$((i + 1))
  done
  return 1
}

# カレントディレクトリ名（or 引数）のセッションに attach。無ければ作成する実体。
_tt_impl () {
  _tt_require_tty || return 1

  local name
  if [ -n "$1" ]; then
    name="$1"
  else
    name=$(basename "$PWD")
  fi

  # ドットをアンダースコアに置換（tmuxのセッション名では.が区切り文字として解釈されるため）
  name="${name//./_}"

  # 自動復元を「待って」から attach した場合だけ、attach 時に復元所要秒を flash する。
  # サーバ既存の高速パス（復元なし）や rc=3（保存なしで復元が走らない）では立てない。
  local flash_restore=0

  # OS 再起動直後などサーバ未起動のときは、空セッションを先に作らず
  # tmux の自動復元（continuum + resurrect）を完了させてから attach する。
  # 先に t で 5 窓を作ると総ペイン数 > 1 となり、resurrect の restore_from_scratch が
  # 無効化されてスクロールバック（バッファ・ログ）が復元されなくなるため
  # （vendor/tmux-plugins/tmux-resurrect/scripts/restore.sh の detect_if_restoring_from_scratch）。
  if ! tmux has-session 2>/dev/null; then
    # サーバ未起動 = これから新規起動して自動復元を待つ局面。サーバを起こす前に「socket が
    # 消えたのにプロセスだけ残った孤児 tmux サーバ」を回収しておく。孤児が残っていると
    # continuum の Gate2（another_tmux_server_running_on_startup が ^tmux プロセス数で判定。
    # vendor helpers.sh は socket 生存を見ない）が「他サーバ在り」と誤判定し、auto-restore を
    # 丸ごと skip する（2026-06-28 に判明した 17 日間 復元不発の真因）。reap は生存 socket を
    # 持つプロセス（実サーバ・接続中 client）には絶対に触れない（scripts 側の不変条件）。
    local _reap="${_TMUX_SESSION_LIB:h:h}/scripts/tmux_reap_orphan_servers.sh"
    # TT_SKIP_REAP は unit テスト用の抜け穴。reap は実 pgrep/lsof/kill で動き tmux スタブで
    # 傍受できないため、_tt_impl のロジックを検証する test_tt.sh が実プロセステーブルを
    # 触らないようにする（テストが実環境に副作用を持つのは、今回の不具合と同型なので避ける）。
    if [ -z "${TT_SKIP_REAP:-}" ] && [ -x "$_reap" ]; then "$_reap" 2>/dev/null || true; fi

    # tmux サーバはセッションが 1 つも無いと即終了する。復元完了までサーバを生かす hold を置く。
    # hold により総ペイン数 = 1 となり restore_from_scratch が有効化され、
    # 保存セッションがスクロールバックごと復元される。
    local hold="__tt_hold_$$"
    tmux new-session -d -s "$hold"
    _tt_wait_for_restore
    local rc=$?
    case "$rc" in
      0)
        # 復元完了（フラグ確認済み）。hold を畳み、attach 時に所要秒を flash する。
        tmux kill-session -t "$hold" 2>/dev/null
        flash_restore=1 ;;
      3)
        # 復元が走らない（保存なし or 自動復元無効）。hold を畳んで通常の attach/create へ。
        tmux kill-session -t "$hold" 2>/dev/null ;;
      *)
        # 1=タイムアウト / 2=完了検知フック未設定。ここで hold を畳んだり 5 窓を作ると
        # 進行中の復元に割り込み、部分復元やサーバ巻き込み kill を招く（Codex P1/P2）。
        # よって新規作成はせず、既に復元済みなら目的セッション、まだなら hold に attach
        # して安全に抜ける。rc=2 は _tmux.conf 未反映等のデプロイ skew なので警告する。
        [ "$rc" -eq 2 ] && echo "tt: _tmux.conf の @resurrect-hook-post-restore-all が未設定です（復元完了を待てません。反映してください）。" >&2
        if tmux has-session -t "$name" 2>/dev/null; then
          tmux attach-session -t "$name"
        else
          tmux attach-session -t "$hold"
        fi
        return ;;
    esac
  fi

  if tmux has-session -t "$name" 2>/dev/null; then
    # 復元を待った場合のみ、post-restore-all フックが格納した所要秒を読み、
    # attach と同じコマンド列で display-message する（attach 後はクライアントが
    # 接続済みなので確実に見える。フック側で出すと表示先が無く見えない）。
    # -d 5000 で 5 秒表示（display-time をグローバルに書き換えない）。
    local dur=""
    [ "$flash_restore" = 1 ] && dur="$(tmux show -gqv @tt-restore-duration 2>/dev/null)"
    if [ -n "$dur" ]; then
      tmux attach-session -t "$name" \; display-message -d 5000 "tmux 復元: ${dur}s（${name}）"
    else
      tmux attach-session -t "$name"
    fi
  else
    _t_impl "$name"   # 公開ラッパー t ではなく実体を呼ぶ（無駄な再 source を避ける）
  fi
}

# --- 公開コマンド（薄いラッパー）------------------------------------------------
# 実行のたびに lib を読み直してから実体を呼ぶ＝編集が即反映される。
# source 失敗（編集中の構文エラー等）で t/tt 自体が使えなくならないよう、source は
# ガードし、失敗時は直近ロード済みの実体で続行する（実体呼び出しまで止めない）。
t ()  { [[ -r "$_TMUX_SESSION_LIB" ]] && source "$_TMUX_SESSION_LIB"; _t_impl "$@"; }
tt () { [[ -r "$_TMUX_SESSION_LIB" ]] && source "$_TMUX_SESSION_LIB"; _tt_impl "$@"; }
