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

# セッション名のドットとコロンをアンダースコアに置換する（tmux の target 指定では . と : が
# 区切り文字。tmux 3.1+ は new-session -s 時に自ら . : を _ へサイレント置換するため、こちらも
# 同じ置換をしないと「作られた名前 (foo_bar)」と「target に使う名前 (foo:bar)」が食い違い、
# new-window / attach が target 解決に失敗して attach 不能なセッションが残る）。
# t / tt 両方の入口で必ず通すこと（過去に tt 側だけ直して t 側が漏れた同型バグあり）。
_tt_sanitize_session_name () {
  print -rn -- "${1//[.:]/_}"
}

# 新規に 5 窓のセッションを作って attach する実体。
_t_impl () {
  _tt_require_tty || return 1

  local name
  if [ -z "${1-}" ]; then
    name="s$(date +%s)"  # セッション名が重複しないように一意の名前を生成
  else
    name="$(_tt_sanitize_session_name "$1")"
  fi

  # 既存名なら中断する。new-session の duplicate エラーを無視して進むと、後続の
  # new-window ×4 が「既存セッションが在るからこそ」成功し、作業中セッションに
  # 空 window を注入してしまう（tt proj のつもりの t proj という 1 文字違い誤用で発火）。
  if tmux has-session -t "=$name" 2>/dev/null; then
    print -r -- "t: セッション '$name' は既に存在します（attach するなら tt を使ってください）。" >&2
    return 1
  fi

  tmux new-session -d -s "$name"
  tmux new-window -t "$name"
  tmux new-window -t "$name"
  tmux new-window -t "$name"
  tmux new-window -t "$name"

  # 先頭 window の選択は「:^」(最小番号 window) を使う。「:0」決め打ちは
  # _tmux.conf の base-index 1 と食い違い、window 0 が存在せず毎回失敗していた
  # (結果: エラー表示 + 最後に作った window 5 がアクティブのまま attach)。
  tmux select-window -t "$name:^"
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
  # 復元スクリプトが未設定 = resurrect plugin 未ロード（DOTFILES_DIR 誤設定やリポジトリ移動で
  # _tmux.conf の plugin load ガード `[ -f "$f" ] &&` が両 plugin を silent skip した等）だと
  # 復元は絶対に走らない。@continuum-restore=on と post-restore-all フックは plugin と独立に
  # set されるためそれらだけでは検知できず、last があると毎 boot 20 秒待って空 hold に落ちる。
  # continuum_restore.sh の no-op 条件（@resurrect-restore-script-path 空）をミラーして即 degrade。
  if [ -z "$(tmux show -gqv @resurrect-restore-script-path 2>/dev/null)" ]; then
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

# 過去 boot の rc=1/2 で残置され、last に焼き付いて復元されてきた stale hold を掃除する。
# rc=1/2 では hold を畳まず attach するため（進行中復元への割り込み防止）、実セッションが
# 出来ると保存が再開し stale hold も last に含まれ、次 boot で復元されて 1 個ずつ蓄積する。
# 生きた作業を殺さないよう三重条件で厳しく絞る:
#   (a) 名前が __tt_hold_<pid> で、その pid が「並行 tt」でない。並行 tt の pid は必ず
#       zsh プロセス（tt は zsh 関数として動く）なので、pid 死亡に加えて「生存しているが
#       zsh でない」も並行 tt でないとみなす。pid だけの kill -0 判定だと、boot 跨ぎで
#       stale hold の pid がデーモン等に再利用された場合に GC が恒久的に skip され、
#       空セッションが last に焼き付いたまま毎 boot 復元され続ける
#   (b) client が attach していない（session_attached=0）
#   (c) pristine 形状（1 window かつ 1 pane。`tmux new-session -d` 直後の姿）
# rc=1/2 で hold に attach してユーザーが作業を始めた hold は (b) または (c) で必ず除外される。
_tt_gc_stale_holds () {
  local sname wins attached spid panes
  tmux list-sessions -F '#{session_name} #{session_windows} #{session_attached}' 2>/dev/null \
  | while read -r sname wins attached; do
      case "$sname" in
        "${TT_HOLD_PREFIX:-__tt_hold_}"*) ;;
        *) continue ;;
      esac
      spid="${sname#${TT_HOLD_PREFIX:-__tt_hold_}}"
      case "$spid" in ''|*[!0-9]*) continue ;; esac   # pid 部が数値でない → 触らない
      # (a) pid 生存 かつ zsh プロセス = 並行 tt → 触らない。zsh 以外への pid 再利用は
      #     (b)(c) の判定へ進む（attach 中 / 育った hold は依然そちらで守られる）。
      #     ps -o comm= は macOS で実行パス or ログインシェルの "-zsh"、Linux で "zsh"。
      if kill -0 "$spid" 2>/dev/null; then
        case "$(ps -o comm= -p "$spid" 2>/dev/null)" in
          (*zsh*) continue ;;
        esac
      fi
      [ "${attached:-0}" = "0" ] || continue           # (b) attach 中 → 触らない
      [ "${wins:-0}" = "1" ] || continue               # (c) 1 window でない → 触らない
      panes="$(tmux list-panes -t "=$sname" 2>/dev/null | grep -c .)"
      [ "$panes" = "1" ] || continue                   # (c) 1 pane でない → 触らない
      tmux kill-session -t "=$sname" 2>/dev/null
    done
}

# hold セッション名プレフィックス（scripts/tmux_resurrect_debounced_save.sh の TT_HOLD_PREFIX と
# 一致させること。grep: __tt_hold_）。_tt_gc_stale_holds が参照する。
TT_HOLD_PREFIX="${TT_HOLD_PREFIX:-__tt_hold_}"

# カレントディレクトリ名（or 引数）のセッションに attach。無ければ作成する実体。
_tt_impl () {
  _tt_require_tty || return 1

  local name
  if [ -n "$1" ]; then
    name="$1"
  else
    name=$(basename "$PWD")
  fi

  # . : → _ の置換（理由は _tt_sanitize_session_name のコメント参照）
  name="$(_tt_sanitize_session_name "$name")"

  # 自動復元を「待って」から attach した場合だけ、attach 時に復元所要秒を flash する。
  # サーバ既存の高速パス（復元なし）や rc=3（保存なしで復元が走らない）では立てない。
  local flash_restore=0

  # OS 再起動直後などサーバ未起動のときは、空セッションを先に作らず
  # tmux の自動復元（continuum + resurrect）を完了させてから attach する。
  # 先に t で 5 窓を作ると総ペイン数 > 1 となり、resurrect の restore_from_scratch が
  # 無効化されてスクロールバック（バッファ・ログ）が復元されなくなるため
  # （vendor/tmux-plugins/tmux-resurrect/scripts/restore.sh の detect_if_restoring_from_scratch）。
  # KNOWN LIMITATION (未対応・2026-07-02 バグハントで確定 P3): bootstrap 区間 (この if 全体) は
  #   相互排除が無いため、再起動直後に 2 端末でほぼ同時に tt すると壊れうる。
  #   (i) 両方が has-session 失敗 → 両方 hold 作成 → 一方のサーバ起動時の conf source で
  #       もう一方の new-session client も ^tmux に数えられ、continuum Gate2 (プロセス数>1) が
  #       破れて auto-restore が silent skip → 両者 20s 待って空 hold に落ちる。
  #   (ii) 先着のサーバが立つと後着の has-session が成功 → bootstrap を迂回して 169 行以降へ →
  #       目的セッション未復元なら _t_impl が復元進行中に 5 窓を作り restore_from_scratch を
  #       無効化 / 名前衝突で window 混線。
  #   未対応の理由: 正しい修正は bootstrap 区間を ~/.cache の mkdir lock で直列化しつつ、後着が
  #   tmux を叩くと自分が Gate2 を汚すため「ファイルシステムで lock 解放を待つ (tmux に触れない)」
  #   設計が要る + 非 bootstrap 経路 (169行) でも @tt-restore-in-progress 待ちが要る、と両輪で
  #   スコープが大きい。発火は「再起動直後に複数端末で同時 tt」に限られ、失敗も可視 (20s 待ち /
  #   混線) で retry (kill-server → tt) で回復するため、当面は未対応とし本コメントで申し送る。
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
    local hold="${TT_HOLD_PREFIX}$$"
    tmux new-session -d -s "$hold"
    _tt_wait_for_restore
    local rc=$?
    case "$rc" in
      0)
        # 復元完了（フラグ確認済み）。hold を畳み、attach 時に所要秒を flash する。
        # -t は attach / GC と同じく =exact match（万一 hold 名が消えていた場合の
        # prefix 一致落ちで別 __tt_hold_* を巻き込まない）。
        tmux kill-session -t "=$hold" 2>/dev/null
        flash_restore=1 ;;
      3)
        # 復元が走らない（保存なし or 自動復元無効）。hold を畳んで通常の attach/create へ。
        tmux kill-session -t "=$hold" 2>/dev/null ;;
      *)
        # 1=タイムアウト / 2=完了検知フック未設定。ここで hold を畳んだり 5 窓を作ると
        # 進行中の復元に割り込み、部分復元やサーバ巻き込み kill を招く（Codex P1/P2）。
        # よって新規作成はせず、既に復元済みなら目的セッション、まだなら hold に attach
        # して安全に抜ける。rc=2 は _tmux.conf 未反映等のデプロイ skew なので警告する。
        [ "$rc" -eq 2 ] && echo "tt: _tmux.conf の @resurrect-hook-post-restore-all が未設定です（復元完了を待てません。反映してください）。" >&2
        if tmux has-session -t "=$name" 2>/dev/null; then
          tmux attach-session -t "=$name"
        else
          tmux attach-session -t "=$hold"
        fi
        return ;;
    esac
  fi

  # サーバにセッションが揃った後（既存サーバ / 復元完了 rc=0 / 復元不要 rc=3）に、
  # 過去 boot から復元されてきた stale hold を掃除する。rc=1/2（復元進行中）は上の case で
  # return 済みでここには来ない（進行中復元に触らない）。GC は三重条件で実作業 hold を守る。
  _tt_gc_stale_holds

  # -t は「=名前」で exact match を強制する。素の名前だと tmux の target 解決が
  # プレフィックス一致に落ち、`tt dot` が既存の dotfiles セッションに誤 attach する
  # （exact 一致が無い場合のみ prefix 一致になる仕様。tmux 3.5a 実測）。
  if tmux has-session -t "=$name" 2>/dev/null; then
    # 復元を待った場合のみ、post-restore-all フックが格納した所要秒を読み、
    # attach と同じコマンド列で display-message する（attach 後はクライアントが
    # 接続済みなので確実に見える。フック側で出すと表示先が無く見えない）。
    # -d 5000 で 5 秒表示（display-time をグローバルに書き換えない）。
    local dur=""
    [ "$flash_restore" = 1 ] && dur="$(tmux show -gqv @tt-restore-duration 2>/dev/null)"
    if [ -n "$dur" ]; then
      tmux attach-session -t "=$name" \; display-message -d 5000 "tmux 復元: ${dur}s（${name}）"
    else
      tmux attach-session -t "=$name"
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
